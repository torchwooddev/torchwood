package realtime

// Realtime 配额常量（v2 设计 §4.4；P2 不修改 config.proto，避免无关的
// generate-config 噪音）。
const (
	// MaxConnectionsPerUser 是每个用户（user:{UserID} / admin:{ActorID}）
	// 的最大并发连接数。超限拒绝新连。
	MaxConnectionsPerUser = 4
	// MaxSubscriptionsPerConn 是每个连接的最大订阅数。超限返回
	// RESOURCE_EXHAUSTED，连接保持。
	MaxSubscriptionsPerConn = 32
)
