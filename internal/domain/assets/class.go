// Package assets 定义 v3 统一资产系统的领域模型、端口与写路径服务（设计 §2）：
// 四类别矩阵、defs / holdings / ledger、五动词（Grant/Consume/Transfer/Mutate/Expire）。
package assets

import (
	"fmt"
	"strings"
)

// Class 是资产定义类别（锁定，设计 §2.3）。
type Class string

const (
	ClassCurrency    Class = "currency"
	ClassStack       Class = "stack"
	ClassInstance    Class = "instance"
	ClassEntitlement Class = "entitlement"
)

// IsValid 报告 class 是否为四类别之一。
func (c Class) IsValid() bool {
	switch c {
	case ClassCurrency, ClassStack, ClassInstance, ClassEntitlement:
		return true
	}
	return false
}

// OwnerType 是持有者类型（D14：一期仅 user，列保留 group/project）。
type OwnerType string

const (
	OwnerTypeUser    OwnerType = "user"
	OwnerTypeGroup   OwnerType = "group"
	OwnerTypeProject OwnerType = "project"
)

// IsValid 报告 owner_type 取值是否合法。
func (t OwnerType) IsValid() bool {
	switch t {
	case OwnerTypeUser, OwnerTypeGroup, OwnerTypeProject:
		return true
	}
	return false
}

// DefStatus 是定义状态。
type DefStatus string

const (
	DefStatusActive   DefStatus = "active"
	DefStatusArchived DefStatus = "archived"
)

func (s DefStatus) IsValid() bool {
	switch s {
	case DefStatusActive, DefStatusArchived:
		return true
	}
	return false
}

// EntryKind 是流水类型。
type EntryKind string

const (
	KindGrant       EntryKind = "grant"
	KindConsume     EntryKind = "consume"
	KindTransferOut EntryKind = "transfer_out"
	KindTransferIn  EntryKind = "transfer_in"
	KindMutate      EntryKind = "mutate"
	KindExpire      EntryKind = "expire"
	KindAdjust      EntryKind = "adjust"
)

func (k EntryKind) IsValid() bool {
	switch k {
	case KindGrant, KindConsume, KindTransferOut, KindTransferIn, KindMutate, KindExpire, KindAdjust:
		return true
	}
	return false
}

const (
	// EventDomain 是经济事件信封的 domain 字段（客户端按 domain 分流，D17）。
	EventDomain = "economy"
	// 事件目录（设计 §5.1）。
	EventGranted     = "economy.assets.granted"
	EventConsumed    = "economy.assets.consumed"
	EventTransferred = "economy.assets.transferred"
	EventMutated     = "economy.assets.mutated"
	EventExpired     = "economy.assets.expired"
)

// AccountsChannel 返回资产事件的 Realtime 频道（D17 单一 accounts.{userId}）。
func AccountsChannel(userID string) string {
	return "accounts." + userID
}

// EventNameForKind 把流水 kind 映射到 outbox 事件名。
func EventNameForKind(k EntryKind) string {
	switch k {
	case KindGrant:
		return EventGranted
	case KindConsume:
		return EventConsumed
	case KindTransferOut, KindTransferIn:
		return EventTransferred
	case KindMutate:
		return EventMutated
	case KindExpire:
		return EventExpired
	default:
		return EventGranted
	}
}

// NormalizeOwnerType 一期强制 user（D14）；空值视为 user。
func NormalizeOwnerType(t OwnerType) (OwnerType, error) {
	if t == "" {
		return OwnerTypeUser, nil
	}
	if !t.IsValid() {
		return "", fmt.Errorf("%w: %s", ErrInvalidOwnerType, t)
	}
	if t != OwnerTypeUser {
		return "", fmt.Errorf("%w: phase-1 owner_type must be user, got %s", ErrInvalidOwnerType, t)
	}
	return OwnerTypeUser, nil
}

