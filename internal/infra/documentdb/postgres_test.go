package documentdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/query"
	"github.com/uptrace/bun/driver/pgdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPostgresDocumentDatabase_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	// Create a custom database and collection.
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", nil, nil, nil, true))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, []databases.Index{
		{ID: "title_key", Type: "key", Attributes: []string{"title"}},
	}, nil, true))

	// Create document.
	created, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		Data: map[string]any{
			"title": "Hello World",
			"views": 42,
		},
	}, []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "update", Role: "any"},
		{Type: "delete", Role: "any"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	// Get document.
	got, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, "Hello World", got.Data["title"])

	// Update document.
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "posts", databases.DocumentUpdate{
		Document: databases.Document{
			ID: got.ID,
			Data: map[string]any{
				"views": 100,
			},
		},
		ExpectedVersion: got.Version,
	}, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, float64(100), updated.Data["views"])

	// List with Appwrite-style query.
	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`greaterThan("views",50)`, `orderDesc("$createdAt")`},
	}, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, int64(1), list.TotalCount)

	// Count.
	count, err := docDB.CountDocuments(ctx, projectID, "app", "posts", databases.Query{Queries: []string{`equal("title","Hello World")`}}, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	// Delete.
	require.NoError(t, docDB.DeleteDocument(ctx, projectID, "app", "posts", created.ID, databases.DeleteOptions{ExpectedVersion: updated.Version}, databases.Principal{Roles: []string{"any"}}))
	got2, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Nil(t, got2)
}

