package documentdb

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// occTestProject 创建带 "app"."docs" 用户集合与默认系统集合的测试环境。
func occTestProject(t *testing.T, ctx context.Context) (databases.DocumentDB, string, int64, func()) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))
	cleanupFn := func() {
		db.Close()
		cleanup()
	}
	return docDB, projectID, internalID, cleanupFn
}

func occPrincipal() databases.Principal {
	return databases.Principal{Roles: []string{"users", "user:u1"}}
}

func occCreate(t *testing.T, docDB databases.DocumentDB, projectID, id string) databases.Document {
	t.Helper()
	created, err := docDB.CreateDocument(context.Background(), projectID, "app", "docs", databases.Document{
		ID:   id,
		Data: map[string]any{"title": "t1", "views": 1},
	}, []databases.Permission{
		{Type: "read", Role: "user:u1"},
		{Type: "update", Role: "user:u1"},
		{Type: "delete", Role: "user:u1"},
	}, occPrincipal())
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version, "Create 后 version 应为 1")
	return created
}

// TestUpdateDocument_VersionRequired：用户集合 Update（含仅 increment / 仅 permissions）
// 未携带 ExpectedVersion → ErrVersionRequired，行不变。
func TestUpdateDocument_VersionRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, _, cleanup := occTestProject(t, ctx)
	defer cleanup()

	created := occCreate(t, docDB, projectID, "d1")

	cases := []struct {
		name   string
		update databases.DocumentUpdate
	}{
		{"无 data", databases.DocumentUpdate{Document: databases.Document{ID: created.ID, Data: map[string]any{"title": "x"}}}},
		{"仅 increment", databases.DocumentUpdate{Document: databases.Document{ID: created.ID}, Increment: map[string]int64{"views": 5}}},
		{"仅 permissions", databases.DocumentUpdate{Document: databases.Document{ID: created.ID}, Permissions: []databases.Permission{{Type: "read", Role: "user:u1"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", tc.update, occPrincipal())
			require.ErrorIs(t, err, databases.ErrVersionRequired)
		})
	}

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "t1", got.Data["title"])
	require.Equal(t, int64(1), got.Version, "失败更新不得改行/版本")
}

// TestUpdateDocument_VersionMismatch：ExpectedVersion 与当前行不符 →
// ErrVersionMismatch，行不变（不覆盖并发写）。
func TestUpdateDocument_VersionMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, _, cleanup := occTestProject(t, ctx)
	defer cleanup()

	created := occCreate(t, docDB, projectID, "d1")

	_, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document:        databases.Document{ID: created.ID, Data: map[string]any{"title": "stale write"}},
		ExpectedVersion: 99,
	}, occPrincipal())
	require.ErrorIs(t, err, databases.ErrVersionMismatch)

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "t1", got.Data["title"], "version 不匹配时行必须保持不变")
	require.Equal(t, int64(1), got.Version)
}

// TestUpdateDocument_IncrementRequiresVersion：Increment 与普通 Update 同一条
// 强制路径——不传 version → ErrVersionRequired；传对 → +1 成功。
func TestUpdateDocument_IncrementRequiresVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, _, cleanup := occTestProject(t, ctx)
	defer cleanup()

	created := occCreate(t, docDB, projectID, "d1")

	_, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document:  databases.Document{ID: created.ID},
		Increment: map[string]int64{"views": 5},
	}, occPrincipal())
	require.ErrorIs(t, err, databases.ErrVersionRequired, "Increment 未带 version 必须拒绝")

	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document:        databases.Document{ID: created.ID},
		Increment:       map[string]int64{"views": 5},
		ExpectedVersion: 1,
	}, occPrincipal())
	require.NoError(t, err)
	require.EqualValues(t, 6, updated.Data["views"])
	require.Equal(t, int64(2), updated.Version, "Increment 成功写必须 +1")
}

// TestDeleteDocument_VersionMismatch：Delete 带错 version → ErrVersionMismatch，
// 行保留；不传 version → ErrVersionRequired。
func TestDeleteDocument_VersionMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, _, cleanup := occTestProject(t, ctx)
	defer cleanup()

	created := occCreate(t, docDB, projectID, "d1")

	err := docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, databases.DeleteOptions{}, occPrincipal())
	require.ErrorIs(t, err, databases.ErrVersionRequired, "Delete 未带 version 必须拒绝")

	err = docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, databases.DeleteOptions{ExpectedVersion: 99}, occPrincipal())
	require.ErrorIs(t, err, databases.ErrVersionMismatch)

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got, "version 不匹配时行必须保留")

	require.NoError(t, docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, databases.DeleteOptions{ExpectedVersion: got.Version}, occPrincipal()))
	gone, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, gone)
}

