package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/deeploop-ai/graviton/internal/domain/databases"
	domainauth "github.com/deeploop-ai/graviton/internal/domain/auth"
	"github.com/deeploop-ai/graviton/internal/domain/projects"
	"github.com/deeploop-ai/graviton/internal/domain/shared"
	"github.com/deeploop-ai/graviton/internal/domain/users"
	"github.com/deeploop-ai/graviton/internal/pkg/config"
	"github.com/deeploop-ai/graviton/pkg/idgen"
	"github.com/deeploop-ai/graviton/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Validator struct {
	cfg               *config.AppConfig
	apiKeyRepo        projects.APIKeyRepository
	adminRepo         projects.ConsoleAdminRepository
	adminProjectRepo  projects.ConsoleAdminProjectRepository
	adminRevokeStore  domainauth.AdminTokenRevokeStore
	docDB             databases.DocumentDB
	sessionCodec      *SessionCookieCodec
}

func NewValidator(
	cfg *config.AppConfig,
	apiKeyRepo projects.APIKeyRepository,
	adminRepo projects.ConsoleAdminRepository,
	adminProjectRepo projects.ConsoleAdminProjectRepository,
	adminRevokeStore domainauth.AdminTokenRevokeStore,
	docDB databases.DocumentDB,
) *Validator {
	return &Validator{
		cfg:              cfg,
		apiKeyRepo:       apiKeyRepo,
		adminRepo:        adminRepo,
		adminProjectRepo: adminProjectRepo,
		adminRevokeStore: adminRevokeStore,
		docDB:            docDB,
		sessionCodec:     NewSessionCookieCodec(string(jwtparser.DeriveKey(cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeSessionCookie))),
	}
}

func (v *Validator) ValidateToken(ctx context.Context, token string) (*shared.Principal, error) {
	return v.ValidateCredential(ctx, token, shared.CredentialTypeToken)
}

func (v *Validator) ValidateCredential(ctx context.Context, raw string, credentialType shared.CredentialType) (*shared.Principal, error) {
	switch credentialType {
	case shared.CredentialTypeAPIKey:
		return v.validateAPIKey(ctx, raw)
	case shared.CredentialTypeToken:
		claims, ok := v.parseJWT(raw)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		return v.principalFromJWT(ctx, claims)
	case shared.CredentialTypeSession:
		// Try JWT first (console or token-style session).
		if claims, ok := v.parseJWT(raw); ok {
			return v.principalFromJWT(ctx, claims)
		}
		projectID, sessionID, err := v.sessionCodec.Verify(raw)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid session")
		}
		return v.principalFromSession(ctx, projectID, sessionID)
	}
	return nil, status.Error(codes.Unauthenticated, "unsupported credential type")
}

// parseJWT verifies a token against the purpose-derived sub-keys. A token only
// verifies under the key it was signed with, so trying both domains is safe;
// principalFromJWT then dispatches on the signed ActorKind claim.
func (v *Validator) parseJWT(raw string) (*jwtparser.Claims, bool) {
	secret := v.cfg.GetSecurity().GetJwt().GetSecret()
	if claims, ok := jwtparser.Parse(jwtparser.DeriveKey(secret, jwtparser.PurposeAdminJWT), raw); ok {
		return claims, true
	}
	return jwtparser.Parse(jwtparser.DeriveKey(secret, jwtparser.PurposeEndUserJWT), raw)
}

func (v *Validator) validateAPIKey(ctx context.Context, raw string) (*shared.Principal, error) {
	hash := sha256.Sum256([]byte(raw))
	hashStr := hex.EncodeToString(hash[:])
	key, err := v.apiKeyRepo.GetAPIKeyBySecretHash(ctx, hashStr)
	if err != nil {
		return nil, status.Error(codes.Internal, "api key validation failed")
	}
	if key == nil || !key.Enabled {
		return nil, status.Error(codes.Unauthenticated, "invalid or disabled api key")
	}
	if key.ExpireAt != nil && key.ExpireAt.Before(time.Now()) {
		return nil, status.Error(codes.Unauthenticated, "api key expired")
	}
	return &shared.Principal{
		ActorID:        idgen.ID(key.ID),
		ActorKind:      shared.ActorKindService,
		CredentialType: shared.CredentialTypeAPIKey,
		ProjectID:      key.ProjectID,
		APIKeyID:       key.ID,
		Roles:          []string{"keys"},
		Permissions:    key.Scopes,
	}, nil
}