// TestPostgresDocumentDatabase_UpsertDocument (T2): UpsertDocument inserts when
// no row matches the conflict columns and updates (data/_updated_at/_updated_by
// + permissions replaced) when one does. conflictColumns must match a unique
// index on the collection table.
func TestPostgresDocumentDatabase_UpsertDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "channel_id", Key: "channel_id", Type: "string", Size: 64},
		{ID: "user_id", Key: "user_id", Type: "string", Size: 64},
		{ID: "last_read_seq", Key: "last_read_seq", Type: "integer"},
	}, []databases.Index{
		{ID: "member_key", Type: "unique", Attributes: []string{"channel_id", "user_id"}},
	}, nil, true))

	// First upsert: no matching row → insert.
	upserted, err := docDB.UpsertDocument(ctx, projectID, "app", "members", databases.Document{
		ID: "m1",
		Data: map[string]any{
			"channel_id":    "ch1",
			"user_id":       "u1",
			"last_read_seq": 10,
		},
	}, []string{"channel_id", "user_id"}, []databases.Permission{
		{Type: "read", Role: "user:u1"},
		{Type: "update", Role: "user:u1"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, float64(10), upserted.Data["last_read_seq"])

	// Second upsert: row matches the conflict columns → update.
	upserted, err = docDB.UpsertDocument(ctx, projectID, "app", "members", databases.Document{
		ID: "m1",
		Data: map[string]any{
			"channel_id":    "ch1",
			"user_id":       "u1",
			"last_read_seq": 42,
		},
	}, []string{"channel_id", "user_id"}, []databases.Permission{
		{Type: "read", Role: "user:u1"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, float64(42), upserted.Data["last_read_seq"])

	// GetDocument confirms the data was updated and permissions replaced.
	got, err := docDB.GetDocument(ctx, projectID, "app", "members", "m1", databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, float64(42), got.Data["last_read_seq"])
	require.Len(t, got.Permissions, 1)
	require.Equal(t, databases.Permission{Type: "read", Role: "user:u1"}, got.Permissions[0])
}

// TestPostgresDocumentDatabase_UpsertDocument_PrivilegeEscalationRejected
// (P0-1): a principal holding only collection-level create must not be able to
// update another user's row by submitting a new _id whose conflict columns
// collide with an existing row.
func TestPostgresDocumentDatabase_UpsertDocument_PrivilegeEscalationRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "users", "Users", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "email_key", Type: "unique", Attributes: []string{"email"}},
	}, []databases.Permission{
		// 集合仅授予 create:users：攻击者（users）可创建新文档，但没有任何
		// update 权限——越权路径若命中他人行，必须被文档级 update 校验拦下。
		{Type: "create", Role: "users"},
	}, true))

	// 文档 A：系统主体创建，权限只授予 owner，不含攻击者。
	_, err := docDB.CreateDocument(ctx, projectID, "app", "users", databases.Document{
		ID: "doc-a",
		Data: map[string]any{
			"email": "a@x.com",
			"name":  "original",
		},
	}, []databases.Permission{
		{Type: "read", Role: "user:owner"},
		{Type: "update", Role: "user:owner"},
		{Type: "delete", Role: "user:owner"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	// 攻击者：仅持集合级 create 权限（默认 create:users），无任何文档级权限。
	attacker := databases.Principal{Roles: []string{"users"}}
	_, err = docDB.UpsertDocument(ctx, projectID, "app", "users", databases.Document{
		ID: "doc-b",
		Data: map[string]any{
			"email": "a@x.com", // 命中 doc-a 的唯一索引 → 实际会 UPDATE doc-a
			"name":  "hacked",
		},
	}, []string{"email"}, nil, attacker)
	require.ErrorIs(t, err, ErrPermissionDenied)

	// 文档 A 必须未被修改。
	got, err := docDB.GetDocument(ctx, projectID, "app", "users", "doc-a", databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "original", got.Data["name"])
}

// TestPostgresDocumentDatabase_UpsertDocument_ConcurrentRace (P0-1 并发版)：
// victim 与 attacker 并发 upsert 同一冲突值。advisory lock 串行化后：
// attacker 先执行 → 插入自己的行，随后 victim 的 upsert 覆盖为 victim 数据；
// attacker 后执行 → 预查命中 victim 行 → update 权限拒绝（ErrPermissionDenied）。
// 两种交错下最终数据都必须保持 victim 的值，attacker 无法改写。
func TestPostgresDocumentDatabase_UpsertDocument_ConcurrentRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "users", "Users", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "email_key", Type: "unique", Attributes: []string{"email"}},
	}, []databases.Permission{
		{Type: "create", Role: "users"},
	}, true))

	start := make(chan struct{})
	errCh := make(chan error, 2)
	victim := func() {
		<-start
		_, err := docDB.UpsertDocument(ctx, projectID, "app", "users", databases.Document{
			ID:   "victim-doc",
			Data: map[string]any{"email": "race@x.com", "name": "original"},
		}, []string{"email"}, nil, databases.SystemPrincipal)
		errCh <- err
	}
	attacker := func() {
		<-start
		_, err := docDB.UpsertDocument(ctx, projectID, "app", "users", databases.Document{
			ID:   "attacker-doc",
			Data: map[string]any{"email": "race@x.com", "name": "hacked"},
		}, []string{"email"}, nil, databases.Principal{Roles: []string{"users"}})
		errCh <- err
	}
	go victim()
	go attacker()
	close(start)
	victimErr := <-errCh
	attackerErr := <-errCh

	require.NoError(t, victimErr)
	if attackerErr != nil {
		require.ErrorIs(t, attackerErr, ErrPermissionDenied)
	}

	// 唯一键保证集合内仅一行，且数据必须保持 victim 的值。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "users", databases.Query{
		Queries: []string{`equal("email","race@x.com")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "original", list.Documents[0].Data["name"])
}

func TestPostgresDocumentDatabase_Permissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	created, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		Data: map[string]any{
			"email": "perm@torchwood.local",
			"name":  "Permission Test",
		},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	// User without permission cannot read.
	list, err := docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// User with permission can read.
	list, err = docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)

	// System roles bypass permissions.
	list, err = docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(list.Documents), 1)

	// Get without permission is denied.
	_, err = docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "users", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	// Get with permission succeeds.
	got, err := docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "users", created.ID, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
	require.Equal(t, "perm@torchwood.local", got.Data["email"])
}

func TestEnsureSystemCollections_MultipleProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	docDB := NewPostgresDocumentDB(db, nil)

	projectA, internalA, cleanupA := testutil.CreateTestProject(ctx, db)
	defer cleanupA()
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectA, internalA))

	// Second project must use a unique name (projects.name is unique).
	projectB := &model.Project{
		ID:        "testprojectb",
		Name:      "Test Project B",
		Status:    "active",
		Settings:  map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err := db.NewInsert().Model(projectB).Exec(ctx)
	require.NoError(t, err)
	defer func() { _, _ = db.NewDelete().Model((*model.Project)(nil)).Where("id = ?", projectB.ID).Exec(ctx) }()
	var internalB int64
	require.NoError(t, db.NewSelect().Model((*model.Project)(nil)).Column("internal_id").Where("id = ?", projectB.ID).Scan(ctx, &internalB))

	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectB.ID, internalB))

	collA, err := docDB.GetCollection(ctx, projectA, databases.SystemDatabaseID, "users")
	require.NoError(t, err)
	require.NotNil(t, collA)

	collB, err := docDB.GetCollection(ctx, projectB.ID, databases.SystemDatabaseID, "users")
	require.NoError(t, err)
	require.NotNil(t, collB)
}

// TestErrDuplicateKey_DomainAlias (#8): infra alias must be the same instance
// as the domain error so errors.Is comparisons keep working.
func TestErrDuplicateKey_DomainAlias(t *testing.T) {
	require.Equal(t, databases.ErrDuplicateKey, ErrDuplicateKey)
	require.True(t, errors.Is(ErrDuplicateKey, databases.ErrDuplicateKey))
	require.True(t, errors.Is(databases.ErrDuplicateKey, ErrDuplicateKey))
}

// TestDeleteCollection_CleansPerms (#3): deleting a collection must remove its
// _perms rows so that recreating the same collection cannot leak old
// document-level permissions onto new documents.
func TestDeleteCollection_CleansPerms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))

	createColl := func() {
		t.Helper()
		require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
			{ID: "title", Key: "title", Type: "string", Size: 256},
		}, nil, nil, true))
	}
	countPerms := func() int {
		t.Helper()
		var n int
		sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE _tenant = ? AND _collection = ?`, permsTableName(testSchema(t, projectID, "app")))
		require.NoError(t, db.DB.QueryRowContext(ctx, sql, internalID, "notes").Scan(&n))
		return n
	}

	createColl()
	_, err := docDB.CreateDocument(ctx, projectID, "app", "notes", databases.Document{
		ID:   "doc-1",
		Data: map[string]any{"title": "secret"},
	}, []databases.Permission{{Type: "read", Role: "user:alice"}}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, 1, countPerms())

	// After deletion no _perms rows may remain for the collection.
	require.NoError(t, docDB.DeleteCollection(ctx, projectID, "app", "notes"))
	require.Equal(t, 0, countPerms())

	// Recreate the same collection and a document with the same ID but no
	// document-level permissions: alice must NOT see it (no old-perms leak).
	createColl()
	_, err = docDB.CreateDocument(ctx, projectID, "app", "notes", databases.Document{
		ID:   "doc-1",
		Data: map[string]any{"title": "fresh"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	alice := databases.Principal{Roles: []string{"user:alice"}}
	list, err := docDB.ListDocuments(ctx, projectID, "app", "notes", databases.Query{Queries: []string{`equal("$id","doc-1")`}}, alice)
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// System can still see the recreated document.
	sys, err := docDB.ListDocuments(ctx, projectID, "app", "notes", databases.Query{Queries: []string{`equal("$id","doc-1")`}}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, sys.Documents, 1)
}

// TestListDocuments_MultiValueEqualNotEqual (#4): multi-value equal/notEqual
// must work on non-text columns (BIGINT) and keep single/multi value behavior
// on string columns.
func TestListDocuments_MultiValueEqualNotEqual(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, nil, nil, true))

	ids := make([]string, 0, 5)
	for i, title := range []string{"a", "b", "c", "d", "e"} {
		created, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
			Data: map[string]any{"title": title, "views": i + 1},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
		ids = append(ids, created.ID)
	}
	_ = ids

	// Multi-value equal on an integer column.
	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("views",[1,2,3])`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 3)
	got := map[float64]bool{}
	for _, d := range list.Documents {
		got[d.Data["views"].(float64)] = true
	}
	require.True(t, got[1] && got[2] && got[3])

	// Multi-value notEqual on an integer column.
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`notEqual("views",[1,2])`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 3)
	got = map[float64]bool{}
	for _, d := range list.Documents {
		got[d.Data["views"].(float64)] = true
	}
	require.True(t, got[3] && got[4] && got[5])

	// Single value equal on a string column (regression).
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title","a")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "a", list.Documents[0].Data["title"])

	// Multi-value equal on a string column (regression).
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title",["a","b"])`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 2)

	// Single value notEqual on a string column (regression).
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`notEqual("title","a")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 4)
}

// TestListDocuments_AstOr compiles proto/AST or into SQL OR (two matching rows).
func TestListDocuments_AstOr(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	for _, title := range []string{"alpha", "beta", "gamma"} {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
			Data: map[string]any{"title": title},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	ast := &query.Query{
		Filter: &query.Filter{Op: query.OpOr, Children: []*query.Filter{
			{Op: query.OpEqual, Attribute: "title", Values: []string{"alpha"}},
			{Op: query.OpEqual, Attribute: "title", Values: []string{"gamma"}},
		}},
	}
	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{AST: ast}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(2), list.TotalCount)
	got := map[string]bool{}
	for _, d := range list.Documents {
		got[d.Data["title"].(string)] = true
	}
	require.True(t, got["alpha"] && got["gamma"])
	require.False(t, got["beta"])

	count, err := docDB.CountDocuments(ctx, projectID, "app", "posts", databases.Query{AST: ast}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
}

// TestListDocuments_SelectProjection (#6a): select() must filter Data to the
// chosen keys while system fields always remain on the Document struct, with
// $id/$createdAt/$updatedAt aliases honored.
func TestListDocuments_SelectProjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "profiles", "Profiles", []databases.Attribute{
		{ID: "name", Key: "name", Type: "string", Size: 256},
		{ID: "age", Key: "age", Type: "integer"},
		{ID: "email", Key: "email", Type: "string", Size: 256},
	}, nil, nil, true))

	created, err := docDB.CreateDocument(ctx, projectID, "app", "profiles", databases.Document{
		Data: map[string]any{"name": "alice", "age": 30, "email": "a@b.c"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	// Projection to ["name","age"]: Data only holds those keys.
	list, err := docDB.ListDocuments(ctx, projectID, "app", "profiles", databases.Query{
		Queries: []string{`select(["name","age"])`, `limit(10)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	doc := list.Documents[0]
	require.Equal(t, map[string]any{"name": "alice", "age": float64(30)}, doc.Data)
	require.NotEmpty(t, doc.ID)
	require.False(t, doc.CreatedAt.IsZero())

	// Projection to ["$id"]: alias maps to the system _id field, so Data is
	// empty while the system fields remain.
	list, err = docDB.ListDocuments(ctx, projectID, "app", "profiles", databases.Query{
		Queries: []string{`select(["$id"])`, `limit(10)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	doc = list.Documents[0]
	require.Empty(t, doc.Data)
	require.Equal(t, created.ID, doc.ID)
	require.False(t, doc.CreatedAt.IsZero())
}

// TestListDocuments_CursorPagination (#6b): cursorAfter/cursorBefore keyset
// pagination with default DESC ordering, explicit orderAsc, reverse cursor,
// and error cases (missing cursor doc / invalid order field).
func TestListDocuments_CursorPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "seqdocs", "SeqDocs", []databases.Attribute{
		{ID: "seq", Key: "seq", Type: "integer"},
		{ID: "age", Key: "age", Type: "integer"},
	}, nil, nil, true))

	// Create d1..d4 with strictly increasing _created_at (d4 latest).
	for i := 1; i <= 4; i++ {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "seqdocs", databases.Document{
			ID:   "d" + string(rune('0'+i)),
			Data: map[string]any{"seq": i, "age": 50 - i*10},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}
	orderDESC := []string{"d4", "d3", "d2", "d1"}

	// Default ordering (DESC) cursorAfter pagination: page 1 = [d4, d3],
	// page 2 with cursor on the last id of page 1 = [d2, d1] (no overlap,
	// list exhausted).
	page1, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, page1.Documents, 2)
	require.Equal(t, orderDESC[:2], []string{page1.Documents[0].ID, page1.Documents[1].ID})

	page2, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`, `cursorAfter("` + page1.Documents[1].ID + `")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, page2.Documents, 2)
	require.Equal(t, orderDESC[2:], []string{page2.Documents[0].ID, page2.Documents[1].ID})
	require.NotEqual(t, page1.Documents[0].ID, page2.Documents[0].ID)
	require.NotEqual(t, page1.Documents[1].ID, page2.Documents[0].ID)

	// cursorAfter + orderAsc("age"): ages 10,20,30,40 → page1 [d4,d3],
	// page2 [d2,d1].
	asc1, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`orderAsc("age")`, `limit(2)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, asc1.Documents, 2)
	require.Equal(t, "d4", asc1.Documents[0].ID)
	require.Equal(t, "d3", asc1.Documents[1].ID)

	asc2, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`orderAsc("age")`, `limit(2)`, `cursorAfter("` + asc1.Documents[1].ID + `")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, asc2.Documents, 2)
	require.Equal(t, "d2", asc2.Documents[0].ID)
	require.Equal(t, "d1", asc2.Documents[1].ID)

	// cursorBefore reverse pagination (default DESC): the predicate
	// (created_at, _id) > cursor selects the rows before the cursor in the
	// DESC result order, i.e. the previous page [d4, d3] before d2.
	rev, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`, `cursorBefore("d2")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, rev.Documents, 2)
	require.Equal(t, "d4", rev.Documents[0].ID)
	require.Equal(t, "d3", rev.Documents[1].ID)

	// Cursor document does not exist → InvalidArgument.
	_, err = docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`, `cursorAfter("nope-not-exists")`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Invalid order field in cursor mode → InvalidArgument (no silent skip).
	_, err = docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`orderAsc("bad field")`, `limit(2)`, `cursorAfter("d1")`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestListDocuments_PaginationGuards (A1/F3-2): page_size 负数回退默认页大小；
// DSL 未显式指定 limit 时 page_size 生效（此前 ParseMany 恒注入 50 掩盖该行为）；
// DSL limit 优先于 page_size；DSL limit(-1)/offset(-1) 在解析期报错（fail-fast）；
// offset 超上限 → InvalidArgument。
func TestListDocuments_PaginationGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "n", Key: "n", Type: "integer"},
	}, nil, nil, true))

	for i := 0; i < 7; i++ {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
			Data: map[string]any{"n": i},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	// page_size=5 且 DSL 未显式指定 limit → 返回 5 条，total=7，NextPageToken 非空。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		PageSize: 5,
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 5)
	require.Equal(t, int64(7), list.TotalCount)
	require.NotEmpty(t, list.NextPageToken)

	// 第二页：剩余 2 条，无 NextPageToken。
	list2, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		PageSize:  5,
		PageToken: list.NextPageToken,
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list2.Documents, 2)
	require.Empty(t, list2.NextPageToken)

	// page_size=-1 → 回退默认页大小 50，7 条全部返回。
	list, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		PageSize: -1,
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 7)

	// page_size=0 → 同默认 50，7 条全部返回。
	list, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 7)

	// DSL limit 显式指定时优先于 page_size。
	list, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		PageSize: 5,
		Queries:  []string{`limit(3)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 3)

	// DSL limit(-1) → 解析期 fail-fast 报错（不产生 LIMIT -1）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`limit(-1)`},
	}, databases.SystemPrincipal)
	require.Error(t, err)

	// DSL offset(-1) → 解析期 fail-fast 报错。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`offset(-1)`},
	}, databases.SystemPrincipal)
	require.Error(t, err)

	// offset 超上限 → InvalidArgument（List 与 Count 一致）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		Queries: []string{`offset(10001)`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = docDB.CountDocuments(ctx, projectID, "app", "docs", databases.Query{Queries: []string{`offset(10001)`}}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 非法 PageToken → InvalidArgument（对齐 ListCollections）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		PageToken: "not-a-valid-token",
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestListDocuments_InputLimits (A2): queries 条数、单条长度、equal 多值个数
// 超上限均报 InvalidArgument；正常调用不受影响。
func TestListDocuments_InputLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	// 101 条 queries → InvalidArgument。
	queries := make([]string, maxQueryCount+1)
	for i := range queries {
		queries[i] = `limit(1)`
	}
	_, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{Queries: queries}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = docDB.CountDocuments(ctx, projectID, "app", "posts", databases.Query{Queries: queries}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 超长查询串 → InvalidArgument。
	long := `equal("title","` + strings.Repeat("a", maxQueryStringLen) + `")`
	_, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{Queries: []string{long}}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 1001 个 equal 值 → InvalidArgument（查询串长度在上限内，命中值数限制）。
	values := strings.Repeat(`"a",`, maxFilterValues) + `"a"`
	_, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title",[` + values + `])`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 正常查询不受影响。
	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title","hello")`, `limit(10)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)
}

