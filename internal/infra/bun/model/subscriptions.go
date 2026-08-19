package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// SubscriptionPlan 是订阅计划行（public.subscription_plans，v3 设计 §3.1）。
type SubscriptionPlan struct {
	bun.BaseModel `bun:"table:subscription_plans,alias:sp"`

	ID                 string          `bun:"id,pk"`
	ProjectID          string          `bun:"project_id,notnull"`
	Code               string          `bun:"code,notnull"`
	Name               string          `bun:"name,notnull"`
	Amount             int64           `bun:"amount,notnull"`
	Currency           string          `bun:"currency,notnull"`
	Interval           string          `bun:"interval,notnull"`
	IntervalDays       int64           `bun:"interval_days,notnull"`
	GraceDays          int32           `bun:"grace_days,notnull"`
	TrialDays          int32           `bun:"trial_days,notnull"`
	Benefits           json.RawMessage `bun:"benefits,type:jsonb,notnull"`
	ProviderOverrides  json.RawMessage `bun:"provider_overrides,type:jsonb"`
	Status             string          `bun:"status,notnull"`
	CreatedAt          time.Time       `bun:"created_at,notnull"`
	UpdatedAt          time.Time       `bun:"updated_at,notnull"`
}

// Subscription 是订阅合同行（public.subscriptions，v3 设计 §3.1）。
type Subscription struct {
	bun.BaseModel `bun:"table:subscriptions,alias:ss"`

	ID                 string          `bun:"id,pk"`
	ProjectID          string          `bun:"project_id,notnull"`
	UserID             string          `bun:"user_id,notnull"`
	PlanID             string          `bun:"plan_id,notnull"`
	Mode               string          `bun:"mode,notnull"`
	Provider           *string         `bun:"provider"`
	ProviderSubID      *string         `bun:"provider_sub_id"`
	Status             string          `bun:"status,notnull"`
	CurrentPeriodStart time.Time       `bun:"current_period_start,notnull"`
	CurrentPeriodEnd   time.Time       `bun:"current_period_end,notnull"`
	CancelAtPeriodEnd  bool            `bun:"cancel_at_period_end,notnull"`
	GraceUntil         *time.Time      `bun:"grace_until"`
	BillingAssetCode   *string         `bun:"billing_asset_code"`
	Benefits           json.RawMessage `bun:"benefits,type:jsonb,notnull"`
	IdempotencyKey     string          `bun:"idempotency_key,notnull"`
	CreatedAt          time.Time       `bun:"created_at,notnull"`
	UpdatedAt          time.Time       `bun:"updated_at,notnull"`
}
