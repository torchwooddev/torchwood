// 集合/属性/索引 DDL 与 catalog 元数据：建表、_version 列生命周期、索引表达式构建（fulltext 对齐见 W-E）、权限表写入。
package documentdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

func (p *postgresDocumentDB) CreateCollection(ctx context.Context, projectID, databaseID, collectionID, name string, attrs []databases.Attribute, idxs []databases.Index, perms []databases.Permission, documentSecurity bool) error {
	// sentinel 只允许系统名单集合（测试重建旧文档表）；生产入口已
	// RejectExternalDatabaseID，不得对 "_" 建业务集合。
	if databaseID == ident.ProjectDataPlaneID && !databases.IsSystemCollectionID(collectionID) {
		return status.Error(codes.InvalidArgument, "only system collections may be created in the project data plane")
	}
	// 长度二道防线（app 层已校验）：表名/列名 ≤63；索引名拼接 idx_<coll>_<id>
	// 与默认时间索引 idx_<coll>_tenant_created 均不得超 63（PG 截断防护）。
	if err := validatePhysicalNameLen("collection id", collectionID); err != nil {
		return p.mapError(err)
	}
	for _, attr := range attrs {
		if err := rejectArrayAttribute(attr); err != nil {
			return p.mapError(err)
		}
		if err := validatePhysicalNameLen("attribute key", attr.Key); err != nil {
			return p.mapError(err)
		}
	}
	for _, idx := range idxs {
		if err := validateIndexDefinition(idx); err != nil {
			return p.mapError(err)
		}
		if err := validateIndexNameLen(collectionID, idx.ID); err != nil {
			return p.mapError(err)
		}
	}
	if err := validateIndexNameLen(collectionID, "tenant_created"); err != nil {
		return p.mapError(err)
	}
	if err := p.EnsureCatalog(ctx, projectID); err != nil {
		return p.mapError(err)
	}
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return p.mapError(err)
	}
	// R4-J5-3：_tenant 列默认值取实时 internal_id，防陈旧缓存烤进新表。
	if internalID, err = p.resolveInternalIDFresh(ctx, projectID); err != nil {
		return p.mapError(err)
	}

	// DDL 与 document_* 元数据包进同一事务（PG 支持事务内 DDL），
	// 任一步失败整体回滚，避免"物理表建成而元数据缺失"。
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := p.ensureSchemaAndPerms(txCtx, schema); err != nil {
			return err
		}
		isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
		if err := p.createCollectionTable(txCtx, schema, collectionID, internalID, attrs, isSystem); err != nil {
			return err
		}
		// CREATE TABLE IF NOT EXISTS 不会给存量表补列；DDL 路径一次 reconcile。
		if err := p.reconcileVersionColumn(txCtx, schema, collectionID, isSystem); err != nil {
			return err
		}
		for _, idx := range idxs {
			if err := p.createCollectionIndex(txCtx, schema, collectionID, idx); err != nil {
				return err
			}
		}
		return p.createCollectionMetadata(txCtx, projectID, databaseID, collectionID, name, attrs, idxs, perms, documentSecurity)
	}))
}