// TestEnsureSystemCollections_Idempotent (A3): 重复调用 EnsureSystemCollections
// （含全新实例、无进程内缓存）必须幂等成功。
func TestEnsureSystemCollections_Idempotent(t *testing.T) {
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
	fresh := NewPostgresDocumentDB(db, nil)
	require.NoError(t, fresh.EnsureSystemCollections(ctx, projectID, internalID))
	coll, err := fresh.GetCollection(ctx, projectID, databases.SystemDatabaseID, "users")
	require.NoError(t, err)
	require.NotNil(t, coll)
}

// TestCreateCollectionMetadata_IdempotentSystemRow (A3): 系统集合元数据集合行已存在时
// 重复 createCollectionMetadata 幂等成功（DO NOTHING + 行数判断），子表插入被跳过。
func TestCreateCollectionMetadata_IdempotentSystemRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	spec := systemCollectionSpecs(projectID)["users"]
	p := &postgresDocumentDB{db: db}
	err := p.createCollectionMetadata(ctx, projectID, databases.SystemDatabaseID, "users", spec.name, spec.attrs, spec.indexes, spec.permissions, true)
	require.NoError(t, err)
}

// TestCreateCollectionMetadata_DuplicateUserCollection (A3): 用户集合建同名同 ID
// 集合，第二次经 DO NOTHING 行数判断返回 ErrDuplicateKey，MapDocumentDBError 后为
// AlreadyExists（既有映射，不依赖 A6 新增）。
func TestCreateCollectionMetadata_DuplicateUserCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", nil, nil, nil, true))

	err := docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", nil, nil, nil, true)
	require.ErrorIs(t, err, ErrDuplicateKey)
	require.Equal(t, codes.AlreadyExists, status.Code(shared.MapDocumentDBError(err)))
}

