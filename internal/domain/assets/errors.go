package assets

import "errors"

var (
	// ErrMatrix 是四类别属性合法性矩阵违规（建 def / Grant / Mutate）。
	ErrMatrix = errors.New("assets: class matrix violation")
	// ErrInvalidQuantity 是数量非正或类型不合法。
	ErrInvalidQuantity = errors.New("assets: invalid quantity")
	// ErrInsufficient 是余额/数量不足（整体失败，无部分扣减）。
	ErrInsufficient = errors.New("assets: insufficient quantity")
	// ErrMaxQuantity 是超过定义 max_quantity。
	ErrMaxQuantity = errors.New("assets: exceeds max_quantity")
	// ErrNotTradable 是对不可转让定义发起 Transfer。
	ErrNotTradable = errors.New("assets: not tradable")
	// ErrUniquePerOwner 是 unique_per_owner 定义已存在持有。
	ErrUniquePerOwner = errors.New("assets: unique_per_owner already held")
	// ErrExpiresAtRequired 是 entitlement Grant 未带 expires_at。
	ErrExpiresAtRequired = errors.New("assets: expires_at required")
	// ErrInvalidOwnerType 是 owner_type 非法或非一期允许值。
	ErrInvalidOwnerType = errors.New("assets: invalid owner_type")
	// ErrDefArchived 是对已归档定义发起写操作。
	ErrDefArchived = errors.New("assets: def is archived")
	// ErrDefNotFound 是定义不存在。
	ErrDefNotFound = errors.New("assets: def not found")
	// ErrHoldingNotFound 是持有行不存在。
	ErrHoldingNotFound = errors.New("assets: holding not found")
	// ErrConcurrent 是 OCC / 行锁冲突。
	ErrConcurrent = errors.New("assets: concurrently modified")
	// ErrIdempotencyRequired 是写操作未带幂等键。
	ErrIdempotencyRequired = errors.New("assets: idempotency_key is required")
	// ErrInvalidCode 是定义 code 格式非法。
	ErrInvalidCode = errors.New("assets: invalid def code")
	// ErrDuplicateCode 是 (project_id, code) 冲突。
	ErrDuplicateCode = errors.New("assets: def code already exists")
	// ErrOwnerRequired 是 owner_id / from_owner_id / to_owner_id 为空。
	ErrOwnerRequired = errors.New("assets: owner_id is required")
	// ErrHoldingIDRequired 是 holding_id 为空。
	ErrHoldingIDRequired = errors.New("assets: holding_id is required")
	// ErrSameOwner 是转让双方相同。
	ErrSameOwner = errors.New("assets: cannot transfer to the same owner")
	// ErrProjectRequired 是写路径未注入 project_id（app 应先注入）。
	ErrProjectRequired = errors.New("assets: missing project context")
)
