package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/uptrace/bun"
)

const (
	// outboxPollInterval 是领取轮询间隔（与 subscriber 的 XREADGROUP
	// BLOCK 200ms 对齐，v2 设计 §3.4）。
	outboxPollInterval = 200 * time.Millisecond
	// outboxBatchSize 是每轮领取上限（FOR UPDATE SKIP LOCKED，32 行）。
	outboxBatchSize = 32
	// maxOutboxAttempts 是 XADD 失败的最大重试次数，超限迁入死信表。
	maxOutboxAttempts = 10
	// outboxRedispatchAfter 是整进程挂死兜底窗口：dispatched_at 超过
	// 该时长仍未 published（server 无人消费）的行被重新领取再 XADD。
	outboxRedispatchAfter = 2 * time.Minute
	// outboxMaxBackoff 是 XADD 失败后 available_at 退避上限。
	outboxMaxBackoff = time.Minute
	// outboxCleanupInterval 是已发布行/死信行清理周期。
	outboxCleanupInterval = 10 * time.Minute
	// outboxPublishedRetention 是已发布行保留窗口（排障/重放核对）。
	outboxPublishedRetention = 24 * time.Hour
	// outboxDeadRetention 是死信行保留窗口。
	outboxDeadRetention = 30 * 24 * time.Hour
	// outboxStatementTimeout 是每条 DB 语句的独立超时（W-H per-语句 deadline，
	// 防单条慢查询卡住 200ms 轮询；事务整体取 2*statement）。
	outboxStatementTimeout = 5 * time.Second
)

// outbox 领取与投递指标（前缀 torchwood_，注册到默认注册表，
// worker 进程未挂 /metrics 端点，仅统计计数，供日后接入）。
var (
	outboxPending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "torchwood_outbox_pending",
		Help: "Number of outbox rows not yet published (published_at IS NULL).",
	})
	outboxPublishTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_outbox_publish_total",
		Help: "Total outbox rows enqueued to the realtime transport.",
	}, []string{"result"})
	outboxPublishLag = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "torchwood_outbox_publish_lag_seconds",
		Help:    "Latency between event creation and transport enqueue.",
		Buckets: prometheus.DefBuckets,
	})
	// outboxDead 是死信表当前行数（W-J：死信可观测——非零即需人工介入，
	// 重放工具见修复方案 W-J 残留）。
	outboxDead = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "torchwood_outbox_dead",
		Help: "Number of rows in document_events_outbox_dead.",
	})
)

func init() {
	prometheus.MustRegister(outboxPending, outboxPublishTotal, outboxPublishLag, outboxDead)
}

// OutboxWorker 领取 document_events_outbox 并把完整信封 XADD 到
// Redis Stream（v2 设计 §3.4）。成功只标 dispatched_at，**不**标
// published_at（后者由 server 进程的 RealtimeSubscriber 在 XACK 后标记）。
type OutboxWorker struct {
	db        *clients.Database
	transport shared.RealtimeTransport
	logger    *slog.Logger
}

// NewOutboxWorker 构造 outbox → transport 的领取循环。
func NewOutboxWorker(db *clients.Database, transport shared.RealtimeTransport, logger *slog.Logger) *OutboxWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboxWorker{db: db, transport: transport, logger: logger}
}

// Run 以 200ms 间隔轮询领取；低频 ticker 清理已发布/死信行；ctx 取消即返回。
func (w *OutboxWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()
	cleanup := time.NewTicker(outboxCleanupInterval)
	defer cleanup.Stop()
	// 启动即清一次，避免短命进程错过周期窗口。
	w.cleanupOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-cleanup.C:
			w.cleanupOnce(ctx)
		}
		if err := w.pollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("outbox claim failed", "error", err)
		}
	}
}

// pollOnce 领取一轮：主路径（dispatched_at IS NULL）+ 2min 整进程挂死
// 兜底（dispatched_at 过旧）合并为同一查询（v2 设计 §3.4 领取 SQL）。
func (w *OutboxWorker) pollOnce(ctx context.Context) error {
	func() {
		ctx2, cancel := context.WithTimeout(ctx, outboxStatementTimeout)
		defer cancel()
		if n, err := w.db.Conn(ctx2).NewSelect().Model((*model.DocumentEventsOutbox)(nil)).
			Where("published_at IS NULL").Count(ctx2); err == nil {
			outboxPending.Set(float64(n))
		}
	}()

	rows, err := w.claim(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		w.dispatch(ctx, &rows[i])
	}
	return nil
}

