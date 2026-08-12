package functions

import (
	"context"
	"time"
)

// FunctionRepo 持久化函数/部署/变量/执行记录（bun 静态表适配）。
type FunctionRepo interface {
	CreateFunction(ctx context.Context, fn *Function) error
	GetFunction(ctx context.Context, projectID, functionID string) (*Function, error)
	ListFunctions(ctx context.Context, projectID string) ([]Function, error)
	UpdateFunction(ctx context.Context, fn *Function) error
	DeleteFunction(ctx context.Context, projectID, functionID string) error

	CreateDeployment(ctx context.Context, d *Deployment) error
	GetDeployment(ctx context.Context, projectID, functionID, deploymentID string) (*Deployment, error)
	ListDeployments(ctx context.Context, projectID, functionID string) ([]Deployment, error)
	UpdateDeployment(ctx context.Context, d *Deployment) error
	DeleteDeployment(ctx context.Context, projectID, functionID, deploymentID string) error

	SetVariables(ctx context.Context, projectID, functionID string, vars map[string]string) error
	GetVariables(ctx context.Context, projectID, functionID string) (map[string]string, error)

	CreateExecution(ctx context.Context, e *ExecutionRecord) error
	GetExecution(ctx context.Context, projectID, functionID, executionID string) (*ExecutionRecord, error)
	ListExecutions(ctx context.Context, projectID, functionID string, limit int) ([]ExecutionRecord, error)
	UpdateExecution(ctx context.Context, e *ExecutionRecord) error
	// RecoverOrphanExecutions 将停留 building/running 超过 staleAfter 的记录
	// 标记为 failed（worker 启动对账；queued 任务仍在队列中，不应标记）。
	RecoverOrphanExecutions(ctx context.Context, staleAfter time.Duration) (int64, error)
	// PruneOldExecutions 清理超过 keepRecent 条的最新之外记录（保留策略）。
	PruneOldExecutions(ctx context.Context, functionID string, keepRecent int) error
}

// Function 状态常量。
const (
	DeploymentStatusPending  = "pending"
	DeploymentStatusBuilding = "building"
	DeploymentStatusReady    = "ready"
	DeploymentStatusFailed   = "failed"

	ExecutionStatusQueued    = "queued"
	ExecutionStatusBuilding  = "building"
	ExecutionStatusRunning   = "running"
	ExecutionStatusCompleted = "completed"
	ExecutionStatusFailed    = "failed"
)

type Function struct {
	ID             string
	ProjectID      string
	Name           string
	Runtime        string
	Entrypoint     string
	TimeoutSeconds int
	Spec           string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Deployment struct {
	ID         string
	FunctionID string
	ProjectID  string
	Size       int64
	Status     string // pending/building/ready/failed
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Variable struct {
	ID         string
	FunctionID string
	Key        string
	Value      string
}

// ExecutionRecord 是执行记录实体；命名区别于 executor 入参 Execution。
type ExecutionRecord struct {
	ID                string
	FunctionID        string
	ProjectID         string
	DeploymentID      string
	Status            string
	Response          string
	ResponseTruncated bool
	Stdout            string
	StdoutTruncated   bool
	Stderr            string
	StderrTruncated   bool
	StatusCode        int
	DurationMS        int64
	Error             string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
