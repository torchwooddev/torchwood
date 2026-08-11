package documentdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/query"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrDuplicateKey re-exports the domain duplicate-key error.
var ErrDuplicateKey = databases.ErrDuplicateKey

// ErrPermissionDenied re-exports the domain permission error.
var ErrPermissionDenied = databases.ErrPermissionDenied

var safeNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var docIDRe = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`)

// systemCollectionsWriteProtected 是禁止非系统主体直接写入的系统集合（纵深防御，
// 正常情况下客户端 API 已在用例层拦截）。仅对 default 库生效，
// 自定义数据库中的同名集合不受影响。
var systemCollectionsWriteProtected = map[string]struct{}{
	"users":      {},
	"sessions":   {},
	"identities": {},
}

func isWriteProtectedSystemCollection(databaseID, collectionID string) bool {
	if databaseID != "default" {
		return false
	}
	_, ok := systemCollectionsWriteProtected[collectionID]
	return ok
}

const maxQueryLimit = 100

// maxQueryOffset 是 offset 深翻页上限，超过则拒绝（防 10^9 量级 offset 拖慢查询）。
const maxQueryOffset = 10000

// A2 输入上限：queries 条数 / 单条查询串长度 / equal 多值个数。
const maxQueryCount = 100
const maxQueryStringLen = 4096
const maxFilterValues = 1000

type postgresDocumentDB struct {
	db *clients.Database

	// in-process caches keyed by projectID; safe for concurrent use.
	internalIDCache sync.Map // projectID -> int64
	bootstrapCache  sync.Map // projectID -> struct{} (system collections ensured)
	// keysPermsCleaned 标记已完成"keys 角色系统集合写权限收窄"清理的项目
	// （安全评审 C1 第 3 层 / M2 存量迁移）；进程重启后重复执行 DELETE/UPDATE 幂等无害。
	keysPermsCleaned sync.Map // projectID -> struct{}
}

func NewPostgresDocumentDB(db *clients.Database) databases.DocumentDB {
	return &postgresDocumentDB{db: db}
}

func (p *postgresDocumentDB) conn(ctx context.Context) bun.IDB {
	return p.db.Conn(ctx)
}

func (p *postgresDocumentDB) CreateDatabase(ctx context.Context, projectID, id, name string) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, id)
	if _, err := p.db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema))); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := p.ensurePermsTable(ctx, schema); err != nil {
		return err
	}
	m := &model.DocumentDatabase{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err = p.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (p *postgresDocumentDB) GetDatabase(ctx context.Context, projectID, id string) (*databases.Collection, error) {
	m := new(model.DocumentDatabase)
	err := p.db.NewSelect().Model(m).Where("project_id = ? AND id = ?", projectID, id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &databases.Collection{
		ID:        m.ID,
		ProjectID: m.ProjectID,
		Name:      m.Name,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (p *postgresDocumentDB) ListDatabases(ctx context.Context, projectID string) ([]databases.Collection, error) {
	var ms []model.DocumentDatabase
	err := p.db.NewSelect().Model(&ms).Where("project_id = ?", projectID).Order("created_at DESC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]databases.Collection, len(ms))
	for i := range ms {
		out[i] = databases.Collection{
			ID:        ms[i].ID,
			ProjectID: ms[i].ProjectID,
			Name:      ms[i].Name,
			CreatedAt: ms[i].CreatedAt,
			UpdatedAt: ms[i].UpdatedAt,
		}
	}
	return out, nil
}

func (p *postgresDocumentDB) DeleteDatabase(ctx context.Context, projectID, id string) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, id)
	if _, err := p.db.DB.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, quoteIdent(schema))); err != nil {
		return err
	}
	_, err = p.db.NewDelete().Model((*model.DocumentDatabase)(nil)).Where("id = ? AND project_id = ?", id, projectID).Exec(ctx)
	return err
}

func (p *postgresDocumentDB) CreateCollection(ctx context.Context, projectID, databaseID, collectionID, name string, attrs []databases.Attribute, idxs []databases.Index, perms []databases.Permission, documentSecurity bool) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	if err := p.ensureSchemaAndPerms(ctx, schema); err != nil {
		return err
	}

	if err := p.createCollectionTable(ctx, schema, collectionID, internalID, attrs); err != nil {
		return err
	}
	for _, idx := range idxs {
		if err := p.createCollectionIndex(ctx, schema, collectionID, idx); err != nil {
			return err
		}
	}

	return p.createCollectionMetadata(ctx, projectID, databaseID, collectionID, name, attrs, idxs, perms, documentSecurity)
}

func (p *postgresDocumentDB) GetCollection(ctx context.Context, projectID, databaseID, collectionID string) (*databases.Collection, error) {
	m := new(model.DocumentCollection)
	err := p.conn(ctx).NewSelect().Model(m).
		Where("project_id = ? AND database_id = ? AND id = ?", projectID, databaseID, collectionID).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p.mapCollection(ctx, m)
}

func (p *postgresDocumentDB) ListCollections(ctx context.Context, projectID, databaseID string, q databases.ListQuery) ([]databases.Collection, databases.ListMeta, error) {
	pageSize := int(q.PageSize)
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > maxQueryLimit {
		pageSize = maxQueryLimit
	}
	offset := 0
	if q.PageToken != "" {
		off, err := crud.DecodePageToken(q.PageToken)
		if err != nil {
			return nil, databases.ListMeta{}, err
		}
		offset = off
	}

	var total int64
	count, err := p.db.NewSelect().Model((*model.DocumentCollection)(nil)).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).Count(ctx)
	if err != nil {
		return nil, databases.ListMeta{}, err
	}
	total = int64(count)

	var ms []model.DocumentCollection
	err = p.db.NewSelect().Model(&ms).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).
		Order("created_at DESC").
		Limit(pageSize).Offset(offset).Scan(ctx)
	if err != nil {
		return nil, databases.ListMeta{}, err
	}
	if len(ms) == 0 {
		return []databases.Collection{}, databases.ListMeta{TotalCount: total}, nil
	}

	collectionIDs := make([]string, 0, len(ms))
	for i := range ms {
		collectionIDs = append(collectionIDs, ms[i].ID)
	}

	var allAttrs []model.DocumentAttribute
	if err := p.conn(ctx).NewSelect().Model(&allAttrs).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).
		Where("collection_id IN (?)", bun.In(collectionIDs)).
		Scan(ctx); err != nil {
		return nil, databases.ListMeta{}, err
	}
	attrsByColl := make(map[string][]model.DocumentAttribute, len(ms))
	for i := range allAttrs {
		attrsByColl[allAttrs[i].CollectionID] = append(attrsByColl[allAttrs[i].CollectionID], allAttrs[i])
	}

	var allIdxs []model.DocumentIndex
	if err := p.conn(ctx).NewSelect().Model(&allIdxs).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).
		Where("collection_id IN (?)", bun.In(collectionIDs)).
		Scan(ctx); err != nil {
		return nil, databases.ListMeta{}, err
	}
	idxsByColl := make(map[string][]model.DocumentIndex, len(ms))
	for i := range allIdxs {
		idxsByColl[allIdxs[i].CollectionID] = append(idxsByColl[allIdxs[i].CollectionID], allIdxs[i])
	}

	out := make([]databases.Collection, len(ms))
	for i := range ms {
		c, err := mapCollectionRow(&ms[i], attrsByColl[ms[i].ID], idxsByColl[ms[i].ID])
		if err != nil {
			return nil, databases.ListMeta{}, err
		}
		out[i] = *c
	}
	meta := databases.ListMeta{TotalCount: total}
	if offset+len(ms) < int(total) {
		meta.NextPageToken = crud.EncodePageToken(offset + len(ms))
	}
	return out, meta, nil
}

func (p *postgresDocumentDB) DeleteCollection(ctx context.Context, projectID, databaseID, collectionID string) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	if _, err := p.db.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE _tenant = ? AND _collection = ?`, permsTableName(schema)), internalID, collectionID); err != nil {
		return err
	}
	if _, err := p.db.DB.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, tableName(schema, collectionID))); err != nil {
		return err
	}
	_, err = p.db.NewDelete().Model((*model.DocumentCollection)(nil)).
		Where("project_id = ? AND database_id = ? AND id = ?", projectID, databaseID, collectionID).Exec(ctx)
	return err
}

