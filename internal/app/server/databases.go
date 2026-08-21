package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/torchwooddev/torchwood/internal/app/documents"
	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

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
	docs        *documents.Documents
}

func NewDatabases(projectRepo projects.Repository, docDB databases.DocumentDB) *Databases {
	return &Databases{projectRepo: projectRepo, docDB: docDB, docs: documents.New(docDB)}
}

func (d *Databases) documentsCore() *documents.Documents {
	if d.docs != nil {
		return d.docs
	}
	return documents.New(d.docDB)
}

func allowPrivilegedGrant(principal databases.Principal) bool {
	return principal.PlatformAdmin || principal.HasRole("keys")
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
	if err := shared.RejectExternalDatabaseID(id); err != nil {
		return err
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
	list, err := d.docDB.ListDatabases(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]databases.Collection, 0, len(list))
	for _, db := range list {
		if db.ID == ident.ProjectDataPlaneID {
			continue
		}
		out = append(out, db)
	}
	return out, nil
}

func (d *Databases) GetDatabase(ctx context.Context, projectID, databaseID string) (*databases.Collection, error) {
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return nil, err
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.GetDatabase(ctx, projectID, databaseID)
}

func (d *Databases) DeleteDatabase(ctx context.Context, projectID, databaseID string) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
		if err := d.validateAttribute(attr); err != nil {
			return err
		}
	}
	if len(perms) == 0 {
		perms = databases.DefaultCollectionPermissions()
	}
	return d.docDB.CreateCollection(ctx, projectID, databaseID, collectionID, name, attrs, idxs, perms, documentSecurity)
}

// validateAttribute 拒绝系统保留列（含 _version）以及 array=true。
// 物理列是标量，catalog 不得写入 IsArray=true。
func (d *Databases) validateAttribute(attr databases.Attribute) error {
	if _, ok := databases.ReservedAttributeKeys[attr.Key]; ok {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("attribute key %q is reserved", attr.Key))
	}
	if attr.Array {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("attribute %q: array is not supported", attr.Key))
	}
	return nil
}

func (d *Databases) ListCollections(ctx context.Context, projectID, databaseID string, q databases.ListQuery) ([]databases.Collection, int64, string, error) {
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return nil, 0, "", err
	}
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return nil, err
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return d.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
}

func (d *Databases) DeleteCollection(ctx context.Context, projectID, databaseID, collectionID string) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
		if err := databases.ValidateGrantablePermissions(principal, *patch.Permissions, allowPrivilegedGrant(principal)); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
	}
	return d.docDB.UpdateCollection(ctx, projectID, databaseID, collectionID, patch)
}

func (d *Databases) CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr databases.Attribute) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
	if err := d.validateAttribute(attr); err != nil {
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
	}
	if err := d.ValidateIdentifier(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return err
	}
	// 禁止直接写入系统集合（项目数据面 sentinel），先于 GetCollection 拦截
	// （避免泄露集合存在性，安全评审 C1 第 1 层）。对外 database_id 已拒 `_`，
	// 本守卫是 adapter 绕过时的纵深防御。
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
// groups/memberships/buckets/files 放行（docDB 权限过滤兜底）；
// users/sessions/identities 仅 PlatformAdmin 可读，其余主体直接拒绝。
func (d *Databases) ensureReadableCollection(ctx context.Context, projectID, databaseID, collectionID string, principal databases.Principal) error {
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return err
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

// redactSensitiveCollectionData 剔除项目数据面高敏系统集合文档中的敏感字段
// （专用 API 不公开的字段），空 data 允许；业务库同名集合不受影响。
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
	return d.documentsCore().CreateDocument(ctx, projectID, databaseID, collectionID, documentID, data, perms, principal, documents.WriteOptions{
		AllowPrivilegedGrant: allowPrivilegedGrant(principal),
	})
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
	docs, total, next, err := d.documentsCore().ListDocuments(ctx, projectID, databaseID, collectionID, q, principal)
	if err != nil {
		return nil, 0, "", err
	}
	for i := range docs {
		redactSensitiveCollectionData(projectID, databaseID, collectionID, &docs[i])
	}
	return docs, total, next, nil
}

func (d *Databases) GetDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureReadableCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	doc, err := d.documentsCore().GetDocument(ctx, projectID, databaseID, collectionID, documentID, principal)
	if err != nil {
		return nil, err
	}
	redactSensitiveCollectionData(projectID, databaseID, collectionID, doc)
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
	return d.documentsCore().UpdateDocument(ctx, projectID, databaseID, collectionID, documentID, data, perms, increment, principal, version, documents.WriteOptions{
		AllowPrivilegedGrant: allowPrivilegedGrant(principal),
	})
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
	return d.documentsCore().UpsertDocument(ctx, projectID, databaseID, collectionID, documentID, data, conflictColumns, perms, principal, documents.WriteOptions{
		AllowPrivilegedGrant: allowPrivilegedGrant(principal),
	})
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
	return d.documentsCore().BulkUpdateDocuments(ctx, projectID, databaseID, collectionID, documentIDs, data, perms, principal, documents.WriteOptions{
		AllowPrivilegedGrant: allowPrivilegedGrant(principal),
	})
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
	return d.documentsCore().BulkDeleteDocuments(ctx, projectID, databaseID, collectionID, documentIDs, principal)
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
	return d.documentsCore().DeleteDocument(ctx, projectID, databaseID, collectionID, documentID, principal, version)
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
	return d.documentsCore().CountDocuments(ctx, projectID, databaseID, collectionID, queries, principal)
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
