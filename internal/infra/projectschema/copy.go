package projectschema

import (
	"context"
	"fmt"
	"strings"

	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

var stagingTables = []string{
	"sys_files",
	"sys_memberships",
	"sys_sessions",
	"sys_identities",
	"sys_buckets",
	"sys_groups",
	"sys_users",
}

var documentSystemTables = []string{
	"users",
	"sessions",
	"identities",
	"groups",
	"memberships",
	"buckets",
	"files",
}

// 插入顺序：users → groups → buckets → 其余（满足 FK）。
var copyInsertOrder = []string{
	"users",
	"groups",
	"buckets",
	"sessions",
	"identities",
	"memberships",
	"files",
}

type copyAction int

const (
	copyNoop copyAction = iota + 1
	copyRun
)

func detectCopyAction(docUsersHasID, stagingHasID bool) (copyAction, error) {
	if !docUsersHasID {
		return copyNoop, nil
	}
	if !stagingHasID {
		return 0, fmt.Errorf("copy system documents: sys_users missing (000008 not applied)")
	}
	return copyRun, nil
}

// CopySystemDocuments 把 tw_<project> 文档系统集合拷到 sys_* staging。
// 无文档 users（或无列 _id）时返回 nil。文档表在而 sys_users 不在则报错。
// 孤儿行与源表唯一冲突 fail-closed。失败不写 schema_migrations。
func CopySystemDocuments(ctx context.Context, db *clients.Database, projectID string) error {
	if err := ident.ValidateSchemaResourceID(projectID); err != nil {
		return fmt.Errorf("invalid project id: %w", err)
	}
	schema, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		return err
	}
	quoted := quoteIdent(schema)

	docUsersHasID, err := hasColumn(ctx, db, schema, "users", "_id")
	if err != nil {
		return err
	}
	stagingHasID, err := hasColumn(ctx, db, schema, "sys_users", "id")
	if err != nil {
		return err
	}
	action, err := detectCopyAction(docUsersHasID, stagingHasID)
	if err != nil {
		return fmt.Errorf("%w in %s", err, schema)
	}
	if action == copyNoop {
		return nil
	}

	present, err := presentDocumentTables(ctx, db, schema)
	if err != nil {
		return err
	}

	// 整段事务：冲突检查失败不碰 staging；TRUNCATE+INSERT 失败则回滚。
	return db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := checkCopyConflicts(txCtx, db, quoted, present); err != nil {
			return err
		}
		if err := truncateStaging(txCtx, db, quoted); err != nil {
			return err
		}
		return insertStaging(txCtx, db, quoted, present)
	})
}

func hasColumn(ctx context.Context, db *clients.Database, schema, table, column string) (bool, error) {
	var exists bool
	err := db.Conn(ctx).QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = ?
      AND table_name = ?
      AND column_name = ?
)`, schema, table, column).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("copy system documents: inspect %s.%s.%s: %w", schema, table, column, err)
	}
	return exists, nil
}

func presentDocumentTables(ctx context.Context, db *clients.Database, schema string) (map[string]bool, error) {
	out := make(map[string]bool, len(documentSystemTables))
	for _, name := range documentSystemTables {
		ok, err := hasColumn(ctx, db, schema, name, "_id")
		if err != nil {
			return nil, err
		}
		out[name] = ok
	}
	return out, nil
}

func qtable(quoted, table string) string {
	return quoted + "." + quoteIdent(table)
}

func queryExists(ctx context.Context, db *clients.Database, sql string) (bool, error) {
	var exists bool
	if err := db.Conn(ctx).QueryRowContext(ctx, sql).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func hasMissingParent(ctx context.Context, db *clients.Database, quoted, child, childCol, parent, parentCol string, skipNull, parentPresent bool) (bool, error) {
	childT := qtable(quoted, child)
	cc := quoteIdent(childCol)
	if !parentPresent {
		if skipNull {
			return queryExists(ctx, db, fmt.Sprintf(
				`SELECT EXISTS (SELECT 1 FROM %s c WHERE c.%s IS NOT NULL AND c.%s <> '')`,
				childT, cc, cc))
		}
		return queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s)`, childT))
	}
	parentT := qtable(quoted, parent)
	pc := quoteIdent(parentCol)
	if skipNull {
		return queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM %s c
    WHERE c.%s IS NOT NULL AND c.%s <> ''
      AND NOT EXISTS (SELECT 1 FROM %s p WHERE p.%s = c.%s)
)`, childT, cc, cc, parentT, pc, cc))
	}
	return queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM %s c
    WHERE c.%s IS NULL OR c.%s = ''
       OR NOT EXISTS (SELECT 1 FROM %s p WHERE p.%s = c.%s)
)`, childT, cc, cc, parentT, pc, cc))
}

