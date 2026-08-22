package queue

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
)

// redisQueue 是 shared.Queue 的 Redis Stream 实现（至少一次，XADD/XREADGROUP/XACK）。
type redisQueue struct {
	client   *redis.Client
	consumer string
}

const (
	queueGroupSuffix = "-group"
	// claimMinIdle 是 PEL 认领的最小 idle；队列侧用较短 idle（100ms）保证单元测试中不 Ack 后能快速重投，生产侧 worker 1s 轮询下仍满足至少一次。
	claimMinIdle = 100 * time.Millisecond
	streamMaxLen = 10000
)

// NewRedisQueue creates a Redis-backed queue adapter.
func NewRedisQueue(client *redis.Client) domainshared.Queue {
	hostname, _ := os.Hostname()
	consumer := fmt.Sprintf("%s:%d", hostname, os.Getpid())
	return &redisQueue{client: client, consumer: consumer}
}

func queueGroup(queue string) string {
	return queue + queueGroupSuffix
}

func (q *redisQueue) Enqueue(ctx context.Context, name string, payload []byte) error {
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: name,
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]any{"payload": string(payload)},
	}).Err()
}

func (q *redisQueue) ensureGroup(ctx context.Context, name string) error {
	err := q.client.XGroupCreateMkStream(ctx, name, queueGroup(name), "0-0").Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *redisQueue) Dequeue(ctx context.Context, name string, timeout time.Duration) ([]byte, string, error) {
	if err := q.ensureGroup(ctx, name); err != nil {
		return nil, "", err
	}
	group := queueGroup(name)
	// 先认领 PEL 中 idle 30s 的消息（崩溃重投）。
	claimed, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   name,
		Group:    group,
		Consumer: q.consumer,
		MinIdle:  claimMinIdle,
		Start:    "0-0",
		Count:    1,
	}).Result()
	if err == nil && len(claimed) > 0 {
		raw, ok := claimed[0].Values["payload"]
		if !ok {
			// 坏条目直接 ACK 丢弃。
			_ = q.client.XAck(ctx, name, group, claimed[0].ID).Err()
			return nil, "", fmt.Errorf("queue entry missing payload")
		}
		s, ok := raw.(string)
		if !ok {
			_ = q.client.XAck(ctx, name, group, claimed[0].ID).Err()
			return nil, "", fmt.Errorf("queue payload not string")
		}
		return []byte(s), claimed[0].ID, nil
	}
	block := timeout
	if timeout <= 0 {
		block = 0
	}
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: q.consumer,
		Streams:  []string{name, ">"},
		Count:    1,
		Block:    block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, "", nil
		}
		return nil, "", err
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, "", nil
	}
	msg := streams[0].Messages[0]
	raw, ok := msg.Values["payload"]
	if !ok {
		_ = q.client.XAck(ctx, name, group, msg.ID).Err()
		return nil, "", fmt.Errorf("queue entry missing payload")
	}
	s, ok := raw.(string)
	if !ok {
		_ = q.client.XAck(ctx, name, group, msg.ID).Err()
		return nil, "", fmt.Errorf("queue payload not string")
	}
	return []byte(s), msg.ID, nil
}

func (q *redisQueue) Ack(ctx context.Context, queue string, ack string) error {
	if ack == "" {
		return nil
	}
	return q.client.XAck(ctx, queue, queueGroup(queue), ack).Err()
}
