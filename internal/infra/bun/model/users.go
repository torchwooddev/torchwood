package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// User 对应项目 schema 的 sys_users（E5-4 再 rename 为 users）。
type User struct {
	bun.BaseModel `bun:"table:sys_users,alias:u"`

	ID            string          `bun:"id,pk"`
	Email         string          `bun:"email,notnull"`
	PasswordHash  string          `bun:"password_hash,notnull,default:''"`
	Name          string          `bun:"name,notnull,default:''"`
	Status        string          `bun:"status,notnull,default:'active'"`
	EmailVerified bool            `bun:"email_verified,notnull,default:false"`
	PendingEmail  string          `bun:"pending_email,notnull,default:''"`
	Phone         string          `bun:"phone,notnull,default:''"`
	PhoneVerified bool            `bun:"phone_verified,notnull,default:false"`
	Labels        json.RawMessage `bun:"labels,type:jsonb,notnull"`
	Prefs         json.RawMessage `bun:"prefs,type:jsonb,notnull"`
	Factors       json.RawMessage `bun:"factors,type:jsonb,notnull"`
	CreatedAt     time.Time       `bun:"created_at,notnull"`
	UpdatedAt     time.Time       `bun:"updated_at,notnull"`
}
