package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// AssetDef 是资产定义行（public.asset_defs，v3 设计 §2.4）。
type AssetDef struct {
	bun.BaseModel `bun:"table:asset_defs,alias:ad"`

	ID             string          `bun:"id,pk"`
	ProjectID      string          `bun:"project_id,notnull"`
	Code           string          `bun:"code,notnull"`
	Name           string          `bun:"name,notnull"`
	Class          string          `bun:"class,notnull"`
	Decimals       int32           `bun:"decimals,notnull"`
	MaxQuantity    *int64          `bun:"max_quantity"`
	ExpiresIn      *int64          `bun:"expires_in"`
	Tradable       bool            `bun:"tradable,notnull"`
	UniquePerOwner bool            `bun:"unique_per_owner,notnull"`
	Upgradeable    bool            `bun:"upgradeable,notnull"`
	Metadata       json.RawMessage `bun:"metadata,type:jsonb"`
	Status         string          `bun:"status,notnull"`
	CreatedAt      time.Time       `bun:"created_at,notnull"`
	UpdatedAt      time.Time       `bun:"updated_at,notnull"`
}

// AssetHolding 是物化持有行（public.asset_holdings，v3 设计 §2.4）。
type AssetHolding struct {
	bun.BaseModel `bun:"table:asset_holdings,alias:ah"`

	ID        string          `bun:"id,pk"`
	ProjectID string          `bun:"project_id,notnull"`
	OwnerType string          `bun:"owner_type,notnull"`
	OwnerID   string          `bun:"owner_id,notnull"`
	DefID     string          `bun:"def_id,notnull"`
	Quantity  int64           `bun:"quantity,notnull"`
	ExpiresAt *time.Time      `bun:"expires_at"`
	Level     int32           `bun:"level,notnull"`
	Metadata  json.RawMessage `bun:"metadata,type:jsonb"`
	BucketKey string          `bun:"bucket_key,notnull"`
	Version   int64           `bun:"version,notnull"`
	CreatedAt time.Time       `bun:"created_at,notnull"`
	UpdatedAt time.Time       `bun:"updated_at,notnull"`
}

// AssetLedgerEntry 是 append-only 流水行（public.asset_ledger_entries）。
type AssetLedgerEntry struct {
	bun.BaseModel `bun:"table:asset_ledger_entries,alias:ale"`

	ID             string          `bun:"id,pk"`
	ProjectID      string          `bun:"project_id,notnull"`
	HoldingID      *string         `bun:"holding_id"`
	OwnerType      string          `bun:"owner_type,notnull"`
	OwnerID        string          `bun:"owner_id,notnull"`
	DefID          string          `bun:"def_id,notnull"`
	Kind           string          `bun:"kind,notnull"`
	Delta          int64           `bun:"delta,notnull"`
	QuantityAfter  int64           `bun:"quantity_after,notnull"`
	ExpiresAt      *time.Time      `bun:"expires_at"`
	BucketKey      string          `bun:"bucket_key,notnull"`
	RefType        *string         `bun:"ref_type"`
	RefID          *string         `bun:"ref_id"`
	IdempotencyKey string          `bun:"idempotency_key,notnull"`
	TxID           *string         `bun:"tx_id"`
	Operator       json.RawMessage `bun:"operator,type:jsonb"`
	CreatedAt      time.Time       `bun:"created_at,notnull"`
}
