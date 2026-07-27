package console_test

import (
	"context"
	"testing"
	"time"

	domainauth "github.com/deeploop-ai/graviton/internal/domain/auth"
	"github.com/deeploop-ai/graviton/internal/domain/shared"
	"github.com/deeploop-ai/graviton/internal/infra/auth"
	"github.com/deeploop-ai/graviton/internal/app/console"
	"github.com/deeploop-ai/graviton/internal/pkg/config"
	"github.com/deeploop-ai/graviton/internal/pkg/contexts"
	"github.com/deeploop-ai/graviton/pkg/jwtparser"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type memAdminRevokeStore struct {
	revoked map[string]time.Time
}

func newMemAdminRevokeStore() *memAdminRevokeStore {
	return &memAdminRevokeStore{revoked: map[string]time.Time{}}
}

func (s *memAdminRevokeStore) RevokeBefore(_ context.Context, adminID string, revokedAt time.Time, _ time.Duration) error {
	if existing, ok := s.revoked[adminID]; !ok || revokedAt.After(existing) {
		s.revoked[adminID] = revokedAt
	}
	return nil
}

func (s *memAdminRevokeStore) RevokedBefore(_ context.Context, adminID string) (time.Time, error) {
	return s.revoked[adminID], nil
}

var _ domainauth.AdminTokenRevokeStore = (*memAdminRevokeStore)(nil)

func testConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{
				Secret:     "console-auth-test-secret",
				RefreshTtl: "168h",
			},
		},
	}
}

func TestAuth_SignOut_RevokesAdminTokens(t *testing.T) {
	t.Parallel()
	ctx := contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorKind: shared.ActorKindAdmin,
		UserID:    "admin-1",
	})
	store := newMemAdminRevokeStore()
	authUC := console.NewAuth(testConfig(), nil, store, nil)

	require.NoError(t, authUC.SignOut(ctx))
	revoked, err := store.RevokedBefore(ctx, "admin-1")
	require.NoError(t, err)
	require.False(t, revoked.IsZero())
}

func TestAuth_SignOut_ExpiredTokenStillRevokes(t *testing.T) {
	t.Parallel()
	store := newMemAdminRevokeStore()
	authUC := console.NewAuth(testConfig(), nil, store, nil)

	// No principal in context (access token expired); the raw token is only
	// available in the request metadata.
	expiredToken, err := jwtparser.Generate(jwtparser.DeriveKey(testConfig().GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeAdminJWT), jwtparser.Claims{
		UserID:    "admin-9",
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Add(-48 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-24 * time.Hour).Unix(),
	})
	require.NoError(t, err)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+expiredToken))

	require.NoError(t, authUC.SignOut(ctx))
	revoked, err := store.RevokedBefore(ctx, "admin-9")
	require.NoError(t, err)
	require.False(t, revoked.IsZero())
}

func TestAuth_SignOut_ExpiredTokenWrongSignatureIgnored(t *testing.T) {
	t.Parallel()
	store := newMemAdminRevokeStore()
	authUC := console.NewAuth(testConfig(), nil, store, nil)

	forgedToken, err := jwtparser.Generate([]byte("another-secret"), jwtparser.Claims{
		UserID:    "admin-9",
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  time.Now().Add(-48 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-24 * time.Hour).Unix(),
	})
	require.NoError(t, err)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+forgedToken))

	require.NoError(t, authUC.SignOut(ctx))
	revoked, err := store.RevokedBefore(ctx, "admin-9")
	require.NoError(t, err)
	require.True(t, revoked.IsZero())
}

func TestAuth_RefreshToken_RejectsRevokedAdmin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	issuedAt := time.Now().Add(-time.Hour)
	store := newMemAdminRevokeStore()
	require.NoError(t, store.RevokeBefore(ctx, "admin-1", time.Now(), time.Hour))

	refreshToken, err := jwtparser.Generate(jwtparser.DeriveKey(testConfig().GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeAdminJWT), jwtparser.Claims{
		UserID:    "admin-1",
		Username:  "admin@graviton.local",
		ActorKind: "admin",
		Roles:     []string{"admin"},
		TokenType: jwtparser.TokenTypeRefresh,
		IssuedAt:  issuedAt.Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)

	authUC := console.NewAuth(testConfig(), nil, store, nil)
	_, err = authUC.RefreshToken(ctx, console.RefreshTokenCommand{RefreshToken: refreshToken})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuth_ValidateCredential_ChecksRevokeStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMemAdminRevokeStore()
	require.NoError(t, store.RevokeBefore(ctx, "admin-1", time.Now(), time.Hour))

	issuedAt := time.Now().Add(-2 * time.Hour).Unix()
	token, err := jwtparser.Generate(jwtparser.DeriveKey(testConfig().GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeAdminJWT), jwtparser.Claims{
		UserID:    "admin-1",
		ActorKind: "admin",
		TokenType: jwtparser.TokenTypeAccess,
		IssuedAt:  issuedAt,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)

	v := auth.NewValidator(
		testConfig(),
		nil,
		nil,
		nil,
		store,
		nil,
	)
	_, err = v.ValidateToken(ctx, token)
	require.Error(t, err)
}
