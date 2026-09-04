package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestDatabases_DocumentCRUD covers P1 Sprint 1 document API use cases.
func TestDatabases_DocumentCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB, nil)
	principal := databases.Principal{Roles: []string{"keys"}}

	const (
		dbID   = "app"
		collID = "posts"
	)
	require.NoError(t, uc.CreateDatabase(ctx, projectID, dbID, "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, dbID, collID, "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, nil, nil, true))

	created, _, err := uc.CreateDocument(ctx, projectID, dbID, collID, "", map[string]any{
		"title": "Hello Torchwood",
		"views": 1,
	}, databases.DefaultCollectionPermissions(), principal, "")
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "Hello Torchwood", created.Data["title"])

	got, err := uc.GetDocument(ctx, projectID, dbID, collID, created.ID, principal)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	updated, _, err := uc.UpdateDocument(ctx, projectID, dbID, collID, created.ID, map[string]any{
		"views": 99,
	}, nil, nil, principal, &created.Version, "")
	require.NoError(t, err)
	require.Equal(t, float64(99), updated.Data["views"])
	require.Equal(t, int64(2), updated.Version)

	list, total, _, err := uc.ListDocuments(ctx, projectID, dbID, collID, databases.Query{
		AST: &query.Query{Filter: query.Eq("title", "Hello Torchwood"), Orders: []query.Order{{Attribute: "$createdAt", Desc: true}}},
	}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)

	count, err := uc.CountDocuments(ctx, projectID, dbID, collID, databases.Query{AST: &query.Query{Filter: query.Eq("title", "Hello Torchwood")}}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	_, delErr := uc.DeleteDocument(ctx, projectID, dbID, collID, created.ID, principal, &updated.Version, "")
	require.NoError(t, delErr)
	_, err = uc.GetDocument(ctx, projectID, dbID, collID, created.ID, principal)
	require.Error(t, err)
}

// TestDatabases_UpsertDocument (T2): UpsertDocument inserts a new document
// when no row matches the conflict columns and updates it when one does.
func TestDatabases_UpsertDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB, nil)
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, nil, true))

	upserted, _, err := uc.UpsertDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "a@example.com",
		"name":  "Alice",
	}, []string{"email"}, databases.DefaultCollectionPermissions(), principal, "")
	require.NoError(t, err)
	require.Equal(t, "m1", upserted.ID)
	require.Equal(t, "Alice", upserted.Data["name"])

	updated, _, err := uc.UpsertDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "a@example.com",
		"name":  "Alice Updated",
	}, []string{"email"}, databases.DefaultCollectionPermissions(), principal, "")
	require.NoError(t, err)
	require.Equal(t, "m1", updated.ID)
	require.Equal(t, "Alice Updated", updated.Data["name"])

	got, err := uc.GetDocument(ctx, projectID, "app", "members", updated.ID, principal)
	require.NoError(t, err)
	require.Equal(t, "Alice Updated", got.Data["name"])
}

func TestDatabases_UpsertDocument_Validation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB, nil)
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	_, _, err := uc.UpsertDocument(ctx, projectID, "app", "posts", "", nil, []string{"title"}, nil, principal, "")
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	_, _, err = uc.UpsertDocument(ctx, projectID, "app", "posts", "", map[string]any{"title": "t"}, nil, nil, principal, "")
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

// TestDatabases_UpsertDocument_EmptyACESeed：keys 主体空 ACE upsert 的插入支与
// 更新支都必须能读回、可改（回归：原实现种 read:__private__，且更新支把目标行
// ACL 整体替换为 __private__，keys 自己创建的文档被锁死）。集合只授 create:keys
// 不授 read，读回必须依赖文档级种子 ACE，精确复现锁死面。
func TestDatabases_UpsertDocument_EmptyACESeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB, nil)
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, []databases.Permission{{Type: "create", Role: "keys"}}, true))

	// 插入支：空 ACE 种子为 read/update/delete:keys，读回与修改不依赖集合级 read。
	upserted, _, err := uc.UpsertDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "seed@example.com",
	}, []string{"email"}, nil, principal, "")
	require.NoError(t, err)
	require.Equal(t, "m1", upserted.ID)
	requirePermsMatchRoles(t, upserted.Permissions, "keys")

	got, err := uc.GetDocument(ctx, projectID, "app", "members", "m1", principal)
	require.NoError(t, err)
	require.Equal(t, "seed@example.com", got.Data["email"])
	requirePermsMatchRoles(t, got.Permissions, "keys")

	updated, _, err := uc.UpdateDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "seed2@example.com",
	}, nil, nil, principal, &got.Version, "")
	require.NoError(t, err)
	require.Equal(t, "seed2@example.com", updated.Data["email"])

	// 更新支：conflict 命中已有行，种子不得把目标行 ACL 替换为 __private__。
	updatedAgain, _, err := uc.UpsertDocument(ctx, projectID, "app", "members", "m2", map[string]any{
		"email": "seed2@example.com",
	}, []string{"email"}, nil, principal, "")
	require.NoError(t, err)
	require.Equal(t, "m1", updatedAgain.ID)
	requirePermsMatchRoles(t, updatedAgain.Permissions, "keys")

	gotAgain, err := uc.GetDocument(ctx, projectID, "app", "members", "m1", principal)
	require.NoError(t, err)
	require.Equal(t, int64(3), gotAgain.Version)

	_, _, err = uc.UpdateDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "seed3@example.com",
	}, nil, nil, principal, &gotAgain.Version, "")
	require.NoError(t, err)
}

// requirePermsMatchRoles 断言 perms 恰好是 role 的 read/update/delete 三元组
// （seedDocumentPermissions 的种子形态，不含 __private__）。
func requirePermsMatchRoles(t *testing.T, perms []databases.Permission, role string) {
	t.Helper()
	require.Len(t, perms, 3)
	got := map[string]string{}
	for _, p := range perms {
		got[p.Type] = p.Role
	}
	for _, typ := range []string{"read", "update", "delete"} {
		require.Equalf(t, role, got[typ], "permission %s:%s missing creator seed", typ, role)
	}
}
