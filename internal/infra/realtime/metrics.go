// Package realtime 提供 server 进程内的 WebSocket Hub 与 Redis Streams
// 最后一跳（v2 设计 §3.4 / §4.5）：Subscriber 从 Stream 消费完整信封
// （含 acl）写入 Hub，Hub 按订阅频道 + 写前/写后 ACL 快照扇出
// ClientPayload()（无 acl）帧。
package realtime

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Realtime 指标（前缀 torchwood_，注册到默认注册表，metrics 端点
// /metrics 由 internal/infra/server 的 MetricsServer 暴露）。
var (
	// RealtimeSubscriptions 是当前订阅数（channel × conn 对）。
	RealtimeSubscriptions = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "torchwood_realtime_subscriptions",
		Help: "Current realtime channel subscriptions.",
	})
	// RealtimeEventsDeliveredTotal 是 Hub 成功入队的事件帧计数。
	RealtimeEventsDeliveredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_realtime_events_delivered_total",
		Help: "Realtime event frames enqueued to connections.",
	}, []string{"event"})
	// RealtimeEventsDroppedTotal 是发送 chan 满载丢弃的事件计数。
	RealtimeEventsDroppedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_realtime_events_dropped_total",
		Help: "Realtime event frames dropped.",
	}, []string{"reason"})
	// RealtimeStreamLen 是 Redis Stream 当前条目数。
	RealtimeStreamLen = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "torchwood_realtime_stream_len",
		Help: "Redis realtime stream length.",
	})
	// RealtimePelLen 是消费者组当前 PEL（pending entries）长度。
	RealtimePelLen = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "torchwood_realtime_pel_len",
		Help: "Realtime consumer group pending entries length.",
	})
)

func init() {
	prometheus.MustRegister(
		RealtimeSubscriptions,
		RealtimeEventsDeliveredTotal,
		RealtimeEventsDroppedTotal,
		RealtimeStreamLen,
		RealtimePelLen,
	)
}
