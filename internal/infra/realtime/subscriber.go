package realtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
)

const (
	// maxRedisBackoff 是 Redis 断线重试退避上限。
	maxRedisBackoff = 30 * time.Second
)

// Subscriber 是 server 进程的 Pub/Sub 消费者（A6 修复后的广播模式）：
//
//	Enqueue: worker PUBLISH torchwood:realtime（完整信封 JSON）
//	Subscriber: SUBSCRIBE torchwood:realtime → Hub.Dispatch → UPDATE outbox SET published_at
//
// WHY: 广播保证多 server 副本各自 Hub 都收到同一事件，consumer group 的“每条只给一个消费者”会导致在线副本静默。
type Subscriber struct {
	client *redis.Client
	db     *clients.Database
	hub    *Hub
	logger *slog.Logger

	consumer string
}

// NewRealtimeSubscriber 构造 Pub/Sub 消费端。consumer 为 hostname:pid
// （仅用于日志标识，Pub/Sub 无组语义）。
func NewRealtimeSubscriber(client *redis.Client, db *clients.Database, hub *Hub, logger *slog.Logger) *Subscriber {
	if logger == nil {
		logger = slog.Default()
	}
	hostname, _ := os.Hostname()
	return &Subscriber{
		client:   client,
		db:       db,
		hub:      hub,
		logger:   logger,
		consumer: fmt.Sprintf("%s:%d", hostname, os.Getpid()),
	}
}

// Run 阻塞订阅循环；ctx 取消后返回。断线时指数退避重连，不退出进程。
func (s *Subscriber) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if err := s.subscribeLoop(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.logger.Error("realtime subscribe loop failed", "error", err)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil
			}
			backoff *= 2
			if backoff > maxRedisBackoff {
				backoff = maxRedisBackoff
			}
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		backoff = time.Second
	}
}

// subscribeLoop 单次订阅会话：SUBSCRIBE → 消费直至 ctx 取消或连接丢失。
func (s *Subscriber) subscribeLoop(ctx context.Context) error {
	pubsub := s.client.Subscribe(ctx, realtimeChannel)
	defer pubsub.Close()
	// 等待订阅确认。
	if _, err := pubsub.Receive(ctx); err != nil {
		return err
	}
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("pubsub channel closed")
			}
			if msg == nil {
				continue
			}
			s.processPayload(ctx, msg.Payload)
		}
	}
}

// processPayload 解码完整信封（含 acl）→ Hub.Dispatch → 标 published_at。
func (s *Subscriber) processPayload(ctx context.Context, payload string) {
	if payload == "" {
		s.logger.Error("realtime pubsub empty payload")
		return
	}
	ev, err := infraevents.UnmarshalEnvelope([]byte(payload))
	if err != nil {
		s.logger.Error("realtime pubsub malformed", "error", err)
		return
	}
	s.hub.Dispatch(ev)
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.db.Conn(ctx2).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
		Set("published_at = NOW()").
		Where("event_id = ?", ev.EventID).Exec(ctx2); err != nil {
		s.logger.Error("mark outbox published failed", "event_id", ev.EventID, "error", err)
	}
}

var _ shared.RealtimeFanout = (*Subscriber)(nil)
