package databases

import "context"

// Catalog 是只读 catalog：不跑 DDL、不 Apply 项目迁移。
type Catalog interface {
	GetDatabase(ctx context.Context, projectID, id string) (*Database, error)
	ListDatabases(ctx context.Context, projectID string) ([]Database, error)
	GetCollection(ctx context.Context, projectID, databaseID, collectionID string) (*Collection, error)
	ListCollections(ctx context.Context, projectID, databaseID string, q ListQuery) ([]Collection, ListMeta, error)
}

// SchemaApplier 变更 catalog / 集合 DDL。EnsureCatalog 是读旁路唯一允许
// projectschema.Apply 的出口（启动 / 建项）；GetCollection 禁止调用。
type SchemaApplier interface {
	CreateDatabase(ctx context.Context, projectID, id, name string) error
	DeleteDatabase(ctx context.Context, projectID, id string) error
	CreateCollection(ctx context.Context, projectID, databaseID, collectionID, name string, attrs []Attribute, idxs []Index, perms []Permission, documentSecurity bool) error
	UpdateCollection(ctx context.Context, projectID, databaseID, collectionID string, patch CollectionPatch) error
	DeleteCollection(ctx context.Context, projectID, databaseID, collectionID string) error
	CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr Attribute) error
	DeleteAttribute(ctx context.Context, projectID, databaseID, collectionID, key string) error
	CreateIndex(ctx context.Context, projectID, databaseID, collectionID string, idx Index) error
	DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error
	EnsureCatalog(ctx context.Context, projectID string) error
}

// Documents 是文档 CRUD。Principal 是 ACL 主体（本波不去掉）。
// List 必须 SQL 下推 _perms，不得改成 fetch-then-Check。
type Documents interface {
	CreateDocument(ctx context.Context, projectID, databaseID, collectionID string, doc Document, perms []Permission, principal Principal) (Document, error)
	// UpsertDocument inserts doc, or when a row already matches the conflict
	// columns, updates its data, _updated_at, _updated_by and replaces its
	// document permissions with perms. conflictColumns must match a unique
	// index on the collection table.
	UpsertDocument(ctx context.Context, projectID, databaseID, collectionID string, doc Document, conflictColumns []string, perms []Permission, principal Principal) (Document, error)
	GetDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, principal Principal) (*Document, error)
	UpdateDocument(ctx context.Context, projectID, databaseID, collectionID string, update DocumentUpdate, principal Principal) (Document, error)
	DeleteDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, opts DeleteOptions, principal Principal) error
	ListDocuments(ctx context.Context, projectID, databaseID, collectionID string, q Query, principal Principal) (*DocumentList, error)
	CountDocuments(ctx context.Context, projectID, databaseID, collectionID string, q Query, principal Principal) (int64, error)
	// SumDocumentField sums a numeric column across a collection (e.g. file sizes
	// for storage usage), scoped by the caller's read permissions.
	SumDocumentField(ctx context.Context, projectID, databaseID, collectionID, field string, principal Principal) (int64, error)
	BulkUpdateDocuments(ctx context.Context, projectID, databaseID, collectionID string, documentIDs []string, data map[string]any, perms []Permission, principal Principal) (int64, error)
	BulkDeleteDocuments(ctx context.Context, projectID, databaseID, collectionID string, documentIDs []string, principal Principal) (int64, error)
	// ExecuteTransactions 在单事务内顺序执行异构 op 批（事务内核 Phase 1，
	// redesign §4.8）：按 (_tenant, doc) 排序预加 advisory 锁防批间死锁，
	// 事件同事务且顺序 = op 序；ATOMIC 任一失败整批回滚（返回带 op index
	// 的错误），PARTIAL 逐 op savepoint 容错、已成功不回滚。
	ExecuteTransactions(ctx context.Context, projectID, databaseID string, ops []TransactionOp, mode TransactionMode, principal Principal) ([]TransactionOpResult, error)
}

// DocumentDB 嵌入三端口，现有注入点多数不用改签名。
type DocumentDB interface {
	Catalog
	SchemaApplier
	Documents
}

var (
	_ Catalog       = (DocumentDB)(nil)
	_ SchemaApplier = (DocumentDB)(nil)
	_ Documents     = (DocumentDB)(nil)
)
