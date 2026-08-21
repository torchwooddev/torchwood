package databases

import (
	"time"

	"github.com/torchwooddev/torchwood/pkg/query"
)

type Attribute struct {
	ID       string
	Key      string
	Type     string // string, integer, float, boolean, datetime, email, url, json
	Size     int
	Required bool
	Default  any
	Array    bool
	Options  map[string]any
}

type Index struct {
	ID         string
	Type       string // key, unique, fulltext
	Attributes []string
	Orders     []string
}

type Permission struct {
	Type string // read, create, update, delete
	Role string // any, users, user:{id}, keys, admin, group:{id}, ...
}

type Document struct {
	ID          string
	Tenant      int64
	Data        map[string]any
	Permissions []Permission
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	Version     int64 // 顶层；用户集合为当前 _version，系统集合恒为 0；不进 Data
}

type Query struct {
	Queries   []string
	PageSize  int32
	PageToken string
	// AST is the typed query (proto codec). Dual-stack with Queries.
	AST *query.Query
}

// ListQuery carries pagination parameters for collection listing.
type ListQuery struct {
	PageSize  int32
	PageToken string
}

// ListMeta reports pagination metadata for collection listing.
type ListMeta struct {
	TotalCount    int64
	NextPageToken string
}

type CollectionPatch struct {
	Name             string
	Permissions      *[]Permission
	DocumentSecurity *bool
	Disabled         *bool
}

type DocumentUpdate struct {
	Document    Document
	Permissions []Permission
	Increment   map[string]int64
	// ExpectedVersion：用户集合且 !SkipVersion 时必填，须等于当前行 _version。
	ExpectedVersion int64
	// SkipVersion：Bulk 内部循环、Upsert 更新支专用。仍执行 _version = _version + 1。
	SkipVersion bool
}

// DeleteOptions 携带 DeleteDocument 的 OCC 参数。
type DeleteOptions struct {
	ExpectedVersion int64
	SkipVersion     bool
}

// ReservedAttributeKeys 是禁止作为用户属性的系统列（含 _version）。
// ValidateIdentifier 允许 "_" 前缀，必须在属性创建路径显式拒绝。
var ReservedAttributeKeys = map[string]struct{}{
	"_id": {}, "_tenant": {}, "_created_at": {}, "_updated_at": {},
	"_created_by": {}, "_updated_by": {}, "_version": {}, "_perms": {},
}

type DocumentList struct {
	Documents     []Document
	TotalCount    int64
	NextPageToken string
}

type Collection struct {
	ID               string
	DatabaseID       string
	ProjectID        string
	Name             string
	DocumentSecurity bool
	Disabled         bool
	IsSystem         bool
	Permissions      []Permission
	Attributes       []Attribute
	Indexes          []Index
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
