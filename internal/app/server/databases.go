package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// maxBulkOperations 是 Bulk 写入单次条数上限（A4）。
const maxBulkOperations = 1000

// serverSensitiveCollectionFields 是高敏系统集合（users/sessions/identities）
// 经 Server Databases API 读取时的脱敏字段清单；专用 API 不公开这些字段。
var serverSensitiveCollectionFields = map[string][]string{
	"users":      {"password_hash", "pending_email", "phone", "phone_verified", "labels", "prefs"},
	"sessions":   {"secret_hash", "factors", "user_agent", "ip", "country"},
	"identities": {"provider_data", "provider_uid"},
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
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := ident.ValidateSchemaResourceID(id); err != nil {
		return err
	}
	if id == "default" {
		return status.Error(codes.InvalidArgument, "default database cannot be created")
	}
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
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
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
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
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
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	for _, attr := range attrs {
		if err := d.validateAttributeKey(attr.Key); err != nil {
			return err
		}
	}
	if len(perms) == 0 {
		perms = databases.DefaultCollectionPermissions()
	}
	return d.docDB.CreateCollection(ctx, projectID, databaseID, collectionID, name, attrs, idxs, perms, documentSecurity)
}

// validateAttributeKey 拒绝系统保留列（含 _version）作为用户属性。
func (d *Databases) validateAttributeKey(key string) error {
	if _, ok := databases.ReservedAttributeKeys[key]; ok {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("attribute key %q is reserved", key))
	}
	return nil
}

func (d *Databases) ListCollections(ctx context.Context, projectID, databaseID string, q databases.ListQuery) ([]databases.Collection, int64, string, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, 0, "", err
	}
	cols, meta, err := d.docDB.ListCollections(ctx, projectID, databaseID, q)
	if err != nil {
		return nil, 0, "", shared.MapDocumentDBError(err)
	}
	return cols, meta.TotalCount, meta.NextPageToken, nil
}

func (d *Databases) GetCollection(ctx context.Context, projectID, databaseID, collectionID string) (*databases.Collection, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
}

func (d *Databases) DeleteCollection(ctx context.Context, projectID, databaseID, collectionID string) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return d.docDB.DeleteCollection(ctx, projectID, databaseID, collectionID)
}

func (d *Databases) UpdateCollection(ctx context.Context, projectID, databaseID, collectionID string, patch databases.CollectionPatch, principal databases.Principal) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	if patch.Permissions != nil {
		if err := databases.ValidateGrantablePermissions(principal, *patch.Permissions, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
	}
	return d.docDB.UpdateCollection(ctx, projectID, databaseID, collectionID, patch)
}

func (d *Databases) CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr databases.Attribute) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
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
	if err := d.validateAttributeKey(attr.Key); err != nil {
		return err
	}
	attr.Type = strings.ToLower(attr.Type)
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return d.docDB.CreateAttribute(ctx, projectID, databaseID, collectionID, attr)
}

func (d *Databases) CreateIndex(ctx context.Context, projectID, databaseID, collectionID string, idx databases.Index) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
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
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return d.docDB.CreateIndex(ctx, projectID, databaseID, collectionID, idx)
}

func (d *Databases) DeleteAttribute(ctx context.Context, projectID, databaseID, collectionID, key string) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return d.docDB.DeleteAttribute(ctx, projectID, databaseID, collectionID, key)
}

func (d *Databases) DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return shared.MapDocumentDBError(databases.ErrPermissionDenied)
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
	// 禁止直接写入系统集合（仅限 default 库），先于 GetCollection 拦截
	// （避免泄露集合存在性，安全评审 C1 第 1 层）。
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
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

