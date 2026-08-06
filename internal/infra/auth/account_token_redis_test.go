package auth_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/torchwoodio/torchwood/internal/infra/auth"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisAccountTokenStore_Verification(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := auth.NewRedisAccountTokenStore(rdb)
	ctx := context.Background()

	secret, _, err := store.CreateVerificationToken(ctx, "proj-1", "user-1", "user@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, secret)

	require.NoError(t, store.VerifyVerificationToken(ctx, "proj-1", "user-1", secret))
	require.Error(t, store.VerifyVerificationToken(ctx, "proj-1", "user-1", secret))
}
