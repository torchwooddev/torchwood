package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// UsageRollup 是小时用量聚合行（public.usage_rollups，v3 设计 §4.2）。
type UsageRollup struct {
	bun.BaseModel `bun:"table:usage_rollups,alias:ur"`

	ID          string    `bun:"id,pk"`
	ProjectID   string    `bun:"project_id,notnull"`
	Metric      string    `bun:"metric,notnull"`
	PeriodStart time.Time `bun:"period_start,notnull"`
	Value       int64     `bun:"value,notnull"`
	CreatedAt   time.Time `bun:"created_at,notnull"`
	UpdatedAt   time.Time `bun:"updated_at,notnull"`
}

// BillingStatement 是月账单文档行（public.billing_statements，v3 设计 §4.2）。
type BillingStatement struct {
	bun.BaseModel `bun:"table:billing_statements,alias:bs"`

	ID          string          `bun:"id,pk"`
	ProjectID   string          `bun:"project_id,notnull"`
	PeriodStart time.Time       `bun:"period_start,notnull"`
	PeriodEnd   time.Time       `bun:"period_end,notnull"`
	Status      string          `bun:"status,notnull"`
	Details     json.RawMessage `bun:"details,type:jsonb,notnull"`
	CreatedAt   time.Time       `bun:"created_at,notnull"`
	UpdatedAt   time.Time       `bun:"updated_at,notnull"`
	FinalizedAt *time.Time      `bun:"finalized_at"`
}
