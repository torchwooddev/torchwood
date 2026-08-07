package auth

import (
	"context"
	"fmt"
	"time"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultSessionTTL = 7 * 24 * time.Hour

// SessionService implements domainauth.SessionService.
type SessionService struct {
	cfg          *config.AppConfig
	docDB        databases.DocumentDB
	sessionCodec *SessionCookieCodec
	roles        domainauth.UserRoleResolver
	rotation     domainauth.RefreshRotationStore
}

func NewSessionService(
	cfg *config.AppConfig,
	docDB databases.DocumentDB,
	roles domainauth.UserRoleResolver,
	rotation domainauth.RefreshRotationStore,
) *SessionService {
	return &SessionService{
		cfg:          cfg,
		docDB:        docDB,
		sessionCodec: NewSessionCookieCodec(string(jwtparser.DeriveKey(cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeSessionCookie))),
		roles:        roles,
		rotation:     rotation,
	}
}

func (s *SessionService) CreateSessionAndTokens(ctx context.Context, projectID, userID, email, provider string) (*domainauth.TokenBundle, string, error) {
	if provider == "" {
		provider = domainauth.ProviderEmail
	}
	client := contexts.ClientInfoFrom(ctx)

	expireAt := time.Now().Add(defaultSessionTTL)
	sessionID := idgen.UUID().String()
	sessionSecret := idgen.UUID().String()
	sessionDoc := databases.Document{
		ID: sessionID,
		Data: map[string]any{
			"user_id":     userID,
			"secret_hash": sessionSecret,
			"provider":    provider,
			"expire_at":   expireAt.Format(time.RFC3339Nano),
			"user_agent":  client.UserAgent,
			"ip":          client.IP,
		},
	}
	sessionPerms := sessionPermissions(userID)
	if _, err := s.docDB.CreateDocument(ctx, projectID, "default", "sessions", sessionDoc, sessionPerms, databases.SystemPrincipal); err != nil {
		return nil, "", err
	}
	return s.IssueTokens(ctx, projectID, userID, email, sessionID)
}

func (s *SessionService) IssueTokens(ctx context.Context, projectID, userID, email, sessionID string) (*domainauth.TokenBundle, string, error) {
	return s.IssueTokensWithRefreshID(ctx, projectID, userID, email, sessionID, idgen.UUID().String())
}

func (s *SessionService) IssueTokensWithRefreshID(ctx context.Context, projectID, userID, email, sessionID, refreshTokenID string) (*domainauth.TokenBundle, string, error) {
	accessTTL := 15 * time.Minute
	if d, err := time.ParseDuration(s.cfg.GetSecurity().GetJwt().GetAccessTtl()); err == nil {
		accessTTL = d
	}
	refreshTTL := defaultSessionTTL
	if d, err := time.ParseDuration(s.cfg.GetSecurity().GetJwt().GetRefreshTtl()); err == nil {
		refreshTTL = d
	}

	now := time.Now()
	baseRoles, err := s.roles.LoadUserRoles(ctx, projectID, userID)
	if err != nil {
		return nil, "", err
	}
	accessClaims := jwtparser.Claims{
		TokenID:   idgen.UUID().String(),
		UserID:    userID,
		Username:  email,
		ActorKind: "end_user",
		ProjectID: projectID,
		SessionID: sessionID,
		TokenType: jwtparser.TokenTypeAccess,
		Roles:     baseRoles,
		ExpiresAt: now.Add(accessTTL).Unix(),
		IssuedAt:  now.Unix(),
	}
	endUserKey := jwtparser.DeriveKey(s.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeEndUserJWT)
	accessToken, err := jwtparser.Generate(endUserKey, accessClaims)
	if err != nil {
		return nil, "", err
	}
	refreshClaims := accessClaims
	refreshClaims.TokenID = refreshTokenID
	refreshClaims.TokenType = jwtparser.TokenTypeRefresh
	refreshClaims.ExpiresAt = now.Add(refreshTTL).Unix()
	refreshToken, err := jwtparser.Generate(endUserKey, refreshClaims)
	if err != nil {
		return nil, "", err
	}

	if s.rotation != nil {
		if err := s.rotation.Register(ctx, domainauth.RefreshRotationKey(projectID, sessionID), refreshTokenID, refreshTTL); err != nil {
			return nil, "", err
		}
	}

	cookie := s.sessionCodec.Sign(projectID, sessionID)
	return &domainauth.TokenBundle{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ExpiresAt:      accessClaims.ExpiresAt,
		RefreshTokenID: refreshTokenID,
	}, cookie, nil
}

func (s *SessionService) EnsureActiveSession(ctx context.Context, projectID, sessionID, userID string) error {
	sessionDoc, err := s.docDB.GetDocument(ctx, projectID, "default", "sessions", sessionID, databases.SystemPrincipal)
	if err != nil {
		return status.Error(codes.Unauthenticated, "session lookup failed")
	}
	if sessionDoc == nil {
		return status.Error(codes.Unauthenticated, "session not found or revoked")
	}
	if uid, _ := sessionDoc.Data["user_id"].(string); uid != userID {
		return status.Error(codes.Unauthenticated, "invalid session")
	}
	if expireAtRaw, ok := sessionDoc.Data["expire_at"]; ok {
		expireAt, err := parseSessionTime(expireAtRaw)
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

// DeleteSessionsByUser removes every session document owned by the user.
func (s *SessionService) DeleteSessionsByUser(ctx context.Context, projectID, userID string) error {
	list, err := s.docDB.ListDocuments(ctx, projectID, "default", "sessions", databases.Query{
		Queries: []string{query.BuildEqual("user_id", userID)},
	}, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	for i := range list.Documents {
		if err := s.docDB.DeleteDocument(ctx, projectID, "default", "sessions", list.Documents[i].ID, databases.SystemPrincipal); err != nil {
			return err
		}
	}
	return nil
}

func sessionPermissions(userID string) []databases.Permission {
	// sessions 集合的 keys 只读；update/delete 仅限 owner（user:<id>）与 admin
	// （安全评审 C1 第 3 层 / M2：任意 scope 的 API key 不得改删会话）。
	return []databases.Permission{
		{Type: "read", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "delete", Role: "admin"},
	}
}

func parseSessionTime(v any) (time.Time, error) {
	return ParseSessionTime(v)
}

// ParseSessionTime decodes session expire_at values from document storage.
func ParseSessionTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		return time.Parse(time.RFC3339Nano, t)
	}
	return time.Time{}, fmt.Errorf("unsupported time type")
}
