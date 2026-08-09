package main

import (
	"context"
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
func NewWorker(functions *appfunctions.Functions, queue domainshared.Queue) *Worker {
	return &Worker{
		functions: functions,
		queue:     queue,
		logger:    slog.Default(),
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
			w.logger.Error("process execution failed", "error", err)
		}
	}
}
