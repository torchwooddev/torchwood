package functions

import "context"

// Execution describes a single function invocation.
type Execution struct {
	FunctionID   string
	DeploymentID string // 构建产物镜像标识（{registry}/func-{functionID}-{deploymentID}）
	Runtime      string // e.g. node-18.0, python-3.11
	SourcePath   string // path or archive location of function source
	Entrypoint   string // e.g. "index.main"
	Spec         string // 资源规格（shared-1x / shared-2x）
	Timeout      int64  // seconds
	Env          map[string]string
	Data         string // JSON payload
}

// ExecutionResult is the output of a function invocation.
type ExecutionResult struct {
	StatusCode int
	Stdout     string
	Stderr     string
	Response   string
	DurationMS int64
}

// Executor is the function runtime port.
type Executor interface {
	// Build 将 zip 代码包构建为镜像（解压校验 → 生成 Dockerfile → docker build）。
	Build(ctx context.Context, functionID, deploymentID, zipPath string) error
	Execute(ctx context.Context, exec Execution) (*ExecutionResult, error)
	// RemoveImage 删除构建产物镜像（幂等，失败由调用方记日志）。
	RemoveImage(ctx context.Context, functionID, deploymentID string) error
}
