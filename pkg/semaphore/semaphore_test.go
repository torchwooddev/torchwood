package semaphore

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestInMemorySemaphore(t *testing.T) {
	t.Parallel()
	sem := NewInMemory(2)
	ok, rel, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, rel)
	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.False(t, ok, "third acquire should fail")
	rel()
	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRedisSemaphore_Basic(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	sem := NewRedis(rdb, "test:sem:basic", 2, 10*time.Second)
	require.NotNil(t, sem)

	ok, rel1, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, rel1)

	ok, rel2, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.False(t, ok, "third acquire should be rejected")

	rel1()
	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	rel2()
	// after release, should be able to acquire again
	ok, rel3, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	rel3()
}

func TestRedisSemaphore_TTLExpiry(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	sem := NewRedis(rdb, "test:sem:ttl", 1, 1*time.Second)
	ok, _, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.False(t, ok)

	// Fast-forward miniredis to expire the key
	mr.FastForward(2 * time.Second)

	ok, rel, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok, "after TTL expiry should be acquirable")
	rel()
}

func TestRedisSemaphore_LuaCompareAndDel(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	sem := NewRedis(rdb, "test:sem:lua", 1, 10*time.Second)
	ok, rel, err := sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// Simulate another holder overwriting the key after TTL (but before release)
	// by manually setting a different token
	require.NoError(t, mr.Set("test:sem:lua:slot:0", "other-token"))

	// Our release should not delete the other holder's key (Lua compare-and-del)
	rel()
	val2, err2 := mr.Get("test:sem:lua:slot:0")
	require.NoError(t, err2)
	require.Equal(t, "other-token", val2, "release must not delete slot owned by another token")

	// Now the slot is still occupied, next acquire should fail
	ok, _, err = sem.TryAcquire(context.Background())
	require.NoError(t, err)
	require.False(t, ok)
}
