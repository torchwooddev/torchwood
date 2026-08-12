package client

import (
	"context"
	"fmt"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Databases struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
}

// clientDocumentIDReserved 是 Client API 的 REST 字面量路由段
// （documents/count），document_id 不得取 count（F11-3，方案 B）。
var clientDocumentIDReserved = map[string]struct{}{"count": {}}

func NewDatabases(projectRepo projects.Repository, docDB databases.DocumentDB) *Databases {
	return &Databases{projectRepo: projectRepo, docDB: docDB}
}

func (d *Databases) loadProject(ctx context.Context, projectID string) (*projects.Project, error) {
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	project, err := d.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := d.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, fmt.Errorf("ensure system collections: %w", err)
	}
	return project, nil
}

func (d *Databases) resolveProject(ctx context.Context) (*projects.Project, databases.Principal, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.ProjectID == "" || p.UserID == "" {
		return nil, databases.Principal{}, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	project, err := d.loadProject(ctx, p.ProjectID)
	if err != nil {
		return nil, databases.Principal{}, err
	}
	return project, databases.Principal{Roles: p.Roles}, nil
}

func (d *Databases) resolveReadPrincipal(ctx context.Context, projectID string) (string, databases.Principal, error) {
	if p, ok := contexts.Principal(ctx); ok && p.UserID != "" {
		if projectID != "" && projectID != p.ProjectID {
			return "", databases.Principal{}, status.Error(codes.InvalidArgument, "project_id mismatch")
		}
		project, err := d.loadProject(ctx, p.ProjectID)
		if err != nil {
			return "", databases.Principal{}, err
		}
		return project.ID, databases.Principal{Roles: p.Roles}, nil
	}
	if projectID == "" {
		if p, ok := contexts.Principal(ctx); ok && p.ProjectID != "" {
			projectID = p.ProjectID
		}
	}
	project, err := d.loadProject(ctx, projectID)
	if err != nil {
		return "", databases.Principal{}, err
	}
	return project.ID, databases.GuestPrincipal, nil
}

func (d *Databases) ensureCollection(ctx context.Context, databaseID, collectionID string) (string, databases.Principal, error) {
	project, principal, err := d.resolveProject(ctx)
	if err != nil {
		return "", databases.Principal{}, err
	}
	return d.ensureCollectionForProject(ctx, project.ID, databaseID, collectionID, principal, false)
}

func (d *Databases) ensureCollectionForRead(ctx context.Context, projectID, databaseID, collectionID string) (string, databases.Principal, error) {
	pid, principal, err := d.resolveReadPrincipal(ctx, projectID)
	if err != nil {
		return "", databases.Principal{}, err
	}
	return d.ensureCollectionForProject(ctx, pid, databaseID, collectionID, principal, true)
}

func (d *Databases) ensureCollectionForProject(ctx context.Context, projectID, databaseID, collectionID string, principal databases.Principal, readOnly bool) (string, databases.Principal, error) {
	// 系统集合仅限 default 库判定；写路径拒绝全部系统集合，
	// 读路径仅拒绝高敏系统集合（users/sessions/identities，有 Account 专用 API）。
	if databases.IsSystemCollection(projectID, databaseID, collectionID) &&
		(!readOnly || databases.IsSensitiveSystemCollectionID(collectionID)) {
		return "", databases.Principal{}, shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	col, err := d.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return "", databases.Principal{}, err
	}
	if col == nil {
		return "", databases.Principal{}, status.Error(codes.NotFound, "collection not found")
	}
	if col.Disabled {
		return "", databases.Principal{}, shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return projectID, principal, nil
}

func (d *Databases) CreateDocument(
	ctx context.Context,
	databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
) (*databases.Document, error) {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	if _, reserved := clientDocumentIDReserved[documentID]; reserved {
		return nil, status.Errorf(codes.InvalidArgument, "document_id %q is reserved", documentID)
	}
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}
	p, _ := contexts.Principal(ctx)
	if len(perms) == 0 {
		perms = ownerDocumentPermissions(p.UserID)
	}
	perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
	if err := databases.ValidateGrantablePermissions(principal, perms, false); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	created, err := d.docDB.CreateDocument(ctx, projectID, databaseID, collectionID, databases.Document{
		ID:   documentID,
		Data: data,
	}, perms, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("create document: %w", err))
	}
	// adapter 已用 SystemPrincipal 读回；不再以调用方 principal 重读，
	// 避免文档权限不含调用方 read 时返回 403（数据已落库的半完成状态）。
	return &created, nil
}

