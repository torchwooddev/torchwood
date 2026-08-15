package documentdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

func (p *postgresDocumentDB) ensureCollectionAccessible(coll *databases.Collection, principal databases.Principal) error {
	if coll == nil {
		return ErrPermissionDenied
	}
	if coll.Disabled && !principal.IsSystem() {
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
	defer rows.Close()

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
	projectID, schema, collectionID, docID string,
	tenant int64,
	permType string,
	principal databases.Principal,
	coll *databases.Collection,
) error {
	if principal.IsSystem() {
		return nil
	}
	if coll == nil {
		var err error
		coll, err = p.getCollectionForAccess(ctx, projectID, schemaDatabaseID(schema), collectionID)
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
	if principal.IsSystem() {
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
		return 0, err
	}
	return affected, nil
}

func (p *postgresDocumentDB) bulkUpdateDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	data map[string]any,
	perms []databases.Permission,
	principal databases.Principal,
) (int64, error) {
	var affected int64
	for _, docID := range documentIDs {
		update := databases.DocumentUpdate{
			Document:    databases.Document{ID: docID, Data: data},
			Permissions: perms,
			// Bulk 是唯一允许跳过 OCC 的 Update 调用方（LWW 语义）；仍 _version + 1。
			SkipVersion: true,
		}
		if _, err := p.UpdateDocument(ctx, projectID, databaseID, collectionID, update, principal); err != nil {
			return affected, err
		}
		affected++
	}
	return affected, nil
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
		return 0, err
	}
	return affected, nil
}

func (p *postgresDocumentDB) bulkDeleteDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	documentIDs []string,
	principal databases.Principal,
) (int64, error) {
	var affected int64
	for _, docID := range documentIDs {
		// Bulk 是唯一允许跳过 OCC 的 Delete 调用方（LWW 语义）。
		if err := p.DeleteDocument(ctx, projectID, databaseID, collectionID, docID, databases.DeleteOptions{SkipVersion: true}, principal); err != nil {
			return affected, err
		}
		affected++
	}
	return affected, nil
}

func (p *postgresDocumentDB) DeleteAttribute(ctx context.Context, projectID, databaseID, collectionID, key string) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	if !safeNameRe.MatchString(key) {
		return fmt.Errorf("invalid attribute key: %s", key)
	}
	schema := schemaName(internalID, databaseID)
	return p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if _, err := p.conn(txCtx).ExecContext(txCtx,
			fmt.Sprintf(`ALTER TABLE %s DROP COLUMN IF EXISTS %s`, tableName(schema, collectionID), quoteIdent(key)),
		); err != nil {
			return err
		}
		_, err := p.conn(txCtx).NewDelete().Model((*model.DocumentAttribute)(nil)).
			Where("project_id = ? AND database_id = ? AND collection_id = ? AND key = ?", projectID, databaseID, collectionID, key).
			Exec(txCtx)
		return err
	})
}

func (p *postgresDocumentDB) DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	// R02-P1-1：DROP INDEX 与 document_indexes 元数据删除包进同一事务，
	// 任一步失败整体回滚，避免"物理索引已删而 catalog 仍记录"。
	return p.db.RunInTx(ctx, func(txCtx context.Context) error {
		idxName := quoteIdent(fmt.Sprintf("idx_%s_%s", collectionID, indexID))
		if _, err := p.conn(txCtx).ExecContext(txCtx,
			fmt.Sprintf(`DROP INDEX IF EXISTS %s.%s`, quoteIdent(schema), idxName),
		); err != nil {
			return err
		}
		_, err := p.conn(txCtx).NewDelete().Model((*model.DocumentIndex)(nil)).
			Where("project_id = ? AND database_id = ? AND collection_id = ? AND id = ?", projectID, databaseID, collectionID, indexID).
			Exec(txCtx)
		return err
	})
}
