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

	attemptMu sync.Mutex
	attempts  map[string]int
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

	w.wg.Add(w.workers)
	for i := 0; i < w.workers; i++ {
		go func() {
			defer w.wg.Done()
			w.consume(runCtx)
		}()
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
			if w.requeue(payload) {
				if qerr := w.queue.Enqueue(ctx, domainshared.QueueFunctionsExecutions, payload); qerr != nil {
					w.logger.Error("re-enqueue failed", "error", qerr)
				}
			} else {
				w.failPayload(ctx, payload, "worker retries exhausted")
			}
		}
	}
}

// requeue 记录 payload 的重试次数（进程内存，best-effort）；超过上限返回 false。
// ⚠️ 已知限制（R07-P3-8，本轮选择不做持久化）：重试计数仅存于进程内存，
// worker 重启会清零，瞬时失败任务可能被重试超过 maxProcessAttempts 次；
// 因队列消息被重新入队，重启后计数丢失无法从 ExecutionRecord 恢复
// （schema 无重试计数字段）。超限兜底 MarkExecutionFailed 仍保证任务最终失败
// 标记，不会无限重试（每次重启仅多出 ≤ maxProcessAttempts 次）。
// 未来可改为：ExecutionRecord 增加 retry_count 列 + 每失败原子自增，
// 或队列 payload 内嵌 attempt 计数。
func (w *Worker) requeue(payload []byte) bool {
	key := string(payload)
	w.attemptMu.Lock()
	defer w.attemptMu.Unlock()
	if w.attempts == nil {
		w.attempts = make(map[string]int)
	}
	w.attempts[key]++
	if w.attempts[key] > maxProcessAttempts {
		delete(w.attempts, key)
		return false
	}
	return true
}

// retryMessage 是 worker 侧解析队列 payload 的最小字段集合（用于兜底标记 failed）。
type retryMessage struct {
	ExecutionID string `json:"execution_id"`
	FunctionID  string `json:"function_id"`
	ProjectID   string `json:"project_id"`
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