func (d *Databases) ListDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	q databases.Query,
) ([]databases.Document, int64, string, error) {
	pid, principal, err := d.ensureCollectionForRead(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return nil, 0, "", err
	}
	list, err := d.docDB.ListDocuments(ctx, pid, databaseID, collectionID, q, principal)
	if err != nil {
		return nil, 0, "", shared.MapDocumentDBError(err)
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (d *Databases) GetDocument(ctx context.Context, projectID, databaseID, collectionID, documentID string) (*databases.Document, error) {
	pid, principal, err := d.ensureCollectionForRead(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	doc, err := d.docDB.GetDocument(ctx, pid, databaseID, collectionID, documentID, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(err)
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "document not found")
	}
	return doc, nil
}

// clientDocumentUpdateProtectedFields 是客户端 UpdateDocument 中禁止修改的敏感字段，
// 用户只能通过认证用例（ChangePassword/VerifyEmail 等）间接修改这些字段。
var clientDocumentUpdateProtectedFields = map[string]struct{}{
	"password_hash":  {},
	"email_verified": {},
	"labels":         {},
	"status":         {},
}

func (d *Databases) UpdateDocument(
	ctx context.Context,
	databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
	increment map[string]int64,
) (*databases.Document, error) {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 && len(perms) == 0 && len(increment) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data, permissions, or increment is required")
	}
	if len(perms) > 0 {
		perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
		if err := databases.ValidateGrantablePermissions(principal, perms, false); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	// 过滤敏感字段，客户端无法通过 UpdateDocument 直接修改认证/状态相关数据。
	filtered := make(map[string]any, len(data))
	for k, v := range data {
		if _, ok := clientDocumentUpdateProtectedFields[k]; ok {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 && len(perms) == 0 && len(increment) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied")
	}
	updated, err := d.docDB.UpdateDocument(ctx, projectID, databaseID, collectionID, databases.DocumentUpdate{
		Document:    databases.Document{ID: documentID, Data: filtered},
		Permissions: perms,
		Increment:   increment,
	}, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("update document: %w", err))
	}
	return &updated, nil
}

func (d *Databases) UpsertDocument(
	ctx context.Context,
	databaseID, collectionID, documentID string,
	data map[string]any,
	conflictColumns []string,
	perms []databases.Permission,
) (*databases.Document, error) {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}
	if len(conflictColumns) == 0 {
		return nil, status.Error(codes.InvalidArgument, "conflict_columns is required")
	}
	p, _ := contexts.Principal(ctx)
	if len(perms) == 0 {
		perms = ownerDocumentPermissions(p.UserID)
	}
	perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
	if err := databases.ValidateGrantablePermissions(principal, perms, false); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// 与 UpdateDocument 一致：客户端无法通过 UpsertDocument 直接修改
	// 认证/状态相关敏感字段。
	filtered := make(map[string]any, len(data))
	for k, v := range data {
		if _, ok := clientDocumentUpdateProtectedFields[k]; ok {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied")
	}
	upserted, err := d.docDB.UpsertDocument(ctx, projectID, databaseID, collectionID, databases.Document{
		ID:   documentID,
		Data: filtered,
	}, conflictColumns, perms, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("upsert document: %w", err))
	}
	return &upserted, nil
}

func (d *Databases) DeleteDocument(ctx context.Context, databaseID, collectionID, documentID string) error {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return err
	}
	return shared.MapDocumentDBError(d.docDB.DeleteDocument(ctx, projectID, databaseID, collectionID, documentID, principal))
}

func (d *Databases) CountDocuments(ctx context.Context, projectID, databaseID, collectionID string, queries []string) (int64, error) {
	pid, principal, err := d.ensureCollectionForRead(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return 0, err
	}
	count, err := d.docDB.CountDocuments(ctx, pid, databaseID, collectionID, queries, principal)
	return count, shared.MapDocumentDBError(err)
}

func ownerDocumentPermissions(userID string) []databases.Permission {
	userRole := fmt.Sprintf("user:%s", userID)
	return []databases.Permission{
		{Type: "read", Role: userRole},
		{Type: "update", Role: userRole},
		{Type: "delete", Role: userRole},
	}
}
