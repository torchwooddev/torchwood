package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// serverSystemCollections 是 Server Databases API 禁止直接读写的系统集合。
// 系统集合只能经专用服务（Users/Teams/Storage/Auth）访问，防止 databases scope
// 的 API key 直接操纵认证/团队/存储数据（安全评审 C1）。
var serverSystemCollections = map[string]struct{}{
	"users":       {},
	"sessions":    {},
	"identities":  {},
	"teams":       {},
	"memberships": {},
	"buckets":     {},
	"files":       {},
}

func isServerSystemCollection(collectionID string) bool {
	_, ok := serverSystemCollections[collectionID]
	return ok
}

type Databases struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
}

func NewDatabases(projectRepo projects.Repository, docDB databases.DocumentDB) *Databases {
	return &Databases{projectRepo: projectRepo, docDB: docDB}
}

func (d *Databases) resolveProject(ctx context.Context, projectID string) (*projects.Project, error) {
	p, err := d.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	return p, nil
}

func (d *Databases) CreateDatabase(ctx context.Context, projectID, id, name string) error {
	if name == "" {
		return status.Error(codes.InvalidArgument, "name is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.CreateDatabase(ctx, projectID, id, name)
}

func (d *Databases) ListDatabases(ctx context.Context, projectID string) ([]databases.Collection, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.ListDatabases(ctx, projectID)
}

func (d *Databases) GetDatabase(ctx context.Context, projectID, databaseID string) (*databases.Collection, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.GetDatabase(ctx, projectID, databaseID)
}

func (d *Databases) DeleteDatabase(ctx context.Context, projectID, databaseID string) error {
	// "default" 库承载全部系统集合，删除会破坏"项目存在 ⇒ schema 存在"不变式
	// （安全评审 M6 配套），禁止删除。
	if databaseID == "default" {
		return status.Error(codes.InvalidArgument, "default database cannot be deleted")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.DeleteDatabase(ctx, projectID, databaseID)
}

func (d *Databases) CreateCollection(ctx context.Context, projectID, databaseID, collectionID, name string, attrs []databases.Attribute, idxs []databases.Index, perms []databases.Permission, documentSecurity bool) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "id is required")
	}
	if name == "" {
		return status.Error(codes.InvalidArgument, "name is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if len(perms) == 0 {
		perms = databases.DefaultCollectionPermissions()
	}
	return d.docDB.CreateCollection(ctx, projectID, databaseID, collectionID, name, attrs, idxs, perms, documentSecurity)
}

func (d *Databases) ListCollections(ctx context.Context, projectID, databaseID string) ([]databases.Collection, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.ListCollections(ctx, projectID, databaseID)
}

func (d *Databases) GetCollection(ctx context.Context, projectID, databaseID, collectionID string) (*databases.Collection, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
}

func (d *Databases) DeleteCollection(ctx context.Context, projectID, databaseID, collectionID string) error {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.DeleteCollection(ctx, projectID, databaseID, collectionID)
}

func (d *Databases) UpdateCollection(ctx context.Context, projectID, databaseID, collectionID string, patch databases.CollectionPatch) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.UpdateCollection(ctx, projectID, databaseID, collectionID, patch)
}

func (d *Databases) CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr databases.Attribute) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if err := d.ValidateIdentifier(attr.Key); err != nil {
		return status.Error(codes.InvalidArgument, "key is required")
	}
	if err := d.ValidateAttributeType(attr.Type); err != nil {
		return err
	}
	attr.Type = strings.ToLower(attr.Type)
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.CreateAttribute(ctx, projectID, databaseID, collectionID, attr)
}

func (d *Databases) CreateIndex(ctx context.Context, projectID, databaseID, collectionID string, idx databases.Index) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if err := d.ValidateIndex(idx); err != nil {
		return err
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.CreateIndex(ctx, projectID, databaseID, collectionID, idx)
}

func (d *Databases) DeleteAttribute(ctx context.Context, projectID, databaseID, collectionID, key string) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.DeleteAttribute(ctx, projectID, databaseID, collectionID, key)
}

func (d *Databases) DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return d.docDB.DeleteIndex(ctx, projectID, databaseID, collectionID, indexID)
}

func (d *Databases) ensureCollection(ctx context.Context, projectID, databaseID, collectionID string, principal databases.Principal) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	// 禁止直接读写系统集合，先于 GetCollection 拦截（避免泄露集合存在性，
	// 安全评审 C1 第 1 层）。
	if isServerSystemCollection(collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	col, err := d.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return err
	}
	if col == nil {
		return status.Error(codes.NotFound, "collection not found")
	}
	if col.Disabled && !principal.IsSystem() && !principal.PlatformAdmin {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return nil
}

