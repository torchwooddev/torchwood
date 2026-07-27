package auth_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/deeploop-ai/graviton/internal/infra/auth"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRedisLoginThrottle_EmailBudget(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	throttle := auth.NewRedisLoginThrottle(rdb)
	ctx := context.Background()

	require.NoError(t, throttle.Check(ctx, "end_user", "user@example.com", ""))
	for i := 0; i < 10; i++ {
		require.NoError(t, throttle.RecordFailure(ctx, "end_user", "user@example.com", ""))
	}
	err = throttle.Check(ctx, "end_user", "user@example.com", "")
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())

	// A different namespace is not affected.
	require.NoError(t, throttle.Check(ctx, "admin", "user@example.com", ""))

	// Reset clears the failure budget.
	require.NoError(t, throttle.Reset(ctx, "end_user", "user@example.com", ""))
	require.NoError(t, throttle.Check(ctx, "end_user", "user@example.com", ""))
}

func TestRedisLoginThrottle_IPBudget(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	throttle := auth.NewRedisLoginThrottle(rdb)
	ctx := context.Background()

	for i := 0; i < 30; i++ {
		require.NoError(t, throttle.RecordFailure(ctx, "end_user", "", "1.2.3.4"))
	}
	err = throttle.Check(ctx, "end_user", "", "1.2.3.4")
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())
}
