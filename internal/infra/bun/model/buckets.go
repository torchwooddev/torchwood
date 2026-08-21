package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Bucket 是项目 schema 内的存储桶元数据行；表名由 ModelTableExpr 限定。
type Bucket struct {
	bun.BaseModel `bun:"alias:b"`

	ID          string          `bun:"id,pk"`
	Name        string          `bun:"name,notnull"`
	Permissions json.RawMessage `bun:"permissions,type:jsonb,notnull"`
	Public      bool            `bun:"public,notnull,default:false"`
	CreatedAt   time.Time       `bun:"created_at,notnull"`
	UpdatedAt   time.Time       `bun:"updated_at,notnull"`
}