// TestBulkUpdateDocuments_RollbackOnFailure (A4): Bulk 事务化——第 2 条为不存在的
// 文档 ID（UpdateDocument 尾随 GetDocument 返回 nil → error）时整体回滚，
// 第 1 条的更新不得生效。
func TestBulkUpdateDocuments_RollbackOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	doc1, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "original"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	n, err := docDB.BulkUpdateDocuments(ctx, projectID, "app", "docs",
		[]string{doc1.ID, "missing-id"},
		map[string]any{"title": "changed"}, nil, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, int64(0), n)

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", doc1.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "original", got.Data["title"])
}

// TestListDocuments_SystemPathRawPGError (A6/A7): SystemPrincipal（信任路径，跳过
// 白名单）查询未声明列 → adapter 直调返回原始 PG 错误（映射在 app 层生效），
// 错误链可 errors.As 到 *pgdriver.Error。
func TestListDocuments_SystemPathRawPGError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	_, err := docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries: []string{`equal("nonexistent_col","x")`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	// pgdriver@v1.2.18 readError 返回 Error 值（非指针），与 isUniqueViolation 一致用值断言。
	var pgErr pgdriver.Error
	require.True(t, errors.As(err, &pgErr), "error chain must reach pgdriver.Error: %v", err)
	require.Equal(t, "42703", pgErr.Field('C'))
}

