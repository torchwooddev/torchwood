package client

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	infraauth "github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCheckAnonymousSessionRateLimit(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	a := &Account{rateLimiter: infraauth.NewRedisRateLimiter(rdb)}
	ctx := context.Background()

	for i := 0; i < anonymousSessionIPLimit; i++ {
		require.NoError(t, a.checkAnonymousSessionRateLimit(ctx, "203.0.113.1"))
	}
	err = a.checkAnonymousSessionRateLimit(ctx, "203.0.113.1")
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())

	// 其他 IP 不受影响。
	require.NoError(t, a.checkAnonymousSessionRateLimit(ctx, "203.0.113.2"))

	// 空 IP 不做限制。
	require.NoError(t, a.checkAnonymousSessionRateLimit(ctx, ""))
}

func TestCheckAnonymousSessionRateLimit_NilTolerated(t *testing.T) {
	t.Parallel()

	a := &Account{}
	require.NoError(t, a.checkAnonymousSessionRateLimit(context.Background(), "203.0.113.1"))
}
