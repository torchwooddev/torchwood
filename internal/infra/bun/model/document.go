package model

import (
	"time"

	"github.com/uptrace/bun"
)

// 全局 catalog 两表（db/migrations/000025，redesign §4.2 / C1）：
// catalog_databases 简单行；catalog_collections 把 attrs/indexes/permissions
// 以 JSONB 列合一（每项目四表模型已退役）。JSONB 列用 string 承载——
// bun/pgdriver 对 jsonb 列的 string 参数按文本协议发送由 PG 隐式转型，
// 扫描侧 []byte→string 直通；结构化编解码在 documentdb 的映射层完成。

type DocumentDatabase struct {
	bun.BaseModel `bun:"table:catalog_databases,alias:ddb"`

	ProjectID  string    `bun:"project_id,pk"`
	DatabaseID string    `bun:"database_id,pk"`
	Name       string    `bun:"name,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
	UpdatedAt  time.Time `bun:"updated_at,notnull"`
}

type DocumentCollection struct {
	bun.BaseModel `bun:"table:catalog_collections,alias:dc"`

	ProjectID        string    `bun:"project_id,pk"`
	DatabaseID       string    `bun:"database_id,pk"`
	CollectionID     string    `bun:"collection_id,pk"`
	Name             string    `bun:"name,notnull"`
	PhysicalName     string    `bun:"physical_name,notnull"`
	DocumentSecurity bool      `bun:"document_security,notnull"`
	Disabled         bool      `bun:"disabled,notnull,default:false"`
	IsSystem         bool      `bun:"is_system,notnull,default:false"`
	Permissions      string    `bun:"permissions,notnull"`
	Attrs            string    `bun:"attrs,notnull"`
	Indexes          string    `bun:"indexes,notnull"`
	SchemaVersion    int64     `bun:"schema_version,notnull"`
	DDLSeq           int64     `bun:"ddl_seq,notnull"`
	CreatedAt        time.Time `bun:"created_at,notnull"`
	UpdatedAt        time.Time `bun:"updated_at,notnull"`
}