func (p *postgresDocumentDB) UpdateCollection(ctx context.Context, projectID, databaseID, collectionID string, patch databases.CollectionPatch) error {
	if _, err := p.resolveInternalID(ctx, projectID); err != nil {
		return err
	}
	if patch.Permissions != nil {
		if err := p.setCollectionPermissions(ctx, projectID, databaseID, collectionID, *patch.Permissions); err != nil {
			return err
		}
	}
	var sets []string
	var args []any
	if patch.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, patch.Name)
	}
	if patch.DocumentSecurity != nil {
		sets = append(sets, "document_security = ?")
		args = append(args, *patch.DocumentSecurity)
	}
	if patch.Disabled != nil {
		sets = append(sets, "disabled = ?")
		args = append(args, *patch.Disabled)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now(), projectID, databaseID, collectionID)
	_, err := p.conn(ctx).ExecContext(ctx,
		`UPDATE document_collections SET `+strings.Join(sets, ", ")+` WHERE project_id = ? AND database_id = ? AND id = ?`,
		args...,
	)
	return err
}

func (p *postgresDocumentDB) CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr databases.Attribute) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	colSQL, err := attributeColumnSQL(attr)
	if err != nil {
		return err
	}
	if _, err := p.db.DB.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s`, tableName(schema, collectionID), colSQL)); err != nil {
		return err
	}
	m := &model.DocumentAttribute{
		ID:           attr.ID,
		CollectionID: collectionID,
		DatabaseID:   databaseID,
		ProjectID:    projectID,
		Key:          attr.Key,
		Type:         attr.Type,
		Required:     attr.Required,
		IsArray:      attr.Array,
		CreatedAt:    time.Now(),
	}
	if attr.Size > 0 {
		m.Size = &attr.Size
	}
	_, err = p.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (p *postgresDocumentDB) CreateIndex(ctx context.Context, projectID, databaseID, collectionID string, idx databases.Index) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	if err := p.createCollectionIndex(ctx, schema, collectionID, idx); err != nil {
		return err
	}
	m := &model.DocumentIndex{
		ID:           idx.ID,
		CollectionID: collectionID,
		DatabaseID:   databaseID,
		ProjectID:    projectID,
		Type:         idx.Type,
		Attributes:   idx.Attributes,
		Orders:       idx.Orders,
		CreatedAt:    time.Now(),
	}
	_, err = p.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (p *postgresDocumentDB) CreateDocument(ctx context.Context, projectID, databaseID, collectionID string, doc databases.Document, perms []databases.Permission, principal databases.Principal) (databases.Document, error) {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return doc, err
	}
	if doc.ID == "" {
		doc.ID = idgen.UUID().String()
	}
	schema := schemaName(internalID, databaseID)
	tbl := tableName(schema, collectionID)

	// Check collection-level "create" permission before inserting.
	if !principal.IsSystem() {
		if isWriteProtectedSystemCollection(databaseID, collectionID) {
			return doc, ErrPermissionDenied
		}
		coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return doc, err
		}
		if err := p.ensureCollectionAccessible(coll, principal); err != nil {
			return doc, err
		}
		if coll != nil && !databases.CollectionAllows(coll.Permissions, "create", databases.ExpandPermissionRoles(principal.Roles)) {
			return doc, ErrPermissionDenied
		}
	}

	columns, placeholders, args := buildInsertParts(doc)
	createdBy := userIDFromPrincipal(principal)
	if createdBy != "" {
		if columns != "" {
			columns += ", "
			placeholders += ", "
		}
		columns += quoteIdent("_created_by") + ", " + quoteIdent("_updated_by")
		placeholders += "?, ?"
		args = append(args, createdBy, createdBy)
	}
	args = append([]any{doc.ID}, args...)
	allPlaceholders := "?"
	if columns != "" {
		allPlaceholders = "?, " + placeholders
		columns = ", " + columns
	}
	sql := fmt.Sprintf(`INSERT INTO %s (_id%s) VALUES (%s)`, tbl, columns, allPlaceholders)
	if _, err := p.conn(ctx).ExecContext(ctx, sql, args...); err != nil {
		if isUniqueViolation(err) {
			return doc, fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
		}
		return doc, fmt.Errorf("insert document: %w", err)
	}
	if err := p.setPermissions(ctx, schema, collectionID, doc.ID, internalID, perms); err != nil {
		return doc, err
	}
	created, err := p.GetDocument(ctx, projectID, databaseID, collectionID, doc.ID, databases.SystemPrincipal)
	if err != nil {
		return doc, err
	}
	if created == nil {
		return doc, errors.New("document not found after insert")
	}
	return *created, nil
}

// UpsertDocument inserts doc, or when a row already matches conflictColumns
// (which must correspond to a unique index on the collection table), updates
// its data, _updated_at, _updated_by and replaces document permissions with
// perms (Appwrite-style replace semantics). The insert and update are
// performed atomically by a single INSERT ... ON CONFLICT (...) DO UPDATE SET
// statement.
func (p *postgresDocumentDB) UpsertDocument(ctx context.Context, projectID, databaseID, collectionID string, doc databases.Document, conflictColumns []string, perms []databases.Permission, principal databases.Principal) (databases.Document, error) {
	if err := validateDocID(doc.ID); err != nil {
		return doc, err
	}
	if len(conflictColumns) == 0 {
		return doc, status.Error(codes.InvalidArgument, "conflict columns are required")
	}
	conflictCols := make([]string, 0, len(conflictColumns))
	for _, col := range conflictColumns {
		if !safeNameRe.MatchString(col) || strings.HasPrefix(col, "_") {
			return doc, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid conflict column: %s", col))
		}
		conflictCols = append(conflictCols, quoteIdent(col))
	}
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return doc, err
	}
	schema := schemaName(internalID, databaseID)
	tbl := tableName(schema, collectionID)

	if !principal.IsSystem() {
		if isWriteProtectedSystemCollection(databaseID, collectionID) {
			return doc, ErrPermissionDenied
		}
		coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return doc, err
		}
		if err := p.ensureCollectionAccessible(coll, principal); err != nil {
			return doc, err
		}
		// 已存在文档（行存在）→ 按 UpdateDocument 语义做文档级 update 检查；
		// 否则为插入路径 → 以集合级 create 权限为准（CreateDocument 语义）。
		var exists bool
		if err := p.conn(ctx).QueryRowContext(ctx, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE _id = ? AND _tenant = ?)`, tbl), doc.ID, internalID).Scan(&exists); err != nil {
			return doc, err
		}
		if exists {
			if err := p.checkDocumentPermission(ctx, projectID, schema, collectionID, doc.ID, internalID, "update", principal); err != nil {
				return doc, err
			}
		} else if coll != nil && !databases.CollectionAllows(coll.Permissions, "create", databases.ExpandPermissionRoles(principal.Roles)) {
			return doc, ErrPermissionDenied
		}
	}

	// INSERT 部分（含 _created_by/_updated_by 审计列，与 CreateDocument 一致）。
	columns, placeholders, args := buildInsertParts(doc)
	createdBy := userIDFromPrincipal(principal)
	if createdBy != "" {
		if columns != "" {
			columns += ", "
			placeholders += ", "
		}
		columns += quoteIdent("_created_by") + ", " + quoteIdent("_updated_by")
		placeholders += "?, ?"
		args = append(args, createdBy, createdBy)
	}
	args = append([]any{doc.ID}, args...)
	allPlaceholders := "?"
	if columns != "" {
		allPlaceholders = "?, " + placeholders
		columns = ", " + columns
	}
	// ON CONFLICT 的 SET 部分（含 _updated_at/_updated_by 更新，与 UpdateDocument 一致）。
	setParts, setArgs := buildUpdateParts(doc, userIDFromPrincipal(principal))
	if len(setParts) == 0 {
		return doc, status.Error(codes.InvalidArgument, "no fields to upsert")
	}
	args = append(args, setArgs...)
	sql := fmt.Sprintf(`INSERT INTO %s (_id%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s`,
		tbl, columns, allPlaceholders, strings.Join(conflictCols, ", "), strings.Join(setParts, ", "))
	if _, err := p.conn(ctx).ExecContext(ctx, sql, args...); err != nil {
		if isUniqueViolation(err) {
			return doc, fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
		}
		return doc, fmt.Errorf("upsert document: %w", err)
	}
	// 文档级权限替换语义（与 UpdateDocument 一致）：非空时先清后写。
	if len(perms) > 0 {
		if err := p.clearPermissions(ctx, schema, collectionID, doc.ID, internalID); err != nil {
			return doc, err
		}
		if err := p.setPermissions(ctx, schema, collectionID, doc.ID, internalID, perms); err != nil {
			return doc, err
		}
	}
	upserted, err := p.GetDocument(ctx, projectID, databaseID, collectionID, doc.ID, databases.SystemPrincipal)
	if err != nil {
		return doc, err
	}
	if upserted == nil {
		return doc, errors.New("document not found after upsert")
	}
	return *upserted, nil
}

