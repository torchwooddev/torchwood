package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// PaymentOrder 是支付订单行（public.payment_orders，v3 设计 §1.3）。
// status：created → paying → paid | failed | closed → refunding → refunded。
type PaymentOrder struct {
	bun.BaseModel `bun:"table:payment_orders,alias:po"`

	ID                string          `bun:"id,pk"`
	ProjectID         string          `bun:"project_id,notnull"`
	UserID            string          `bun:"user_id,notnull"`
	Provider          string          `bun:"provider,notnull"`
	IdempotencyKey    string          `bun:"idempotency_key,notnull"`
	ProviderSessionID *string         `bun:"provider_session_id"`
	ProviderOrderID   *string         `bun:"provider_order_id"`
	Amount            int64           `bun:"amount,notnull"`
	Currency          string          `bun:"currency,notnull"`
	PurposeKind       string          `bun:"purpose_kind,notnull"`
	Purpose           json.RawMessage `bun:"purpose,type:jsonb,notnull"`
	Status            string          `bun:"status,notnull"`
	CreatedAt         time.Time       `bun:"created_at,notnull"`
	UpdatedAt         time.Time       `bun:"updated_at,notnull"`
	PaidAt            *time.Time      `bun:"paid_at"`
	ExpiresAt         time.Time       `bun:"expires_at,notnull"`
}

// PaymentCallbackEvent 是渠道回调事件登记行（幂等锚点二：
// (provider, provider_event_id) 唯一，v3 设计 §1.3）。
type PaymentCallbackEvent struct {
	bun.BaseModel `bun:"table:payment_callback_events,alias:pce"`

	ID              string          `bun:"id,pk"`
	ProjectID       *string         `bun:"project_id"`
	Provider        string          `bun:"provider,notnull"`
	ProviderEventID string          `bun:"provider_event_id,notnull"`
	EventType       string          `bun:"event_type,notnull"`
	OrderID         *string         `bun:"order_id"`
	Payload         json.RawMessage `bun:"payload,type:jsonb,notnull"`
	CreatedAt       time.Time       `bun:"created_at,notnull"`
}

// PaymentFulfillment 是订单 paid 同事务落下的履约记录
// （public.payment_fulfillments，v3 设计 §1.5）。
type PaymentFulfillment struct {
	bun.BaseModel `bun:"table:payment_fulfillments,alias:pf"`

	ID          string         `bun:"id,pk"`
	OrderID     string         `bun:"order_id,notnull"`
	ProjectID   string         `bun:"project_id,notnull"`
	PurposeKind string         `bun:"purpose_kind,notnull"`
	Ref         string         `bun:"ref,notnull"`
	Status      string         `bun:"status,notnull"`
	Detail      map[string]any `bun:"detail,type:jsonb"`
	CreatedAt   time.Time      `bun:"created_at,notnull"`
	UpdatedAt   time.Time      `bun:"updated_at,notnull"`
}
