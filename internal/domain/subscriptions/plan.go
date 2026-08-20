package subscriptions

import (
	"encoding/json"
	"fmt"
	"time"
)

// Plan 是订阅计划行（public.subscription_plans）。
type Plan struct {
	ID                string
	ProjectID         string
	Code              string
	Name              string
	Amount            int64 // 最小货币单位，>=0（bigint，禁止 float）
	Currency          string
	Interval          Interval
	IntervalDays      int64
	GraceDays         int32
	TrialDays         int32
	Benefits          Benefits
	ProviderOverrides ProviderOverrides
	Status            PlanStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Benefits 是资产发放清单快照（设计 §3.2）：激活/续期成功时调资产系统发放。
// 数量一律 int64 最小单位。
type Benefits struct {
	Grants       []BenefitGrant       `json:"grants"`
	Entitlements []BenefitEntitlement `json:"entitlements"`
}

// BenefitGrant 是一次性发放（currency/stack/instance Grant）。
type BenefitGrant struct {
	AssetCode string `json:"asset_code"`
	Quantity  int64  `json:"quantity"`
	ExpiresIn *int64 `json:"expires_in,omitempty"` // 秒；缺省=定义默认 / 永不过期
}

// BenefitEntitlement 是权益类发放：首次 Grant，续期 Mutate 延长 expires_at。
type BenefitEntitlement struct {
	AssetCode string `json:"asset_code"`
	Tier      int32  `json:"tier"`
}

// ProviderOverrides 是渠道侧映射（hosted Stripe price_id）。
type ProviderOverrides struct {
	StripePriceID string `json:"stripe_price_id,omitempty"`
}

// Validate 校验计划字段（金额/数量 bigint、interval 合法）。
func (p *Plan) Validate() error {
	if p == nil {
		return fmt.Errorf("subscriptions: nil plan")
	}
	if p.Amount < 0 {
		return fmt.Errorf("subscriptions: amount must be >= 0")
	}
	if len(p.Currency) != 3 {
		return fmt.Errorf("subscriptions: currency must be a 3-letter ISO-4217 code")
	}
	if !p.Interval.IsValid() {
		return fmt.Errorf("subscriptions: invalid interval %q", p.Interval)
	}
	if p.Interval == IntervalCustomDays && p.IntervalDays <= 0 {
		return fmt.Errorf("subscriptions: custom_days requires interval_days > 0")
	}
	if p.GraceDays < 0 {
		return fmt.Errorf("subscriptions: grace_days must be >= 0")
	}
	if p.TrialDays < 0 {
		return fmt.Errorf("subscriptions: trial_days must be >= 0")
	}
	return p.Benefits.Validate()
}

// Validate 校验 benefits：asset_code 非空、quantity > 0、无负 expires_in。
func (b Benefits) Validate() error {
	for i, g := range b.Grants {
		if g.AssetCode == "" {
			return fmt.Errorf("subscriptions: grants[%d].asset_code is required", i)
		}
		if g.Quantity <= 0 {
			return fmt.Errorf("subscriptions: grants[%d].quantity must be a positive integer", i)
		}
		if g.ExpiresIn != nil && *g.ExpiresIn <= 0 {
			return fmt.Errorf("subscriptions: grants[%d].expires_in must be > 0 when set", i)
		}
	}
	for i, e := range b.Entitlements {
		if e.AssetCode == "" {
			return fmt.Errorf("subscriptions: entitlements[%d].asset_code is required", i)
		}
	}
	return nil
}

// MarshalBenefits 序列化 benefits JSONB（空切片写成 [] 而非 null）。
func MarshalBenefits(b Benefits) (json.RawMessage, error) {
	if b.Grants == nil {
		b.Grants = []BenefitGrant{}
	}
	if b.Entitlements == nil {
		b.Entitlements = []BenefitEntitlement{}
	}
	return json.Marshal(b)
}

// UnmarshalBenefits 反序列化 benefits JSONB。
func UnmarshalBenefits(raw json.RawMessage) (Benefits, error) {
	var b Benefits
	if len(raw) == 0 || string(raw) == "null" {
		return Benefits{Grants: []BenefitGrant{}, Entitlements: []BenefitEntitlement{}}, nil
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return Benefits{}, fmt.Errorf("subscriptions: decode benefits: %w", err)
	}
	if b.Grants == nil {
		b.Grants = []BenefitGrant{}
	}
	if b.Entitlements == nil {
		b.Entitlements = []BenefitEntitlement{}
	}
	return b, nil
}

// MarshalOverrides 序列化 provider_overrides。
func MarshalOverrides(o ProviderOverrides) (json.RawMessage, error) {
	if o.StripePriceID == "" {
		return nil, nil
	}
	return json.Marshal(o)
}

// UnmarshalOverrides 反序列化 provider_overrides。
func UnmarshalOverrides(raw json.RawMessage) (ProviderOverrides, error) {
	var o ProviderOverrides
	if len(raw) == 0 || string(raw) == "null" {
		return o, nil
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return o, fmt.Errorf("subscriptions: decode provider_overrides: %w", err)
	}
	return o, nil
}
