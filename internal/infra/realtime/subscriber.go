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
	"github.com/uptrace/bun"
)

const (
	// maxRedisBackoff 是 Redis 断线重试退避上限。
	maxRedisBackoff = 30 * time.Second

	// publishedAtFlushInterval 是 published_at 标记的攒批窗口（Round4 J5-5）：
	// 广播模式下每事件每副本单行 UPDATE 写放大，攒批为
	// UPDATE ... WHERE event_id IN (...)，200ms 或 32 条先到者触发。
	publishedAtFlushInterval = 200 * time.Millisecond
	// publishedAtBatchSize 是单批 event_id 上限（与 outbox 领取批量同量级）。
	publishedAtBatchSize = 32
	// publishedAtQueueLen 是标记队列容量；打满时丢弃该条标记（事件已广播，
	// 后果仅是该行延迟到 redispatch 窗口被重投，at-least-once 语义不变）。
	publishedAtQueueLen = 1024
	// publishedAtFlushTimeout 是每批 UPDATE 的独立超时。
	publishedAtFlushTimeout = 5 * time.Second
)

// Subscriber 是 server 进程的 Pub/Sub 消费者（A6 修复后的广播模式）：
//
//	Enqueue: worker PUBLISH torchwood:realtime（完整信封 JSON）
//	Subscriber: SUBSCRIBE torchwood:realtime → Hub.Dispatch → 批量 UPDATE outbox SET published_at
//
// WHY: 广播保证多 server 副本各自 Hub 都收到同一事件，consumer group 的“每条只给一个消费者”会导致在线副本静默。
type Subscriber struct {
	client *redis.Client
	db     *clients.Database
	hub    *Hub
	logger *slog.Logger

	consumer string

	// markCh 是 published_at 标记队列：processPayload 入队，
	// markLoop 后台攒批落库（Round4 J5-5）。
	markCh chan string
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
		markCh:   make(chan string, publishedAtQueueLen),
	}
}

// Run 阻塞订阅循环；ctx 取消后返回。断线时指数退避重连，不退出进程。
// 另起 goroutine 运行 markLoop（published_at 攒批），返回前等待其排空退出。
func (s *Subscriber) Run(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.markLoop()
	}()
	defer func() {
		close(s.markCh) // 停止入队并让 markLoop 排空后退出
		<-done
	}()

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
	defer func() { _ = pubsub.Close() }()
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

// processPayload 解码完整信封（含 acl）→ Hub.Dispatch → 入队 published_at
// 标记（由 markLoop 攒批落库，Round4 J5-5）。队列满载时丢弃标记并告警：
// 事件本身已完成广播，丢标的后果是该 outbox 行在 redispatch 窗口后被重投
// （at-least-once），不丢事件。
func (s *Subscriber) processPayload(_ context.Context, payload string) {
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
	select {
	case s.markCh <- ev.EventID:
	default:
		s.logger.Warn("outbox published marker queue full; dropping marker",
			"event_id", ev.EventID)
	}
}

// markLoop 消费标记队列，按「200ms 或 32 条」批量把 published_at 落库。
// 使用独立 Background ctx + 单批超时：关停排空阶段父 ctx 可能已取消，
// 但最后一批标记仍应尽力写入。失败仅告警（行会被 redispatch 兜底）。
func (s *Subscriber) markLoop() {
	pending := make([]string, 0, publishedAtBatchSize)
	ticker := time.NewTicker(publishedAtFlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(pending) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), publishedAtFlushTimeout)
		_, err := s.db.Conn(ctx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
			Set("published_at = NOW()").
			Where("event_id IN (?)", bun.List(pending)).Exec(ctx)
		cancel()
		if err != nil {
			s.logger.Error("mark outbox published failed",
				"events", len(pending), "error", err)
		}
		pending = pending[:0]
	}

	for {
		select {
		case id, ok := <-s.markCh:
			if !ok {
				flush()
				return
			}
			pending = append(pending, id)
			if len(pending) >= publishedAtBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

var _ shared.RealtimeFanout = (*Subscriber)(nil)
