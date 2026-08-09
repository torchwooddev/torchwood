package model

import (
	"time"

	"github.com/uptrace/bun"
)

type Function struct {
	bun.BaseModel `bun:"table:functions,alias:f"`

	ID             string    `bun:"id,pk"`
	ProjectID      string    `bun:"project_id,notnull"`
	Name           string    `bun:"name,notnull"`
	Runtime        string    `bun:"runtime,notnull"`
	Entrypoint     string    `bun:"entrypoint,notnull,default:'index.main'"`
	TimeoutSeconds int       `bun:"timeout_seconds,notnull,default:15"`
	Spec           string    `bun:"spec,notnull,default:'shared-1x'"`
	Enabled        bool      `bun:"enabled,notnull,default:true"`
	CreatedAt      time.Time `bun:"created_at,notnull"`
	UpdatedAt      time.Time `bun:"updated_at,notnull"`
}

type FunctionDeployment struct {
	bun.BaseModel `bun:"table:function_deployments,alias:fd"`

	ID         string    `bun:"id,pk"`
	FunctionID string    `bun:"function_id,notnull"`
	ProjectID  string    `bun:"project_id,notnull"`
	Size       int64     `bun:"size,notnull,default:0"`
	Status     string    `bun:"status,notnull,default:'pending'"`
	Error      string    `bun:"error,notnull,default:''"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
	UpdatedAt  time.Time `bun:"updated_at,notnull"`
}

type FunctionVariable struct {
	bun.BaseModel `bun:"table:function_variables,alias:fv"`

	ID         string `bun:"id,pk"`
	FunctionID string `bun:"function_id,notnull"`
	ProjectID  string `bun:"project_id,notnull"`
	Key        string `bun:"key,notnull"`
	Value      string `bun:"value,notnull"`
}

type FunctionExecution struct {
	bun.BaseModel `bun:"table:function_executions,alias:fe"`

	ID                string    `bun:"id,pk"`
	FunctionID        string    `bun:"function_id,notnull"`
	ProjectID         string    `bun:"project_id,notnull"`
	DeploymentID      string    `bun:"deployment_id,notnull"`
	Status            string    `bun:"status,notnull,default:'queued'"`
	Response          string    `bun:"response,notnull,default:''"`
	ResponseTruncated bool      `bun:"response_truncated,notnull,default:false"`
	Stdout            string    `bun:"stdout,notnull,default:''"`
	StdoutTruncated   bool      `bun:"stdout_truncated,notnull,default:false"`
	Stderr            string    `bun:"stderr,notnull,default:''"`
	StderrTruncated   bool      `bun:"stderr_truncated,notnull,default:false"`
	StatusCode        int       `bun:"status_code,notnull,default:0"`
	DurationMS        int64     `bun:"duration_ms,notnull,default:0"`
	Error             string    `bun:"error,notnull,default:''"`
	CreatedAt         time.Time `bun:"created_at,notnull"`
	UpdatedAt         time.Time `bun:"updated_at,notnull"`
}
