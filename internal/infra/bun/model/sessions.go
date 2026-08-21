package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Session 对应项目 schema 的 sys_sessions（E5-4 再 rename 为 sessions）。
type Session struct {
	bun.BaseModel `bun:"table:sys_sessions,alias:s"`

	ID         string          `bun:"id,pk"`
	UserID     string          `bun:"user_id,notnull"`
	SecretHash string          `bun:"secret_hash,notnull"`
	Provider   string          `bun:"provider,notnull,default:'email'"`
	UserAgent  string          `bun:"user_agent,notnull,default:''"`
	IP         string          `bun:"ip,notnull,default:''"`
	Country    string          `bun:"country,notnull,default:''"`
	Factors    json.RawMessage `bun:"factors,type:jsonb,notnull"`
	ExpireAt   time.Time       `bun:"expire_at,notnull"`
	CreatedAt  time.Time       `bun:"created_at,notnull"`
	UpdatedAt  time.Time       `bun:"updated_at,notnull"`
}