// ValidateDefMatrix 按四类别矩阵校验定义属性（设计 §2.3）。
// 建 def / 更新 def 时调用；违规返回 ErrMatrix。
func ValidateDefMatrix(d *Def) error {
	if d == nil {
		return fmt.Errorf("%w: def is nil", ErrMatrix)
	}
	if !d.Class.IsValid() {
		return fmt.Errorf("%w: invalid class %q", ErrMatrix, d.Class)
	}
	if d.Decimals < 0 || d.Decimals > 18 {
		return fmt.Errorf("%w: decimals must be in [0, 18]", ErrMatrix)
	}
	if d.MaxQuantity != nil && *d.MaxQuantity <= 0 {
		return fmt.Errorf("%w: max_quantity must be positive when set", ErrMatrix)
	}
	if d.ExpiresIn != nil && *d.ExpiresIn <= 0 {
		return fmt.Errorf("%w: expires_in must be positive when set", ErrMatrix)
	}

	switch d.Class {
	case ClassCurrency:
		// 代币不允许有有效期（D10）；有期「代币」按 stack 建模。
		if d.ExpiresIn != nil {
			return fmt.Errorf("%w: currency must not have expires_in", ErrMatrix)
		}
		if d.Upgradeable {
			return fmt.Errorf("%w: currency cannot be upgradeable", ErrMatrix)
		}
		// unique_per_owner 天然（一币种一行），允许显式 true；false 也按一行处理。
	case ClassStack:
		if d.Upgradeable {
			return fmt.Errorf("%w: stack cannot be upgradeable (homogeneous buckets)", ErrMatrix)
		}
		if d.Decimals != 0 {
			return fmt.Errorf("%w: stack decimals must be 0", ErrMatrix)
		}
	case ClassInstance:
		if d.Decimals != 0 {
			return fmt.Errorf("%w: instance decimals must be 0", ErrMatrix)
		}
	case ClassEntitlement:
		if d.Tradable {
			return fmt.Errorf("%w: entitlement cannot be tradable", ErrMatrix)
		}
		if d.Decimals != 0 {
			return fmt.Errorf("%w: entitlement decimals must be 0", ErrMatrix)
		}
		// unique_per_owner 天然；expires_in 可选（Grant 仍必须落到绝对 expires_at）。
	}
	return nil
}

// ValidateGrant 按类别校验发放数量 / 有效期（设计 §2.3 / §2.6）。
func ValidateGrant(class Class, quantity int64, expiresAtSet bool) error {
	if quantity <= 0 {
		return fmt.Errorf("%w: quantity must be a positive integer", ErrInvalidQuantity)
	}
	switch class {
	case ClassCurrency:
		if expiresAtSet {
			return fmt.Errorf("%w: currency grant must not set expires_at", ErrMatrix)
		}
	case ClassInstance:
		if quantity != 1 {
			return fmt.Errorf("%w: instance quantity must be 1", ErrMatrix)
		}
	case ClassEntitlement:
		if quantity != 1 {
			return fmt.Errorf("%w: entitlement quantity must be 1", ErrMatrix)
		}
		if !expiresAtSet {
			return fmt.Errorf("%w: entitlement grant requires expires_at", ErrExpiresAtRequired)
		}
	case ClassStack:
		// quantity ≥ 1，expires_at 可选。
	default:
		return fmt.Errorf("%w: invalid class %q", ErrMatrix, class)
	}
	return nil
}

// ValidateConsumeQuantity 校验消耗数量（instance 可为 N 行 × 1）。
func ValidateConsumeQuantity(class Class, quantity int64) error {
	if quantity <= 0 {
		return fmt.Errorf("%w: quantity must be a positive integer", ErrInvalidQuantity)
	}
	if class == ClassInstance && quantity < 1 {
		return fmt.Errorf("%w: instance consume quantity must be >= 1", ErrMatrix)
	}
	return nil
}

// ValidateMutateClass 仅 instance / entitlement 允许 Mutate（设计 §2.5）。
func ValidateMutateClass(class Class) error {
	switch class {
	case ClassInstance, ClassEntitlement:
		return nil
	default:
		return fmt.Errorf("%w: mutate is only allowed on instance/entitlement, got %s", ErrMatrix, class)
	}
}

// NormalizeCode 规范化定义 code（小写、去空白）。
func NormalizeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}
