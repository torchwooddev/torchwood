package documentdb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

func (p *postgresDocumentDB) ensureCollectionAccessible(coll *databases.Collection, principal databases.Principal) error {
	if coll == nil {
		return ErrPermissionDenied
	}
	if coll.Disabled && !principal.BypassesDocumentACL() {
		return ErrPermissionDenied
	}
	return nil
}

func (p *postgresDocumentDB) getCollectionForAccess(ctx context.Context, projectID, databaseID, collectionID string) (*databases.Collection, error) {
	coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	return coll, nil
}

func (p *postgresDocumentDB) getDocumentPermissions(ctx context.Context, schema, collectionID, docID string, tenant int64) ([]databases.Permission, bool, error) {
	permsTable := permsTableName(schema)
	rows, err := p.conn(ctx).QueryContext(ctx,
		fmt.Sprintf(`SELECT _type, _permission FROM %s WHERE _tenant = ? AND _collection = ? AND _document = ?`, permsTable),
		tenant, collectionID, docID,
	)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()

	var perms []databases.Permission
	for rows.Next() {
		var typ, role string
		if err := rows.Scan(&typ, &role); err != nil {
			return nil, false, err
		}
		perms = append(perms, databases.Permission{Type: typ, Role: role})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return perms, len(perms) > 0, nil
}

// checkDocumentPermission 校验文档级权限；coll 为调用方已获取的集合
// （避免重复查询 N+1），nil 时内部获取。
func (p *postgresDocumentDB) checkDocumentPermission(
	ctx context.Context,
	projectID, databaseID, schema, collectionID, docID string,
	tenant int64,
	permType string,
	principal databases.Principal,
	coll *databases.Collection,
) error {
	if principal.BypassesDocumentACL() {
		return nil
	}
	if coll == nil {
		var err error
		coll, err = p.getCollectionForAccess(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return err
		}
	}
	if err := p.ensureCollectionAccessible(coll, principal); err != nil {
		return err
	}
	docPerms, docHasPerms, err := p.getDocumentPermissions(ctx, schema, collectionID, docID, tenant)
	if err != nil {
		return err
	}
	if !databases.AllowsDocumentAccess(coll, docPerms, docHasPerms, permType, principal.Roles) {
		return ErrPermissionDenied
	}
	return nil
}

func (p *postgresDocumentDB) listPermissionFilter(
	ctx context.Context,
	projectID, databaseID, collectionID, schema string,
	coll *databases.Collection,
	principal databases.Principal,
) (where string, args []any, err error) {
	if principal.BypassesDocumentACL() {
		return "", nil, nil
	}
	if err := p.ensureCollectionAccessible(coll, principal); err != nil {
		return "", nil, err
	}
	if databases.ListAccessDenied(coll, principal.Roles) {
		return "", nil, ErrPermissionDenied
	}
	if databases.SkipDocumentPermissionFilter(coll, principal.Roles) {
		return "", nil, nil
	}

	expanded := databases.ExpandPermissionRoles(principal.Roles)
	permsTable := permsTableName(schema)
	// 用户集合 documentSecurity=true 且集合级有 read（系统集合+集合级 read 已在上方
	// Skip 分支返回）：文档有 _perms 必须匹配读权限，无 _perms 由集合级 read 兜底
	// （与 AllowsDocumentAccess 的 docHasPerms=false → collOK 一致，B1）。
	// 两个子查询均关联 p._tenant = d._tenant（A5）。
	if databases.CollectionAllows(coll.Permissions, "read", expanded) && coll.DocumentSecurity {
		where = fmt.Sprintf(
			`(EXISTS (SELECT 1 FROM %s p WHERE p._tenant = d._tenant AND p._collection = ? AND p._document = d._id AND p._type = 'read' AND p._permission = ANY(?::text[])) OR NOT EXISTS (SELECT 1 FROM %s p2 WHERE p2._tenant = d._tenant AND p2._collection = ? AND p2._document = d._id))`,
			permsTable, permsTable,
		)
		args = []any{collectionID, pgTextArray(expanded), collectionID}
		return where, args, nil
	}
	where = fmt.Sprintf(
		`EXISTS (SELECT 1 FROM %s p WHERE p._tenant = d._tenant AND p._collection = ? AND p._document = d._id AND p._type = 'read' AND p._permission = ANY(?::text[]))`,
		permsTable,
	)
	args = []any{collectionID, pgTextArray(expanded)}
	return where, args, nil
}

// attachDocumentPermissionsBatch 单条 IN 查询取回整页文档的 _perms（W-D：
// 取代每文档一次点查的 N+1，页大小 50 时权限读取从 51 次查询降到 1 次）。
// 未命中行保持 Permissions=nil（与逐条 attach 语义一致：无 ACE 文档回空）。
func (p *postgresDocumentDB) attachDocumentPermissionsBatch(ctx context.Context, schema, collectionID string, tenant int64, docs []databases.Document) error {
	if len(docs) == 0 {
		return nil
	}
	ids := make([]string, len(docs))
	for i := range docs {
		ids[i] = docs[i].ID
	}
	permsTable := permsTableName(schema)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+2)
	args = append(args, tenant, collectionID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := p.conn(ctx).QueryContext(ctx,
		fmt.Sprintf(`SELECT _document, _type, _permission FROM %s WHERE _tenant = ? AND _collection = ? AND _document IN (%s) ORDER BY _document`, permsTable, placeholders),
		args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	byDoc := make(map[string][]databases.Permission, len(docs))
	for rows.Next() {
		var docID, typ, role string
		if err := rows.Scan(&docID, &typ, &role); err != nil {
			return err
		}
		byDoc[docID] = append(byDoc[docID], databases.Permission{Type: typ, Role: role})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range docs {
		docs[i].Permissions = byDoc[docs[i].ID]
	}
	return nil
}

func (p *postgresDocumentDB) attachDocumentPermissions(ctx context.Context, schema, collectionID string, tenant int64, doc *databases.Document) error {
	if doc == nil {
		return nil
	}
	perms, _, err := p.getDocumentPermissions(ctx, schema, collectionID, doc.ID, tenant)
	if err != nil {
		return err
	}
	doc.Permissions = perms
	return nil
}

func buildIncrementParts(increment map[string]int64) (setParts []string, args []any) {
	for k, delta := range increment {
		if !safeNameRe.MatchString(k) || strings.HasPrefix(k, "_") || delta == 0 {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = COALESCE(%s, 0) + ?", quoteIdent(k), quoteIdent(k)))
		args = append(args, delta)
	}
	return setParts, args
}

func (p *postgresDocumentDB) BulkUpdateDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
) (int64, error) {
	if len(documentIDs) == 0 {
		return 0, nil
	}
	// 已在外层事务中（上层 RunInTx）时不嵌套，直接复用外层事务；
	// 否则整体包在单个事务里，中途失败整体回滚（行为从"部分成功"收紧为"原子"）。
	if clients.InTx(ctx) {
		return p.bulkUpdateDocuments(ctx, projectID, databaseID, collectionID, documentIDs, data, perms, principal)
	}
	var affected int64
	if err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		n, err := p.bulkUpdateDocuments(txCtx, projectID, databaseID, collectionID, documentIDs, data, perms, principal)
		affected = n
		return err
	}); err != nil {
		return 0, p.mapError(err)
	}
	return affected, nil
}

// bulkInChunk / bulkPermsRowChunk 是批量化语句的分片上限（PG 单语句 65535
// 参数上限的防御；MaxBulkOperations=1000 之下 IN 列表通常单批完成，
// _perms 多值 INSERT 以"行"为单位分片）。
const (
	bulkInChunk       = 900
	bulkPermsRowChunk = 2000
)

// inPlaceholders 生成 n 个 ? 占位符（IN 列表用）。
func inPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// eachIDChunk 按上限分片遍历 docID 列表，fn 返回错误时短路。
func eachIDChunk(ids []string, chunkSize int, fn func(chunk []string) error) error {
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		if err := fn(ids[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// dedupeIDs 去重保序：批量化后同一 _id 在 IN 列表 / 多值 VALUES 中重复出现
// 只会命中一行，bulk 语义按"唯一文档集合"执行（affected = 唯一文档数）。
func dedupeIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// getDocumentPermissionsBatch 单条 IN 查询批量取回多文档 _perms（R5-P2-6：
// 取代逐文档 getDocumentPermissions 点查）；返回 docID -> perms 映射，
// 未命中文档不在 map 中（docHasPerms=false，与单条路径空切片语义一致）。
func (p *postgresDocumentDB) getDocumentPermissionsBatch(ctx context.Context, schema, collectionID string, tenant int64, docIDs []string) (map[string][]databases.Permission, error) {
	byDoc := make(map[string][]databases.Permission, len(docIDs))
	permsTable := permsTableName(schema)
	err := eachIDChunk(docIDs, bulkInChunk, func(chunk []string) error {
		args := make([]any, 0, len(chunk)+2)
		args = append(args, tenant, collectionID)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := p.conn(ctx).QueryContext(ctx,
			fmt.Sprintf(`SELECT _document, _type, _permission FROM %s WHERE _tenant = ? AND _collection = ? AND _document IN (%s) ORDER BY _document`, permsTable, inPlaceholders(len(chunk))),
			args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var docID, typ, role string
			if err := rows.Scan(&docID, &typ, &role); err != nil {
				return err
			}
			byDoc[docID] = append(byDoc[docID], databases.Permission{Type: typ, Role: role})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return byDoc, nil
}

// clearPermissionsBatch 一条 IN DELETE 批量清除多文档 _perms（R5-P2-6）。
func (p *postgresDocumentDB) clearPermissionsBatch(ctx context.Context, schema, collectionID string, documentIDs []string, tenant int64) error {
	permsTable := permsTableName(schema)
	return eachIDChunk(documentIDs, bulkInChunk, func(chunk []string) error {
		args := make([]any, 0, len(chunk)+2)
		args = append(args, tenant, collectionID)
		for _, id := range chunk {
			args = append(args, id)
		}
		_, err := p.conn(ctx).ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE _tenant = ? AND _collection = ? AND _document IN (%s)`, permsTable, inPlaceholders(len(chunk))),
			args...)
		return err
	})
}

// setPermissionsBatch 一条多值 INSERT 批量写 N 文档 × M 权限（ON CONFLICT
// DO NOTHING，与单文档 setPermissions 一致）；行数超上限按 bulkPermsRowChunk
// 分片（每行 5 参数，分片后仍远小于逐文档 N 条语句）。
func (p *postgresDocumentDB) setPermissionsBatch(ctx context.Context, schema, collectionID string, documentIDs []string, tenant int64, perms []databases.Permission) error {
	if len(perms) == 0 || len(documentIDs) == 0 {
		return nil
	}
	type permRow struct {
		docID string
		perm  databases.Permission
	}
	rows := make([]permRow, 0, len(documentIDs)*len(perms))
	for _, docID := range documentIDs {
		for _, perm := range perms {
			rows = append(rows, permRow{docID: docID, perm: perm})
		}
	}
	base := fmt.Sprintf(`INSERT INTO %s (_tenant, _collection, _document, _type, _permission) VALUES `, permsTableName(schema))
	for start := 0; start < len(rows); start += bulkPermsRowChunk {
		end := min(start+bulkPermsRowChunk, len(rows))
		chunk := rows[start:end]
		vals := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)*5)
		for i, r := range chunk {
			vals[i] = "(?, ?, ?, ?, ?)"
			args = append(args, tenant, collectionID, r.docID, r.perm.Type, r.perm.Role)
		}
		if _, err := p.conn(ctx).ExecContext(ctx, base+strings.Join(vals, ", ")+" ON CONFLICT DO NOTHING", args...); err != nil {
			return err
		}
	}
	return nil
}

// bulkPrefetchContext 承载批量化路径的共享预取结果：集合（一次 GetCollection
// 复用于逐文档权限判定与事件 ACL 快照）与 _perms 批量映射（既供权限判定也
// 作事件写前 ACL 快照）。
type bulkPrefetchContext struct {
	coll       *databases.Collection
	permsByDoc map[string][]databases.Permission
}

// prefetchBulkAccess 做批量化前置工作：集合可达性 + 逐文档 permType 权限
// 全量校验（同一 databases.AllowsDocumentAccess 判定，非 SQL 谓词改写）+
// 事件所需写前 ACL 批量预取。任一文档被拒 → ErrPermissionDenied（调用方
// 整体回滚，all-or-nothing）。
func (p *postgresDocumentDB) prefetchBulkAccess(
	ctx context.Context,
	projectID, databaseID, schema, collectionID string,
	ids []string,
	tenant int64,
	permType string,
	principal databases.Principal,
	needEventACL bool,
) (*bulkPrefetchContext, error) {
	pc := &bulkPrefetchContext{permsByDoc: map[string][]databases.Permission{}}
	if !principal.BypassesDocumentACL() {
		coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return nil, err
		}
		if err := p.ensureCollectionAccessible(coll, principal); err != nil {
			return nil, err
		}
		pc.coll = coll
		if pc.permsByDoc, err = p.getDocumentPermissionsBatch(ctx, schema, collectionID, tenant, ids); err != nil {
			return nil, err
		}
		for _, docID := range ids {
			docPerms := pc.permsByDoc[docID]
			if !databases.AllowsDocumentAccess(coll, docPerms, len(docPerms) > 0, permType, principal.Roles) {
				return nil, ErrPermissionDenied
			}
		}
	} else if needEventACL {
		var err error
		if pc.permsByDoc, err = p.getDocumentPermissionsBatch(ctx, schema, collectionID, tenant, ids); err != nil {
			return nil, err
		}
	}
	return pc, nil
}

// collectionForEvents 取事件 ACL 快照所需的集合（Bulk 路径一次取回复用，
// 权限校验已取时直接复用；principal 旁路 ACL 时此处才产生一次查询）。
func (p *postgresDocumentDB) collectionForEvents(ctx context.Context, projectID, databaseID, collectionID string, pc *bulkPrefetchContext) (*databases.Collection, error) {
	if pc.coll != nil {
		return pc.coll, nil
	}
	return p.GetCollection(ctx, projectID, databaseID, collectionID)
}

// bulkUpdateDocuments 是 BulkUpdateDocuments 的事务体（R5-P2-6 批量化）：
// 先全量校验（docID、写保护系统集合、逐文档 update 权限——批量预取 _perms
// + 同一 AllowsDocumentAccess 判定），再单条 UPDATE ... IN ... RETURNING 取
// 写后快照，随后 _perms 批量替换、每文档一条 outbox。任一文档不存在
// （RETURNING 行数不足）或权限拒绝 → 整体回滚（all-or-nothing，与原
// 逐条循环语义一致）。Bulk 是唯一 SkipVersion=true 的 Update 调用方（LWW）：
// 无 ExpectedVersion 比对，但非系统集合仍 _version = _version + 1。
func (p *postgresDocumentDB) bulkUpdateDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
) (int64, error) {
	for _, docID := range documentIDs {
		if err := validateDocID(docID); err != nil {
			return 0, err
		}
	}
	ids := dedupeIDs(documentIDs)
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return 0, err
	}
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.requireVersionColumn(ctx, schema, collectionID, isSystem); err != nil {
		return 0, err
	}
	// 写保护系统集合防线与单条 updateDocument 一致：owner（user:<docID>）
	// 例外，批量下要求每个文档都命中例外，否则整体拒绝。
	if !principal.BypassesDocumentACL() && isWriteProtectedSystemCollection(databaseID, collectionID) {
		for _, docID := range ids {
			if !principal.HasRole(fmt.Sprintf("user:%s", docID)) {
				return 0, ErrPermissionDenied
			}
		}
	}
	wantEvents := p.pub != nil && !isSystem
	pc, err := p.prefetchBulkAccess(ctx, projectID, databaseID, schema, collectionID, ids, internalID, "update", principal, wantEvents)
	if err != nil {
		return 0, err
	}
	if wantEvents {
		if pc.coll, err = p.collectionForEvents(ctx, projectID, databaseID, collectionID, pc); err != nil {
			return 0, err
		}
	}
	// 同一 data 组装 SET 子句（与 updateDocument 三分支一致：data 非空 →
	// 字段 + 审计列；仅权限变更 → 只刷审计列；两者皆空 → ErrNoFieldsToUpdate）。
	updatedBy := userIDFromPrincipal(principal)
	setParts, args := buildUpdateParts(databases.Document{Data: data}, updatedBy)
	if len(setParts) == 0 && len(perms) == 0 {
		return 0, fmt.Errorf("%w", databases.ErrNoFieldsToUpdate)
	}
	if len(setParts) == 0 {
		// R02-P1-4：仅权限变更时同样刷新审计列（_updated_at/_updated_by）。
		setParts = append(setParts, "_updated_at = ?")
		args = append(args, time.Now())
		if updatedBy != "" {
			setParts = append(setParts, quoteIdent("_updated_by")+" = ?")
			args = append(args, updatedBy)
		}
	}
	if !isSystem {
		// 每次成功写 +1（含权限-only 更新；SkipVersion 同样 +1）。
		setParts = append(setParts, "_version = _version + 1")
	}
	// 单条 UPDATE ... IN ... RETURNING to_jsonb(d.*)：写后快照一步取回（与
	// GetDocument 的行→JSON 扫描同语义）；RETURNING 行数不足说明有文档不
	// 存在（权限已全过）→ ErrDocumentNotFound 整体回滚。
	tbl := tableName(schema, collectionID)
	byID := make(map[string]*databases.Document, len(ids))
	if err := eachIDChunk(ids, bulkInChunk, func(chunk []string) error {
		sqlArgs := make([]any, 0, len(args)+len(chunk)+1)
		sqlArgs = append(sqlArgs, args...)
		for _, id := range chunk {
			sqlArgs = append(sqlArgs, id)
		}
		sqlArgs = append(sqlArgs, internalID)
		rows, err := p.conn(ctx).QueryContext(ctx, fmt.Sprintf(
			`UPDATE %s AS d SET %s WHERE d._id IN (%s) AND d._tenant = ? RETURNING to_jsonb(d.*) AS doc`,
			tbl, strings.Join(setParts, ", "), inPlaceholders(len(chunk))),
			sqlArgs...)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
			}
			return fmt.Errorf("bulk update documents: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			doc, err := parseDocumentJSON(raw)
			if err != nil {
				return err
			}
			if doc != nil {
				byID[doc.ID] = doc
			}
		}
		return rows.Err()
	}); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			return 0, fmt.Errorf("%w", databases.ErrDocumentNotFound)
		}
	}
	// _perms 替换：非空时先清后写（各一条批量语句，语义与单条路径一致）。
	if len(perms) > 0 {
		if err := p.clearPermissionsBatch(ctx, schema, collectionID, ids, internalID); err != nil {
			return 0, err
		}
		if err := p.setPermissionsBatch(ctx, schema, collectionID, ids, internalID, perms); err != nil {
			return 0, err
		}
	}
	// 每文档一条 update 事件（实时订阅按 doc 过滤）：version/data=写后
	// （RETURNING 快照），acl=写前（批量预取的 permsByDoc）。data.permissions
	// 与单条路径尾随读回一致：替换时为新 perms，否则为写前 perms。
	if wantEvents {
		for _, docID := range ids {
			updated := *byID[docID]
			pre := pc.permsByDoc[docID]
			if len(perms) > 0 {
				updated.Permissions = perms
			} else {
				updated.Permissions = pre
			}
			if err := p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, docID,
				domainevents.EventDocumentsUpdate, updated.Version, &updated, pre, len(pre) > 0, pc.coll); err != nil {
				return 0, err
			}
		}
	}
	return int64(len(ids)), nil
}

func (p *postgresDocumentDB) BulkDeleteDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	principal databases.Principal,
) (int64, error) {
	if len(documentIDs) == 0 {
		return 0, nil
	}
	if clients.InTx(ctx) {
		return p.bulkDeleteDocuments(ctx, projectID, databaseID, collectionID, documentIDs, principal)
	}
	var affected int64
	if err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		n, err := p.bulkDeleteDocuments(txCtx, projectID, databaseID, collectionID, documentIDs, principal)
		affected = n
		return err
	}); err != nil {
		return 0, p.mapError(err)
	}
	return affected, nil
}

// bulkDeleteDocuments 是 BulkDeleteDocuments 的事务体（R5-P2-6 批量化）：
// 先全量校验（docID、写保护系统集合、逐文档 delete 权限——批量预取 +
// 同一 AllowsDocumentAccess 判定），非系统集合再 FOR UPDATE 批量锁行取写前
// _version（行锁语义保留；缺失 → ErrDocumentNotFound 整体回滚），随后批量
// 清 _perms、批量删行，最后每文档一条 delete outbox（version/acl 均写前）。
// Bulk 是唯一 SkipVersion=true 的 Delete 调用方（LWW）；系统集合与单条
// deleteDocument 一致：不做存在性检查、不发事件。
func (p *postgresDocumentDB) bulkDeleteDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	principal databases.Principal,
) (int64, error) {
	for _, docID := range documentIDs {
		if err := validateDocID(docID); err != nil {
			return 0, err
		}
	}
	ids := dedupeIDs(documentIDs)
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return 0, err
	}
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.requireVersionColumn(ctx, schema, collectionID, isSystem); err != nil {
		return 0, err
	}
	// 写保护系统集合防线：delete 路径无 owner 例外（与 deleteDocument 一致）。
	if !principal.BypassesDocumentACL() && isWriteProtectedSystemCollection(databaseID, collectionID) {
		return 0, ErrPermissionDenied
	}
	wantEvents := p.pub != nil && !isSystem
	pc, err := p.prefetchBulkAccess(ctx, projectID, databaseID, schema, collectionID, ids, internalID, "delete", principal, wantEvents)
	if err != nil {
		return 0, err
	}
	if wantEvents {
		if pc.coll, err = p.collectionForEvents(ctx, projectID, databaseID, collectionID, pc); err != nil {
			return 0, err
		}
	}
	tbl := tableName(schema, collectionID)
	// 写前 _version（FOR UPDATE 批量锁行，保留单条路径的行锁语义）：事件
	// version 与 acl 都基于写前状态；有文档不存在 → 整体回滚。
	versions := make(map[string]int64, len(ids))
	if !isSystem {
		if err := eachIDChunk(ids, bulkInChunk, func(chunk []string) error {
			sqlArgs := make([]any, 0, len(chunk)+1)
			for _, id := range chunk {
				sqlArgs = append(sqlArgs, id)
			}
			sqlArgs = append(sqlArgs, internalID)
			rows, err := p.conn(ctx).QueryContext(ctx, fmt.Sprintf(
				`SELECT d._id, d._version FROM %s AS d WHERE d._id IN (%s) AND d._tenant = ? FOR UPDATE`,
				tbl, inPlaceholders(len(chunk))),
				sqlArgs...)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var docID string
				var version int64
				if err := rows.Scan(&docID, &version); err != nil {
					return err
				}
				versions[docID] = version
			}
			return rows.Err()
		}); err != nil {
			return 0, err
		}
		for _, id := range ids {
			if _, ok := versions[id]; !ok {
				return 0, fmt.Errorf("%w", databases.ErrDocumentNotFound)
			}
		}
	}
	// 批量清 _perms + 批量删行（与单条路径同事务序：先 _perms 后数据行）。
	if err := p.clearPermissionsBatch(ctx, schema, collectionID, ids, internalID); err != nil {
		return 0, err
	}
	var deleted int64
	if err := eachIDChunk(ids, bulkInChunk, func(chunk []string) error {
		sqlArgs := make([]any, 0, len(chunk)+1)
		for _, id := range chunk {
			sqlArgs = append(sqlArgs, id)
		}
		sqlArgs = append(sqlArgs, internalID)
		res, err := p.conn(ctx).ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM %s WHERE _id IN (%s) AND _tenant = ?`, tbl, inPlaceholders(len(chunk))),
			sqlArgs...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted += n
		return nil
	}); err != nil {
		return 0, err
	}
	if !isSystem && deleted != int64(len(ids)) {
		// FOR UPDATE 已锁全部行，行数缺失只可能来自违反不变量的并发路径；
		// 防御性整体回滚（系统集合与单条路径一致：不做存在性检查）。
		return 0, fmt.Errorf("%w", databases.ErrDocumentNotFound)
	}
	// 每文档一条 delete 事件：version=写前、acl=写前、data=nil。
	if wantEvents {
		for _, docID := range ids {
			pre := pc.permsByDoc[docID]
			if err := p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, docID,
				domainevents.EventDocumentsDelete, versions[docID], nil, pre, len(pre) > 0, pc.coll); err != nil {
				return 0, err
			}
		}
	}
	return int64(len(ids)), nil
}

func (p *postgresDocumentDB) DeleteAttribute(ctx context.Context, projectID, databaseID, collectionID, key string) error {
	if _, err := p.resolveInternalID(ctx, projectID); err != nil {
		return p.mapError(err)
	}
	if !safeNameRe.MatchString(key) {
		return p.mapError(fmt.Errorf("invalid attribute key: %s", key))
	}
	_, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return p.mapError(err)
	}
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		// B8：同事务清理依赖该属性的索引，避免幽灵索引指向已删列。
		cat, err := p.catalogIdent(projectID)
		if err != nil {
			return err
		}
		var idxs []*model.DocumentIndex
		if err := p.conn(txCtx).NewSelect().Model((*model.DocumentIndex)(nil)).
			ModelTableExpr("?.document_indexes AS di", cat).
			Where("project_id = ? AND database_id = ? AND collection_id = ?", projectID, databaseID, collectionID).
			Scan(txCtx, &idxs); err != nil {
			return err
		}
		for _, idx := range idxs {
			contains := false
			for _, attr := range idx.Attributes {
				if attr == key {
					contains = true
					break
				}
			}
			if !contains {
				continue
			}
			idxName := quoteIdent(fmt.Sprintf("idx_%s_%s", collectionID, idx.ID))
			if _, err := p.conn(txCtx).ExecContext(txCtx,
				fmt.Sprintf(`DROP INDEX IF EXISTS %s.%s`, quoteIdent(schema), idxName),
			); err != nil {
				return err
			}
			if _, err := p.conn(txCtx).NewDelete().Model((*model.DocumentIndex)(nil)).
				ModelTableExpr("?.document_indexes AS di", cat).
				Where("project_id = ? AND database_id = ? AND collection_id = ? AND id = ?", projectID, databaseID, collectionID, idx.ID).
				Exec(txCtx); err != nil {
				return err
			}
		}
		if _, err := p.conn(txCtx).ExecContext(txCtx,
			fmt.Sprintf(`ALTER TABLE %s DROP COLUMN IF EXISTS %s`, tableName(schema, collectionID), quoteIdent(key)),
		); err != nil {
			return err
		}
		_, err = p.conn(txCtx).NewDelete().Model((*model.DocumentAttribute)(nil)).
			ModelTableExpr("?.document_attributes AS da", cat).
			Where("project_id = ? AND database_id = ? AND collection_id = ? AND key = ?", projectID, databaseID, collectionID, key).
			Exec(txCtx)
		return err
	}))
}

func (p *postgresDocumentDB) DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error {
	if _, err := p.resolveInternalID(ctx, projectID); err != nil {
		return p.mapError(err)
	}
	_, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return p.mapError(err)
	}
	// R02-P1-1：DROP INDEX 与 document_indexes 元数据删除包进同一事务，
	// 任一步失败整体回滚，避免"物理索引已删而 catalog 仍记录"。
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		idxName := quoteIdent(fmt.Sprintf("idx_%s_%s", collectionID, indexID))
		if _, err := p.conn(txCtx).ExecContext(txCtx,
			fmt.Sprintf(`DROP INDEX IF EXISTS %s.%s`, quoteIdent(schema), idxName),
		); err != nil {
			return err
		}
		cat, err := p.catalogIdent(projectID)
		if err != nil {
			return err
		}
		_, err = p.conn(txCtx).NewDelete().Model((*model.DocumentIndex)(nil)).
			ModelTableExpr("?.document_indexes AS di", cat).
			Where("project_id = ? AND database_id = ? AND collection_id = ? AND id = ?", projectID, databaseID, collectionID, indexID).
			Exec(txCtx)
		return err
	}))
}