// TestListDocuments_QueryFieldWhitelist (A7): 非 System 路径查询字段白名单
// （未声明列/非法 order → InvalidArgument）；search 需 fulltext 索引列
// （无索引集合 → InvalidArgument，files.name → 可用）。
func TestListDocuments_QueryFieldWhitelist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	bob := databases.Principal{Roles: []string{"user:bob"}}

	// 非 System 查询未声明列 → InvalidArgument。
	_, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("nonexistent","x")`},
	}, bob)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 非 System 查询未声明列（Count）→ InvalidArgument。
	_, err = docDB.CountDocuments(ctx, projectID, "app", "posts", databases.Query{Queries: []string{`equal("nonexistent","x")`}}, bob)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// order 非法字段 → InvalidArgument（不再静默跳过）。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`orderDesc("bad field")`},
	}, bob)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// search 对无 fulltext 索引的集合 → InvalidArgument。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`search("title","hello")`},
	}, bob)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// search 命中 fulltext 索引列（files.name_fulltext）→ 可用。
	keysPrincipal := databases.Principal{Roles: []string{"keys"}}
	_, err = docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "files", databases.Query{
		Queries: []string{`search("name","hello")`},
	}, keysPrincipal)
	require.NoError(t, err)

	// 合法字段（声明 attr + 系统列）不受影响。
	_, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title","x")`, `orderAsc("$id")`, `limit(5)`},
	}, bob)
	require.NoError(t, err)
}

