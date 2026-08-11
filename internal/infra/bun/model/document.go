package model

import (
	"time"

	"github.com/uptrace/bun"
)

type DocumentDatabase struct {
	bun.BaseModel `bun:"table:document_databases,alias:ddb"`

	ID        string    `bun:"id,pk"`
	ProjectID string    `bun:"project_id,pk"`
	Name      string    `bun:"name,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
	UpdatedAt time.Time `bun:"updated_at,notnull"`
}

type DocumentCollection struct {
	bun.BaseModel `bun:"table:document_collections,alias:dc"`

	ID               string    `bun:"id,pk"`
	DatabaseID       string    `bun:"database_id,pk"`
	ProjectID        string    `bun:"project_id,pk"`
	Name             string    `bun:"name,notnull"`
	DocumentSecurity bool      `bun:"document_security,notnull"`
	Disabled         bool      `bun:"disabled,notnull,default:false"`
	IsSystem         bool      `bun:"is_system,notnull,default:false"`
	Permissions      []string  `bun:"permissions,array"`
	CreatedAt        time.Time `bun:"created_at,notnull"`
	UpdatedAt        time.Time `bun:"updated_at,notnull"`
}

type DocumentAttribute struct {
	bun.BaseModel `bun:"table:document_attributes,alias:da"`

	ID           string         `bun:"id,pk"`
	CollectionID string         `bun:"collection_id,pk"`
	DatabaseID   string         `bun:"database_id,pk"`
	ProjectID    string         `bun:"project_id,pk"`
	Key          string         `bun:"key,notnull"`
	Type         string         `bun:"type,notnull"`
	Size         *int           `bun:"size"`
	Required     bool           `bun:"required,notnull,default:false"`
	IsArray      bool           `bun:"is_array,notnull,default:false"`
	DefaultValue *string        `bun:"default_value"`
	Options      map[string]any `bun:"options,type:jsonb"`
	CreatedAt    time.Time      `bun:"created_at,notnull"`
}

type DocumentIndex struct {
	bun.BaseModel `bun:"table:document_indexes,alias:di"`

	ID           string    `bun:"id,pk"`
	CollectionID string    `bun:"collection_id,pk"`
	DatabaseID   string    `bun:"database_id,pk"`
	ProjectID    string    `bun:"project_id,pk"`
	Type         string    `bun:"type,notnull"`
	Attributes   []string  `bun:"attributes,array"`
	Orders       []string  `bun:"orders,array"`
	CreatedAt    time.Time `bun:"created_at,notnull"`
}
