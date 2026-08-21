package assets

import (
	"encoding/json"
	"time"
)

// Scope 是写路径的项目与操作者快照。鉴权由 app 完成后再注入；领域不读 principal。
type Scope struct {
	ProjectID string
	Operator  json.RawMessage
}

// OpResult 是五动词的统一返回：流水（可能多行）+ 是否幂等重放。
type OpResult struct {
	Entries          []LedgerEntry
	IdempotentReplay bool
}

// GrantCommand 是发放入参。
type GrantCommand struct {
	OwnerType      OwnerType
	OwnerID        string
	DefCode        string
	Quantity       int64
	ExpiresAt      *time.Time
	Level          int32
	Metadata       json.RawMessage
	IdempotencyKey string
	RefType        string
	RefID          string
}

// ConsumeCommand 是消耗入参（FEFO）。
type ConsumeCommand struct {
	OwnerType      OwnerType
	OwnerID        string
	DefCode        string
	Quantity       int64
	IdempotencyKey string
	RefType        string
	RefID          string
}

// TransferCommand 是转让入参（原子：transfer_out + transfer_in 共享 ref_id）。
type TransferCommand struct {
	FromOwnerID    string
	ToOwnerID      string
	DefCode        string
	Quantity       int64
	IdempotencyKey string
	RefType        string
	RefID          string
}

// MutateCommand 是实例/权益属性变更入参。
type MutateCommand struct {
	HoldingID      string
	Level          *int32
	ExpiresAt      *time.Time
	Metadata       json.RawMessage
	IdempotencyKey string
	RefType        string
	RefID          string
}

// ExpireCommand 是单行过期/强制失效入参。
type ExpireCommand struct {
	HoldingID      string
	IdempotencyKey string
}