func (p *postgresDocumentDB) GetCollection(ctx context.Context, projectID, databaseID, collectionID string) (*databases.Collection, error) {
	cat, err := p.catalogIdent(projectID)
	if err != nil {
		return nil, p.mapError(err)
	}
	m := new(model.DocumentCollection)
	err = p.conn(ctx).NewSelect().Model(m).
		ModelTableExpr("?.document_collections AS dc", cat).
		Where("project_id = ? AND database_id = ? AND id = ?", projectID, databaseID, collectionID).Scan(ctx)
	if err != nil {
		if p.catalogAbsent(ctx, projectID, err) {
			return nil, nil
		}
		return nil, p.mapError(err)
	}
	coll, mapErr := p.mapCollection(ctx, m)
	if mapErr != nil {
		return nil, p.mapError(mapErr)
	}
	return coll, nil
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
			return nil, databases.ListMeta{}, p.mapError(err)
		}
		offset = off
	}

	cat, err := p.catalogIdent(projectID)
	if err != nil {
		return nil, databases.ListMeta{}, p.mapError(err)
	}

	var total int64
	count, err := p.conn(ctx).NewSelect().Model((*model.DocumentCollection)(nil)).
		ModelTableExpr("?.document_collections AS dc", cat).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).Count(ctx)
	if err != nil {
		if p.catalogAbsent(ctx, projectID, err) {
			return []databases.Collection{}, databases.ListMeta{}, nil
		}
		return nil, databases.ListMeta{}, p.mapError(err)
	}
	total = int64(count)

	var ms []model.DocumentCollection
	err = p.conn(ctx).NewSelect().Model(&ms).
		ModelTableExpr("?.document_collections AS dc", cat).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).
		Order("created_at DESC").
		Limit(pageSize).Offset(offset).Scan(ctx)
	if err != nil {
		if p.catalogAbsent(ctx, projectID, err) {
			return []databases.Collection{}, databases.ListMeta{}, nil
		}
		return nil, databases.ListMeta{}, p.mapError(err)
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
		ModelTableExpr("?.document_attributes AS da", cat).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).
		Where("collection_id IN (?)", bun.List(collectionIDs)).
		Scan(ctx); err != nil {
		if p.catalogAbsent(ctx, projectID, err) {
			return []databases.Collection{}, databases.ListMeta{}, nil
		}
		return nil, databases.ListMeta{}, p.mapError(err)
	}
	attrsByColl := make(map[string][]model.DocumentAttribute, len(ms))
	for i := range allAttrs {
		attrsByColl[allAttrs[i].CollectionID] = append(attrsByColl[allAttrs[i].CollectionID], allAttrs[i])
	}

	var allIdxs []model.DocumentIndex
	if err := p.conn(ctx).NewSelect().Model(&allIdxs).
		ModelTableExpr("?.document_indexes AS di", cat).
		Where("project_id = ? AND database_id = ?", projectID, databaseID).
		Where("collection_id IN (?)", bun.List(collectionIDs)).
		Scan(ctx); err != nil {
		if p.catalogAbsent(ctx, projectID, err) {
			return []databases.Collection{}, databases.ListMeta{}, nil
		}
		return nil, databases.ListMeta{}, p.mapError(err)
	}
	idxsByColl := make(map[string][]model.DocumentIndex, len(ms))
	for i := range allIdxs {
		idxsByColl[allIdxs[i].CollectionID] = append(idxsByColl[allIdxs[i].CollectionID], allIdxs[i])
	}

	out := make([]databases.Collection, len(ms))
	for i := range ms {
		c, err := mapCollectionRow(&ms[i], attrsByColl[ms[i].ID], idxsByColl[ms[i].ID])
		if err != nil {
			return nil, databases.ListMeta{}, p.mapError(err)
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
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return p.mapError(err)
	}
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if _, err := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`DELETE FROM %s WHERE _tenant = ? AND _collection = ?`, permsTableName(schema)), internalID, collectionID); err != nil {
			return err
		}
		if _, err := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, tableName(schema, collectionID))); err != nil {
			return err
		}
		cat, err := p.catalogIdent(projectID)
		if err != nil {
			return err
		}
		// F4-2：物理表删除后同步清理属性/索引元数据，否则删了建不回来。
		if _, err := p.conn(txCtx).NewDelete().Model((*model.DocumentAttribute)(nil)).
			ModelTableExpr("?.document_attributes AS da", cat).
			Where("project_id = ? AND database_id = ? AND collection_id = ?", projectID, databaseID, collectionID).Exec(txCtx); err != nil {
			return err
		}
		if _, err := p.conn(txCtx).NewDelete().Model((*model.DocumentIndex)(nil)).
			ModelTableExpr("?.document_indexes AS di", cat).
			Where("project_id = ? AND database_id = ? AND collection_id = ?", projectID, databaseID, collectionID).Exec(txCtx); err != nil {
			return err
		}
		_, err = p.conn(txCtx).NewDelete().Model((*model.DocumentCollection)(nil)).
			ModelTableExpr("?.document_collections AS dc", cat).
			Where("project_id = ? AND database_id = ? AND id = ?", projectID, databaseID, collectionID).Exec(txCtx)
		return err
	}))
}

