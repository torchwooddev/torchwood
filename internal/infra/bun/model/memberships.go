package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Membership 是项目 schema 内的组成员行；表名由 ModelTableExpr 限定。
type Membership struct {
	bun.BaseModel `bun:"alias:m"`

	ID        string          `bun:"id,pk"`
	GroupID   string          `bun:"group_id,notnull"`
	UserID    *string         `bun:"user_id"`
	Email     string          `bun:"email,notnull,default:''"`
	Name      string          `bun:"name,notnull,default:''"`
	Roles     json.RawMessage `bun:"roles,type:jsonb,notnull"`
	Status    string          `bun:"status,notnull,default:'pending'"`
	InvitedAt *time.Time      `bun:"invited_at"`
	JoinedAt  *time.Time      `bun:"joined_at"`
	CreatedAt time.Time       `bun:"created_at,notnull"`
	UpdatedAt time.Time       `bun:"updated_at,notnull"`
}