// TestListDocuments_SensitiveFieldBlacklist (A7): default 库系统集合的凭据/令牌类列
// （users.password_hash 等）禁止作为过滤条件 → InvalidArgument；自定义库同名集合不受影响。
func TestListDocuments_SensitiveFieldBlacklist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	keysPrincipal := databases.Principal{Roles: []string{"keys"}}

	// users（default 库系统集合）：password_hash 禁止过滤。
	_, err := docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries: []string{`equal("password_hash","x")`},
	}, keysPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// sessions.secret_hash / identities.provider_data 同样禁止。
	_, err = docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Query{
		Queries: []string{`equal("secret_hash","x")`},
	}, keysPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "identities", databases.Query{
		Queries: []string{`equal("provider_data","x")`},
	}, keysPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 非敏感声明列（email）可查。
	_, err = docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries: []string{`equal("email","x@y.z")`},
	}, keysPrincipal)
	require.NoError(t, err)

	// 自定义库同名集合不受黑名单影响（可建同名集合，白名单外字段仅按 attrs 校验）。
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "users", "Users", []databases.Attribute{
		{ID: "password_hash", Key: "password_hash", Type: "string", Size: 512},
	}, nil, nil, true))
	_, err = docDB.ListDocuments(ctx, projectID, "app", "users", databases.Query{
		Queries: []string{`equal("password_hash","x")`},
	}, keysPrincipal)
	require.NoError(t, err)
}

