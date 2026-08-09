package shared

import (
	"context"
	"time"
)

// QueueFunctionsExecutions 是函数异步执行的队列名（Redis List）。
const QueueFunctionsExecutions = "torchwood:queue:functions-executions"

// Queue 是异步任务队列端口（MVP：Redis List BRPOP 实现）。
type Queue interface {
	Enqueue(ctx context.Context, queue string, payload []byte) error
	// Dequeue 阻塞等待任务；timeout<=0 时阻塞直到有任务或 ctx 取消。
	Dequeue(ctx context.Context, queue string, timeout time.Duration) ([]byte, error)
}
