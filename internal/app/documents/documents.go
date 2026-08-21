package documents

import (
	"context"
	"fmt"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MaxBulkOperations 是 Bulk 写入单次条数上限（A4）。
const MaxBulkOperations = 1000

// WriteOptions 是 Client/Server 策略投影传给文档核的授权差异。
type WriteOptions struct {
	// AllowPrivilegedGrant 为 true 时跳过「必须持有被授予角色」
	// （Server platform admin / keys）；Client 为 false。
	AllowPrivilegedGrant bool
}

// Documents 是 Client/Server 共用的文档 CRUD 核：OCC、grant 展开/校验、
// MapDocumentDBError、系统集合 version 归零。集合守卫、guest 读、owner
// 默认 ACE、敏感字段过滤、脱敏留在包装层。
type Documents struct {
	docDB databases.DocumentDB
}

// New 构造文档核。docDB 不得为 nil。
func New(docDB databases.DocumentDB) *Documents {
	return &Documents{docDB: docDB}
}

// DocumentDB 返回注入的端口（包装层 catalog / EnsureSystemCollections 复用）。
func (d *Documents) DocumentDB() databases.DocumentDB { return d.docDB }

func (d *Documents) CreateDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
	opts WriteOptions,
) (*databases.Document, error) {
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}
	perms, err := applyGrant(principal, perms, opts.AllowPrivilegedGrant)
	if err != nil {
		return nil, err
	}
	created, err := d.docDB.CreateDocument(ctx, projectID, databaseID, collectionID, databases.Document{
		ID:   documentID,
		Data: data,
	}, perms, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("create document: %w", err))
	}
	return &created, nil
}

func (d *Documents) ListDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	q databases.Query,
	principal databases.Principal,
) ([]databases.Document, int64, string, error) {
	ast, err := ResolveQuery(q)
	if err != nil {
		return nil, 0, "", err
	}
	list, err := d.docDB.ListDocuments(ctx, projectID, databaseID, collectionID, databases.Query{
		Queries:   q.Queries,
		AST:       ast,
		PageSize:  ast.PageSize,
		PageToken: ast.PageToken,
	}, principal)
	if err != nil {
		return nil, 0, "", shared.MapDocumentDBError(err)
	}
	for i := range list.Documents {
		zeroSystemDocumentVersion(projectID, databaseID, collectionID, &list.Documents[i])
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (d *Documents) GetDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
) (*databases.Document, error) {
	doc, err := d.docDB.GetDocument(ctx, projectID, databaseID, collectionID, documentID, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(err)
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "document not found")
	}
	zeroSystemDocumentVersion(projectID, databaseID, collectionID, doc)
	return doc, nil
}

func (d *Documents) UpdateDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
	increment map[string]int64,
	principal databases.Principal,
	version *int64,
	opts WriteOptions,
) (*databases.Document, error) {
	if err := shared.UpdateDocumentVersionRequired(version); err != nil {
		return nil, err
	}
	if len(data) == 0 && len(perms) == 0 && len(increment) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data, permissions, or increment is required")
	}
	if len(perms) > 0 {
		var err error
		perms, err = applyGrant(principal, perms, opts.AllowPrivilegedGrant)
		if err != nil {
			return nil, err
		}
	}
	if len(data) == 0 {
		data = map[string]any{}
	}
	updated, err := d.docDB.UpdateDocument(ctx, projectID, databaseID, collectionID, databases.DocumentUpdate{
		Document:        databases.Document{ID: documentID, Data: data},
		Permissions:     perms,
		Increment:       increment,
		ExpectedVersion: *version,
	}, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("update document: %w", err))
	}
	return &updated, nil
}

func (d *Documents) UpsertDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	data map[string]any,
	conflictColumns []string,
	perms []databases.Permission,
	principal databases.Principal,
	opts WriteOptions,
) (*databases.Document, error) {
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}
	if len(conflictColumns) == 0 {
		return nil, status.Error(codes.InvalidArgument, "conflict_columns is required")
	}
	perms, err := applyGrant(principal, perms, opts.AllowPrivilegedGrant)
	if err != nil {
		return nil, err
	}
	upserted, err := d.docDB.UpsertDocument(ctx, projectID, databaseID, collectionID, databases.Document{
		ID:   documentID,
		Data: data,
	}, conflictColumns, perms, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("upsert document: %w", err))
	}
	return &upserted, nil
}

func (d *Documents) DeleteDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
	version *int64,
) error {
	if err := shared.UpdateDocumentVersionRequired(version); err != nil {
		return err
	}
	return shared.MapDocumentDBError(d.docDB.DeleteDocument(ctx, projectID, databaseID, collectionID, documentID, databases.DeleteOptions{ExpectedVersion: *version}, principal))
}

func (d *Documents) CountDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	q databases.Query,
	principal databases.Principal,
) (int64, error) {
	ast, err := ResolveQuery(q)
	if err != nil {
		return 0, err
	}
	count, err := d.docDB.CountDocuments(ctx, projectID, databaseID, collectionID, databases.Query{Queries: q.Queries, AST: ast}, principal)
	return count, shared.MapDocumentDBError(err)
}

func (d *Documents) BulkUpdateDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
	opts WriteOptions,
) (int64, error) {
	if err := validateBulkIDs(documentIDs); err != nil {
		return 0, err
	}
	if len(perms) > 0 {
		var err error
		perms, err = applyGrant(principal, perms, opts.AllowPrivilegedGrant)
		if err != nil {
			return 0, err
		}
	}
	n, err := d.docDB.BulkUpdateDocuments(ctx, projectID, databaseID, collectionID, documentIDs, data, perms, principal)
	return n, shared.MapDocumentDBError(err)
}

func (d *Documents) BulkDeleteDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	principal databases.Principal,
) (int64, error) {
	if err := validateBulkIDs(documentIDs); err != nil {
		return 0, err
	}
	n, err := d.docDB.BulkDeleteDocuments(ctx, projectID, databaseID, collectionID, documentIDs, principal)
	return n, shared.MapDocumentDBError(err)
}

func validateBulkIDs(documentIDs []string) error {
	if len(documentIDs) == 0 {
		return status.Error(codes.InvalidArgument, "document_ids is required")
	}
	if len(documentIDs) > MaxBulkOperations {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("document_ids exceeds maximum of %d", MaxBulkOperations))
	}
	return nil
}

func applyGrant(principal databases.Principal, perms []databases.Permission, allowPrivileged bool) ([]databases.Permission, error) {
	perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
	if err := databases.ValidateGrantablePermissions(principal, perms, allowPrivileged); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return perms, nil
}

func zeroSystemDocumentVersion(projectID, databaseID, collectionID string, doc *databases.Document) {
	if doc == nil {
		return
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		doc.Version = 0
	}
}
