package client

import (
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func setupRefreshRotationAccount(t *testing.T) (context.Context, *Account, databases.DocumentDB, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	t.Cleanup(cleanup)

	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	account := NewTestAccountWithRedis(buildTestConfig(), projectRepo, docDB, rdb)
	return ctx, account, docDB, projectID
}

func signUpForRefresh(t *testing.T, ctx context.Context, account *Account, projectID, email string) *TokenBundle {
	t.Helper()
	_, tokens, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     email,
		Password:  "User@123",
		Name:      "Refresh Rotation",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokens.RefreshToken)
	require.NotEmpty(t, tokens.RefreshTokenID)
	return tokens
}

func parseRefreshClaims(t *testing.T, cfgSecret, token string) *jwtparser.Claims {
	t.Helper()
	claims, ok := jwtparser.Parse(jwtparser.DeriveKey(cfgSecret, jwtparser.PurposeEndUserJWT), token)
	require.True(t, ok)
	return claims
}

func TestAccount_RefreshToken_RotationAndReuseDetection(t *testing.T) {
	ctx, account, docDB, projectID := setupRefreshRotationAccount(t)
	tokens := signUpForRefresh(t, ctx, account, projectID, "rotation@torchwood.local")

	// First refresh rotates the token.
	newTokens, _, err := account.RefreshToken(ctx, RefreshTokenCommand{
		ProjectID:    projectID,
		RefreshToken: tokens.RefreshToken,
	})
	require.NoError(t, err)
	require.NotEmpty(t, newTokens.RefreshToken)
	require.NotEqual(t, tokens.RefreshTokenID, newTokens.RefreshTokenID)

	// Reusing the old refresh token is rejected and kills the session.
	_, _, err = account.RefreshToken(ctx, RefreshTokenCommand{
		ProjectID:    projectID,
		RefreshToken: tokens.RefreshToken,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())

	claims := parseRefreshClaims(t, buildTestConfig().GetSecurity().GetJwt().GetSecret(), tokens.RefreshToken)
	sessionDoc, err := docDB.GetDocument(ctx, projectID, "default", "sessions", claims.SessionID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, sessionDoc)

	// The rotated token is dead too, because its session was deleted.
	_, _, err = account.RefreshToken(ctx, RefreshTokenCommand{
		ProjectID:    projectID,
		RefreshToken: newTokens.RefreshToken,
	})
	require.Error(t, err)
}

func TestAccount_RefreshToken_ConcurrentRefreshSingleWinner(t *testing.T) {
	ctx, account, _, projectID := setupRefreshRotationAccount(t)
	tokens := signUpForRefresh(t, ctx, account, projectID, "concurrent@torchwood.local")

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := account.RefreshToken(ctx, RefreshTokenCommand{
				ProjectID:    projectID,
				RefreshToken: tokens.RefreshToken,
			})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, successes)
}
