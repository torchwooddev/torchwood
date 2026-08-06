package functions

import (
	"context"
	"fmt"

	"github.com/torchwoodio/torchwood/internal/domain/functions"
	"github.com/torchwoodio/torchwood/internal/pkg/config"
)

// dockerExecutor is a Docker-based functions executor (P0 stub).
// It validates the request and returns a synthetic success response.
type dockerExecutor struct {
	cfg *config.AppConfig
}

// NewDockerExecutor creates a new Docker executor.
func NewDockerExecutor(cfg *config.AppConfig) functions.Executor {
	return &dockerExecutor{cfg: cfg}
}

func (d *dockerExecutor) Execute(ctx context.Context, exec functions.Execution) (*functions.ExecutionResult, error) {
	if exec.Runtime == "" {
		return nil, fmt.Errorf("runtime is required")
	}
	if exec.Entrypoint == "" {
		return nil, fmt.Errorf("entrypoint is required")
	}

	// P0 stub: in a real implementation this would use the Docker SDK
	// to pull the runtime image, create a container, mount the source,
	// inject env vars, run the entrypoint, and stream logs back.
	return &functions.ExecutionResult{
		StatusCode: 200,
		Stdout:     fmt.Sprintf("P0 stub executed function %s with runtime %s and entrypoint %s", exec.FunctionID, exec.Runtime, exec.Entrypoint),
		Stderr:     "",
		Response:   `{"status":"ok"}`,
		DurationMS: 0,
	}, nil
}