func (p *postgresDocumentDB) UpdateCollection(ctx context.Context, projectID, databaseID, collectionID string, patch databases.CollectionPatch) error {
	if _, err := p.resolveInternalID(ctx, projectID); err != nil {
		return p.mapError(err)
	}
	// 权限替换与字段更新同一事务（任一失败整体回滚，避免"权限已换、
	// 元数据未更"的半更新）；权限-only 变更同样刷 updated_at（审计列统一）。
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if patch.Permissions != nil {
			if err := p.setCollectionPermissions(txCtx, projectID, databaseID, collectionID, *patch.Permissions); err != nil {
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
		// 空 patch（无权限、无字段）保持 no-op，不刷审计列。
		if patch.Permissions == nil && len(sets) == 0 {
			return nil
		}
		sets = append(sets, "updated_at = ?")
		args = append(args, time.Now(), projectID, databaseID, collectionID)
		catSQL, err := p.catalogQuoted(projectID)
		if err != nil {
			return err
		}
		_, err = p.conn(txCtx).ExecContext(txCtx,
			fmt.Sprintf(`UPDATE %s.document_collections SET `, catSQL)+strings.Join(sets, ", ")+` WHERE project_id = ? AND database_id = ? AND id = ?`,
			args...,
		)
		return err
	}))
}

