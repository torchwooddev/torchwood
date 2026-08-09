package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
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

func TestRedisAccountTokenStore_MagicURL(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := auth.NewRedisAccountTokenStore(rdb)
	ctx := context.Background()

	secret, expireAt, err := store.CreateMagicURLToken(ctx, "proj-1", "user-1", "user@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	require.False(t, expireAt.IsZero())

	// 一次性消费：成功后再次验证失败。
	require.NoError(t, store.VerifyMagicURLToken(ctx, "proj-1", "user-1", secret))
	require.Error(t, store.VerifyMagicURLToken(ctx, "proj-1", "user-1", secret))

	// 错误 secret 拒绝。
	secret2, _, err := store.CreateMagicURLToken(ctx, "proj-1", "user-1", "user@example.com")
	require.NoError(t, err)
	require.Error(t, store.VerifyMagicURLToken(ctx, "proj-1", "user-1", secret2+"0"))
}

func TestRedisAccountTokenStore_MagicURLExpiry(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := auth.NewRedisAccountTokenStore(rdb)
	ctx := context.Background()

	secret, _, err := store.CreateMagicURLToken(ctx, "proj-1", "user-1", "user@example.com")
	require.NoError(t, err)
	mr.FastForward(61 * time.Minute) // > 1h TTL
	require.Error(t, store.VerifyMagicURLToken(ctx, "proj-1", "user-1", secret))

	// purpose 隔离：recovery 的 token 不能用于 magic_url，反之亦然。
	recoverySecret, _, err := store.CreateRecoveryToken(ctx, "proj-1", "user-1", "user@example.com")
	require.NoError(t, err)
	require.Error(t, store.VerifyMagicURLToken(ctx, "proj-1", "user-1", recoverySecret))
}
