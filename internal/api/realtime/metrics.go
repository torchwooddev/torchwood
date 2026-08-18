package realtime

import (
	"github.com/prometheus/client_golang/prometheus"
)

// WS 握手/连接指标（前缀 torchwood_，注册到默认注册表，metrics 端点
// /metrics 由 internal/infra/server 的 MetricsServer 暴露）。
var (
	// RealtimeConnections 是当前 WS 连接数（按 project 分桶）。
	RealtimeConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "torchwood_realtime_connections",
		Help: "Current realtime WebSocket connections by project.",
	}, []string{"project_id"})
	// RealtimeHandshakeTotal 是握手结果计数（result=ok|unauthenticated|exhausted）。
	RealtimeHandshakeTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "torchwood_realtime_handshake_total",
		Help: "Realtime handshake outcomes.",
	}, []string{"result"})
)

func init() {
	prometheus.MustRegister(
		RealtimeConnections,
		RealtimeHandshakeTotal,
	)
}
