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

func TestRedisMFAChallengeStore_OneTimeConsume(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := auth.NewRedisMFAChallengeStore(rdb)
	ctx := context.Background()

	token, expireAt, err := store.Create(ctx, "proj-1", "user-1")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.False(t, expireAt.IsZero())

	projectID, userID, err := store.Consume(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "proj-1", projectID)
	require.Equal(t, "user-1", userID)

	// 二次消费拒绝（GETDEL 已删除）。
	_, _, err = store.Consume(ctx, token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid or expired challenge")
}

func TestRedisMFAChallengeStore_Expired(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := auth.NewRedisMFAChallengeStore(rdb)
	ctx := context.Background()

	token, _, err := store.Create(ctx, "proj-1", "user-1")
	require.NoError(t, err)
	mr.FastForward(6 * time.Minute) // > 5 min TTL
	_, _, err = store.Consume(ctx, token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid or expired challenge")
}

func TestRedisMFAChallengeStore_UnknownToken(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := auth.NewRedisMFAChallengeStore(rdb)
	_, _, consumeErr := store.Consume(context.Background(), "does-not-exist")
	require.Error(t, consumeErr)
	require.Contains(t, consumeErr.Error(), "invalid or expired challenge")
}