func (p *postgresDocumentDB) CreateAttribute(ctx context.Context, projectID, databaseID, collectionID string, attr databases.Attribute) error {
	if _, err := p.resolveInternalID(ctx, projectID); err != nil {
		return p.mapError(err)
	}
	// 第二道防线（app 层已校验）：直调 adapter 也不得把系统保留列（含 _version）
	// 当作用户属性 ADD COLUMN——否则会与 OCC 列类型/语义冲突。
	if _, ok := databases.ReservedAttributeKeys[attr.Key]; ok {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("attribute key %q is reserved", attr.Key))
	}
	// 物理列是标量：不得把 IsArray=true 写入 catalog。
	if err := rejectArrayAttribute(attr); err != nil {
		return err
	}
	// 长度二道防线（app 层已校验）：物理列名 ≤63（PG 截断防护）。
	if err := validatePhysicalNameLen("attribute key", attr.Key); err != nil {
		return err
	}
	_, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return p.mapError(err)
	}
	colSQL, err := attributeColumnSQL(attr)
	if err != nil {
		return p.mapError(err)
	}
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := p.reconcileVersionColumn(txCtx, schema, collectionID, isSystem); err != nil {
			return err
		}
		if _, err := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s`, tableName(schema, collectionID), colSQL)); err != nil {
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
		// default 与 DDL（attributeColumnSQL 的 DEFAULT）同源落 catalog：物理列
		// 生效但元数据缺失曾是契约断裂（GetCollection 读不回 default）。
		if attr.Default != nil {
			def := fmt.Sprint(attr.Default)
			m.DefaultValue = &def
		}
		cat, err := p.catalogIdent(projectID)
		if err != nil {
			return err
		}
		_, err = p.conn(txCtx).NewInsert().Model(m).
			ModelTableExpr("?.document_attributes AS da", cat).Exec(txCtx)
		return err
	}))
}

func (p *postgresDocumentDB) CreateIndex(ctx context.Context, projectID, databaseID, collectionID string, idx databases.Index) error {
	if err := validateIndexDefinition(idx); err != nil {
		return p.mapError(err)
	}
	// 长度二道防线（app 层已校验）：索引名拼接 idx_<coll>_<id> ≤63。
	if err := validateIndexNameLen(collectionID, idx.ID); err != nil {
		return p.mapError(err)
	}
	_, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return p.mapError(err)
	}
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := p.reconcileVersionColumn(txCtx, schema, collectionID, isSystem); err != nil {
			return err
		}
		if err := p.createCollectionIndex(txCtx, schema, collectionID, idx); err != nil {
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
		cat, err := p.catalogIdent(projectID)
		if err != nil {
			return err
		}
		_, err = p.conn(txCtx).NewInsert().Model(m).
			ModelTableExpr("?.document_indexes AS di", cat).Exec(txCtx)
		return err
	}))
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

func (p *postgresDocumentDB) createCollectionTable(ctx context.Context, schema, collectionID string, tenant int64, attrs []databases.Attribute, isSystem bool) error {
	cols := []string{
		"_id TEXT NOT NULL",
		fmt.Sprintf("_tenant BIGINT NOT NULL DEFAULT %d", tenant),
		"_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"_created_by TEXT",
		"_updated_by TEXT",
	}
	// 仅用户集合追加 _version（OCC）；系统集合不加列。
	if !isSystem {
		cols = append(cols, "_version BIGINT NOT NULL DEFAULT 1")
	}
	for _, attr := range attrs {
		colSQL, err := attributeColumnSQL(attr)
		if err != nil {
			return err
		}
		cols = append(cols, colSQL)
	}
	cols = append(cols, "PRIMARY KEY (_tenant, _id)")
	existed := false
	if !isSystem {
		var err error
		existed, err = p.collectionTableExists(ctx, schema, collectionID)
		if err != nil {
			return err
		}
	}
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t\t%s\n\t)", tableName(schema, collectionID), strings.Join(cols, ",\n\t\t"))
	_, err := p.conn(ctx).ExecContext(ctx, sql)
	createdHere := !isSystem && !existed
	if err != nil && isUniqueViolation(err) {
		// 并发建同名表时 PG 可能在 pg_type 类型注册上撞唯一约束（既有竞态，
		// CreateCollection 并发首请求触发）：表已由另一事务建成，
		// 重试一次即变为幂等 no-op。
		_, err = p.conn(ctx).ExecContext(ctx, sql)
		createdHere = false
	}
	if err != nil {
		return err
	}
	// 本事务新建且带 _version：打标，避免同事务写路径把未提交列写入 cache。
	if createdHere {
		p.markVersionAlterTx(ctx, schema, collectionID)
	}
	// R5-J3-1（D-P1-3）：用户集合默认时间索引——新建即建，存量表经
	// IF NOT EXISTS 幂等（系统表跳过，与 _version 列的处理一致）。
	if !isSystem {
		if err := p.ensureTenantCreatedIndex(ctx, schema, collectionID); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresDocumentDB) collectionTableExists(ctx context.Context, schema, collectionID string) (bool, error) {
	var exists bool
	err := p.conn(ctx).QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?)`,
		schema, collectionID,
	).Scan(&exists)
	return exists, err
}

// markVersionAlterTx 记录"本事务内新建 _version 列"的键。增长模型：每条
// DDL 事务一条（txid|schema.collection），随 txid 单调累积、进程生命周期内
// 不清理——量级为每集合创建/加列各一次（远小于业务写路径），可接受。
func (p *postgresDocumentDB) markVersionAlterTx(ctx context.Context, schema, collectionID string) {
	key := schema + "." + collectionID
	alterKey := p.txid(ctx) + "|" + key
	if alterKey != "|"+key {
		p.versionAlterTx.Store(alterKey, struct{}{})
	}
}

// requireVersionColumn 供文档写路径使用：只检查 _version 是否为 bigint，**不 ALTER**。
// 缺列 → ErrVersionColumnUnavailable；类型冲突 → ErrVersionColumnConflict。
func (p *postgresDocumentDB) requireVersionColumn(ctx context.Context, schema, collectionID string, isSystem bool) error {
	if isSystem {
		return nil
	}
	ready, err := p.versionColumnReady(ctx, schema, collectionID)
	if err != nil {
		return err
	}
	if !ready {
		return databases.ErrVersionColumnUnavailable
	}
	return nil
}

