package shared

import (
	"context"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/events"
)

// QueueFunctionsExecutions 是函数异步执行的队列名（Redis List）。
const QueueFunctionsExecutions = "torchwood:queue:functions-executions"

// Queue 是异步任务队列端口（MVP：Redis List BRPOP 实现）。
type Queue interface {
	Enqueue(ctx context.Context, queue string, payload []byte) error
	// Dequeue 阻塞等待任务；timeout<=0 时阻塞直到有任务或 ctx 取消。
	Dequeue(ctx context.Context, queue string, timeout time.Duration) ([]byte, error)
}

// EventPublisher 是用户集合文档写事件的 transactional outbox 端口
// （v2 设计 §3.2）。Publish 必须感知 ctx 中的 bun.Tx（clients.Conn）：
// 写路径在同一事务内调用，事件与文档行同 COMMIT；未在事务中则自行
// 短事务插入。若 ctx 带 events.TransactionID，写入信封 transaction_id。
type EventPublisher interface {
	Publish(ctx context.Context, ev events.Envelope) error
}
