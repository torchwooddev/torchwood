package documentdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/infra/bun/model"
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
	rows, err := p.db.DB.QueryContext(ctx,
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

func (p *postgresDocumentDB) checkDocumentPermission(
	ctx context.Context,
	projectID, schema, collectionID, docID string,
	tenant int64,
	permType string,
	principal databases.Principal,
) error {
	if principal.IsSystem() {
		return nil
	}
	coll, err := p.getCollectionForAccess(ctx, projectID, schemaDatabaseID(schema), collectionID)
	if err != nil {
		return err
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
	principal databases.Principal,
) (where string, args []any, err error) {
	if principal.IsSystem() {
		return "", nil, nil
	}
	coll, err := p.getCollectionForAccess(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return "", nil, err
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
	where = fmt.Sprintf(
		`EXISTS (SELECT 1 FROM %s p WHERE p._collection = ? AND p._document = d._id AND p._type = 'read' AND p._permission = ANY(?::text[]))`,
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
	var affected int64
	for _, docID := range documentIDs {
		update := databases.DocumentUpdate{
			Document:    databases.Document{ID: docID, Data: data},
			Permissions: perms,
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
	var affected int64
	for _, docID := range documentIDs {
		if err := p.DeleteDocument(ctx, projectID, databaseID, collectionID, docID, principal); err != nil {
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
	if _, err := p.db.DB.ExecContext(ctx,
		fmt.Sprintf(`ALTER TABLE %s DROP COLUMN IF EXISTS %s`, tableName(schema, collectionID), quoteIdent(key)),
	); err != nil {
		return err
	}
	_, err = p.conn(ctx).NewDelete().Model((*model.DocumentAttribute)(nil)).
		Where("project_id = ? AND database_id = ? AND collection_id = ? AND key = ?", projectID, databaseID, collectionID, key).
		Exec(ctx)
	return err
}

func (p *postgresDocumentDB) DeleteIndex(ctx context.Context, projectID, databaseID, collectionID, indexID string) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	schema := schemaName(internalID, databaseID)
	idxName := quoteIdent(fmt.Sprintf("idx_%s_%s", collectionID, indexID))
	if _, err := p.db.DB.ExecContext(ctx,
		fmt.Sprintf(`DROP INDEX IF EXISTS %s.%s`, quoteIdent(schema), idxName),
	); err != nil {
		return err
	}
	_, err = p.conn(ctx).NewDelete().Model((*model.DocumentIndex)(nil)).
		Where("project_id = ? AND database_id = ? AND collection_id = ? AND id = ?", projectID, databaseID, collectionID, indexID).
		Exec(ctx)
	return err
}
