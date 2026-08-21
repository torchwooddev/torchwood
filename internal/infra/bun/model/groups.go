package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Group 是项目 schema 内的用户组行；表名由 ModelTableExpr 限定。
type Group struct {
	bun.BaseModel `bun:"alias:g"`

	ID          string          `bun:"id,pk"`
	Name        string          `bun:"name,notnull"`
	Permissions json.RawMessage `bun:"permissions,type:jsonb,notnull"`
	Total       int64           `bun:"total,notnull,default:0"`
	Prefs       json.RawMessage `bun:"prefs,type:jsonb,notnull"`
	CreatedAt   time.Time       `bun:"created_at,notnull"`
	UpdatedAt   time.Time       `bun:"updated_at,notnull"`
}
