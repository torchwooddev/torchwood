package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
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
)

func init() {
	prometheus.MustRegister(outboxPending, outboxPublishTotal, outboxPublishLag)
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

// Run 以 200ms 间隔轮询领取；ctx 取消即返回。
func (w *OutboxWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
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
// 兜底（dispatched_at 过旧）合并为同一查询，同一 FOR UPDATE SKIP LOCKED
// 语义（v2 设计 §3.4 领取 SQL）。
func (w *OutboxWorker) pollOnce(ctx context.Context) error {
	if n, err := w.db.Conn(ctx).NewSelect().Model((*model.DocumentEventsOutbox)(nil)).
		Where("published_at IS NULL").Count(ctx); err == nil {
		outboxPending.Set(float64(n))
	}

	var rows []model.DocumentEventsOutbox
	err := w.db.Conn(ctx).NewSelect().Model(&rows).
		Column("event_id", "payload", "channel", "created_at", "attempts").
		Where("published_at IS NULL").
		Where("available_at <= NOW()").
		Where("(dispatched_at IS NULL OR dispatched_at < NOW() - INTERVAL '2 minutes')").
		Order("available_at").
		Limit(outboxBatchSize).
		For("UPDATE SKIP LOCKED").
		Scan(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		w.dispatch(ctx, &rows[i])
	}
	return nil
}

// dispatch 把单行信封 XADD 到 Stream；成功刷新 dispatched_at，失败
// attempts+1 指数退避或迁入死信表。
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
	if _, err := w.db.Conn(ctx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
		Set("dispatched_at = NOW()").
		Where("event_id = ?", row.EventID).Exec(ctx); err != nil {
		w.logger.Error("mark outbox dispatched failed", "event_id", row.EventID, "error", err)
	}
}

// failRow 记录 XADD 失败：attempts+1、available_at 指数退避（上限 60s）；
// attempts 达到 maxOutboxAttempts 时把行迁入 document_events_outbox_dead。
func (w *OutboxWorker) failRow(ctx context.Context, row *model.DocumentEventsOutbox, cause error) {
	attempts := row.Attempts + 1
	if attempts >= maxOutboxAttempts {
		if err := w.db.RunInTx(ctx, func(txCtx context.Context) error {
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
	if _, err := w.db.Conn(ctx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
		Set("attempts = attempts + 1").
		Set("available_at = ?", time.Now().Add(backoff)).
		Where("event_id = ?", row.EventID).Exec(ctx); err != nil {
		w.logger.Error("mark outbox retry failed", "event_id", row.EventID, "error", err)
	}
}