// TestCreateDocument_AuditColumns (#12): _created_by/_updated_by are filled
// from the principal's first user:<id> role; keys-only principals leave them
// empty; user data cannot spoof _-prefixed audit fields.
func TestCreateDocument_AuditColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	perms := []databases.Permission{
		{Type: "create", Role: "user:abc"}, {Type: "read", Role: "user:abc"}, {Type: "update", Role: "user:abc"},
		{Type: "create", Role: "keys"}, {Type: "read", Role: "keys"}, {Type: "update", Role: "keys"},
	}
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "audit", "Audit", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, perms, false))

	// user:abc principal: spoofed _created_at/_created_by/_updated_by in data
	// are ignored; audit columns carry "abc".
	created, err := docDB.CreateDocument(ctx, projectID, "app", "audit", databases.Document{
		Data: map[string]any{
			"title":       "t1",
			"_created_at": "2000-01-01T00:00:00Z",
			"_created_by": "spoof",
			"_updated_by": "spoof",
			"not_a_col!":  "ignored",
		},
	}, nil, databases.Principal{Roles: []string{"user:abc"}})
	require.NoError(t, err)
	require.Equal(t, "abc", created.CreatedBy)
	require.Equal(t, "abc", created.UpdatedBy)
	require.False(t, created.CreatedAt.IsZero())
	require.WithinDuration(t, time.Now(), created.CreatedAt, time.Hour)
	require.NotContains(t, created.Data, "_created_at")
	require.NotContains(t, created.Data, "_created_by")
	require.NotContains(t, created.Data, "_updated_by")
	require.Equal(t, "t1", created.Data["title"])

	// Update as user:abc → UpdatedBy == "abc".
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "audit", databases.DocumentUpdate{
		Document: databases.Document{
			ID:   created.ID,
			Data: map[string]any{"title": "t2"},
		},
		ExpectedVersion: created.Version,
	}, databases.Principal{Roles: []string{"user:abc"}})
	require.NoError(t, err)
	require.Equal(t, "abc", updated.UpdatedBy)
	require.Equal(t, "t2", updated.Data["title"])

	// keys-only principal (no user:<id> role) → audit columns empty.
	keysDoc, err := docDB.CreateDocument(ctx, projectID, "app", "audit", databases.Document{
		Data: map[string]any{"title": "k1"},
	}, nil, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Empty(t, keysDoc.CreatedBy)
	require.Empty(t, keysDoc.UpdatedBy)
}

// TestDeleteIndex_RecreateSameIndex (R02-P1-1)：DeleteIndex 事务化——删除后
// catalog 与物理索引一致，同名索引可重建（不撞 document_indexes 唯一约束）。
func TestDeleteIndex_RecreateSameIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "title_key", Type: "key", Attributes: []string{"title"}},
	}, nil, true))

	coll, err := docDB.GetCollection(ctx, projectID, "app", "posts")
	require.NoError(t, err)
	require.Len(t, coll.Indexes, 1)

	require.NoError(t, docDB.DeleteIndex(ctx, projectID, "app", "posts", "title_key"))

	coll, err = docDB.GetCollection(ctx, projectID, "app", "posts")
	require.NoError(t, err)
	require.Empty(t, coll.Indexes)

	// 同名重建必须成功（物理索引 DROP 与元数据删除均已完成）。
	require.NoError(t, docDB.CreateIndex(ctx, projectID, "app", "posts", databases.Index{
		ID:         "title_key",
		Type:       "key",
		Attributes: []string{"title"},
	}))
	coll, err = docDB.GetCollection(ctx, projectID, "app", "posts")
	require.NoError(t, err)
	require.Len(t, coll.Indexes, 1)
	require.Equal(t, "title_key", coll.Indexes[0].ID)
}

// TestCreateDatabase_RollbackOnMetadataFailure (R02-P1-2)：CreateDatabase 整体
// 事务化——元数据 INSERT 失败（预插同 PK 行）时，事务内已建的 schema 必须回滚，
// 不允许出现"schema 存在而元数据缺失"。
func TestCreateDatabase_RollbackOnMetadataFailure(t *testing.T) {
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

	// 预插同 (project_id, id) 元数据行，令 INSERT 撞复合主键。
	_, err := db.DB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s.document_databases (id, project_id, name, created_at, updated_at) VALUES ('app', ?, 'preexisting', NOW(), NOW())`,
		quoteIdent(testProjectSchema(t, projectID))),
		projectID)
	require.NoError(t, err)

	err = docDB.CreateDatabase(ctx, projectID, "app", "Application DB")
	require.Error(t, err)
	require.True(t, isUniqueViolation(err), "expected unique violation, got: %v", err)

	// 事务回滚后 schema 必须不存在（to_regnamespace 返回 NULL）。
	var reg any
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT to_regnamespace(?)`, testSchema(t, projectID, "app")).Scan(&reg))
	require.Nil(t, reg)

	// 元数据未被篡改。
	_, err = docDB.GetDatabase(ctx, projectID, "app")
	require.NoError(t, err)
}

