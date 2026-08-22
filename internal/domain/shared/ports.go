package shared

import (
	"context"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/events"
)

// QueueFunctionsExecutions 是函数异步执行的队列名（Redis Stream，至少一次）。
const QueueFunctionsExecutions = "torchwood:queue:functions-executions"

// Queue 是异步任务队列端口（A7 修复：至少一次）。
// Dequeue 返回的 ack Token 需在处理成功后 Ack，否则消息在 PEL/inflight 超时后重投。
type Queue interface {
	Enqueue(ctx context.Context, queue string, payload []byte) error
	// Dequeue 阻塞等待任务；timeout<=0 时阻塞直到有任务或 ctx 取消。
	// 无任务时 payload==nil 且 ack=="".
	Dequeue(ctx context.Context, queue string, timeout time.Duration) (payload []byte, ack string, err error)
	Ack(ctx context.Context, queue string, ack string) error
}

// EventPublisher 是用户集合文档写事件的 transactional outbox 端口
// （v2 设计 §3.2）。调用方应在 uow.Run 内 Publish，与业务写同一工作单元；
// 实现可从 ctx 读取连接。未在工作单元内则自行短事务插入。
type EventPublisher interface {
	Publish(ctx context.Context, ev events.Envelope) error
}

// RealtimeTransport 是 outbox → server Hub 的最后一跳（至少一次，
// v2 设计 §3.4）。P2 实现：Redis Streams + consumer group；日后可换
// MessageLoop，频道与信封不变。worker 进程只负责 Enqueue（XADD），
// 不持有 WebSocket。
type RealtimeTransport interface {
	Enqueue(ctx context.Context, ev events.Envelope) error
}

// RealtimeFanout 仅存在于 cmd/server 进程：从 transport 拉消息并写入
// Hub（XGROUP/XAUTOCLAIM/XREADGROUP → Hub.Dispatch → XACK → published_at）。
type RealtimeFanout interface {
	Run(ctx context.Context) error
}

// RealtimeConn 是 Hub 侧的连接句柄。Send 只承载出站帧
// （{"type":"event","channel":...,"payload":ClientPayload()}，无 acl）。
type RealtimeConn struct {
	ID            string
	PlatformAdmin bool
	DocPrincipal  databases.Principal
	Send          chan map[string]any
}

// RealtimeHub 是 server 进程内订阅表端口（v2 设计 §4.5）：WS handler
// 经它注册/摘除订阅，RealtimeFanout 实现侧按频道 + ACL 快照扇出。
// 只存在于 cmd/server 进程。
type RealtimeHub interface {
	// Subscribe 把 conn 注册到频道（幂等：同一连接重复订阅不重复计数）。
	Subscribe(channel string, conn *RealtimeConn)
	// Unsubscribe 把 conn 从单个频道移除。
	Unsubscribe(channel, connID string)
	// Remove 把 conn 从全部频道移除（连接关闭时调用）。
	Remove(connID string)
}