// reconcileVersionColumn 是用户集合 _version 列的 DDL 对账，仅写 DDL 调用：
//
//   - isSystem == true：直接返回（系统集合无 _version）。
//   - 列不存在 → ALTER TABLE ADD COLUMN IF NOT EXISTS _version BIGINT NOT NULL DEFAULT 1
//     （存量行 DEFAULT 1 回填）。
//   - 列已存在且为 bigint → 就绪。
//   - 列已存在但非 bigint（存量用户属性抢占 _version）→ fail-closed：
//     返回 ErrVersionColumnConflict，禁止对错误类型做 _version = _version + 1。
//
// 缓存：仅非事务内见到已提交的 int8 才写入 versionColumns。事务内 CREATE TABLE /
// ALTER 新建的列只打 versionAlterTx，不写 versionColumns（回滚会撤销列）。
// 文档写路径不得调用本函数：ADD COLUMN 是 AccessExclusiveLock。
//
// R5-J3-1：入口先幂等补建默认时间索引（ensureTenantCreatedIndex）——本函数
// 是用户集合所有 DDL touch（CreateCollection/CreateAttribute/CreateIndex）
// 的汇聚点，存量集合在下次任意 DDL touch 时自动获得该索引；放在 versionColumns
// 缓存短路之前，保证每次 touch 都对账（IF NOT EXISTS 已存在时仅 catalog 查找）。
func (p *postgresDocumentDB) reconcileVersionColumn(ctx context.Context, schema, collectionID string, isSystem bool) error {
	if isSystem {
		return nil
	}
	if err := p.ensureTenantCreatedIndex(ctx, schema, collectionID); err != nil {
		return err
	}
	key := schema + "." + collectionID
	if _, ok := p.versionColumns.Load(key); ok {
		return nil
	}
	alterKey := p.txid(ctx) + "|" + key
	if alterKey != "|"+key {
		if _, ok := p.versionAlterTx.Load(alterKey); ok {
			// 本事务已 ALTER：列存在且类型必为 bigint（本函数保证），直接放行。
			return nil
		}
	}
	udtName, err := p.versionColumnType(ctx, schema, collectionID)
	switch {
	case err == nil && udtName == "int8":
		// 事务内可能是本事务 CREATE TABLE 刚加的列，回滚会撤销，不得缓存。
		if !clients.InTx(ctx) {
			p.versionColumns.Store(key, struct{}{})
		}
		return nil
	case errors.Is(err, sql.ErrNoRows):
		tbl := tableName(schema, collectionID)
		if _, err := p.conn(ctx).ExecContext(ctx,
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS _version BIGINT NOT NULL DEFAULT 1`, tbl),
		); err != nil {
			return fmt.Errorf("add version column: %w", err)
		}
		p.markVersionAlterTx(ctx, schema, collectionID)
		return nil
	case err != nil:
		return err
	default:
		// 用户属性占用了 _version 且类型不是 bigint：拒绝 OCC，禁止拼接
		// _version = _version + 1（会撞类型错误或静默截断）。
		slog.Error("version column exists with unsupported type; OCC disabled fail-closed",
			"schema", schema, "collection", collectionID, "udt_name", udtName)
		return databases.ErrVersionColumnConflict
	}
}

// txid 返回当前事务 ID（文本）；非事务上下文或查询失败时返回 ""。
// 仅 cache miss 时调用（information_schema 查询旁的一次轻量查询）。
func (p *postgresDocumentDB) txid(ctx context.Context) string {
	var id string
	if err := p.conn(ctx).QueryRowContext(ctx, `SELECT txid_current()::text`).Scan(&id); err != nil {
		return ""
	}
	return id
}

// versionColumnType 查询 information_schema 中 _version 列的 udt_name；
// 列不存在返回 (sql.ErrNoRows, "")。
func (p *postgresDocumentDB) versionColumnType(ctx context.Context, schema, collectionID string) (string, error) {
	var udtName string
	err := p.conn(ctx).QueryRowContext(ctx,
		`SELECT udt_name FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = '_version'`,
		schema, collectionID,
	).Scan(&udtName)
	return udtName, err
}

// versionColumnReady 供读路径 $version 与写路径 requireVersionColumn 使用：
// cache miss 时只查 information_schema（不 ALTER）。
// 返回：
//   - (true, nil)：列可用（bigint）。
//   - (false, nil)：列不存在（尚未 reconcile）→ 调用方返回 version_column_unavailable。
//   - (false, ErrVersionColumnConflict)：列存在但非 bigint（用户属性抢占，
//     与 DDL reconcile 同语义 fail-closed）。
//
// 缓存与 reconcileVersionColumn 不同：文档写总在事务内，见到事务开始前已存在的
// int8 必须缓存，否则每次写都查 information_schema。本事务 CREATE TABLE / ALTER
// 新建的列（versionAlterTx）只放行 SQL、不写 versionColumns。
func (p *postgresDocumentDB) versionColumnReady(ctx context.Context, schema, collectionID string) (bool, error) {
	key := schema + "." + collectionID
	if _, ok := p.versionColumns.Load(key); ok {
		return true, nil
	}
	alterKey := p.txid(ctx) + "|" + key
	if alterKey != "|"+key {
		if _, ok := p.versionAlterTx.Load(alterKey); ok {
			// 本事务新建的列（未提交）：SQL 可用但不缓存。
			return true, nil
		}
	}
	udtName, err := p.versionColumnType(ctx, schema, collectionID)
	switch {
	case err == nil && udtName == "int8":
		p.versionColumns.Store(key, struct{}{})
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	default:
		// 读路径遇非 bigint _version：与写路径同码 fail-closed。
		return false, databases.ErrVersionColumnConflict
	}
}

func (p *postgresDocumentDB) createCollectionIndex(ctx context.Context, schema, collectionID string, idx databases.Index) error {
	var plainCols, orderedCols []string
	for i, attr := range idx.Attributes {
		if !safeNameRe.MatchString(attr) {
			return fmt.Errorf("invalid index attribute: %s", attr)
		}
		quoted := quoteIdent(attr)
		plainCols = append(plainCols, quoted)
		order := ""
		if i < len(idx.Orders) && strings.EqualFold(idx.Orders[i], "desc") {
			order = " DESC"
		}
		orderedCols = append(orderedCols, quoted+order)
	}
	idxName := quoteIdent(fmt.Sprintf("idx_%s_%s", collectionID, idx.ID))
	var sql string
	switch strings.ToLower(idx.Type) {
	case "unique":
		sql = fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s)`, idxName, tableName(schema, collectionID), strings.Join(orderedCols, ", "))
	case "fulltext":
		// W-E：查询编译为 to_tsvector('simple', "col"::text)（compilePredicate），
		// 索引表达式必须与之逐字对齐才可被 GIN 命中，否则 search 退化为
		// 全表逐行 to_tsvector。单列限制见 validateIndexDefinition；多列仅
		// 存量 catalog 重建可达（新创建已被入口校验拒绝），保留旧拼接表达式。
		// GIN 忽略 order——用 plainCols（此前拼入 DESC 会产生语法错误 DDL）。
		if len(plainCols) == 1 {
			sql = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s USING gin(to_tsvector('simple', %s::text))`,
				idxName, tableName(schema, collectionID), plainCols[0])
		} else {
			sql = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s USING gin(to_tsvector('simple', %s))`,
				idxName, tableName(schema, collectionID), strings.Join(plainCols, " || ' ' || "))
		}
	default:
		sql = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s)`, idxName, tableName(schema, collectionID), strings.Join(orderedCols, ", "))
	}
	_, err := p.conn(ctx).ExecContext(ctx, sql)
	return err
}

// ensureTenantCreatedIndex 为用户集合幂等补建默认时间索引
// `idx_<coll>_tenant_created ON <tbl>(_tenant, _created_at, _id)`（R5-J3-1，
// D-P1-3）：列表默认排序 `ORDER BY _created_at DESC, _id DESC` 与 keyset 谓词
// `(d._created_at, d._id) < (?, ?)` 均消费该序，缺索引时大集合每页全表扫描
// +排序。PG b-tree 可反向扫描，服务 DESC 无需 DESC 关键字。索引命名与
// createCollectionIndex 的 `idx_<coll>_<id>` 方案一致（63 字节截断风险同面，
// 不单独处理）；IF NOT EXISTS 幂等，DDL 路径重复执行仅一次 catalog 查找。
func (p *postgresDocumentDB) ensureTenantCreatedIndex(ctx context.Context, schema, collectionID string) error {
	idxName := quoteIdent(fmt.Sprintf("idx_%s_tenant_created", collectionID))
	sql := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (_tenant, _created_at, _id)`, idxName, tableName(schema, collectionID))
	if _, err := p.conn(ctx).ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("create tenant_created index: %w", err)
	}
	return nil
}