func (p *postgresDocumentDB) GetDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, principal databases.Principal) (*databases.Document, error) {
	if err := validateDocID(docID); err != nil {
		return nil, err
	}
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	schema := schemaName(internalID, databaseID)
	row := p.conn(ctx).QueryRowContext(ctx, fmt.Sprintf(`SELECT to_jsonb(d.*) AS doc FROM %s d WHERE d._id = ? AND d._tenant = ?`, tableName(schema, collectionID)), docID, internalID)
	doc, err := scanDocumentJSON(row)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, nil
	}
	if err := p.checkDocumentPermission(ctx, projectID, schema, collectionID, docID, internalID, "read", principal); err != nil {
		return nil, err
	}
	if err := p.attachDocumentPermissions(ctx, schema, collectionID, internalID, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (p *postgresDocumentDB) UpdateDocument(ctx context.Context, projectID, databaseID, collectionID string, update databases.DocumentUpdate, principal databases.Principal) (databases.Document, error) {
	doc := update.Document
	if err := validateDocID(doc.ID); err != nil {
		return doc, err
	}
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return doc, err
	}
	schema := schemaName(internalID, databaseID)
	// 非 System 且非文档 owner（user:<id> 匹配）时，禁止写入写保护系统集合（纵深防御，
	// 与 CreateDocument/DeleteDocument 对齐，安全评审 C1 第 2 层）。
	// owner 例外：end-user 自助路径（UpdateAccount/UpdatePrefs）以 user:<id> 角色更新自己的 users 文档。
	if !principal.IsSystem() &&
		!principal.HasRole(fmt.Sprintf("user:%s", doc.ID)) &&
		isWriteProtectedSystemCollection(databaseID, collectionID) {
		return doc, ErrPermissionDenied
	}
	// D3：UpdateDocument 仅检查 update 权限，不再强制 read 预检
	// （对齐 Appwrite/Supabase：update 策略独立于 select 策略；B1 文档级优先下
	// "仅持 update 权限"的文档对持权者可用）。
	if err := p.checkDocumentPermission(ctx, projectID, schema, collectionID, doc.ID, internalID, "update", principal); err != nil {
		return doc, err
	}
	tbl := tableName(schema, collectionID)
	setParts, args := buildUpdateParts(doc, userIDFromPrincipal(principal))
	incParts, incArgs := buildIncrementParts(update.Increment)
	setParts = append(setParts, incParts...)
	args = append(args, incArgs...)
	if len(setParts) == 0 && len(update.Permissions) == 0 {
		return doc, fmt.Errorf("%w", databases.ErrNoFieldsToUpdate)
	}
	if len(setParts) > 0 {
		args = append(args, doc.ID, internalID)
		sql := fmt.Sprintf(`UPDATE %s SET %s WHERE _id = ? AND _tenant = ?`, tbl, strings.Join(setParts, ", "))
		if _, err := p.conn(ctx).ExecContext(ctx, sql, args...); err != nil {
			if isUniqueViolation(err) {
				return doc, fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
			}
			return doc, fmt.Errorf("update document: %w", err)
		}
	}
	if len(update.Permissions) > 0 {
		if err := p.clearPermissions(ctx, schema, collectionID, doc.ID, internalID); err != nil {
			return doc, err
		}
		if err := p.setPermissions(ctx, schema, collectionID, doc.ID, internalID, update.Permissions); err != nil {
			return doc, err
		}
	}
	// D5：尾随读回用 SystemPrincipal（与 CreateDocument 一致）——B1 下把文档权限
	// 改成不含自己 read 的集合后数据已提交，若仍以调用方 principal 读回会返回
	// PermissionDenied（半完成状态）。
	updated, err := p.GetDocument(ctx, projectID, databaseID, collectionID, doc.ID, databases.SystemPrincipal)
	if err != nil {
		return doc, err
	}
	if updated == nil {
		return doc, errors.New("document not found after update")
	}
	return *updated, nil
}

