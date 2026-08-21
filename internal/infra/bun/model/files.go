package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// File 是项目 schema 内的文件元数据行；表名由 ModelTableExpr 限定。
type File struct {
	bun.BaseModel `bun:"alias:f"`

	ID          string          `bun:"id,pk"`
	BucketID    string          `bun:"bucket_id,notnull"`
	Name        string          `bun:"name,notnull"`
	MimeType    string          `bun:"mime_type,notnull,default:''"`
	Size        int64           `bun:"size,notnull,default:0"`
	Metadata    json.RawMessage `bun:"metadata,type:jsonb,notnull"`
	OwnerUserID *string         `bun:"owner_user_id"`
	CreatedAt   time.Time       `bun:"created_at,notnull"`
	UpdatedAt   time.Time       `bun:"updated_at,notnull"`
}
