package client

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func jwtTestConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: "jwt-app-test-secret"},
		},
	}
}

func TestAccount_CreateJWT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	account := NewTestAccountWithRedis(jwtTestConfig(), projectRepo, docDB, rdb)

	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "jwt-user@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Email:     user.Email,
		Roles:     []string{"users", "user:" + user.ID},
	})

	token, err := account.CreateJWT(userCtx)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, ok := jwtparser.Parse(jwtparser.DeriveKey(jwtTestConfig().GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeEndUserJWT), token)
	require.True(t, ok)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, projectID, claims.ProjectID)
	require.Equal(t, "end_user", claims.ActorKind)
	require.Equal(t, jwtparser.TokenTypeAccess, claims.TokenType)
	require.Equal(t, user.Email, claims.Username)
	require.Contains(t, claims.Roles, "user:"+user.ID)

	// 一次性 JWT：携带唯一 jti（TokenID）与一次性消费记录（5min TTL）。
	require.NotEmpty(t, claims.TokenID)
	n, err := rdb.Exists(ctx, "Torchwood:jwt:one-time:"+claims.TokenID).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// TTL ≈ 5 分钟。
	ttl := time.Until(time.Unix(claims.ExpiresAt, 0))
	require.InDelta(t, 5*time.Minute, ttl, float64(30*time.Second))
	require.WithinDuration(t, time.Now().Add(5*time.Minute), time.Unix(claims.ExpiresAt, 0), 30*time.Second)
}

func TestAccount_CreateJWT_Unauthenticated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	account := NewTestAccount(jwtTestConfig(), projectRepo, docDB)

	// 无 principal → 401。
	_, err := account.CreateJWT(ctx)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())

	// 先建立系统集合（SignUp），再查不存在的用户 → 404。
	_, _, _, _, err = account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "jwt-setup@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    "no-such-user",
		Roles:     []string{"users"},
	})
	_, err = account.CreateJWT(userCtx)
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}