// TestBulkUpdate_SkipsVersion：Bulk 是唯一允许跳过 OCC 的 Update/Delete 调用方——
// 不传 version 仍成功，且每行 _version +1。
func TestBulkUpdate_SkipsVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	docDB, projectID, _, cleanup := occTestProject(t, ctx)
	defer cleanup()

	d1 := occCreate(t, docDB, projectID, "d1")
	d2 := occCreate(t, docDB, projectID, "d2")

	affected, err := docDB.BulkUpdateDocuments(ctx, projectID, "app", "docs", []string{d1.ID, d2.ID},
		map[string]any{"title": "bulk"}, nil, occPrincipal())
	require.NoError(t, err)
	require.Equal(t, int64(2), affected)

	got1, err := docDB.GetDocument(ctx, projectID, "app", "docs", d1.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "bulk", got1.Data["title"])
	require.Equal(t, int64(2), got1.Version, "Bulk 更新必须 _version +1（不要求客户端 version）")

	affected, err = docDB.BulkDeleteDocuments(ctx, projectID, "app", "docs", []string{d1.ID, d2.ID}, occPrincipal())
	require.NoError(t, err)
	require.Equal(t, int64(2), affected)
}

// TestUpsert_NoVersionCheck：Upsert 插入支 DEFAULT 1；更新支盲写（无 OCC）但 +1。
func TestUpsert_NoVersionCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, nil, true))

	// 插入支：version=1。
	created, err := docDB.UpsertDocument(ctx, projectID, "app", "members", databases.Document{
		ID:   "u1",
		Data: map[string]any{"email": "a@b.c", "name": "first"},
	}, []string{"email"}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version, "Upsert 插入支 version 应为 1")

	// 更新支：不校验 version（盲写），version +1。
	updated, err := docDB.UpsertDocument(ctx, projectID, "app", "members", databases.Document{
		ID:   "u1",
		Data: map[string]any{"email": "a@b.c", "name": "second"},
	}, []string{"email"}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "second", updated.Data["name"])
	require.Equal(t, int64(2), updated.Version, "Upsert 更新支必须 _version +1")

	got, err := docDB.GetDocument(ctx, projectID, "app", "members", "u1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Version)
}

// TestSystemCollection_NoVersionColumn：系统集合表没有 _version 列，
// 写路径不 ALTER；系统集合 Update/Delete 不要求 version 也不 SET _version。
func TestSystemCollection_NoVersionColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	schema := testSchema(t, projectID, "default")
	var count int
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = ? AND table_name = 'users' AND column_name = '_version'`,
		schema).Scan(&count))
	require.Zero(t, count, "系统集合 users 表不得有 _version 列")

	// 系统集合写路径不要求 version（内部 Users 等高频更新）且不 SET _version。
	userDoc, err := docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		ID:   "u1",
		Data: map[string]any{"email": "a@b.c", "status": "active"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	updated, err := docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userDoc.ID,
		Data: map[string]any{"name": "n"},
	}, nil), databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "n", updated.Data["name"])
	// adapter 读路径缺列视为 1（系统集合恒无该列）；app 层按 IsSystemCollection
	// 归零，wire 契约 Document.version 系统集合为 0。
	require.Equal(t, int64(1), updated.Version)

	require.NoError(t, docDB.DeleteDocument(ctx, projectID, "default", "users", userDoc.ID, databases.DeleteOptions{}, databases.SystemPrincipal))
}

// TestVersionColumn_LazyAlterAndReadFallback：存量用户表（无 _version 列）
// 写路径懒 ALTER（bigint NOT NULL DEFAULT 1）；读路径缺列视为 1、不 ALTER；
// 未 ALTER 前 $version 查询返回 version_column_unavailable，不落 PG 未定义列。
func TestVersionColumn_LazyAlterAndReadFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	// 模拟存量表：先手工建旧形状的表（无 _version），再走 CreateCollection
	// （CREATE TABLE IF NOT EXISTS 幂等保留旧形状，只补元数据）。
	schema := testSchema(t, projectID, "app")
	_, err := db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		_id TEXT NOT NULL,
		_tenant BIGINT NOT NULL,
		"title" TEXT,
		_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		_created_by TEXT,
		_updated_by TEXT,
		PRIMARY KEY (_tenant, _id))`, tableName(schema, "docs")))
	require.NoError(t, err)

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
	}, true))

	// 直接插存量行（无 _version 列）。
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (_id, _tenant) VALUES ('legacy', ?)`, tableName(schema, "docs")), internalID)
	require.NoError(t, err)

	// 读路径：缺列视为 1；且读路径不得触发 ALTER（列仍不存在）。
	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", "legacy", databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Version, "存量表读路径缺列视为 1")
	var count int
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = ? AND table_name = 'docs' AND column_name = '_version'`,
		schema).Scan(&count))
	require.Zero(t, count, "读路径不得 ALTER")

	// 未 ALTER 表上的 $version 查询：version_column_unavailable（不落 PG 42703）。
	// 非 System 主体才会走 validateQueryFields（System 路径信任内部调用）。
	listPrincipal := databases.Principal{Roles: []string{"users", "user:u1"}}
	_, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$version", 2)`},
	}, listPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), databases.ErrVersionColumnUnavailable.Error())

	// 写路径懒 ALTER + 类型检查：合法 bigint 就绪，存量行 DEFAULT 1 回填。
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document: databases.Document{
			ID:   "legacy",
			Data: map[string]any{"title": "t"},
		},
		ExpectedVersion: 1,
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Version, "懒 ALTER 后更新 version 1→2")

	// ALTER 后 $version 查询可用（读路径 cache 复用写路径的确保记录）。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$version", 2)`},
	}, listPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "legacy", list.Documents[0].ID)
}

