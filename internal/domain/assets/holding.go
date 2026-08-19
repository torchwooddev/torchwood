package assets

import (
	"encoding/json"
	"time"
)

// Holding 是物化持有行（public.asset_holdings，设计 §2.4）。
// 消耗/过期删行不留尸体；quantity 必须等于其流水之和（D11）。
type Holding struct {
	ID        string
	ProjectID string
	OwnerType OwnerType
	OwnerID   string
	DefID     string
	Quantity  int64 // bigint 最小单位，>0；到 0 则删行
	ExpiresAt *time.Time
	Level     int32
	Metadata  json.RawMessage
	BucketKey string // 非 instance 空串；instance = id
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Expired 报告持有是否已过期（读路径懒过滤：expires_at <= now）。
func (h *Holding) Expired(now time.Time) bool {
	if h == nil || h.ExpiresAt == nil {
		return false
	}
	return !h.ExpiresAt.After(now)
}

// LedgerEntry 是 append-only 流水行（public.asset_ledger_entries，设计 §2.4）。
// 禁止 UPDATE/DELETE；纠错用反向 adjust。tx_id 预留 D2。
type LedgerEntry struct {
	ID             string
	ProjectID      string
	HoldingID      string // 可空：删行后仍保留原 id 作为历史引用
	OwnerType      OwnerType
	OwnerID        string
	DefID          string
	Kind           EntryKind
	Delta          int64
	QuantityAfter  int64
	ExpiresAt      *time.Time // 当时分桶的到期时刻（currency 恒 NULL）
	BucketKey      string
	RefType        string
	RefID          string
	IdempotencyKey string
	TxID           string
	Operator       json.RawMessage
	CreatedAt      time.Time
}

// Drift 是对账发现的一处 holdings ↔ 流水重放不一致。
type Drift struct {
	ProjectID     string
	OwnerType     OwnerType
	OwnerID       string
	DefID         string
	ExpiresAt     *time.Time
	BucketKey     string
	HoldingQty    int64
	ReplayedQty   int64
	HoldingID     string
	QuantityAfter bool // true = quantity_after 链路断裂
	Detail        string
}

// ReconcileReport 是对账任务结果（一期手动触发）。
type ReconcileReport struct {
	ProjectID     string
	Holdings      int
	Entries       int
	Drifts        []Drift
	CheckedAt     time.Time
	ZeroDrift     bool
	QuantityAfter int // quantity_after 链路断裂条数
}
