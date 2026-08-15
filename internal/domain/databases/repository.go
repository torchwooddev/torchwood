package databases

import "context"

type DocumentDB interface {
	// Database / schema
	CreateDatabase(ctx context.Context, projectID, id, name string) error
	GetDatabase(ctx context.Context, projectID, id string) (*Collection, error)
	ListDatabases(ctx context.Context, projectID string) ([]Collection, error)
	DeleteDatabase(ctx context.Context, projectID, id string) error

	// Collection
	CreateCollection(ctx context.Context, projectID, databaseID, collectionID, name string, attrs []Attribute, idxs []Index, perms []Permission, documentSecurity bool) error
	GetCollection(ctx context.Context, projectID, databaseID, collectionID string) (*Collection, error)
	ListCollections(ctx context.Context, projectID, databaseID string, q ListQuery) ([]Collection, ListMeta, error)
	DeleteCollection(ctx context.Context, projectID, databaseID, collectionID string) error
	UpdateCollection(ctx context.Context, projectID, databaseID, collectionID string, patch CollectionPatch) error

	// Attribute / Index
	CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr Attribute) error
	DeleteAttribute(ctx context.Context, projectID, databaseID, collectionID, key string) error
	CreateIndex(ctx context.Context, projectID, databaseID, collectionID string, idx Index) error
	DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error

	// Document
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
	CountDocuments(ctx context.Context, projectID, databaseID, collectionID string, queries []string, principal Principal) (int64, error)
	// SumDocumentField sums a numeric column across a collection (e.g. file sizes
	// for storage usage), scoped by the caller's read permissions.
	SumDocumentField(ctx context.Context, projectID, databaseID, collectionID, field string, principal Principal) (int64, error)
	BulkUpdateDocuments(ctx context.Context, projectID, databaseID, collectionID string, documentIDs []string, data map[string]any, perms []Permission, principal Principal) (int64, error)
	BulkDeleteDocuments(ctx context.Context, projectID, databaseID, collectionID string, documentIDs []string, principal Principal) (int64, error)

	// System bootstrap
	EnsureSystemCollections(ctx context.Context, projectID string, internalID int64) error
}