// validateIndexDefinition 拒绝与查询编译器不相容的索引定义：fulltext 查询
// 按单列 to_tsvector("col"::text) 编译，多列拼接索引的表达式与任何单字段
// 查询都不匹配——索引永不命中，只会误导用户以为 search 有索引支撑。
func validateIndexDefinition(idx databases.Index) error {
	if strings.ToLower(idx.Type) == "fulltext" && len(idx.Attributes) != 1 {
		return status.Error(codes.InvalidArgument, "fulltext index requires exactly one attribute")
	}
	return nil
}

// rejectArrayAttribute 是 catalog 写入前的第二道防线：物理列是标量，
// 不得把 IsArray=true 写入 document_attributes。
func rejectArrayAttribute(attr databases.Attribute) error {
	if attr.Array {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("attribute %q: array is not supported", attr.Key))
	}
	return nil
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
	cat, err := p.catalogIdent(projectID)
	if err != nil {
		return err
	}
	res, err := p.conn(ctx).NewInsert().Model(coll).
		ModelTableExpr("?.document_collections AS dc", cat).
		On("CONFLICT (project_id, database_id, id) DO NOTHING").Exec(ctx)
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
		if attr.Default != nil {
			def := fmt.Sprint(attr.Default)
			m.DefaultValue = &def
		}
		if _, err := p.conn(ctx).NewInsert().Model(m).
			ModelTableExpr("?.document_attributes AS da", cat).Exec(ctx); err != nil {
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
		if _, err := p.conn(ctx).NewInsert().Model(m).
			ModelTableExpr("?.document_indexes AS di", cat).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresDocumentDB) mapCollection(ctx context.Context, m *model.DocumentCollection) (*databases.Collection, error) {
	cat, err := p.catalogIdent(m.ProjectID)
	if err != nil {
		return nil, err
	}
	var attrs []model.DocumentAttribute
	if err := p.conn(ctx).NewSelect().Model(&attrs).
		ModelTableExpr("?.document_attributes AS da", cat).
		Where("project_id = ? AND database_id = ? AND collection_id = ?", m.ProjectID, m.DatabaseID, m.ID).
		Scan(ctx); err != nil {
		return nil, err
	}
	var idxs []model.DocumentIndex
	if err := p.conn(ctx).NewSelect().Model(&idxs).
		ModelTableExpr("?.document_indexes AS di", cat).
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
		if a.DefaultValue != nil {
			attr.Default = *a.DefaultValue
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
	catSQL, err := p.catalogQuoted(projectID)
	if err != nil {
		return p.mapError(err)
	}
	_, err = p.conn(ctx).ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s.document_collections SET permissions = ? WHERE project_id = ? AND database_id = ? AND id = ?`, catSQL),
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
