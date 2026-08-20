package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lynx-go/lynx"
	appfunctions "github.com/torchwooddev/torchwood/internal/app/functions"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
)

// workerConcurrency 是单进程并发消费 goroutine 数（BRPOP 多消费者互斥由
// Redis 保证，任务间无顺序依赖）。
const workerConcurrency = 4

// dequeuePollInterval 是 BRPOP 轮询超时（配合优雅退出，取消后 1s 内返回）。
const dequeuePollInterval = time.Second

// maxProcessAttempts 是消费瞬时失败的最大重试次数（超限后兜底标 failed）。
const maxProcessAttempts = 3

// Worker 消费函数异步执行队列（torchwood:queue:functions-executions）。
type Worker struct {
	functions *appfunctions.Functions
	queue     domainshared.Queue
	logger    *slog.Logger
	workers   int

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWorker creates the functions execution queue consumer.
func NewWorker(functions *appfunctions.Functions, queue domainshared.Queue, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		functions: functions,
		queue:     queue,
		logger:    logger,
		workers:   workerConcurrency,
	}
}

func (w *Worker) Name() string { return "functions-worker" }

func (w *Worker) Init(ctx lynx.AppContext) error {
	return nil
}

func (w *Worker) Start(ctx context.Context) error {
	// 启动对账：停留 queued/building/running 超过 1h 的记录标记 failed
	// （兜底 Redis 重启丢任务、worker 崩溃孤儿）。
	recovered, err := w.functions.RecoverOrphanExecutions(ctx, time.Hour)
	if err != nil {
		return fmt.Errorf("recover orphan executions: %w", err)
	}
	if recovered > 0 {
		w.logger.Info("recovered orphan executions", "count", recovered)
	}

	runCtx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.cancel = cancel
	w.mu.Unlock()

	for i := 0; i < w.workers; i++ {
		w.wg.Go(func() {
			w.consume(runCtx)
		})
	}
	w.logger.Info("worker started", "workers", w.workers)

	// Start 必须阻塞到应用上下文取消（lynx 把 Start 作为 run.Group actor，
	// 立即返回会被判定为服务完成而触发关停）。
	<-ctx.Done()
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	w.cancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	w.logger.Info("worker stopped")
	return nil
}

func (w *Worker) consume(ctx context.Context) {
	for {
		payload, err := w.queue.Dequeue(ctx, domainshared.QueueFunctionsExecutions, dequeuePollInterval)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("dequeue failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if payload == nil {
			// BRPOP 超时（队列为空），继续轮询。
			continue
		}
		if err := w.functions.ProcessExecutionPayload(ctx, payload); err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, appfunctions.ErrInvalidQueuePayload) {
				// 坏消息永久失败：不重试，仅记日志。
				w.logger.Error("discarding invalid queue payload", "error", err)
				continue
			}
			w.logger.Error("process execution failed", "error", err)
			// 瞬时失败重抛回队（LPUSH），最多 maxProcessAttempts 次；超限兜底标 failed。
			if next, ok := requeue(payload); ok {
				if qerr := w.queue.Enqueue(ctx, domainshared.QueueFunctionsExecutions, next); qerr != nil {
					w.logger.Error("re-enqueue failed", "error", qerr)
				}
			} else {
				w.failPayload(ctx, payload, "worker retries exhausted")
			}
		}
	}
}

// requeue 将瞬时失败的任务重抛回队：解析 payload 内嵌 attempt 计数并 +1，
// 重新 marshal 后由调用方 Enqueue。队列消息是重试计数的唯一事实来源，
// worker 重启/多副本不会清零或重复计数（B2/R07-P3-8，替代旧的进程内存 map）。
// 超限（attempt > maxProcessAttempts）返回 ok=false，由调用方走 failPayload
// 兜底标记 failed；旧消息无 attempt 字段（json.Unmarshal 视为 0）时首次重试
// 即为 attempt=1。
func requeue(payload []byte) (next []byte, ok bool) {
	var m retryMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		// 防御分支：consume 仅对 ProcessExecutionPayload 解析成功的 payload
		// 调用本函数，正常不可达；视为超限避免坏消息无限重试。
		return nil, false
	}
	m.Attempt++
	if m.Attempt > maxProcessAttempts {
		return nil, false
	}
	next, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return next, true
}

// retryMessage 是 worker 侧解析队列 payload 的完整字段集合（与 functions 包
// queueMessage 的 json 字段名一致，JSON 往返无损：execution_id/function_id/
// project_id/data/attempt；用于 requeue 重抛与 failPayload 兜底标记 failed）。
type retryMessage struct {
	ExecutionID string `json:"execution_id"`
	FunctionID  string `json:"function_id"`
	ProjectID   string `json:"project_id"`
	Data        string `json:"data,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
}

// failPayload 解析 payload 并兜底标记执行失败（best-effort）。
func (w *Worker) failPayload(ctx context.Context, payload []byte, reason string) {
	var m retryMessage
	if err := json.Unmarshal(payload, &m); err != nil || m.ExecutionID == "" {
		w.logger.Error("cannot parse payload to mark failed", "error", err, "payload", string(payload))
		return
	}
	if err := w.functions.MarkExecutionFailed(ctx, m.ProjectID, m.FunctionID, m.ExecutionID, reason); err != nil {
		w.logger.Error("mark execution failed", "execution_id", m.ExecutionID, "error", err)
	}
}
