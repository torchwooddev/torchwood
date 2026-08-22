package functions

import (
	"fmt"
	"strings"

	appshared "github.com/torchwooddev/torchwood/internal/app/shared"
	domainbilling "github.com/torchwooddev/torchwood/internal/domain/billing"
	"github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/semaphore"
)

// Functions 是 Functions 服务的 use-case 聚合。
type Functions struct {
	cfg        *config.AppConfig
	executor   functions.Executor
	repo       functions.FunctionRepo
	queue      shared.Queue
	usage      domainbilling.UsageCounter // 可选：函数执行时长计量
	projects   projects.Repository        // 可选：启动对账枚举项目（nil 则 Recover 空操作）
	scanCursor appshared.ProjectRotation  // RecoverOrphanExecutions 轮转游标（串行）
	buildSem   semaphore.Semaphore
	runSem     semaphore.Semaphore
}

func NewFunctions(cfg *config.AppConfig, executor functions.Executor, repo functions.FunctionRepo, queue shared.Queue) *Functions {
	f := &Functions{cfg: cfg, executor: executor, repo: repo, queue: queue}
	f.buildSem = semaphore.NewInMemory(4)
	f.runSem = semaphore.NewInMemory(16)
	return f
}

// NewFunctionsWithUsage 注入用量计数器与项目目录（Wire）；测试仍用 NewFunctions。
func NewFunctionsWithUsage(cfg *config.AppConfig, executor functions.Executor, repo functions.FunctionRepo, queue shared.Queue, usage domainbilling.UsageCounter, projectRepo projects.Repository, sems Semaphores) *Functions {
	f := NewFunctions(cfg, executor, repo, queue)
	f.usage = usage
	f.projects = projectRepo
	if sems.Build != nil {
		f.buildSem = sems.Build
	}
	if sems.Run != nil {
		f.runSem = sems.Run
	}
	return f
}

// WithSemaphores 注入分布式信号量（Wire 覆盖默认内存信号量）。
// buildSem 限制并发构建（默认 4），runSem 限制并发执行（默认 16）。
func (f *Functions) WithSemaphores(buildSem, runSem semaphore.Semaphore) *Functions {
	if buildSem != nil {
		f.buildSem = buildSem
	}
	if runSem != nil {
		f.runSem = runSem
	}
	return f
}

func (f *Functions) getBuildSemaphore() semaphore.Semaphore {
	if f.buildSem != nil {
		return f.buildSem
	}
	return semaphore.NewInMemory(4)
}

func (f *Functions) getRunSemaphore() semaphore.Semaphore {
	if f.runSem != nil {
		return f.runSem
	}
	return semaphore.NewInMemory(16)
}

func sanitizeEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if strings.ContainsAny(k, "\n\r\x00") {
			continue
		}
		out[k] = v
	}
	return out
}

func (f *Functions) RuntimeImage(runtime string) string {
	registry := f.cfg.GetFunctions().GetDocker().GetRegistry()
	if registry == "" {
		registry = "torchwood-funcs"
	}
	return fmt.Sprintf("%s/runtime-%s:latest", registry, runtime)
}
