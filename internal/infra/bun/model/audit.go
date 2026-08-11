package model

import (
	"time"

	"github.com/uptrace/bun"
)

type AuditLog struct {
	bun.BaseModel `bun:"table:audit_logs,alias:al"`

	ID         string         `bun:"id,pk"`
	ProjectID  string         `bun:"project_id"`
	ActorID    string         `bun:"actor_id"`
	ActorKind  string         `bun:"actor_kind,notnull"`
	Action     string         `bun:"action,notnull"`
	ResourceID string         `bun:"resource_id"`
	Status     string         `bun:"status,notnull"`
	IP         string         `bun:"ip"`
	UserAgent  string         `bun:"user_agent"`
	Metadata   map[string]any `bun:"metadata,type:jsonb"`
	CreatedAt  time.Time      `bun:"created_at,notnull"`
}

type AdminProject struct {
	bun.BaseModel `bun:"table:admin_projects,alias:cap"`

	AdminID   string    `bun:"admin_id,pk"`
	ProjectID string    `bun:"project_id,pk"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}