func (p *postgresDocumentDB) DeleteDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, principal databases.Principal) error {
	if err := validateDocID(docID); err != nil {
		return err
	}
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	if !principal.IsSystem() && isWriteProtectedSystemCollection(databaseID, collectionID) {
		return ErrPermissionDenied
	}
	if err := p.checkDocumentPermission(ctx, projectID, schema, collectionID, docID, internalID, "delete", principal); err != nil {
		return err
	}
	if err := p.clearPermissions(ctx, schema, collectionID, docID, internalID); err != nil {
		return err
	}
	_, err = p.conn(ctx).ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE _id = ? AND _tenant = ?`, tableName(schema, collectionID)), docID, internalID)
	return err
}

func (p *postgresDocumentDB) ListDocuments(ctx context.Context, projectID, databaseID, collectionID string, q databases.Query, principal databases.Principal) (*databases.DocumentList, error) {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := validateQueryInput(q.Queries); err != nil {
		return nil, err
	}
	parsed, err := query.ParseMany(q.Queries)
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	schema := schemaName(internalID, databaseID)
	tbl := tableName(schema, collectionID)

	// 非 System 路径显式获取集合一次（coll==nil → NotFound，行为从 403 收紧为 404），
	// 复用给权限过滤与字段白名单校验；System 信任路径零额外查询（跳过白名单）。
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	var coll *databases.Collection
	if !principal.IsSystem() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return nil, err
		}
		if coll == nil {
			return nil, status.Error(codes.NotFound, "collection not found")
		}
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.IsSystem() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return nil, err
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}

	if coll != nil {
		if err := validateQueryFields(parsed, coll, collectionID, isSystem); err != nil {
			return nil, err
		}
	}

	filterWhere, filterArgs, orderSQL, err := buildAppwriteQuery(parsed)
	if err != nil {
		return nil, err
	}
	if filterWhere != "" {
		whereParts = append(whereParts, filterWhere)
		args = append(args, filterArgs...)
	}

	limit := parsed.Limit
	if limit == 0 {
		limit = int(q.PageSize)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	offset := parsed.Offset
	if q.PageToken != "" {
		if off, err := crud.DecodePageToken(q.PageToken); err == nil {
			offset = int(off)
		}
	}
	if offset > maxQueryOffset {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("offset exceeds maximum of %d", maxQueryOffset))
	}

	cursor := ""
	cursorKind := ""
	if parsed.CursorAfter != "" {
		cursor, cursorKind = parsed.CursorAfter, "after"
	} else if parsed.CursorBefore != "" {
		cursor, cursorKind = parsed.CursorBefore, "before"
	}
	if cursor != "" {
		// cursor 与 offset 同时传时 cursor 优先，offset 恒 0
		offset = 0
		// 排序键与方向：仅取 Orders[0]，无显式排序则默认 _created_at DESC
		sortField := "_created_at"
		sortDir := "DESC"
		if len(parsed.Orders) > 0 {
			sortField = mapQueryField(parsed.Orders[0].Attribute)
			if parsed.Orders[0].Desc {
				sortDir = "DESC"
			} else {
				sortDir = "ASC"
			}
		}
		// 排序字段必须显式校验（不能沿用 ORDER 路径的静默跳过）
		if !safeNameRe.MatchString(sortField) {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid order field: %s", parsed.Orders[0].Attribute))
		}
		if err := validateDocID(cursor); err != nil {
			return nil, err
		}
		var cursorValue any
		err := p.conn(ctx).QueryRowContext(ctx,
			fmt.Sprintf(`SELECT %s FROM %s WHERE _id = ? AND _tenant = ?`, quoteIdent(sortField), tbl),
			cursor, internalID,
		).Scan(&cursorValue)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, status.Error(codes.InvalidArgument, "cursor document not found")
			}
			return nil, err
		}
		op := ">"
		if (sortDir == "ASC" && cursorKind == "before") || (sortDir == "DESC" && cursorKind == "after") {
			op = "<"
		}
		whereParts = append(whereParts, fmt.Sprintf(`(d.%s, d._id) %s (?, ?)`, quoteIdent(sortField), op))
		args = append(args, cursorValue, cursor)
		// cursor 模式下 ORDER BY 必须与谓词同构
		orderSQL = fmt.Sprintf(`ORDER BY d.%s %s, d._id %s`, quoteIdent(sortField), sortDir, sortDir)
	}

	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s d WHERE %s`, tbl, strings.Join(whereParts, " AND "))
	var total int64
	if err := p.conn(ctx).QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	querySQL := fmt.Sprintf(`SELECT to_jsonb(d.*) AS doc FROM %s d WHERE %s %s LIMIT ? OFFSET ?`, tbl, strings.Join(whereParts, " AND "), orderSQL)
	args = append(args, limit, offset)

	rows, err := p.conn(ctx).QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []databases.Document
	for rows.Next() {
		doc, err := scanDocumentJSON(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(parsed.Selects) > 0 {
		selected := make(map[string]struct{}, len(parsed.Selects))
		for _, s := range parsed.Selects {
			selected[mapQueryField(s)] = struct{}{}
		}
		for i := range docs {
			for k := range docs[i].Data {
				if _, ok := selected[k]; !ok {
					delete(docs[i].Data, k)
				}
			}
		}
	}

	next := ""
	if len(docs) > 0 && int64(offset+len(docs)) < total {
		next = crud.EncodePageToken(offset + len(docs))
	}
	return &databases.DocumentList{
		Documents:     docs,
		TotalCount:    total,
		NextPageToken: next,
	}, nil
}

func (p *postgresDocumentDB) CountDocuments(ctx context.Context, projectID, databaseID, collectionID string, queries []string, principal databases.Principal) (int64, error) {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if err := validateQueryInput(queries); err != nil {
		return 0, err
	}
	parsed, err := query.ParseMany(queries)
	if err != nil {
		return 0, fmt.Errorf("invalid query: %w", err)
	}
	if parsed.Offset > maxQueryOffset {
		return 0, status.Error(codes.InvalidArgument, fmt.Sprintf("offset exceeds maximum of %d", maxQueryOffset))
	}
	schema := schemaName(internalID, databaseID)
	tbl := tableName(schema, collectionID)

	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	var coll *databases.Collection
	if !principal.IsSystem() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return 0, err
		}
		if coll == nil {
			return 0, status.Error(codes.NotFound, "collection not found")
		}
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.IsSystem() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return 0, err
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}
	if coll != nil {
		if err := validateQueryFields(parsed, coll, collectionID, isSystem); err != nil {
			return 0, err
		}
	}
	filterWhere, filterArgs, _, err := buildAppwriteQuery(parsed)
	if err != nil {
		return 0, err
	}
	if filterWhere != "" {
		whereParts = append(whereParts, filterWhere)
		args = append(args, filterArgs...)
	}

	var total int64
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s d WHERE %s`, tbl, strings.Join(whereParts, " AND "))
	err = p.conn(ctx).QueryRowContext(ctx, sql, args...).Scan(&total)
	return total, err
}

// SumDocumentField 对集合内某数值列求和（如 files.size 用于 storage usage），
// 非 System 主体按 read 权限过滤（仅统计可见文档）。field 白名单校验防注入。
func (p *postgresDocumentDB) SumDocumentField(ctx context.Context, projectID, databaseID, collectionID, field string, principal databases.Principal) (int64, error) {
	if !validColumnName(field) {
		return 0, status.Error(codes.InvalidArgument, "invalid field name")
	}
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return 0, err
	}
	schema := schemaName(internalID, databaseID)
	tbl := tableName(schema, collectionID)

	var coll *databases.Collection
	if !principal.IsSystem() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return 0, err
		}
		if coll == nil {
			return 0, status.Error(codes.NotFound, "collection not found")
		}
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.IsSystem() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return 0, err
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}

	var total int64
	sql := fmt.Sprintf(`SELECT COALESCE(SUM(d.%s), 0) FROM %s d WHERE %s`, quoteIdent(field), tbl, strings.Join(whereParts, " AND "))
	err = p.conn(ctx).QueryRowContext(ctx, sql, args...).Scan(&total)
	return total, err
}

// validColumnName 限制字段名为安全的小写标识符（防 SQL 注入）。
func validColumnName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		case r == '_' && i > 0:
		default:
			return false
		}
	}
	return true
}

func (p *postgresDocumentDB) EnsureSystemCollections(ctx context.Context, projectID string, internalID int64) error {
	if internalID == 0 {
		var err error
		internalID, err = p.resolveInternalID(ctx, projectID)
		if err != nil {
			return err
		}
	}
	dbID := "default"
	schema := schemaName(internalID, dbID)

	schemaBootstrapped := false
	if _, ok := p.bootstrapCache.Load(projectID); ok {
		schemaBootstrapped = true
	}

	if !schemaBootstrapped {
		if err := p.ensureSchemaAndPerms(ctx, schema); err != nil {
			return err
		}
		// Ensure default database metadata row exists.
		exists, err := p.conn(ctx).NewSelect().Model((*model.DocumentDatabase)(nil)).
			Where("id = ? AND project_id = ?", dbID, projectID).Exists(ctx)
		if err != nil {
			return err
		}
		if !exists {
			m := &model.DocumentDatabase{ID: dbID, ProjectID: projectID, Name: "default", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if _, err := p.conn(ctx).NewInsert().Model(m).On("CONFLICT (project_id, id) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
		}
	}

	for _, id := range databases.SystemCollectionIDs {
		spec := systemCollectionSpecs(projectID)[id]
		coll, err := p.GetCollection(ctx, projectID, dbID, id)
		if err != nil {
			return err
		}
		if coll == nil {
			if err := p.CreateCollection(ctx, projectID, dbID, id, spec.name, spec.attrs, spec.indexes, spec.permissions, true); err != nil {
				return fmt.Errorf("create system collection %s: %w", id, err)
			}
			continue
		}
		// 集合已存在 → 幂等补齐缺失属性（存量项目迁移：物理列 + 目录元数据）。
		if err := p.reconcileSystemCollectionAttrs(ctx, projectID, dbID, id, coll, spec); err != nil {
			return err
		}
	}
	// 存量项目 keys 写权限收窄（安全评审 C1 第 3 层 / M2 存量迁移）：幂等清理
	// users/sessions/identities 的 update:keys/delete:keys（文档级 _perms +
	// 集合级 document_collections.permissions 元数据），进程内仅执行一次。
	if _, ok := p.keysPermsCleaned.Load(projectID); !ok {
		if err := p.cleanupKeysWritePerms(ctx, schema); err != nil {
			return err
		}
		p.keysPermsCleaned.Store(projectID, struct{}{})
	}
	p.bootstrapCache.Store(projectID, struct{}{})
	return nil
}

// reconcileSystemCollectionAttrs 幂等补齐存量系统集合中 spec 有而集合缺的属性：
// 直接调 CreateAttribute（infra 层无系统集合守卫，ADD COLUMN IF NOT EXISTS 幂等，
// 一步完成物理列 + document_attributes 元数据）。按属性 Key 比对（存量行 ID 可能
// 不符 {collection}_{key} 约定）；只修"任一缺失"方向，不做反向校验。并发时元数据
// INSERT 撞唯一约束（23505）属正常竞态，忽略。
func (p *postgresDocumentDB) reconcileSystemCollectionAttrs(ctx context.Context, projectID, databaseID, collectionID string, coll *databases.Collection, spec systemCollectionSpec) error {
	existing := make(map[string]struct{}, len(coll.Attributes))
	for _, a := range coll.Attributes {
		existing[a.Key] = struct{}{}
	}
	for _, attr := range spec.attrs {
		if _, ok := existing[attr.Key]; ok {
			continue
		}
		if err := p.CreateAttribute(ctx, projectID, databaseID, collectionID, attr); err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return fmt.Errorf("reconcile attribute %s on system collection %s: %w", attr.Key, collectionID, err)
		}
	}
	return nil
}

// cleanupKeysWritePerms 移除 keys 角色对系统敏感集合（users/sessions/identities）的
// update/delete 权限，只作用于这三个集合；teams/memberships 的 keys 管理权限是
// 合法语义，保留不动。幂等：无匹配行时均为空操作。
func (p *postgresDocumentDB) cleanupKeysWritePerms(ctx context.Context, schema string) error {
	del := fmt.Sprintf(`DELETE FROM %s WHERE _permission = 'keys' AND _type IN ('update','delete') AND _collection IN ('users','sessions','identities')`, permsTableName(schema))
	if _, err := p.conn(ctx).ExecContext(ctx, del); err != nil {
		return fmt.Errorf("cleanup keys perms: %w", err)
	}
	upd := `UPDATE document_collections
		SET permissions = ARRAY(SELECT x FROM unnest(permissions) AS x WHERE x NOT IN ('update:keys','delete:keys'))
		WHERE database_id = 'default' AND id IN ('users','sessions','identities') AND permissions IS NOT NULL`
	if _, err := p.conn(ctx).ExecContext(ctx, upd); err != nil {
		return fmt.Errorf("cleanup keys collection perms: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func schemaName(internalID int64, databaseID string) string {
	return fmt.Sprintf("TORCHWOOD_%d_%s", internalID, databaseID)
}

func tableName(schema, collectionID string) string {
	return quoteIdent(schema) + "." + quoteIdent(collectionID)
}

func permsTableName(schema string) string {
	return quoteIdent(schema) + "." + quoteIdent("_perms")
}

func pgTextArray(items []string) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, `"`+strings.ReplaceAll(item, `"`, `""`)+`"`)
	}
	return `{` + strings.Join(parts, ",") + `}`
}

