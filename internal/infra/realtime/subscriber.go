package realtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
)

const (
	// realtimeGroup 是消费者组名。
	realtimeGroup = "torchwood-realtime"
	// claimMinIdle 是 XAUTOCLAIM 认领 PEL 的最小 idle 时间（扇出后
	// 未 XACK 的条目 30s 后被认领重投）。
	claimMinIdle = 30 * time.Second
	// readBlockTimeout 是 XREADGROUP BLOCK 时长（200ms 一轮）。
	readBlockTimeout = 200 * time.Millisecond
	// claimBatchSize 是每轮认领/读取批量（与领取 SQL LIMIT 32 对齐）。
	claimBatchSize = 32
	// maxRedisBackoff 是 Redis 断线重试退避上限。
	maxRedisBackoff = 30 * time.Second
)

// Subscriber 是 server 进程的 Stream 消费者（v2 设计 §3.4 锁定循环）：
//
//	Start:   XGROUP CREATE torchwood:realtime group 0-0 MKSTREAM
//	        （BUSYGROUP 忽略；0-0 保证建组前 worker 已 XADD 的条目
//	        仍作为本组新消息被 > 读出）
//	        启动先排空 PEL（XAUTOCLAIM 0-0 直到空）
//	Loop:    XAUTOCLAIM（idle 30s）→ XREADGROUP >（BLOCK 200ms）
//	        每条：解码完整信封（含 acl）→ Hub.Dispatch
//	        成功 → XACK → UPDATE outbox SET published_at
//	        失败 → 不 XACK（留 PEL，30s 后认领）
//	        断线 → 指数退避重试，不退出进程
type Subscriber struct {
	client *redis.Client
	db     *clients.Database
	hub    *Hub
	logger *slog.Logger

	consumer string
	// claimIdle 覆盖默认 30s（测试可缩短，生产保持 claimMinIdle）。
	claimIdle time.Duration
}

// NewRealtimeSubscriber 构造 Stream 消费端。consumer 为 hostname:pid
// （进程内唯一，重启后换名不影响组语义）。
func NewRealtimeSubscriber(client *redis.Client, db *clients.Database, hub *Hub, logger *slog.Logger) *Subscriber {
	if logger == nil {
		logger = slog.Default()
	}
	hostname, _ := os.Hostname()
	return &Subscriber{
		client:    client,
		db:        db,
		hub:       hub,
		logger:    logger,
		consumer:  fmt.Sprintf("%s:%d", hostname, os.Getpid()),
		claimIdle: claimMinIdle,
	}
}

// Run 阻塞消费循环；ctx 取消后处理完当前批再返回。
func (s *Subscriber) Run(ctx context.Context) error {
	if err := s.ensureGroup(ctx); err != nil {
		return err
	}
	if err := s.drainPEL(ctx); err != nil {
		s.logger.Warn("realtime PEL drain interrupted", "error", err)
	}

	backoff := time.Second
	for {
		if err := s.pollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.logger.Error("realtime stream poll failed", "error", err)
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
		backoff = time.Second
	}
}

// ensureGroup 建组（0-0 MKSTREAM）；BUSYGROUP 视为已存在忽略。
// Redis 断线时退避重试直到 ctx 取消。
func (s *Subscriber) ensureGroup(ctx context.Context) error {
	for {
		err := s.client.XGroupCreateMkStream(ctx, streamKey, realtimeGroup, "0-0").Err()
		if err == nil || isBusyGroup(err) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.logger.Error("ensure realtime group failed", "error", err)
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

// drainPEL 启动时从 0-0 循环 XAUTOCLAIM 直到空（把崩溃遗留的 PEL
// 条目先消费掉，再进入常规循环）。
func (s *Subscriber) drainPEL(ctx context.Context) error {
	for {
		msgs, _, err := s.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   streamKey,
			Group:    realtimeGroup,
			Consumer: s.consumer,
			MinIdle:  0,
			Start:    "0-0",
			Count:    claimBatchSize,
		}).Result()
		if err != nil {
			return err
		}
		for i := range msgs {
			s.process(ctx, &msgs[i])
		}
		if len(msgs) < claimBatchSize {
			return nil
		}
	}
}

// pollOnce 一轮循环：先 XAUTOCLAIM 认领 idle 30s 的 PEL 条目，再
// XREADGROUP > 读新消息（BLOCK 200ms）。
func (s *Subscriber) pollOnce(ctx context.Context) error {
	s.updateStreamMetrics(ctx)

	claimed, _, err := s.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   streamKey,
		Group:    realtimeGroup,
		Consumer: s.consumer,
		MinIdle:  s.claimIdle,
		Start:    "0-0",
		Count:    claimBatchSize,
	}).Result()
	if err != nil {
		return err
	}
	for i := range claimed {
		s.process(ctx, &claimed[i])
	}

	streams, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    realtimeGroup,
		Consumer: s.consumer,
		Streams:  []string{streamKey, ">"},
		Count:    claimBatchSize,
		Block:    readBlockTimeout,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil // BLOCK 超时无新消息
		}
		return err
	}
	for _, st := range streams {
		for i := range st.Messages {
			s.process(ctx, &st.Messages[i])
		}
	}
	return nil
}

// updateStreamMetrics 刷新 Stream 长度与 PEL 长度 gauge（best-effort）。
func (s *Subscriber) updateStreamMetrics(ctx context.Context) {
	RealtimeStreamLen.Set(float64(s.client.XLen(ctx, streamKey).Val()))
	if pel := s.client.XPending(ctx, streamKey, realtimeGroup).Val(); pel != nil {
		RealtimePelLen.Set(float64(pel.Count))
	}
}

// process 处理单条 Stream 消息：解码完整信封（含 acl）→ Hub.Dispatch
// → 成功 XACK → 标 published_at。扇出前失败不 XACK（留 PEL，30s idle
// 后被本循环 XAUTOCLAIM 认领重投）。坏条目直接 XACK 丢弃防 PEL 卡死。
func (s *Subscriber) process(ctx context.Context, msg *redis.XMessage) {
	raw, ok := msg.Values["payload"]
	if !ok {
		s.logger.Error("realtime stream entry missing payload", "stream_id", msg.ID)
		_ = s.client.XAck(ctx, streamKey, realtimeGroup, msg.ID).Err()
		return
	}
	payload, ok := raw.(string)
	if !ok {
		s.logger.Error("realtime stream entry payload not string", "stream_id", msg.ID)
		_ = s.client.XAck(ctx, streamKey, realtimeGroup, msg.ID).Err()
		return
	}
	ev, err := infraevents.UnmarshalEnvelope([]byte(payload))
	if err != nil {
		s.logger.Error("realtime stream entry malformed", "stream_id", msg.ID, "error", err)
		_ = s.client.XAck(ctx, streamKey, realtimeGroup, msg.ID).Err()
		return
	}

	s.hub.Dispatch(ev)

	if err := s.client.XAck(ctx, streamKey, realtimeGroup, msg.ID).Err(); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logger.Error("realtime stream ack failed",
			"event_id", ev.EventID, "stream_id", msg.ID, "error", err)
		return // 留 PEL，30s 后认领重投（Hub 按 event_id 去重）
	}
	if _, err := s.db.Conn(ctx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
		Set("published_at = NOW()").
		Where("event_id = ?", ev.EventID).Exec(ctx); err != nil {
		s.logger.Error("mark outbox published failed", "event_id", ev.EventID, "error", err)
	}
}

var _ shared.RealtimeFanout = (*Subscriber)(nil)