// claim 在单个事务内 SELECT ... FOR UPDATE SKIP LOCKED 并把领取行标记
// dispatched_at——行锁在整个事务内生效，多副本不会领取同一批行重复 XADD。
// 标记随事务提交持久化；XADD 在事务外进行（不持数据库行锁做 IO）。
// claim 后崩溃的行由 2min redispatch 窗口兜底重发。
func (w *OutboxWorker) claim(ctx context.Context) ([]model.DocumentEventsOutbox, error) {
	var rows []model.DocumentEventsOutbox
	ctx2, cancel := context.WithTimeout(ctx, 2*outboxStatementTimeout)
	defer cancel()
	err := w.db.RunInTx(ctx2, func(txCtx context.Context) error {
		if err := w.db.Conn(txCtx).NewSelect().Model(&rows).
			Column("event_id", "payload", "channel", "created_at", "attempts").
			Where("published_at IS NULL").
			Where("available_at <= NOW()").
			Where(fmt.Sprintf("(dispatched_at IS NULL OR dispatched_at < NOW() - INTERVAL '%d minutes')",
				int(outboxRedispatchAfter.Minutes()))).
			Order("available_at").
			Limit(outboxBatchSize).
			For("UPDATE SKIP LOCKED").
			Scan(txCtx); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, len(rows))
		for i := range rows {
			ids[i] = rows[i].EventID
		}
		_, err := w.db.Conn(txCtx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
			Set("dispatched_at = NOW()").
			Where("event_id IN (?)", bun.List(ids)).
			Exec(txCtx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// cleanupOnce 删除超过保留窗口的已发布行与死信行（表无限增长治理）。
func (w *OutboxWorker) cleanupOnce(ctx context.Context) {
	func() {
		ctx2, cancel := context.WithTimeout(ctx, outboxStatementTimeout)
		defer cancel()
		if n, err := w.db.Conn(ctx2).NewSelect().TableExpr("document_events_outbox_dead").Count(ctx2); err == nil {
			outboxDead.Set(float64(n))
		}
	}()
	func() {
		ctx2, cancel := context.WithTimeout(ctx, outboxStatementTimeout)
		defer cancel()
		if res, err := w.db.Conn(ctx2).NewDelete().Model((*model.DocumentEventsOutbox)(nil)).
			Where("published_at IS NOT NULL").
			Where("published_at < ?", time.Now().Add(-outboxPublishedRetention)).
			Exec(ctx2); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				w.logger.Info("outbox purged published rows", "count", n)
			}
		} else if ctx.Err() == nil {
			w.logger.Error("outbox purge published failed", "error", err)
		}
	}()
	func() {
		ctx2, cancel := context.WithTimeout(ctx, outboxStatementTimeout)
		defer cancel()
		if res, err := w.db.Conn(ctx2).NewRaw(
			`DELETE FROM document_events_outbox_dead WHERE created_at < ?`,
			time.Now().Add(-outboxDeadRetention)).Exec(ctx2); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				w.logger.Info("outbox purged dead rows", "count", n)
			}
		} else if ctx.Err() == nil {
			w.logger.Error("outbox purge dead failed", "error", err)
		}
	}()
}

// dispatch 把单行信封 XADD 到 Stream。dispatched_at 已在 claim 事务内标记，
// 此处无需回写；XADD 失败走 failRow（归还 dispatched_at 并按退避重试）。
func (w *OutboxWorker) dispatch(ctx context.Context, row *model.DocumentEventsOutbox) {
	ev, err := UnmarshalEnvelope(row.Payload)
	if err != nil {
		w.logger.Error("outbox row payload malformed", "event_id", row.EventID, "error", err)
		w.failRow(ctx, row, err)
		return
	}
	if err := w.transport.Enqueue(ctx, ev); err != nil {
		if ctx.Err() != nil {
			return
		}
		outboxPublishTotal.WithLabelValues("error").Inc()
		w.logger.Error("outbox enqueue failed", "event_id", row.EventID, "error", err)
		w.failRow(ctx, row, err)
		return
	}
	outboxPublishTotal.WithLabelValues("ok").Inc()
	outboxPublishLag.Observe(time.Since(ev.CreatedAt).Seconds())
}

// failRow 记录 XADD 失败：attempts+1、available_at 指数退避（上限 60s）、
// 归还 dispatched_at（claim 时预标记的，置 NULL 恢复"按退避快速重试"语义，
// 不必等 2min redispatch 窗口）；attempts 达到 maxOutboxAttempts 时把行
// 迁入 document_events_outbox_dead。
func (w *OutboxWorker) failRow(ctx context.Context, row *model.DocumentEventsOutbox, cause error) {
	attempts := row.Attempts + 1
	if attempts >= maxOutboxAttempts {
		ctx2, cancel := context.WithTimeout(ctx, 2*outboxStatementTimeout)
		defer cancel()
		if err := w.db.RunInTx(ctx2, func(txCtx context.Context) error {
			if _, err := w.db.Conn(txCtx).NewRaw(`INSERT INTO document_events_outbox_dead
				(event_id, project_id, topic, channel, payload, attempts, last_error, created_at)
				SELECT event_id, project_id, topic, channel, payload, ? AS attempts, ? AS last_error, created_at
				FROM document_events_outbox
				WHERE event_id = ?`,
				attempts, cause.Error(), row.EventID).Exec(txCtx); err != nil {
				return err
			}
			_, err := w.db.Conn(txCtx).NewDelete().Model((*model.DocumentEventsOutbox)(nil)).
				Where("event_id = ?", row.EventID).Exec(txCtx)
			return err
		}); err != nil {
			w.logger.Error("outbox dead-letter failed", "event_id", row.EventID, "error", err)
			return
		}
		outboxPublishTotal.WithLabelValues("dead").Inc()
		w.logger.Warn("outbox event dead-lettered",
			"event_id", row.EventID, "attempts", attempts, "error", cause)
		return
	}
	backoff := time.Duration(1<<attempts) * time.Second
	if backoff > outboxMaxBackoff {
		backoff = outboxMaxBackoff
	}
	func() {
		ctx2, cancel := context.WithTimeout(ctx, outboxStatementTimeout)
		defer cancel()
		if _, err := w.db.Conn(ctx2).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
			Set("attempts = attempts + 1").
			Set("available_at = ?", time.Now().Add(backoff)).
			Set("dispatched_at = NULL").
			Where("event_id = ?", row.EventID).Exec(ctx2); err != nil {
			w.logger.Error("mark outbox retry failed", "event_id", row.EventID, "error", err)
		}
	}()
}
