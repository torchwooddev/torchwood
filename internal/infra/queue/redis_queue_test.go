package queue

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisQueue_EnqueueDequeueRoundtrip(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	q := NewRedisQueue(rdb)
	ctx := context.Background()

	// 空队列 BRPOP 超时 → nil,nil（miniredis 最小阻塞粒度 1s）。
	payload, err := q.Dequeue(ctx, "torchwood:queue:functions-executions", 50*time.Millisecond)
	require.NoError(t, err)
	require.Nil(t, payload)

	require.NoError(t, q.Enqueue(ctx, "torchwood:queue:functions-executions", []byte(`{"execution_id":"e1"}`)))
	require.NoError(t, q.Enqueue(ctx, "torchwood:queue:functions-executions", []byte(`{"execution_id":"e2"}`)))

	got, err := q.Dequeue(ctx, "torchwood:queue:functions-executions", time.Second)
	require.NoError(t, err)
	require.Equal(t, `{"execution_id":"e1"}`, string(got), "LPUSH+BRPOP 先进先出")

	got, err = q.Dequeue(ctx, "torchwood:queue:functions-executions", time.Second)
	require.NoError(t, err)
	require.Equal(t, `{"execution_id":"e2"}`, string(got))
}

func TestRedisQueue_DequeueBlocksUntilValue(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	q := NewRedisQueue(rdb)
	ctx := context.Background()

	done := make(chan []byte, 1)
	go func() {
		payload, _ := q.Dequeue(ctx, "torchwood:queue:functions-executions", 5*time.Second)
		done <- payload
	}()
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, q.Enqueue(ctx, "torchwood:queue:functions-executions", []byte("late")))

	select {
	case payload := <-done:
		require.Equal(t, "late", string(payload))
	case <-time.After(3 * time.Second):
		t.Fatal("BRPOP 未在有值到达时返回")
	}
}
