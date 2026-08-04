package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/deeploop-ai/graviton/internal/infra/auth"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRedisRateLimiter_WindowBudget(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	limiter := auth.NewRedisRateLimiter(rdb)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		require.NoError(t, limiter.Allow(ctx, "anonymous:ip:203.0.113.1", 20, time.Hour))
	}
	err = limiter.Allow(ctx, "anonymous:ip:203.0.113.1", 20, time.Hour)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())

	// 其他 IP 不受影响。
	require.NoError(t, limiter.Allow(ctx, "anonymous:ip:203.0.113.2", 20, time.Hour))

	// 窗口过期后恢复。
	mr.FastForward(time.Hour + time.Second)
	require.NoError(t, limiter.Allow(ctx, "anonymous:ip:203.0.113.1", 20, time.Hour))
}

func TestRedisRateLimiter_EmptyKeyNoop(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	limiter := auth.NewRedisRateLimiter(rdb)

	for i := 0; i < 30; i++ {
		require.NoError(t, limiter.Allow(context.Background(), "", 1, time.Minute))
	}
}