func checkCopyConflicts(ctx context.Context, db *clients.Database, quoted string, present map[string]bool) error {
	type orphan struct {
		child, childCol, parent, parentCol, err string
		skipNull                                bool
		need                                    string
	}
	orphans := []orphan{
		{"sessions", "user_id", "users", "_id", "orphan session user_id", false, "sessions"},
		{"identities", "user_id", "users", "_id", "orphan identity user_id", false, "identities"},
		{"memberships", "user_id", "users", "_id", "orphan membership user_id", true, "memberships"},
		{"memberships", "group_id", "groups", "_id", "orphan membership group_id", false, "memberships"},
		{"files", "bucket_id", "buckets", "_id", "orphan file bucket_id", false, "files"},
	}
	for _, o := range orphans {
		if !present[o.need] {
			continue
		}
		ok, err := hasMissingParent(ctx, db, quoted, o.child, o.childCol, o.parent, o.parentCol, o.skipNull, present[o.parent])
		if err != nil {
			return fmt.Errorf("copy system documents: check %s: %w", o.err, err)
		}
		if ok {
			return fmt.Errorf("copy system documents: %s", o.err)
		}
	}

	if present["users"] {
		ok, err := queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM (
        SELECT 1 FROM %s GROUP BY COALESCE(%s, '') HAVING COUNT(*) > 1
    ) d
)`, qtable(quoted, "users"), quoteIdent("email")))
		if err != nil {
			return fmt.Errorf("copy system documents: check duplicate email: %w", err)
		}
		if ok {
			return fmt.Errorf("copy system documents: duplicate users.email")
		}
	}
	if present["identities"] {
		ok, err := queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM (
        SELECT 1 FROM %s
        GROUP BY COALESCE(%s, ''), COALESCE(%s, '')
        HAVING COUNT(*) > 1
    ) d
)`, qtable(quoted, "identities"), quoteIdent("provider"), quoteIdent("provider_uid")))
		if err != nil {
			return fmt.Errorf("copy system documents: check duplicate identities: %w", err)
		}
		if ok {
			return fmt.Errorf("copy system documents: duplicate identities (provider, provider_uid)")
		}
	}
	if present["memberships"] {
		ok, err := queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM (
        SELECT 1 FROM %s
        WHERE %s IS NOT NULL AND %s <> ''
        GROUP BY %s, %s
        HAVING COUNT(*) > 1
    ) d
)`, qtable(quoted, "memberships"), quoteIdent("user_id"), quoteIdent("user_id"),
			quoteIdent("group_id"), quoteIdent("user_id")))
		if err != nil {
			return fmt.Errorf("copy system documents: check duplicate memberships user: %w", err)
		}
		if ok {
			return fmt.Errorf("copy system documents: duplicate memberships (group_id, user_id)")
		}
		ok, err = queryExists(ctx, db, fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM (
        SELECT 1 FROM %s
        WHERE %s IS NOT NULL AND %s <> ''
        GROUP BY %s, %s
        HAVING COUNT(*) > 1
    ) d
)`, qtable(quoted, "memberships"), quoteIdent("email"), quoteIdent("email"),
			quoteIdent("group_id"), quoteIdent("email")))
		if err != nil {
			return fmt.Errorf("copy system documents: check duplicate memberships email: %w", err)
		}
		if ok {
			return fmt.Errorf("copy system documents: duplicate memberships (group_id, email)")
		}
	}
	return nil
}

func truncateStaging(ctx context.Context, db *clients.Database, quoted string) error {
	parts := make([]string, len(stagingTables))
	for i, t := range stagingTables {
		parts[i] = quoted + "." + quoteIdent(t)
	}
	sql := "TRUNCATE TABLE " + strings.Join(parts, ", ") + " CASCADE"
	if _, err := db.Conn(ctx).ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("copy system documents: truncate staging: %w", err)
	}
	return nil
}

func insertStaging(ctx context.Context, db *clients.Database, quoted string, present map[string]bool) error {
	for _, doc := range copyInsertOrder {
		if !present[doc] {
			continue
		}
		sql, err := insertSQL(quoted, doc)
		if err != nil {
			return err
		}
		if _, err := db.Conn(ctx).ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("copy system documents: insert sys_%s: %w", doc, err)
		}
	}
	return nil
}

func jsonbOrDefault(alias, col, def string) string {
	ref := alias + "." + quoteIdent(col)
	return fmt.Sprintf("CASE WHEN %s IS NULL OR jsonb_typeof(%s) = 'null' THEN '%s'::jsonb ELSE %s END", ref, ref, def, ref)
}

