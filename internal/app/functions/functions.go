package functions

import (
	"context"
	"fmt"
	"strings"

	appshared "github.com/torchwooddev/torchwood/internal/app/shared"
	domainbilling "github.com/torchwooddev/torchwood/internal/domain/billing"
	"github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
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
}

func NewFunctions(cfg *config.AppConfig, executor functions.Executor, repo functions.FunctionRepo, queue shared.Queue) *Functions {
	return &Functions{cfg: cfg, executor: executor, repo: repo, queue: queue}
}

// NewFunctionsWithUsage 注入用量计数器与项目目录（Wire）；测试仍用 NewFunctions。
func NewFunctionsWithUsage(cfg *config.AppConfig, executor functions.Executor, repo functions.FunctionRepo, queue shared.Queue, usage domainbilling.UsageCounter, projectRepo projects.Repository) *Functions {
	f := NewFunctions(cfg, executor, repo, queue)
	f.usage = usage
	f.projects = projectRepo
	return f
}

type ExecuteCommand struct {
	FunctionID string
	Runtime    string
	SourcePath string
	Entrypoint string
	Timeout    int64
	Env        map[string]string
	Data       string
}

func (f *Functions) Execute(ctx context.Context, cmd ExecuteCommand) (*functions.ExecutionResult, error) {
	if cmd.Timeout <= 0 {
		cmd.Timeout = 15
	}
	runtime := cmd.Runtime
	if runtime == "" {
		runtime = "node-18.0"
	}
	entrypoint := cmd.Entrypoint
	if entrypoint == "" {
		entrypoint = "index.main"
	}

	exec := functions.Execution{
		FunctionID: cmd.FunctionID,
		Runtime:    runtime,
		SourcePath: cmd.SourcePath,
		Entrypoint: entrypoint,
		Timeout:    cmd.Timeout,
		Env:        sanitizeEnv(cmd.Env),
		Data:       cmd.Data,
	}
	return f.executor.Execute(ctx, exec)
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