func (p *postgresDocumentDB) resolveInternalID(ctx context.Context, projectID string) (int64, error) {
	if cached, ok := p.internalIDCache.Load(projectID); ok {
		return cached.(int64), nil
	}
	var internalID int64
	err := p.conn(ctx).NewSelect().Model((*model.Project)(nil)).Column("internal_id").Where("id = ?", projectID).Scan(ctx, &internalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("project not found: %s", projectID)
		}
		return 0, err
	}
	p.internalIDCache.Store(projectID, internalID)
	return internalID, nil
}

func (p *postgresDocumentDB) ensureSchemaAndPerms(ctx context.Context, schema string) error {
	if _, err := p.conn(ctx).ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema))); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return p.ensurePermsTable(ctx, schema)
}

func (p *postgresDocumentDB) ensurePermsTable(ctx context.Context, schema string) error {
	sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		_id BIGSERIAL PRIMARY KEY,
		_tenant BIGINT NOT NULL,
		_collection TEXT NOT NULL,
		_document TEXT NOT NULL,
		_type TEXT NOT NULL,
		_permission TEXT NOT NULL,
		_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (_tenant, _collection, _document, _type, _permission)
	)`, permsTableName(schema))
	if _, err := p.conn(ctx).ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("create perms table: %w", err)
	}
	idx := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_perms_lookup ON %s (_tenant, _collection, _document, _type)`, permsTableName(schema))
	if _, err := p.conn(ctx).ExecContext(ctx, idx); err != nil {
		return err
	}
	idx2 := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_perms_role ON %s (_tenant, _collection, _type, _permission)`, permsTableName(schema))
	_, err := p.conn(ctx).ExecContext(ctx, idx2)
	return err
}

func (p *postgresDocumentDB) createCollectionTable(ctx context.Context, schema, collectionID string, tenant int64, attrs []databases.Attribute) error {
	cols := []string{
		"_id TEXT NOT NULL",
		fmt.Sprintf("_tenant BIGINT NOT NULL DEFAULT %d", tenant),
		"_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"_created_by TEXT",
		"_updated_by TEXT",
	}
	for _, attr := range attrs {
		colSQL, err := attributeColumnSQL(attr)
		if err != nil {
			return err
		}
		cols = append(cols, colSQL)
	}
	cols = append(cols, "PRIMARY KEY (_tenant, _id)")
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t\t%s\n\t)", tableName(schema, collectionID), strings.Join(cols, ",\n\t\t"))
	_, err := p.conn(ctx).ExecContext(ctx, sql)
	if err != nil && isUniqueViolation(err) {
		// 并发建同名表时 PG 可能在 pg_type 类型注册上撞唯一约束（既有竞态，
		// EnsureSystemCollections 并发首请求触发）：表已由另一事务建成，
		// 重试一次即变为幂等 no-op。
		_, err = p.conn(ctx).ExecContext(ctx, sql)
	}
	return err
}

func (p *postgresDocumentDB) createCollectionIndex(ctx context.Context, schema, collectionID string, idx databases.Index) error {
	var cols []string
	for i, attr := range idx.Attributes {
		if !safeNameRe.MatchString(attr) {
			return fmt.Errorf("invalid index attribute: %s", attr)
		}
		order := ""
		if i < len(idx.Orders) && strings.EqualFold(idx.Orders[i], "desc") {
			order = " DESC"
		}
		cols = append(cols, quoteIdent(attr)+order)
	}
	idxName := quoteIdent(fmt.Sprintf("idx_%s_%s", collectionID, idx.ID))
	var sql string
	switch strings.ToLower(idx.Type) {
	case "unique":
		sql = fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s)`, idxName, tableName(schema, collectionID), strings.Join(cols, ", "))
	case "fulltext":
		sql = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s USING gin(to_tsvector('simple', %s))`, idxName, tableName(schema, collectionID), strings.Join(cols, " || ' ' || "))
	default:
		sql = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s)`, idxName, tableName(schema, collectionID), strings.Join(cols, ", "))
	}
	_, err := p.conn(ctx).ExecContext(ctx, sql)
	return err
}

