package queue

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
)

// redisQueue 是 shared.Queue 的 Redis List 实现（LPUSH + BRPOP）。
type redisQueue struct {
	client *redis.Client
}

// NewRedisQueue creates a Redis-backed queue adapter.
func NewRedisQueue(client *redis.Client) domainshared.Queue {
	return &redisQueue{client: client}
}

func (q *redisQueue) Enqueue(ctx context.Context, name string, payload []byte) error {
	return q.client.LPush(ctx, name, payload).Err()
}

func (q *redisQueue) Dequeue(ctx context.Context, name string, timeout time.Duration) ([]byte, error) {
	block := timeout
	if timeout <= 0 {
		// 阻塞直到有任务或 ctx 取消。
		block = 0
	}
	res, err := q.client.BRPop(ctx, block, name).Result()
	if err != nil {
		if err == redis.Nil {
			// BRPOP 超时（队列为空）。
			return nil, nil
		}
		return nil, err
	}
	// BRPOP 返回 [queueName, value]。
	return []byte(res[1]), nil
}