func (v *Validator) principalFromJWT(ctx context.Context, claims *jwtparser.Claims) (*shared.Principal, error) {
	switch claims.ActorKind {
	case "admin":
		if claims.TokenType != "" && claims.TokenType != jwtparser.TokenTypeAccess {
			return nil, status.Error(codes.Unauthenticated, "invalid token type")
		}
		if err := v.checkAdminTokenRevoked(ctx, claims); err != nil {
			return nil, err
		}
		admin, err := v.adminRepo.GetConsoleAdmin(ctx, claims.UserID)
		if err != nil {
			return nil, status.Error(codes.Internal, "admin lookup failed")
		}
		if admin == nil {
			return nil, status.Error(codes.Unauthenticated, "admin not found")
		}
		return &shared.Principal{
			ActorID:         idgen.ID(admin.ID),
			ActorKind:       shared.ActorKindAdmin,
			CredentialType:  shared.CredentialTypeToken,
			IsPlatformAdmin: admin.Role == "owner" || admin.Role == "admin",
			UserID:          admin.ID,
			Email:           admin.Email,
			Roles:           []string{admin.Role},
		}, nil
	default:
		if claims.SessionID != "" && claims.ProjectID != "" {
			if err := v.validateEndUserSession(ctx, claims.ProjectID, claims.SessionID); err != nil {
				return nil, err
			}
		}
		if claims.TokenType != "" && claims.TokenType != jwtparser.TokenTypeAccess {
			return nil, status.Error(codes.Unauthenticated, "invalid token type")
		}
		if err := v.ensureUserCanAuthenticate(ctx, claims.ProjectID, claims.UserID); err != nil {
			return nil, err
		}
		return &shared.Principal{
			ActorID:        idgen.ID(claims.UserID),
			ActorKind:      shared.ActorKindEndUser,
			CredentialType: shared.CredentialTypeToken,
			ProjectID:      claims.ProjectID,
			UserID:         claims.UserID,
			SessionID:      claims.SessionID,
			Email:          claims.Username,
			Roles:          append([]string{"users", fmt.Sprintf("user:%s", claims.UserID)}, claims.Roles...),
		}, nil
	}
}

func (v *Validator) principalFromSession(ctx context.Context, projectID, sessionID string) (*shared.Principal, error) {
	sessionDoc, err := v.docDB.GetDocument(ctx, projectID, "default", "sessions", sessionID, databases.SystemPrincipal)
	if err != nil {
		return nil, status.Error(codes.Internal, "session lookup failed")
	}
	if sessionDoc == nil {
		return nil, status.Error(codes.Unauthenticated, "session not found")
	}
	expireAtRaw, ok := sessionDoc.Data["expire_at"]
	if ok {
		expireAt, err := parseTime(expireAtRaw)
		if err != nil {
			// Fail closed: unparsable expiry is treated as expired.
			return nil, status.Error(codes.Unauthenticated, "session expired")
		}
		if expireAt.Before(time.Now()) {
			return nil, status.Error(codes.Unauthenticated, "session expired")
		}
	}
	userID, _ := sessionDoc.Data["user_id"].(string)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}
	if err := v.ensureUserCanAuthenticate(ctx, projectID, userID); err != nil {
		return nil, err
	}
	return &shared.Principal{
		ActorID:        idgen.ID(userID),
		ActorKind:      shared.ActorKindEndUser,
		CredentialType: shared.CredentialTypeSession,
		ProjectID:      projectID,
		UserID:         userID,
		SessionID:      sessionID,
		Roles:          []string{"users", fmt.Sprintf("user:%s", userID)},
	}, nil
}

func (v *Validator) validateEndUserSession(ctx context.Context, projectID, sessionID string) error {
	sessionDoc, err := v.docDB.GetDocument(ctx, projectID, "default", "sessions", sessionID, databases.SystemPrincipal)
	if err != nil {
		return status.Error(codes.Unauthenticated, "session lookup failed")
	}
	if sessionDoc == nil {
		return status.Error(codes.Unauthenticated, "session not found or revoked")
	}
	if expireAtRaw, ok := sessionDoc.Data["expire_at"]; ok {
		expireAt, err := parseTime(expireAtRaw)
		if err != nil {
			// Fail closed: unparsable expiry is treated as expired.
			return status.Error(codes.Unauthenticated, "session expired")
		}
		if expireAt.Before(time.Now()) {
			return status.Error(codes.Unauthenticated, "session expired")
		}
	}
	return nil
}

func (v *Validator) ensureUserCanAuthenticate(ctx context.Context, projectID, userID string) error {
	if projectID == "" || userID == "" {
		return nil
	}
	doc, err := v.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return status.Error(codes.Unauthenticated, "user lookup failed")
	}
	if doc == nil {
		return status.Error(codes.Unauthenticated, "user not found")
	}
	statusVal, _ := doc.Data["status"].(string)
	if !users.CanAuthenticate(statusVal) {
		return status.Error(codes.Unauthenticated, "user account is not active")
	}
	return nil
}

func (v *Validator) ValidateAdminProjectAccess(ctx context.Context, principal *shared.Principal) error {
	if principal == nil || principal.ActorKind != shared.ActorKindAdmin {
		return nil
	}
	if principal.ProjectID == "" {
		return nil
	}
	if principal.IsPlatformAdmin {
		return nil
	}
	has, err := v.adminProjectRepo.HasProjectAccess(ctx, principal.UserID, principal.ProjectID)
	if err != nil {
		return status.Error(codes.Internal, "admin project access check failed")
	}
	if !has {
		return status.Error(codes.PermissionDenied, "admin has no access to project")
	}
	return nil
}

func (v *Validator) checkAdminTokenRevoked(ctx context.Context, claims *jwtparser.Claims) error {
	if v.adminRevokeStore == nil || claims == nil || claims.UserID == "" {
		return nil
	}
	revokedBefore, err := v.adminRevokeStore.RevokedBefore(ctx, claims.UserID)
	if err != nil {
		return err
	}
	if !revokedBefore.IsZero() && claims.IssuedAt <= revokedBefore.Unix() {
		return status.Error(codes.Unauthenticated, "token revoked")
	}
	return nil
}

func parseTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		return time.Parse(time.RFC3339Nano, t)
	}
	return time.Time{}, fmt.Errorf("unsupported time type")
}