func attributeColumnSQL(attr databases.Attribute) (string, error) {
	if !safeNameRe.MatchString(attr.Key) {
		return "", fmt.Errorf("invalid attribute key: %s", attr.Key)
	}
	name := quoteIdent(attr.Key)
	dataType := pgTypeFor(attr.Type, attr.Size)
	parts := []string{name, dataType}
	if attr.Required {
		parts = append(parts, "NOT NULL")
	}
	if attr.Default != nil && !attr.Array {
		def, err := formatDefault(attr.Default, attr.Type)
		if err != nil {
			return "", fmt.Errorf("invalid default for attribute %s: %w", attr.Key, err)
		}
		parts = append(parts, fmt.Sprintf("DEFAULT %s", def))
	}
	return strings.Join(parts, " "), nil
}

func pgTypeFor(t string, size int) string {
	switch strings.ToLower(t) {
	case "string", "email", "url":
		if size > 0 && size <= 64000 {
			return fmt.Sprintf("VARCHAR(%d)", size)
		}
		return "TEXT"
	case "integer":
		return "BIGINT"
	case "float":
		return "DOUBLE PRECISION"
	case "boolean":
		return "BOOLEAN"
	case "datetime":
		return "TIMESTAMPTZ"
	case "json":
		return "JSONB"
	default:
		return "TEXT"
	}
}

