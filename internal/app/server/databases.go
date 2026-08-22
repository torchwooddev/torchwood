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

func (d *Databases) ListDatabases(ctx context.Context, projectID string) ([]databases.Database, error) {
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	list, err := d.docDB.ListDatabases(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]databases.Database, 0, len(list))
	for _, db := range list {
		if db.ID == ident.ProjectDataPlaneID {
			continue
		}
		out = append(out, db)
	}
	return out, nil
}

func (d *Databases) GetDatabase(ctx context.Context, projectID, databaseID string) (*databases.Database, error) {
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
	// C1：Server 空文档 ACE 私有（不再回落到集合 read:any）。
	// WHY: 默认集合已去 read:any，但历史集合仍可能含它；空 ACE 若仍 docHasPerms=false 会对 guest 可读。
	// 修订（2026-08 回归）：占位 ACE 给创建者凭证角色保留读写删（与 user:
	// owner 分支同构）——原 read:__private__ 不匹配任何常规角色，keys 创建
	// 的文档自己都读不回，Server API 的创建→读改删往返整体断裂；guest/any
	// 仍被剔除，docHasPerms=true 依旧关闭集合回落，C1 目标不变。
	seeded := false
	if len(perms) == 0 {
		seeded = true
		switch role := ownerUserRole(principal); {
		case role != "":
			// 少见路径：Server 侧带用户身份，保持与 Client 相同的 owner ACE。
			perms = []databases.Permission{
				{Type: "read", Role: role},
				{Type: "update", Role: role},
				{Type: "delete", Role: role},
			}
		case creatorSeedRole(principal) != "":
			role := creatorSeedRole(principal)
			perms = []databases.Permission{
				{Type: "read", Role: role},
				{Type: "update", Role: role},
				{Type: "delete", Role: role},
			}
		default:
			// 无常规角色可绑定（如仅特权旁路的主体）：纯私有标记。
			perms = []databases.Permission{{Type: "read", Role: "__private__"}}
		}
	}
	// seeded 时 ACE 全部为系统推导（非调用方授予意图），不受授予者校验约束。
	return d.documentsCore().CreateDocument(ctx, projectID, databaseID, collectionID, documentID, data, perms, principal, documents.WriteOptions{
		AllowPrivilegedGrant: seeded || allowPrivilegedGrant(principal),
	})
}

// ownerUserRole 返回 principal 的首个 user:<id> 角色（owner ACE 语义），
// 无用户角色时返回空串。
func ownerUserRole(principal databases.Principal) string {
	for _, r := range principal.Roles {
		if strings.HasPrefix(r, "user:") {
			return r
		}
	}
	return ""
}

// creatorSeedRole 返回空 ACE 文档占位绑定用的创建者常规角色（首个非
// user: 前缀、非合成的角色，如 keys/admin）；找不到时返回空串。
func creatorSeedRole(principal databases.Principal) string {
	for _, r := range principal.Roles {
		switch r {
		case "", "any", "guest", "__system__", "__private__":
			continue
		}
		if strings.HasPrefix(r, "user:") {
			continue
		}
		return r
	}
	return ""
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
	return d.documentsCore().ListDocuments(ctx, projectID, databaseID, collectionID, q, principal)
}

func (d *Databases) GetDocument(
	ctx context.Context,
	projectID, databaseID, collectionID, documentID string,
	principal databases.Principal,
) (*databases.Document, error) {
	if err := d.ensureReadableCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return nil, err
	}
	return d.documentsCore().GetDocument(ctx, projectID, databaseID, collectionID, documentID, principal)
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
	if len(perms) == 0 {
		hasUserRole := false
		for _, r := range principal.Roles {
			if strings.HasPrefix(r, "user:") {
				hasUserRole = true
				break
			}
		}
		if hasUserRole {
			var userRole string
			for _, r := range principal.Roles {
				if strings.HasPrefix(r, "user:") {
					userRole = r
					break
				}
			}
			perms = []databases.Permission{
				{Type: "read", Role: userRole},
				{Type: "update", Role: userRole},
				{Type: "delete", Role: userRole},
			}
		} else {
			perms = []databases.Permission{{Type: "read", Role: "__private__"}}
		}
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
	q databases.Query,
	principal databases.Principal,
) (int64, error) {
	if err := d.ensureReadableCollection(ctx, projectID, databaseID, collectionID, principal); err != nil {
		return 0, err
	}
	return d.documentsCore().CountDocuments(ctx, projectID, databaseID, collectionID, q, principal)
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
