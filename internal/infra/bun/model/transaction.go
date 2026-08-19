package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// DocumentTransaction 是单库事务的元数据行（public 元数据库，v2 设计 §5.1）。
// status：pending | committed | rolled_back | expired。
type DocumentTransaction struct {
	bun.BaseModel `bun:"table:document_transactions,alias:dt"`

	ID         string    `bun:"id,pk"`
	ProjectID  string    `bun:"project_id,notnull"`
	DatabaseID string    `bun:"database_id,notnull"`
	Status     string    `bun:"status,notnull"`
	CreatedBy  string    `bun:"created_by,notnull"`
	ExpireAt   time.Time `bun:"expire_at,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
	UpdatedAt  time.Time `bun:"updated_at,notnull"`
}

// DocumentTransactionOp 是事务内暂存的一条文档写操作；
// (transaction_id, seq) 唯一，seq 在行锁内按 COALESCE(MAX(seq),0)+1 分配。
type DocumentTransactionOp struct {
	bun.BaseModel `bun:"table:document_transaction_ops,alias:dto"`

	ID              string          `bun:"id,pk"`
	TransactionID   string          `bun:"transaction_id,notnull"`
	Seq             int32           `bun:"seq,notnull"`
	OpType          string          `bun:"op_type,notnull"`
	CollectionID    string          `bun:"collection_id,notnull"`
	DocumentID      string          `bun:"document_id,notnull"`
	Data            json.RawMessage `bun:"data,type:jsonb"`
	Permissions     []string        `bun:"permissions,array"`
	Increment       json.RawMessage `bun:"increment,type:jsonb"`
	Version         *int64          `bun:"version"`
	ConflictColumns []string        `bun:"conflict_columns,array"`
	CreatedAt       time.Time       `bun:"created_at,notnull"`
}
