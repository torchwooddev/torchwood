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

	// 空队列 → nil,nil
	payload, ack, err := q.Dequeue(ctx, "torchwood:queue:functions-executions", 50*time.Millisecond)
	require.NoError(t, err)
	require.Nil(t, payload)
	require.Empty(t, ack)

	require.NoError(t, q.Enqueue(ctx, "torchwood:queue:functions-executions", []byte(`{"execution_id":"e1"}`)))
	require.NoError(t, q.Enqueue(ctx, "torchwood:queue:functions-executions", []byte(`{"execution_id":"e2"}`)))

	got, ack, err := q.Dequeue(ctx, "torchwood:queue:functions-executions", time.Second)
	require.NoError(t, err)
	require.Equal(t, `{"execution_id":"e1"}`, string(got))
	require.NotEmpty(t, ack)
	require.NoError(t, q.Ack(ctx, "torchwood:queue:functions-executions", ack))

	got, ack, err = q.Dequeue(ctx, "torchwood:queue:functions-executions", time.Second)
	require.NoError(t, err)
	require.Equal(t, `{"execution_id":"e2"}`, string(got))
	require.NoError(t, q.Ack(ctx, "torchwood:queue:functions-executions", ack))
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
		payload, _, _ := q.Dequeue(ctx, "torchwood:queue:functions-executions", 5*time.Second)
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

func TestRedisQueue_NotAckRedelivers(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	q := NewRedisQueue(rdb)
	ctx := context.Background()

	require.NoError(t, q.Enqueue(ctx, "torchwood:queue:functions-executions", []byte(`{"execution_id":"e1"}`)))

	// 第一次 Dequeue 不 Ack
	payload, ack, err := q.Dequeue(ctx, "torchwood:queue:functions-executions", time.Second)
	require.NoError(t, err)
	require.Equal(t, `{"execution_id":"e1"}`, string(payload))
	require.NotEmpty(t, ack)
	// 不 Ack，模拟崩溃

	// 新消费者（同进程但经 XAUTOCLAIM）应能重投
	require.Eventually(t, func() bool {
		got, _, err := q.Dequeue(ctx, "torchwood:queue:functions-executions", 200*time.Millisecond)
		if err != nil {
			return false
		}
		return got != nil && string(got) == `{"execution_id":"e1"}`
	}, 5*time.Second, 100*time.Millisecond)
}