func (d *Databases) CreateDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}
	if err := databases.ValidateGrantablePermissions(principal, perms, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	doc := databases.Document{ID: documentID, Data: data}
	created, err := d.docDB.CreateDocument(ctx, projectID, databaseID, collectionID, doc, perms, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("create document: %w", err))
	}
	got, err := d.docDB.GetDocument(ctx, projectID, databaseID, collectionID, created.ID, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(err)
	}
	if got == nil {
		return nil, status.Error(codes.NotFound, "document not found after create")
	}
	return got, nil
}

func (d *Databases) ListDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	q databases.Query,
	principal databases.Principal,
) ([]databases.Document, int64, string, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, 0, "", err
	}
	list, err := d.docDB.ListDocuments(ctx, projectID, databaseID, collectionID, q, principal)
	if err != nil {
		return nil, 0, "", shared.MapDocumentDBError(err)
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (d *Databases) GetDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	doc, err := d.docDB.GetDocument(ctx, projectID, databaseID, collectionID, documentID, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(err)
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "document not found")
	}
	return doc, nil
}

func (d *Databases) UpdateDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
	increment map[string]int64,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	if len(data) == 0 && len(perms) == 0 && len(increment) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data, permissions, or increment is required")
	}
	if len(perms) > 0 {
		if err := databases.ValidateGrantablePermissions(principal, perms, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	if len(data) == 0 {
		data = map[string]any{}
	}
	updated, err := d.docDB.UpdateDocument(ctx, projectID, databaseID, collectionID, databases.DocumentUpdate{
		Document:    databases.Document{ID: documentID, Data: data},
		Permissions: perms,
		Increment:   increment,
	}, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("update document: %w", err))
	}
	return &updated, nil
}

func (d *Databases) BulkUpdateDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
) (int64, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return 0, err
	}
	if len(documentIDs) == 0 {
		return 0, status.Error(codes.InvalidArgument, "document_ids is required")
	}
	if len(perms) > 0 {
		if err := databases.ValidateGrantablePermissions(principal, perms, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
			return 0, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	n, err := d.docDB.BulkUpdateDocuments(ctx, projectID, databaseID, collectionID, documentIDs, data, perms, principal)
	return n, shared.MapDocumentDBError(err)
}

func (d *Databases) BulkDeleteDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	principal databases.Principal,
) (int64, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return 0, err
	}
	if len(documentIDs) == 0 {
		return 0, status.Error(codes.InvalidArgument, "document_ids is required")
	}
	n, err := d.docDB.BulkDeleteDocuments(ctx, projectID, databaseID, collectionID, documentIDs, principal)
	return n, shared.MapDocumentDBError(err)
}

func (d *Databases) DeleteDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
) error {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return err
	}
	return shared.MapDocumentDBError(d.docDB.DeleteDocument(ctx, projectID, databaseID, collectionID, documentID, principal))
}

func (d *Databases) CountDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	queries []string,
	principal databases.Principal,
) (int64, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return 0, err
	}
	count, err := d.docDB.CountDocuments(ctx, projectID, databaseID, collectionID, queries, principal)
	return count, shared.MapDocumentDBError(err)
}

func (d *Databases) MapAttributeType(t string) string {
	return strings.ToLower(t)
}

func (d *Databases) ValidateIdentifier(id string) error {
	if id == "" {
		return status.Error(codes.InvalidArgument, "identifier is required")
	}
	if !identifierRe.MatchString(id) {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("identifier %q must match %s", id, identifierRe.String()))
	}
	return nil
}

func (d *Databases) ValidateAttributeType(t string) error {
	if t == "" {
		return status.Error(codes.InvalidArgument, "type is required")
	}
	switch strings.ToLower(t) {
	case "string", "integer", "float", "boolean", "datetime", "email", "url", "json":
		return nil
	default:
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("invalid attribute type %q (allowed: string, integer, float, boolean, datetime, email, url, json)", t))
	}
}

func (d *Databases) ValidateIndex(idx databases.Index) error {
	if err := d.ValidateIdentifier(idx.ID); err != nil {
		return status.Error(codes.InvalidArgument, "id is required")
	}
	if idx.Type == "" {
		return status.Error(codes.InvalidArgument, "type is required")
	}
	switch strings.ToLower(idx.Type) {
	case "key", "unique", "fulltext":
	default:
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("invalid index type %q (allowed: key, unique, fulltext)", idx.Type))
	}
	if len(idx.Attributes) == 0 {
		return status.Error(codes.InvalidArgument, "attributes is required")
	}
	for _, attr := range idx.Attributes {
		if err := d.ValidateIdentifier(attr); err != nil {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid index attribute %q", attr))
		}
	}
	return nil
}
