package assets

import (
	"context"
	"encoding/json"
	"time"
)

// DefRepo 持久化 asset_defs。方法感知 ctx 中的事务（clients.Conn）。
type DefRepo interface {
	Insert(ctx context.Context, def *Def) error
	GetByID(ctx context.Context, projectID, defID string) (*Def, error)
	GetByCode(ctx context.Context, projectID, code string) (*Def, error)
	// GetByCodeForShare 在事务内 SELECT ... FOR SHARE，防止 Grant 途中归档。
	GetByCodeForShare(ctx context.Context, projectID, code string) (*Def, error)
	GetByIDForShare(ctx context.Context, projectID, defID string) (*Def, error)
	List(ctx context.Context, projectID string, includeArchived bool, limit int, before time.Time) ([]Def, error)
	Update(ctx context.Context, def *Def) error
}

// HoldingRepo 持久化 asset_holdings。写路径必须在外层事务内调用
// （与 ledger / outbox 同一 sql.Tx，总则 10）。
type HoldingRepo interface {
	Insert(ctx context.Context, h *Holding) error
	GetByID(ctx context.Context, projectID, holdingID string) (*Holding, error)
	GetByIDForUpdate(ctx context.Context, projectID, holdingID string) (*Holding, error)
	// ListForUpdate 锁业主该定义下全部持有行（FEFO 消耗 / Grant 并桶）。
	// 返回顺序：expires_at ASC NULLS LAST, id ASC。
	ListForUpdate(ctx context.Context, projectID string, ownerType OwnerType, ownerID, defID string) ([]Holding, error)
	// ListByOwner 读路径（懒过滤由 use-case 做）；created_at DESC 分页。
	ListByOwner(ctx context.Context, projectID string, ownerType OwnerType, ownerID string, limit int, before time.Time) ([]Holding, error)
	Update(ctx context.Context, h *Holding, expectVersion int64) error
	Delete(ctx context.Context, projectID, holdingID string, expectVersion int64) error
	// ListExpiredInProject 到期扫描（worker）：expires_at <= now，FOR UPDATE SKIP LOCKED。
	ListExpiredInProject(ctx context.Context, projectID string, now time.Time, limit int) ([]Holding, error)
	// ListAllInProject 对账用：项目内全部持有（含已过期未扫行）。
	ListAllInProject(ctx context.Context, projectID string) ([]Holding, error)
}

// LedgerRepo 持久化 asset_ledger_entries（append-only）。
type LedgerRepo interface {
	// InsertIfAbsent 按 (project_id, idempotency_key) 插入；冲突返回已存在行与 false。
	InsertIfAbsent(ctx context.Context, e *LedgerEntry) (existing *LedgerEntry, inserted bool, err error)
	GetByIdempotencyKey(ctx context.Context, projectID, key string) (*LedgerEntry, error)
	ListByRef(ctx context.Context, projectID, refType, refID string) ([]LedgerEntry, error)
	ListByOwner(ctx context.Context, projectID string, ownerType OwnerType, ownerID string, defID string, limit int, before time.Time) ([]LedgerEntry, error)
	// ListAllInProject 对账用：按 created_at, id 升序重放。
	ListAllInProject(ctx context.Context, projectID string) ([]LedgerEntry, error)
}

// OperatorSnapshot 是流水 operator JSONB 的 principal 快照（不含隐私）。
type OperatorSnapshot struct {
	ActorKind      string `json:"actor_kind,omitempty"`
	ActorID        string `json:"actor_id,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	APIKeyID       string `json:"api_key_id,omitempty"`
	IsSystem       bool   `json:"is_system,omitempty"`
	CredentialType string `json:"credential_type,omitempty"`
}

// MarshalOperator 序列化 operator 快照。
func MarshalOperator(s OperatorSnapshot) json.RawMessage {
	raw, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