// ensureReadableCollection 是文档读路径（List/Get/Count）的集合校验：
// teams/memberships/buckets/files 放行（docDB 权限过滤兜底）；
// users/sessions/identities 仅 PlatformAdmin 可读，其余主体直接拒绝。
func (d *Databases) ensureReadableCollection(ctx context.Context, projectID, databaseID, collectionID string, principal databases.Principal) error {
	if err := d.ValidateIdentifier(databaseID); err != nil {
		return status.Error(codes.InvalidArgument, "database_id is required")
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		if databases.IsSensitiveSystemCollectionID(collectionID) && !principal.PlatformAdmin {
			return shared.MapDocumentDBError(databases.ErrPermissionDenied)
		}
		return nil
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

// redactSensitiveCollectionData 剔除 default 库高敏系统集合文档中的敏感字段
// （专用 API 不公开的字段），空 data 允许；自定义库同名集合不受影响。
func redactSensitiveCollectionData(projectID, databaseID, collectionID string, doc *databases.Document) {
	if !databases.IsSystemCollection(projectID, databaseID, collectionID) || !databases.IsSensitiveSystemCollectionID(collectionID) {
		return
	}
	fields, ok := serverSensitiveCollectionFields[collectionID]
	if !ok || doc == nil || len(doc.Data) == 0 {
		return
	}
	for _, f := range fields {
		delete(doc.Data, f)
	}
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
	perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
	if err := databases.ValidateGrantablePermissions(principal, perms, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	doc := databases.Document{ID: documentID, Data: data}
	// adapter 已用 SystemPrincipal 读回完整文档（含审计列）；此处不再以调用方
	// principal 重读，避免权限不含调用方时返回 403（数据已落库的半完成状态）。
	created, err := d.docDB.CreateDocument(ctx, projectID, databaseID, collectionID, doc, perms, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(fmt.Errorf("create document: %w", err))
	}
	return &created, nil
}

func (d *Databases) ListDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	q databases.Query,
	principal databases.Principal,
) ([]databases.Document, int64, string, error) {
	if err := d.ensureReadableCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, 0, "", err
	}
	list, err := d.docDB.ListDocuments(ctx, projectID, databaseID, collectionID, q, principal)
	if err != nil {
		return nil, 0, "", shared.MapDocumentDBError(err)
	}
	for i := range list.Documents {
		redactSensitiveCollectionData(projectID, databaseID, collectionID, &list.Documents[i])
		// 系统集合恒无 _version：读路径归一为 0（契约：Document.version 系统集合为 0）。
		if databases.IsSystemCollection(projectID, databaseID, collectionID) {
			list.Documents[i].Version = 0
		}
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (d *Databases) GetDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureReadableCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	doc, err := d.docDB.GetDocument(ctx, projectID, databaseID, collectionID, documentID, principal)
	if err != nil {
		return nil, shared.MapDocumentDBError(err)
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "document not found")
	}
	redactSensitiveCollectionData(projectID, databaseID, collectionID, doc)
	// 系统集合恒无 _version：读路径归一为 0（契约：Document.version 系统集合为 0）。
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		doc.Version = 0
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
	version *int64,
) (*databases.Document, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	if err := shared.UpdateDocumentVersionRequired(version); err != nil {
		return nil, err
	}
	if len(data) == 0 && len(perms) == 0 && len(increment) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data, permissions, or increment is required")
	}
	if len(perms) > 0 {
		perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
		if err := databases.ValidateGrantablePermissions(principal, perms, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
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

func (d *Databases) UpsertDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	data map[string]any,
	conflictColumns []string,
	perms []databases.Permission,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}
	if len(conflictColumns) == 0 {
		return nil, status.Error(codes.InvalidArgument, "conflict_columns is required")
	}
	perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
	if err := databases.ValidateGrantablePermissions(principal, perms, principal.PlatformAdmin || principal.HasRole("keys")); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
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
	if len(documentIDs) > maxBulkOperations {
		return 0, status.Error(codes.InvalidArgument, fmt.Sprintf("document_ids exceeds maximum of %d", maxBulkOperations))
	}
	if len(perms) > 0 {
		perms = databases.ExpandPermissionTemplates(perms, principal.Roles)
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
	if len(documentIDs) > maxBulkOperations {
		return 0, status.Error(codes.InvalidArgument, fmt.Sprintf("document_ids exceeds maximum of %d", maxBulkOperations))
	}
	n, err := d.docDB.BulkDeleteDocuments(ctx, projectID, databaseID, collectionID, documentIDs, principal)
	return n, shared.MapDocumentDBError(err)
}

func (d *Databases) DeleteDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
	version *int64,
) error {
	if err := d.ensureCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return err
	}
	if err := shared.UpdateDocumentVersionRequired(version); err != nil {
		return err
	}
	return shared.MapDocumentDBError(d.docDB.DeleteDocument(ctx, projectID, databaseID, collectionID, documentID, databases.DeleteOptions{ExpectedVersion: *version}, principal))
}

func (d *Databases) CountDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	queries []string,
	principal databases.Principal,
) (int64, error) {
	if err := d.ensureReadableCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
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
