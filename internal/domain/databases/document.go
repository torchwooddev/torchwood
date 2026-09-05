package databases

import (
	"time"

	"github.com/torchwooddev/torchwood/pkg/query"
)

type Attribute struct {
	ID       string
	Key      string
	Type     string // string, integer, float, boolean, datetime, email, url, json, vector
	Size     int
	Required bool
	Default  any
	Array    bool
	// Dims 是 vector 属性的维度（会话 #10）：仅 type=vector 非零，
	// 合法域 2..2000（pgvector 可索引上限）。维度变更 = 新列 + 数据重灌
	//（换模型即换列名，不走 schema 演进状态机）。
	Dims    int
	Options map[string]any
}

type Index struct {
	ID         string
	Type       string // key, unique, fulltext, hnsw
	Attributes []string
	Orders     []string
	// DistanceMetric 是 hnsw 索引的距离度量（会话 #10）：COSINE | L2 |
	// INNER_PRODUCT。仅 hnsw 类型非空；缺省 COSINE（app 层归一）。
	DistanceMetric string
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
	Version     int64 // 顶层；用户 collection 的 OCC `_version`；系统资源不经本字段；不进 Data
}

type Query struct {
	// Queries 是 DSL 字符串遗留字段：文档查询栈（List/Count/Aggregate）已单
	// AST 化不再消费；仅供边界邻居面（users/buckets/files 等系统静态表
	// listing）携带，勿在新代码使用。
	Queries   []string
	PageSize  int32
	PageToken string
	// AST is the typed query（C7 单一消费形态）。
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

// 数组列原子更新算子（阶段③-b §10.5 P0 写侧，首期四算子）。Intersect/Diff/
// Insert/Filter 挂账转出 POC 前。
const (
	ArrayUpdateOpAppend  = "append"
	ArrayUpdateOpPrepend = "prepend"
	ArrayUpdateOpRemove  = "remove"
	ArrayUpdateOpUnique  = "unique"
)

// ArrayUpdate 是单个数组列的原子更新（编译为单语句 SET 子句，与 data/
// increment 可组合）。APPEND/PREPEND/REMOVE 要求 Values >= 1；UNIQUE 忽略
// Values。仅 array=true 属性可用（adapter 按 catalog attrs 校验）。
type ArrayUpdate struct {
	Op     string
	Values []string
}

type DocumentUpdate struct {
	Document    Document
	Permissions []Permission
	Increment   map[string]int64
	// ArrayUpdates 是数组列原子更新（阶段③-b）：键为属性 key，与 Document.Data
	// 同列冲突由 adapter 拒绝（同一 SET 子句内同列双赋值歧义）。
	ArrayUpdates map[string]ArrayUpdate
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

// ReservedAttributeKeys 是禁止作为用户属性的系统列（含 _version/_acl）。
// ValidateIdentifier 允许 "_" 前缀，必须在属性创建路径显式拒绝。
var ReservedAttributeKeys = map[string]struct{}{
	"_id": {}, "_tenant": {}, "_created_at": {}, "_updated_at": {},
	"_created_by": {}, "_updated_by": {}, "_version": {}, "_perms": {}, "_acl": {},
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

// Database 是 catalog 中的库行，不再复用 Collection。
type Database struct {
	ID        string
	Name      string
	ProjectID string
	CreatedAt time.Time
	UpdatedAt time.Time
}
