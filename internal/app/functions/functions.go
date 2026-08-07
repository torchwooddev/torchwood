package functions

import (
	"context"
	"fmt"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

type Functions struct {
	cfg      *config.AppConfig
	executor functions.Executor
}

func NewFunctions(cfg *config.AppConfig, executor functions.Executor) *Functions {
	return &Functions{cfg: cfg, executor: executor}
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
		registry = "Torchwood"
	}
	return fmt.Sprintf("%s/runtime-%s:latest", registry, runtime)
}