func formatDefault(v any, t string) (string, error) {
	switch strings.ToLower(t) {
	case "boolean":
		b, err := strconv.ParseBool(fmt.Sprint(v))
		if err != nil {
			return "", fmt.Errorf("boolean default: %w", err)
		}
		if b {
			return "TRUE", nil
		}
		return "FALSE", nil
	case "integer":
		n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		if err != nil {
			return "", fmt.Errorf("integer default: %w", err)
		}
		return strconv.FormatInt(n, 10), nil
	case "float":
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		if err != nil {
			return "", fmt.Errorf("float default: %w", err)
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	default:
		return quoteLiteral(fmt.Sprint(v)), nil
	}
}

func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func (p *postgresDocumentDB) createCollectionMetadata(ctx context.Context, projectID, databaseID, collectionID, name string, attrs []databases.Attribute, idxs []databases.Index, perms []databases.Permission, documentSecurity bool) error {
	permStrings := make([]string, 0, len(perms))
	for _, perm := range perms {
		permStrings = append(permStrings, fmt.Sprintf("%s:%s", perm.Type, perm.Role))
	}
	coll := &model.DocumentCollection{
		ID:               collectionID,
		DatabaseID:       databaseID,
		ProjectID:        projectID,
		Name:             name,
		DocumentSecurity: documentSecurity,
		IsSystem:         databases.IsSystemCollection(projectID, databaseID, collectionID),
		Permissions:      permStrings,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	res, err := p.conn(ctx).NewInsert().Model(coll).On("CONFLICT (project_id, database_id, id) DO NOTHING").Exec(ctx)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// 集合行已存在：系统集合视为幂等成功（并发首请求 23505 防护，极端竞态下
		// 子表缺失属人工可修复场景）；用户集合返回 ErrDuplicateKey（→ AlreadyExists）。
		if databases.IsSystemCollection(projectID, databaseID, collectionID) {
			return nil
		}
		return ErrDuplicateKey
	}
	for _, attr := range attrs {
		m := &model.DocumentAttribute{
			ID:           attr.ID,
			CollectionID: collectionID,
			DatabaseID:   databaseID,
			ProjectID:    projectID,
			Key:          attr.Key,
			Type:         attr.Type,
			Required:     attr.Required,
			IsArray:      attr.Array,
			CreatedAt:    time.Now(),
		}
		if attr.Size > 0 {
			m.Size = &attr.Size
		}
		if _, err := p.conn(ctx).NewInsert().Model(m).Exec(ctx); err != nil {
			return err
		}
	}
	for _, idx := range idxs {
		m := &model.DocumentIndex{
			ID:           idx.ID,
			CollectionID: collectionID,
			DatabaseID:   databaseID,
			ProjectID:    projectID,
			Type:         idx.Type,
			Attributes:   idx.Attributes,
			Orders:       idx.Orders,
			CreatedAt:    time.Now(),
		}
		if _, err := p.conn(ctx).NewInsert().Model(m).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresDocumentDB) mapCollection(ctx context.Context, m *model.DocumentCollection) (*databases.Collection, error) {
	var attrs []model.DocumentAttribute
	if err := p.conn(ctx).NewSelect().Model(&attrs).
		Where("project_id = ? AND database_id = ? AND collection_id = ?", m.ProjectID, m.DatabaseID, m.ID).
		Scan(ctx); err != nil {
		return nil, err
	}
	var idxs []model.DocumentIndex
	if err := p.conn(ctx).NewSelect().Model(&idxs).
		Where("project_id = ? AND database_id = ? AND collection_id = ?", m.ProjectID, m.DatabaseID, m.ID).
		Scan(ctx); err != nil {
		return nil, err
	}
	return mapCollectionRow(m, attrs, idxs)
}

func mapCollectionRow(m *model.DocumentCollection, attrs []model.DocumentAttribute, idxs []model.DocumentIndex) (*databases.Collection, error) {
	c := &databases.Collection{
		ID:               m.ID,
		DatabaseID:       m.DatabaseID,
		ProjectID:        m.ProjectID,
		Name:             m.Name,
		DocumentSecurity: m.DocumentSecurity,
		Disabled:         m.Disabled,
		IsSystem:         m.IsSystem,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
	for _, p := range m.Permissions {
		c.Permissions = append(c.Permissions, parsePermission(p))
	}
	for _, a := range attrs {
		attr := databases.Attribute{ID: a.ID, Key: a.Key, Type: a.Type, Required: a.Required, Array: a.IsArray}
		if a.Size != nil {
			attr.Size = *a.Size
		}
		c.Attributes = append(c.Attributes, attr)
	}
	for _, i := range idxs {
		c.Indexes = append(c.Indexes, databases.Index{ID: i.ID, Type: i.Type, Attributes: i.Attributes, Orders: i.Orders})
	}
	return c, nil
}

func parsePermission(s string) databases.Permission {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return databases.Permission{}
	}
	return databases.Permission{Type: parts[0], Role: parts[1]}
}

func (p *postgresDocumentDB) setCollectionPermissions(ctx context.Context, projectID, databaseID, collectionID string, perms []databases.Permission) error {
	var raw []string
	for _, perm := range perms {
		raw = append(raw, fmt.Sprintf("%s:%s", perm.Type, perm.Role))
	}
	_, err := p.conn(ctx).ExecContext(ctx,
		`UPDATE document_collections SET permissions = ? WHERE project_id = ? AND database_id = ? AND id = ?`,
		pgdialect.Array(raw), projectID, databaseID, collectionID)
	return err
}

func (p *postgresDocumentDB) setPermissions(ctx context.Context, schema, collectionID, documentID string, tenant int64, perms []databases.Permission) error {
	if len(perms) == 0 {
		return nil
	}
	base := fmt.Sprintf(`INSERT INTO %s (_tenant, _collection, _document, _type, _permission) VALUES `, permsTableName(schema))
	var vals []string
	var args []any
	for range perms {
		vals = append(vals, "(?, ?, ?, ?, ?)")
	}
	for _, perm := range perms {
		args = append(args, tenant, collectionID, documentID, perm.Type, perm.Role)
	}
	sql := base + strings.Join(vals, ", ") + " ON CONFLICT DO NOTHING"
	_, err := p.conn(ctx).ExecContext(ctx, sql, args...)
	return err
}

func (p *postgresDocumentDB) clearPermissions(ctx context.Context, schema, collectionID, documentID string, tenant int64) error {
	_, err := p.conn(ctx).ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE _tenant = ? AND _collection = ? AND _document = ?`, permsTableName(schema)), tenant, collectionID, documentID)
	return err
}

func buildInsertParts(doc databases.Document) (columns string, placeholders string, args []any) {
	if len(doc.Data) == 0 {
		return "", "", nil
	}
	var cols []string
	var phs []string
	for k, v := range doc.Data {
		if !safeNameRe.MatchString(k) || strings.HasPrefix(k, "_") {
			continue
		}
		cols = append(cols, quoteIdent(k))
		phs = append(phs, "?")
		args = append(args, v)
	}
	return strings.Join(cols, ", "), strings.Join(phs, ", "), args
}

func buildUpdateParts(doc databases.Document, updatedBy string) (setParts []string, args []any) {
	for k, v := range doc.Data {
		if !safeNameRe.MatchString(k) || strings.HasPrefix(k, "_") {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = ?", quoteIdent(k)))
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return nil, nil
	}
	setParts = append(setParts, "_updated_at = ?")
	args = append(args, time.Now())
	if updatedBy != "" {
		setParts = append(setParts, quoteIdent("_updated_by")+" = ?")
		args = append(args, updatedBy)
	}
	return setParts, args
}

// userIDFromPrincipal extracts the first "user:"-prefixed role ID from the
// principal's roles, or "" when no user role is held.
func userIDFromPrincipal(p databases.Principal) string {
	for _, r := range p.Roles {
		if strings.HasPrefix(r, "user:") {
			return strings.TrimPrefix(r, "user:")
		}
	}
	return ""
}

func scanDocumentJSON(scanner interface{ Scan(dest ...any) error }) (*databases.Document, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	doc := &databases.Document{Data: make(map[string]any)}
	if v, ok := payload["_id"].(string); ok {
		doc.ID = v
	}
	if v, ok := payload["_tenant"].(float64); ok {
		doc.Tenant = int64(v)
	}
	if v, ok := payload["_created_at"].(string); ok {
		doc.CreatedAt, _ = time.Parse(time.RFC3339Nano, v)
	}
	if v, ok := payload["_updated_at"].(string); ok {
		doc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, v)
	}
	if v, ok := payload["_created_by"].(string); ok {
		doc.CreatedBy = v
	}
	if v, ok := payload["_updated_by"].(string); ok {
		doc.UpdatedBy = v
	}
	for k, v := range payload {
		if strings.HasPrefix(k, "_") {
			continue
		}
		doc.Data[k] = v
	}
	return doc, nil
}

func isUniqueViolation(err error) bool {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23505"
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505") || strings.Contains(err.Error(), "unique constraint")
}

func schemaDatabaseID(schema string) string {
	// schema = "TORCHWOOD_<n>_<dbID>"; we need the dbID which follows the second "_".
	// But internalID is numeric, so split on "TORCHWOOD_" prefix then on first "_".
	rest := strings.TrimPrefix(schema, "TORCHWOOD_")
	idx := strings.IndexByte(rest, '_')
	if idx < 0 {
		return ""
	}
	return rest[idx+1:]
}

func validateDocID(docID string) error {
	if docID == "" {
		return status.Error(codes.InvalidArgument, "document id is required")
	}
	if !docIDRe.MatchString(docID) {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid document id: %s", docID))
	}
	return nil
}

func mapQueryField(field string) string {
	switch field {
	case "$id", "_id":
		return "_id"
	case "$createdAt", "_created_at":
		return "_created_at"
	case "$updatedAt", "_updated_at":
		return "_updated_at"
	}
	return field
}

// systemQueryFields 是查询白名单中的系统列（含 $ 别名映射后的内部名）。
var systemQueryFields = []string{"_id", "_created_at", "_updated_at"}

// sensitiveQueryFields 是 default 库系统集合中禁止作为查询过滤/排序条件的
// 凭据/令牌类列（任何角色不得探测）；仅按 databases.IsSystemCollection 限定，
// 自定义库同名集合不受影响。phone 等 PII 管理列按 D4 决策保留可查。
var sensitiveQueryFields = map[string]map[string]struct{}{
	"users":      {"password_hash": {}, "prefs": {}, "labels": {}},
	"sessions":   {"secret_hash": {}},
	"identities": {"provider_data": {}},
}

// validateQueryFields 校验非 System 查询路径（A7）：Filters/Orders/Selects 字段
// 白名单（系统列 + 声明 attrs）、敏感列黑名单、search 的 fulltext 索引约束。
// SystemPrincipal 路径不调用本函数（信任内部调用，零额外元数据查询）。
func validateQueryFields(parsed *query.Query, coll *databases.Collection, collectionID string, isSystem bool) error {
	allowed := make(map[string]struct{}, len(systemQueryFields)+len(coll.Attributes))
	for _, f := range systemQueryFields {
		allowed[f] = struct{}{}
	}
	for _, attr := range coll.Attributes {
		allowed[attr.Key] = struct{}{}
	}
	fulltextAttrs := map[string]struct{}{}
	for _, idx := range coll.Indexes {
		if strings.ToLower(idx.Type) == "fulltext" {
			for _, a := range idx.Attributes {
				fulltextAttrs[a] = struct{}{}
			}
		}
	}

	checkField := func(name string) error {
		field := mapQueryField(name)
		if _, ok := allowed[field]; !ok {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid query field: %s", name))
		}
		if isSystem {
			if sensitive, ok := sensitiveQueryFields[collectionID]; ok {
				if _, blocked := sensitive[field]; blocked {
					return status.Error(codes.InvalidArgument, fmt.Sprintf("field is not queryable: %s", name))
				}
			}
		}
		return nil
	}

	for _, f := range parsed.Filters {
		if err := checkField(f.Attribute); err != nil {
			return err
		}
		if f.Op == "search" {
			field := mapQueryField(f.Attribute)
			if _, ok := fulltextAttrs[field]; !ok {
				return status.Error(codes.InvalidArgument, fmt.Sprintf("search requires a fulltext index on: %s", f.Attribute))
			}
		}
	}
	for _, o := range parsed.Orders {
		if err := checkField(o.Attribute); err != nil {
			return err
		}
	}
	for _, s := range parsed.Selects {
		if err := checkField(s); err != nil {
			return err
		}
	}
	return nil
}

// validateQueryInput 校验 List/Count 的 queries 输入上限（A2）：条数、
// 单条长度；超限直接 InvalidArgument，防止超大 IN 参数击穿 PG 参数上限。
func validateQueryInput(queries []string) error {
	if len(queries) > maxQueryCount {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("queries count exceeds maximum of %d", maxQueryCount))
	}
	for _, raw := range queries {
		if len(raw) > maxQueryStringLen {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("query string exceeds maximum length of %d", maxQueryStringLen))
		}
	}
	return nil
}

func buildAppwriteQuery(parsed *query.Query) (string, []any, string, error) {
	var conds []string
	var args []any
	for _, f := range parsed.Filters {
		field := mapQueryField(f.Attribute)
		if !safeNameRe.MatchString(field) {
			return "", nil, "", fmt.Errorf("invalid query field: %s", f.Attribute)
		}
		col := "d." + quoteIdent(field)
		switch f.Op {
		case "equal":
			if len(f.Values) > maxFilterValues {
				return "", nil, "", status.Error(codes.InvalidArgument, fmt.Sprintf("filter values exceed maximum of %d", maxFilterValues))
			}
			if len(f.Values) == 1 {
				conds = append(conds, fmt.Sprintf("%s = ?", col))
				args = append(args, f.Values[0])
			} else {
				phs := strings.TrimSuffix(strings.Repeat("?, ", len(f.Values)), ", ")
				conds = append(conds, fmt.Sprintf("%s IN (%s)", col, phs))
				for _, v := range f.Values {
					args = append(args, v)
				}
			}
		case "notEqual":
			if len(f.Values) > maxFilterValues {
				return "", nil, "", status.Error(codes.InvalidArgument, fmt.Sprintf("filter values exceed maximum of %d", maxFilterValues))
			}
			if len(f.Values) == 1 {
				conds = append(conds, fmt.Sprintf("%s != ?", col))
				args = append(args, f.Values[0])
			} else {
				phs := strings.TrimSuffix(strings.Repeat("?, ", len(f.Values)), ", ")
				conds = append(conds, fmt.Sprintf("%s NOT IN (%s)", col, phs))
				for _, v := range f.Values {
					args = append(args, v)
				}
			}
		case "lessThan":
			conds = append(conds, fmt.Sprintf("%s < ?", col))
			args = append(args, f.Values[0])
		case "lessThanEqual":
			conds = append(conds, fmt.Sprintf("%s <= ?", col))
			args = append(args, f.Values[0])
		case "greaterThan":
			conds = append(conds, fmt.Sprintf("%s > ?", col))
			args = append(args, f.Values[0])
		case "greaterThanEqual":
			conds = append(conds, fmt.Sprintf("%s >= ?", col))
			args = append(args, f.Values[0])
		case "contains":
			conds = append(conds, fmt.Sprintf("%s ILIKE ?", col))
			args = append(args, "%"+f.Values[0]+"%")
		case "startsWith":
			conds = append(conds, fmt.Sprintf("%s ILIKE ?", col))
			args = append(args, f.Values[0]+"%")
		case "endsWith":
			conds = append(conds, fmt.Sprintf("%s ILIKE ?", col))
			args = append(args, "%"+f.Values[0])
		case "search":
			conds = append(conds, fmt.Sprintf("to_tsvector('simple', %s::text) @@ plainto_tsquery('simple', ?)", col))
			args = append(args, f.Values[0])
		case "isNull":
			conds = append(conds, fmt.Sprintf("%s IS NULL", col))
		case "isNotNull":
			conds = append(conds, fmt.Sprintf("%s IS NOT NULL", col))
		case "between":
			if len(f.Values) != 2 {
				return "", nil, "", fmt.Errorf("between requires 2 values")
			}
			conds = append(conds, fmt.Sprintf("%s BETWEEN ? AND ?", col))
			args = append(args, f.Values[0], f.Values[1])
		default:
			return "", nil, "", fmt.Errorf("unsupported filter operator: %s", f.Op)
		}
	}

	orderSQL := "ORDER BY d._created_at DESC"
	if len(parsed.Orders) > 0 {
		var parts []string
		for _, o := range parsed.Orders {
			field := mapQueryField(o.Attribute)
			if !safeNameRe.MatchString(field) {
				return "", nil, "", status.Error(codes.InvalidArgument, fmt.Sprintf("invalid order field: %s", o.Attribute))
			}
			dir := "ASC"
			if o.Desc {
				dir = "DESC"
			}
			parts = append(parts, fmt.Sprintf("d.%s %s", quoteIdent(field), dir))
		}
		if len(parts) > 0 {
			orderSQL = "ORDER BY " + strings.Join(parts, ", ") + ", d._created_at DESC"
		}
	}

	where := ""
	if len(conds) > 0 {
		where = "(" + strings.Join(conds, " AND ") + ")"
	}
	return where, args, orderSQL, nil
}
