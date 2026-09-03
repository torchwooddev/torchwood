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

// 标识符长度上限（POC 期入口治理，封死 PG 63 字节截断把两个仅超长部分不同
// 的名字映射到同一物理对象的问题；redesign 阶段②逻辑/物理名解耦后收紧为
// collectionID ≤36 [a-z0-9-] 并服务端分配物理名，本组上限随之退役）。
const (
	// maxCollectionIDLen 约束物理表名（= collectionID），并为索引名
	// idx_<coll>_<id> / idx_<coll>_tenant_created 的前缀段留出预算。
	maxCollectionIDLen = 40
	// maxAttributeKeyLen 对齐物理列名 63 字节上限。
	maxAttributeKeyLen = 63
	// maxIndexIDLen 约束索引名后缀段（组合长度另见 validateIndexNameLen）。
	maxIndexIDLen = 40
)

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
	if err := d.validateCollectionID(collectionID); err != nil {
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
	for _, idx := range idxs {
		if err := d.ValidateIndex(idx); err != nil {
			return err
		}
		if err := validateIndexNameLen(collectionID, idx.ID); err != nil {
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
	if err := d.validateCollectionID(collectionID); err != nil {
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
	if err := d.validateCollectionID(collectionID); err != nil {
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
	if err := d.validateCollectionID(collectionID); err != nil {
		return status.Error(codes.InvalidArgument, "collection_id is required")
	}
	if err := d.ValidateIndex(idx); err != nil {
		return err
	}
	if err := validateIndexNameLen(collectionID, idx.ID); err != nil {
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
	if err := d.validateCollectionID(collectionID); err != nil {
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
	if err := d.validateCollectionID(collectionID); err != nil {
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
		perms = seedDocumentPermissions(principal)
		seeded = true
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

// seedDocumentPermissions 生成空 ACE 写入的创建者种子（CreateDocument /
// UpsertDocument 共用）：owner user 角色 → creatorSeedRole（keys/admin 等
// 常规凭证角色）→ __private__ 纯私有标记。返回的 perms 恒非空。
func seedDocumentPermissions(principal databases.Principal) []databases.Permission {
	role := ownerUserRole(principal)
	if role == "" {
		role = creatorSeedRole(principal)
	}
	if role != "" {
		return []databases.Permission{
			{Type: "read", Role: role},
			{Type: "update", Role: role},
			{Type: "delete", Role: role},
		}
	}
	// 无常规角色可绑定（如仅特权旁路的主体）：纯私有标记。
	return []databases.Permission{{Type: "read", Role: "__private__"}}
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
	// 空 ACE 种子与 CreateDocument 同语义（seedDocumentPermissions）：原实现
	// 对非 user 主体种 read:__private__，keys 主体 upsert 后自己都读不回；
	// 更新支还会把目标行 ACL 整体替换为 __private__，直接锁死既有文档。
	seeded := false
	if len(perms) == 0 {
		perms = seedDocumentPermissions(principal)
		seeded = true
	}
	return d.documentsCore().UpsertDocument(ctx, projectID, databaseID, collectionID, documentID, data, conflictColumns, perms, principal, documents.WriteOptions{
		AllowPrivilegedGrant: seeded || allowPrivilegedGrant(principal),
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

// ExecuteTransactions 是 Server 面的事务内核入口（redesign §4.8 Phase 1）：
// 逐 op 集合守卫、create/upsert 空 ACE 种子（与单文档 API 同语义）、grant
// 展开校验，随后交 infra 单事务执行器。
func (d *Databases) ExecuteTransactions(
	ctx context.Context,
	projectID, databaseID string,
	ops []databases.TransactionOp,
	mode databases.TransactionMode,
	principal databases.Principal,
) ([]databases.TransactionOpResult, error) {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return nil, err
	}
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ops is required")
	}
	if len(ops) > documents.MaxBulkOperations {
		return nil, status.Errorf(codes.InvalidArgument, "ops count %d exceeds maximum of %d", len(ops), documents.MaxBulkOperations)
	}
	if _, err := d.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	callerAllowed := allowPrivilegedGrant(principal)
	for i := range ops {
		op := &ops[i]
		switch op.Type {
		case databases.TransactionOpCreate, databases.TransactionOpUpdate, databases.TransactionOpUpsert, databases.TransactionOpDelete:
		default:
			return nil, status.Errorf(codes.InvalidArgument, "ops[%d]: invalid op type %q", i, op.Type)
		}
		if err := d.validateCollectionID(op.CollectionID); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "ops[%d]: invalid collection id %q", i, op.CollectionID)
		}
		if databases.IsSystemCollection(projectID, databaseID, op.CollectionID) {
			return nil, status.Errorf(codes.InvalidArgument, "ops[%d]: system collection %q is not writable via transactions", i, op.CollectionID)
		}
		if err := documents.ValidateDocumentPayload(op.Data); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "ops[%d]: %v", i, err)
		}
		// create/upsert 空 ACE 种子与单文档 API 同语义（防锁死；update/delete 的
		// 空 permissions 语义是"不变更文档 ACL"，不种子）。
		// grant 豁免严格 per-op（Phase 1 裁决③）：种子 op 仅豁免自身——种子是
		// 系统推导、非调用方授予意图；豁免外溢到其他 op 会放行显式越权授予。
		opAllowed := callerAllowed
		if len(op.Permissions) == 0 && (op.Type == databases.TransactionOpCreate || op.Type == databases.TransactionOpUpsert) {
			op.Permissions = seedDocumentPermissions(principal)
			opAllowed = true
		}
		perms, err := applyTxGrant(principal, op.Permissions, opAllowed)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "ops[%d]: %v", i, err)
		}
		op.Permissions = perms
	}
	return d.documentsCore().ExecuteTransactions(ctx, projectID, databaseID, ops, mode, principal)
}

// applyTxGrant 是 op 级 grant 展开（空列表跳过校验，与 update 语义一致）。
func applyTxGrant(principal databases.Principal, perms []databases.Permission, allowPrivileged bool) ([]databases.Permission, error) {
	if len(perms) == 0 {
		return nil, nil
	}
	perms, err := documents.ApplyGrant(principal, perms, allowPrivileged)
	if err != nil {
		return nil, err
	}
	return perms, nil
}

func (d *Databases) ValidateIdentifier(id string) error {
	if id == "" {
		return status.Error(codes.InvalidArgument, "identifier is required")
	}
	if len(id) > maxAttributeKeyLen {
		return status.Errorf(codes.InvalidArgument, "identifier %q exceeds maximum length of %d", id, maxAttributeKeyLen)
	}
	if !identifierRe.MatchString(id) {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("identifier %q must match %s", id, identifierRe.String()))
	}
	return nil
}

// validateCollectionID 在通用标识符校验之上叠加集合 ID 专用上限
//（物理表名 + 索引名前缀段预算）。
func (d *Databases) validateCollectionID(id string) error {
	if len(id) > maxCollectionIDLen {
		return status.Errorf(codes.InvalidArgument, "collection id %q exceeds maximum length of %d", id, maxCollectionIDLen)
	}
	return d.ValidateIdentifier(id)
}

// validateIndexNameLen 校验物理索引名 idx_<coll>_<id> 的拼接长度：静态上限
//（coll ≤40 + id ≤40）封不死组合（最长 85 字节 > PG 63），必须叠加本校验。
func validateIndexNameLen(collectionID, indexID string) error {
	if n := 4 + len(collectionID) + 1 + len(indexID); n > maxAttributeKeyLen {
		return status.Errorf(codes.InvalidArgument,
			"index name idx_%s_%s is %d bytes, exceeds the %d-byte identifier limit (shorten collection id or index id)",
			collectionID, indexID, n, maxAttributeKeyLen)
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
	if len(idx.ID) > maxIndexIDLen {
		return status.Errorf(codes.InvalidArgument, "index id %q exceeds maximum length of %d", idx.ID, maxIndexIDLen)
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