// TestVersionColumn_TypeConflictFailClosed：存量用户表已有非 bigint _version 列
// （用户属性抢占）→ 写路径拒绝 OCC（version_column_conflict），禁止类型错误的 +1。
func TestVersionColumn_TypeConflictFailClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	// 模拟存量表：_version 被 TEXT 列抢占。
	schema := testSchema(t, projectID, "app")
	_, err := db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		_id TEXT NOT NULL,
		_tenant BIGINT NOT NULL,
		_version TEXT NOT NULL DEFAULT 'v0',
		_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		_created_by TEXT,
		_updated_by TEXT,
		PRIMARY KEY (_tenant, _id))`, tableName(schema, "docs")))
	require.NoError(t, err)

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", nil, nil, nil, true))

	_, err = docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "x"},
	}, nil, databases.SystemPrincipal)
	require.ErrorIs(t, err, databases.ErrVersionColumnConflict, "非 bigint _version 列必须 fail-closed")

	// 列保持原样（未被改写类型）。
	var udtName string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT udt_name FROM information_schema.columns WHERE table_schema = ? AND table_name = 'docs' AND column_name = '_version'`,
		schema).Scan(&udtName))
	require.Equal(t, "text", udtName)
}

// TestUpdateDocument_EnsureVersionRollbackDoesNotPoisonCache (F1)：写路径在
// 事务内 ALTER 新建 _version 列后若事务回滚（如权限失败），进程 cache 不得
// 记录"已确保"——否则下次 Update 会拼 _version = _version + 1 撞 42703，
// $version 查询 cache hit 同样 42703（规格禁止读路径落 42703）。
func TestUpdateDocument_EnsureVersionRollbackDoesNotPoisonCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
		// 注意：不给集合级 update/delete——文档写权限完全依赖文档级 _perms，
		// 否则无 perms 行的存量行会走集合级兜底（user:other 也能更新）。
	}, true))

	// 模拟"尚未 ALTER 的存量表"：新建集合自带 _version，这里手动 DROP。
	schema := testSchema(t, projectID, "app")
	_, err := db.DB.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s DROP COLUMN _version`, tableName(schema, "docs")))
	require.NoError(t, err)

	// 存量行用 SQL 直插（CreateDocument 是写路径，会先触发懒 ALTER，不能用来造数）；
	// 文档级 read/update 仅授予 owner（user:u1）。
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (_id, _tenant, "title") VALUES ('d1', ?, 't1')`, tableName(schema, "docs")), internalID)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (_tenant, _collection, _document, _type, _permission) VALUES (?, 'docs', 'd1', 'read', 'user:u1'), (?, 'docs', 'd1', 'update', 'user:u1')`,
		permsTableName(schema)), internalID, internalID)
	require.NoError(t, err)

	owner := databases.Principal{Roles: []string{"users", "user:u1"}}

	// 1. 无 update 权限 principal（带合法 ExpectedVersion）→ 权限失败，
	//    事务回滚撤销本事务内 ALTER 的 _version 列。
	_, err = docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document: databases.Document{
			ID:   "d1",
			Data: map[string]any{"title": "hacked"},
		},
		ExpectedVersion: 1,
	}, databases.Principal{Roles: []string{"users", "user:other"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	// 2. 表上仍然没有 _version 列（ALTER 随 ROLLBACK 撤销）。
	var count int
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = ? AND table_name = 'docs' AND column_name = '_version'`,
		schema).Scan(&count))
	require.Zero(t, count, "权限失败回滚后 _version 列必须不存在")

	// 3. 有权限 principal 再 Update（ExpectedVersion=1）→ 成功，不得 42703；
	//    行 _version 变为 2（若 cache 被污染，此处会撞 undefined column）。
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.DocumentUpdate{
		Document: databases.Document{
			ID:   "d1",
			Data: map[string]any{"title": "t2"},
		},
		ExpectedVersion: 1,
	}, owner)
	require.NoError(t, err, "回滚后再次 Update 不得 42703")
	require.Equal(t, int64(2), updated.Version)

	// 4. 同一集合 $version 查询 → 成功（不得 version_column_unavailable，不得 42703）。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$version", 2)`},
	}, owner)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "d1", list.Documents[0].ID)
}

