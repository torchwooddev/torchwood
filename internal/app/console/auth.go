package console

import (
	"context"
	"strings"
	"time"

	domainauth "github.com/deeploop-ai/graviton/internal/domain/auth"
	"github.com/deeploop-ai/graviton/internal/domain/projects"
	"github.com/deeploop-ai/graviton/internal/domain/shared"
	"github.com/deeploop-ai/graviton/internal/pkg/config"
	"github.com/deeploop-ai/graviton/internal/pkg/contexts"
	"github.com/deeploop-ai/graviton/pkg/idgen"
	"github.com/deeploop-ai/graviton/pkg/jwtparser"
	"github.com/deeploop-ai/graviton/pkg/password"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Auth struct {
	cfg              *config.AppConfig
	adminRepo        projects.ConsoleAdminRepository
	adminRevokeStore domainauth.AdminTokenRevokeStore
	loginThrottle    domainauth.LoginThrottle
	rotation         domainauth.RefreshRotationStore
}

func NewAuth(cfg *config.AppConfig, adminRepo projects.ConsoleAdminRepository, adminRevokeStore domainauth.AdminTokenRevokeStore, loginThrottle domainauth.LoginThrottle, rotation domainauth.RefreshRotationStore) *Auth {
	return &Auth{cfg: cfg, adminRepo: adminRepo, adminRevokeStore: adminRevokeStore, loginThrottle: loginThrottle, rotation: rotation}
}

type SignInCommand struct {
	Email    string
	Password string
}

type RefreshTokenCommand struct {
	RefreshToken string
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	// RefreshTokenID is the jti of RefreshToken; used for rotation bookkeeping
	// and never mapped to proto responses.
	RefreshTokenID string
}

func (a *Auth) SignIn(ctx context.Context, cmd SignInCommand) (*TokenPair, error) {
	clientInfo := contexts.ClientInfoFrom(ctx)
	throttleEmail := strings.ToLower(strings.TrimSpace(cmd.Email))
	if err := a.checkLoginThrottle(ctx, throttleEmail, clientInfo.IP); err != nil {
		return nil, err
	}
	invalidCredentials := func() (*TokenPair, error) {
		a.recordLoginFailure(ctx, throttleEmail, clientInfo.IP)
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}
	admin, err := a.adminRepo.GetConsoleAdminByEmail(ctx, cmd.Email)
	if err != nil {
		return nil, status.Error(codes.Internal, "admin lookup failed")
	}
	if admin == nil {
		return invalidCredentials()
	}
	if ok, _ := password.Verify(cmd.Password, admin.PasswordHash); !ok {
		return invalidCredentials()
	}
	a.resetLoginThrottle(ctx, throttleEmail, clientInfo.IP)
	return a.issueAdminTokens(ctx, admin.ID, admin.Email, admin.Role)
}

func (a *Auth) RefreshToken(ctx context.Context, cmd RefreshTokenCommand) (*TokenPair, error) {
	if cmd.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}
	claims, ok := jwtparser.Parse(jwtparser.DeriveKey(a.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeAdminJWT), cmd.RefreshToken)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if claims.TokenType != jwtparser.TokenTypeRefresh || claims.ActorKind != "admin" {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if err := a.checkAdminTokenRevoked(ctx, claims); err != nil {
		return nil, err
	}
	if a.rotation == nil {
		return a.issueAdminTokens(ctx, claims.UserID, claims.Username, firstRole(claims.Roles))
	}

	refreshTTL := a.refreshTTL()
	newRefreshTokenID := idgen.UUID().String()
	result, err := a.rotation.Rotate(ctx, domainauth.RefreshRotationKey("admin", claims.UserID), claims.TokenID, newRefreshTokenID, refreshTTL)
	if err != nil {
		return nil, err
	}
	switch result {
	case domainauth.RotateOK:
		return a.issueAdminTokensWithRefreshID(ctx, claims.UserID, claims.Username, firstRole(claims.Roles), newRefreshTokenID)
	case domainauth.RotateMismatch:
		// 旧 refresh token 被再次使用：判定为重用，撤销该 admin 此前签发的全部 token。
		if a.adminRevokeStore != nil {
			_ = a.adminRevokeStore.RevokeBefore(ctx, claims.UserID, time.Now(), refreshTTL)
		}
		return nil, status.Error(codes.Unauthenticated, "refresh token reuse detected")
	default: // RotateMissing
		return nil, status.Error(codes.Unauthenticated, "session expired")
	}
}

func (a *Auth) SignOut(ctx context.Context) error {
	if a.adminRevokeStore == nil {
		return nil
	}
	adminID := ""
	if p, ok := contexts.Principal(ctx); ok && p.ActorKind == shared.ActorKindAdmin {
		adminID = p.UserID
	}
	if adminID == "" {
		// No principal (e.g. the access token already expired): fall back to the
		// raw credential in the request metadata and revoke best-effort.
		adminID = a.adminIDFromMetadata(ctx)
	}
	if adminID == "" {
		return nil
	}
	return a.adminRevokeStore.RevokeBefore(ctx, adminID, time.Now(), a.refreshTTL())
}

func (a *Auth) refreshTTL() time.Duration {
	refreshTTL := 7 * 24 * time.Hour
	if d, err := time.ParseDuration(a.cfg.GetSecurity().GetJwt().GetRefreshTtl()); err == nil {
		refreshTTL = d
	}
	return refreshTTL
}

// adminIDFromMetadata extracts the admin id from the bearer token or console
// session cookie in the request metadata, tolerating expired tokens while still
// verifying the signature.
func (a *Auth) adminIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	secret := jwtparser.DeriveKey(a.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeAdminJWT)
	if raw := metadataValue(md, "authorization"); raw != "" {
		if parts := strings.Fields(raw); len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			if claims, ok := jwtparser.ParseAllowExpired(secret, parts[1]); ok && claims.ActorKind == "admin" {
				return claims.UserID
			}
		}
	}
	if raw := metadataValue(md, "cookie"); raw != "" {
		for _, part := range strings.Split(raw, ";") {
			name, value, found := strings.Cut(strings.TrimSpace(part), "=")
			if !found || value == "" || name != "GRAVITON_session_console" {
				continue
			}
			if claims, ok := jwtparser.ParseAllowExpired(secret, value); ok && claims.ActorKind == "admin" {
				return claims.UserID
			}
		}
	}
	return ""
}

func metadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func (a *Auth) checkLoginThrottle(ctx context.Context, email, ip string) error {
	if a.loginThrottle == nil {
		return nil
	}
	return a.loginThrottle.Check(ctx, domainauth.LoginNamespaceAdmin, email, ip)
}

func (a *Auth) recordLoginFailure(ctx context.Context, email, ip string) {
	if a.loginThrottle == nil {
		return
	}
	_ = a.loginThrottle.RecordFailure(ctx, domainauth.LoginNamespaceAdmin, email, ip)
}

func (a *Auth) resetLoginThrottle(ctx context.Context, email, ip string) {
	if a.loginThrottle == nil {
		return
	}
	_ = a.loginThrottle.Reset(ctx, domainauth.LoginNamespaceAdmin, email, ip)
}

func (a *Auth) checkAdminTokenRevoked(ctx context.Context, claims *jwtparser.Claims) error {
	if a.adminRevokeStore == nil || claims == nil || claims.UserID == "" {
		return nil
	}
	revokedBefore, err := a.adminRevokeStore.RevokedBefore(ctx, claims.UserID)
	if err != nil {
		return err
	}
	if !revokedBefore.IsZero() && claims.IssuedAt <= revokedBefore.Unix() {
		return status.Error(codes.Unauthenticated, "token revoked")
	}
	return nil
}

func (a *Auth) issueAdminTokens(ctx context.Context, adminID, email, role string) (*TokenPair, error) {
	return a.issueAdminTokensWithRefreshID(ctx, adminID, email, role, idgen.UUID().String())
}

func (a *Auth) issueAdminTokensWithRefreshID(ctx context.Context, adminID, email, role, refreshTokenID string) (*TokenPair, error) {
	accessTTL := 24 * time.Hour
	if d, err := time.ParseDuration(a.cfg.GetSecurity().GetJwt().GetAccessTtl()); err == nil {
		accessTTL = d
	}
	refreshTTL := a.refreshTTL()
	now := time.Now()
	accessClaims := jwtparser.Claims{
		TokenID:   idgen.UUID().String(),
		UserID:    adminID,
		Username:  email,
		ActorKind: "admin",
		Roles:     []string{role},
		TokenType: jwtparser.TokenTypeAccess,
		ExpiresAt: now.Add(accessTTL).Unix(),
		IssuedAt:  now.Unix(),
	}
	adminKey := jwtparser.DeriveKey(a.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeAdminJWT)
	accessToken, err := jwtparser.Generate(adminKey, accessClaims)
	if err != nil {
		return nil, err
	}
	refreshClaims := accessClaims
	refreshClaims.TokenID = refreshTokenID
	refreshClaims.TokenType = jwtparser.TokenTypeRefresh
	refreshClaims.ExpiresAt = now.Add(refreshTTL).Unix()
	refreshToken, err := jwtparser.Generate(adminKey, refreshClaims)
	if err != nil {
		return nil, err
	}
	if a.rotation != nil {
		if err := a.rotation.Register(ctx, domainauth.RefreshRotationKey("admin", adminID), refreshTokenID, refreshTTL); err != nil {
			return nil, err
		}
	}
	return &TokenPair{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ExpiresAt:      accessClaims.ExpiresAt,
		RefreshTokenID: refreshTokenID,
	}, nil
}

func firstRole(roles []string) string {
	if len(roles) == 0 {
		return "admin"
	}
	return roles[0]
}