// TestUpdateDocument_PermissionsOnlyRefreshesAuditColumns (R02-P1-4)：仅变更
// permissions 时同样刷新 _updated_at/_updated_by，数据字段保持不变。
func TestUpdateDocument_PermissionsOnlyRefreshesAuditColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	perms := []databases.Permission{
		{Type: "create", Role: "user:abc"}, {Type: "read", Role: "user:abc"}, {Type: "update", Role: "user:abc"},
		{Type: "create", Role: "keys"}, {Type: "read", Role: "keys"}, {Type: "update", Role: "keys"},
	}
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "audit", "Audit", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, perms, false))

	created, err := docDB.CreateDocument(ctx, projectID, "app", "audit", databases.Document{
		Data: map[string]any{"title": "t1"},
	}, []databases.Permission{
		{Type: "read", Role: "user:abc"}, {Type: "update", Role: "user:abc"},
	}, databases.Principal{Roles: []string{"user:abc"}})
	require.NoError(t, err)
	require.NotEmpty(t, created.CreatedBy)

	// 仅更新 permissions（无数据字段）：审计列必须刷新。
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "audit", databases.DocumentUpdate{
		Document: databases.Document{
			ID: created.ID,
		},
		Permissions: []databases.Permission{
			{Type: "read", Role: "user:abc"},
		},
		ExpectedVersion: created.Version,
	}, databases.Principal{Roles: []string{"user:abc"}})
	require.NoError(t, err)
	require.Equal(t, "abc", updated.UpdatedBy)
	require.True(t, updated.UpdatedAt.After(created.UpdatedAt), "updated_at must be refreshed on permissions-only update")
	require.Equal(t, "t1", updated.Data["title"])
	require.Len(t, updated.Permissions, 1)
	require.Equal(t, databases.Permission{Type: "read", Role: "user:abc"}, updated.Permissions[0])
}

// TestConflictLockKey_NoAmbiguity (R02-P2-2)：conflictLockKey 序列化无歧义——
// 含 \x00 / 空串 / 类型不同的值组合不得生成相同锁键。
func TestConflictLockKey_NoAmbiguity(t *testing.T) {
	// 旧分隔符拼接实现下这两组值键相同（值内含 \x00 的经典碰撞）。
	require.NotEqual(t,
		conflictLockKey([]any{"a\x00b", "c"}),
		conflictLockKey([]any{"a", "b\x00c"}))

	// 空串与分隔符组合。
	require.NotEqual(t,
		conflictLockKey([]any{"ab", ""}),
		conflictLockKey([]any{"a", "b"}))

	// 顺序敏感。
	require.NotEqual(t,
		conflictLockKey([]any{"a", "b"}),
		conflictLockKey([]any{"b", "a"}))

	// 类型区分（int64 vs 字符串同数值）。
	require.NotEqual(t,
		conflictLockKey([]any{int64(1)}),
		conflictLockKey([]any{"1"}))

	// 确定性：相同输入产生相同键。
	require.Equal(t,
		conflictLockKey([]any{"x", int64(42), true}),
		conflictLockKey([]any{"x", int64(42), true}))
}

// TestListDocuments_SameCreatedAtPaginationStable (R08-P2-4)：同 _created_at 的
// 多行在默认排序下按 _id DESC 稳定分页，翻页无重复、无遗漏。
func TestListDocuments_SameCreatedAtPaginationStable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "n", Key: "n", Type: "integer"},
	}, nil, nil, true))

	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		_, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
			ID:   id,
			Data: map[string]any{"n": i},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}
	// 拉平 _created_at：全部改为同一时间戳，使默认排序只能依赖 _id tiebreaker。
	ts := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	_, err := db.DB.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET _created_at = ?`, tableName(testSchema(t, projectID, "app"), "docs")), ts)
	require.NoError(t, err)

	var got []string
	token := ""
	for {
		list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
			PageSize:  2,
			PageToken: token,
		}, databases.SystemPrincipal)
		require.NoError(t, err)
		for _, d := range list.Documents {
			got = append(got, d.ID)
		}
		token = list.NextPageToken
		if token == "" {
			break
		}
	}
	require.Equal(t, []string{"e", "d", "c", "b", "a"}, got)
}

// TestListDocuments_PageSizeClamp (R08-P3-7)：page_size 超过 maxQueryLimit 时
// clamp 到上限，返回条数不超过 maxQueryLimit 且仍有续页 token。
func TestListDocuments_PageSizeClamp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", nil, nil, nil, true))

	for i := 0; i < maxQueryLimit+1; i++ {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
			Data: map[string]any{},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	list, err := docDB.ListDocuments(ctx, projectID, "app", "docs", databases.Query{
		PageSize: maxQueryLimit + 50,
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, maxQueryLimit)
	require.Equal(t, int64(maxQueryLimit+1), list.TotalCount)
	require.NotEmpty(t, list.NextPageToken)
}
