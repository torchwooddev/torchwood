package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Identity 是项目 schema 内的第三方身份行；表名由 ModelTableExpr 限定。
type Identity struct {
	bun.BaseModel `bun:"alias:i"`

	ID            string          `bun:"id,pk"`
	UserID        string          `bun:"user_id,notnull"`
	Provider      string          `bun:"provider,notnull"`
	ProviderUID   string          `bun:"provider_uid,notnull"`
	ProviderEmail string          `bun:"provider_email,notnull,default:''"`
	ProviderData  json.RawMessage `bun:"provider_data,type:jsonb,notnull"`
	ExpireAt      *time.Time      `bun:"expire_at"`
	CreatedAt     time.Time       `bun:"created_at,notnull"`
	UpdatedAt     time.Time       `bun:"updated_at,notnull"`
}