// TestQueryVersion_TypeConflictFailClosed (F4)：读路径遇非 bigint _version 列
// 与写路径同码 version_column_conflict（Fail-closed），仅"列不存在"才是
// version_column_unavailable。
func TestQueryVersion_TypeConflictFailClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	// 存量表：_version 被 TEXT 列抢占。
	schema := testSchema(t, projectID, "app")
	_, err := db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		_id TEXT NOT NULL,
		_tenant BIGINT NOT NULL,
		_version TEXT NOT NULL DEFAULT 'v0',
		_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		_created_by TEXT,
		_updated_by TEXT,
		PRIMARY KEY (_tenant, _id))`, tableName(schema, "docs")))
	require.NoError(t, err)

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", nil, nil, []databases.Permission{
		{Type: "read", Role: "any"},
	}, true))

	_, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`equal("$version", 1)`},
	}, databases.Principal{Roles: []string{"users", "user:u1"}})
	require.ErrorIs(t, err, databases.ErrVersionColumnConflict)
	require.Contains(t, err.Error(), databases.ErrVersionColumnConflict.Error())
	require.NotContains(t, err.Error(), databases.ErrVersionColumnUnavailable.Error())
}

// TestCreateAttribute_AdapterRejectsReservedColumns (F7)：直调 adapter 的
// CreateAttribute 也拒绝系统保留列（含 _version）——app 层校验之外的第二道
// fail-closed，防止 ADD COLUMN 破坏 OCC 列。
func TestCreateAttribute_AdapterRejectsReservedColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", nil, nil, nil, true))

	err := docDB.CreateAttribute(ctx, projectID, "app", "docs", databases.Attribute{Key: "_version", Type: "integer"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), `attribute key "_version" is reserved`)

	// 列未被改动：_version 仍为 bigint（未退化成用户属性）。
	schema := testSchema(t, projectID, "app")
	var udtName string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT udt_name FROM information_schema.columns WHERE table_schema = ? AND table_name = 'docs' AND column_name = '_version'`,
		schema).Scan(&udtName))
	require.Equal(t, "int8", udtName)
}

// TestQueryVersion_SystemCollectionRejected：系统集合 $version 查询 → InvalidArgument
// （系统表无此列），不落 PG。teams 对 keys 角色可读（非敏感系统集合），
// 走 validateQueryFields 的 isSystem 分支。
func TestQueryVersion_SystemCollectionRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.ListDocuments(ctx, projectID, "default", "teams", databases.Query{
		Queries: []string{`equal("$version", 1)`},
	}, databases.Principal{Roles: []string{"keys"}})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "invalid query field")
	require.NotContains(t, err.Error(), "42703", "不得落到 PG 未定义列错误")
	require.False(t, strings.Contains(err.Error(), databases.ErrVersionColumnUnavailable.Error()),
		"系统集合拒绝应走 invalid query field，而非 version_column_unavailable")
}
