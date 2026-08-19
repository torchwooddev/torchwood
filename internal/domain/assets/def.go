package assets

import (
	"encoding/json"
	"time"
)

// Def 是资产定义（public.asset_defs 行，设计 §2.4）。
type Def struct {
	ID             string
	ProjectID      string
	Code           string
	Name           string
	Class          Class
	Decimals       int32 // 仅 currency 展示用；数量仍为 bigint
	MaxQuantity    *int64
	ExpiresIn      *int64 // 默认 TTL 秒
	Tradable       bool
	UniquePerOwner bool
	Upgradeable    bool
	Metadata       json.RawMessage
	Status         DefStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NaturalUniquePerOwner 报告该类是否天然 unique_per_owner（currency / entitlement）。
func (d *Def) NaturalUniquePerOwner() bool {
	if d == nil {
		return false
	}
	switch d.Class {
	case ClassCurrency, ClassEntitlement:
		return true
	}
	return d.UniquePerOwner
}

// AllowsExpiry 报告该类持有是否允许 expires_at。
func (d *Def) AllowsExpiry() bool {
	if d == nil {
		return false
	}
	return d.Class != ClassCurrency
}

// RequiresExpiry 报告 Grant 是否必须落到绝对 expires_at（entitlement）。
func (d *Def) RequiresExpiry() bool {
	return d != nil && d.Class == ClassEntitlement
}

// InstanceBucket 报告该类是否每行一个个体（instance 不并桶）。
func (d *Def) InstanceBucket() bool {
	return d != nil && d.Class == ClassInstance
}