func insertSQL(quoted, doc string) (string, error) {
	src := qtable(quoted, doc)
	dst := qtable(quoted, "sys_"+doc)
	switch doc {
	case "users":
		return fmt.Sprintf(`INSERT INTO %s (
    id, email, password_hash, name, status,
    email_verified, pending_email, phone, phone_verified,
    labels, prefs, factors, created_at, updated_at
)
SELECT
    u.%s,
    COALESCE(u.%s, ''),
    COALESCE(u.%s, ''),
    COALESCE(u.%s, ''),
    COALESCE(NULLIF(u.%s, ''), 'active'),
    COALESCE(u.%s, FALSE),
    COALESCE(u.%s, ''),
    COALESCE(u.%s, ''),
    COALESCE(u.%s, FALSE),
    %s,
    %s,
    %s,
    u.%s,
    u.%s
FROM %s AS u`, dst,
			quoteIdent("_id"), quoteIdent("email"), quoteIdent("password_hash"), quoteIdent("name"), quoteIdent("status"),
			quoteIdent("email_verified"), quoteIdent("pending_email"), quoteIdent("phone"), quoteIdent("phone_verified"),
			jsonbOrDefault("u", "labels", "[]"),
			jsonbOrDefault("u", "prefs", "{}"),
			jsonbOrDefault("u", "factors", "{}"),
			quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	case "groups":
		return fmt.Sprintf(`INSERT INTO %s (
    id, name, permissions, total, prefs, created_at, updated_at
)
SELECT
    g.%s,
    COALESCE(g.%s, ''),
    %s,
    COALESCE(g.%s, 0),
    %s,
    g.%s,
    g.%s
FROM %s AS g`, dst,
			quoteIdent("_id"), quoteIdent("name"),
			jsonbOrDefault("g", "permissions", "[]"),
			quoteIdent("total"),
			jsonbOrDefault("g", "prefs", "{}"),
			quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	case "buckets":
		return fmt.Sprintf(`INSERT INTO %s (
    id, name, permissions, %s, created_at, updated_at
)
SELECT
    b.%s,
    COALESCE(b.%s, ''),
    %s,
    COALESCE(b.%s, FALSE),
    b.%s,
    b.%s
FROM %s AS b`, dst, quoteIdent("public"),
			quoteIdent("_id"), quoteIdent("name"),
			jsonbOrDefault("b", "permissions", "[]"),
			quoteIdent("public"),
			quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	case "sessions":
		return fmt.Sprintf(`INSERT INTO %s (
    id, user_id, secret_hash, provider, user_agent, ip, country,
    factors, expire_at, created_at, updated_at
)
SELECT
    s.%s,
    s.%s,
    s.%s,
    COALESCE(NULLIF(s.%s, ''), 'email'),
    COALESCE(s.%s, ''),
    COALESCE(s.%s, ''),
    COALESCE(s.%s, ''),
    %s,
    s.%s,
    s.%s,
    s.%s
FROM %s AS s`, dst,
			quoteIdent("_id"), quoteIdent("user_id"), quoteIdent("secret_hash"),
			quoteIdent("provider"), quoteIdent("user_agent"), quoteIdent("ip"), quoteIdent("country"),
			jsonbOrDefault("s", "factors", "{}"),
			quoteIdent("expire_at"), quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	case "identities":
		return fmt.Sprintf(`INSERT INTO %s (
    id, user_id, provider, provider_uid, provider_email, provider_data,
    expire_at, created_at, updated_at
)
SELECT
    i.%s,
    i.%s,
    i.%s,
    i.%s,
    COALESCE(i.%s, ''),
    %s,
    i.%s,
    i.%s,
    i.%s
FROM %s AS i`, dst,
			quoteIdent("_id"), quoteIdent("user_id"), quoteIdent("provider"), quoteIdent("provider_uid"),
			quoteIdent("provider_email"),
			jsonbOrDefault("i", "provider_data", "{}"),
			quoteIdent("expire_at"), quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	case "memberships":
		return fmt.Sprintf(`INSERT INTO %s (
    id, group_id, user_id, email, name, roles, status,
    invited_at, joined_at, created_at, updated_at
)
SELECT
    m.%s,
    m.%s,
    NULLIF(m.%s, ''),
    COALESCE(m.%s, ''),
    COALESCE(m.%s, ''),
    %s,
    COALESCE(NULLIF(m.%s, ''), 'pending'),
    m.%s,
    m.%s,
    m.%s,
    m.%s
FROM %s AS m`, dst,
			quoteIdent("_id"), quoteIdent("group_id"), quoteIdent("user_id"),
			quoteIdent("email"), quoteIdent("name"),
			jsonbOrDefault("m", "roles", "[]"),
			quoteIdent("status"),
			quoteIdent("invited_at"), quoteIdent("joined_at"),
			quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	case "files":
		createdBy := quoteIdent("_created_by")
		users := qtable(quoted, "sys_users")
		return fmt.Sprintf(`INSERT INTO %s (
    id, bucket_id, name, mime_type, size, metadata, owner_user_id, created_at, updated_at
)
SELECT
    f.%s,
    f.%s,
    COALESCE(f.%s, ''),
    COALESCE(f.%s, ''),
    COALESCE(f.%s, 0),
    %s,
    CASE
        WHEN NULLIF(f.%s, '') IS NULL THEN NULL
        WHEN EXISTS (SELECT 1 FROM %s su WHERE su.id = f.%s) THEN f.%s
        ELSE NULL
    END,
    f.%s,
    f.%s
FROM %s AS f`, dst,
			quoteIdent("_id"), quoteIdent("bucket_id"), quoteIdent("name"),
			quoteIdent("mime_type"), quoteIdent("size"),
			jsonbOrDefault("f", "metadata", "{}"),
			createdBy, users, createdBy, createdBy,
			quoteIdent("_created_at"), quoteIdent("_updated_at"), src), nil
	default:
		return "", fmt.Errorf("copy system documents: unknown table %s", doc)
	}
}
