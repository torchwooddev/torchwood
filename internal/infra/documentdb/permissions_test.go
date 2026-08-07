package documentdb

import (
	"context"
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPermissions_CollectionLevelFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}

	created, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		Data: map[string]any{"title": "Hello"},
	}, nil, alice)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	got, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "Hello", got.Data["title"])
}

func TestPermissions_DocumentLevelOverridesCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "update", Role: "any"},
		{Type: "delete", Role: "any"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "Secret"},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "user:alice"},
	}, alice)
	require.NoError(t, err)

	_, err = docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, alice)
	require.NoError(t, err)
	require.Equal(t, "Secret", got.Data["title"])
}

func TestPermissions_CreateCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "locked", "Locked", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "user:alice"},
		{Type: "read", Role: "any"},
	}, true))

	_, err := docDB.CreateDocument(ctx, projectID, "app", "locked", databases.Document{
		Data: map[string]any{"title": "test"},
	}, nil, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	_, err = docDB.CreateDocument(ctx, projectID, "app", "locked", databases.Document{
		Data: map[string]any{"title": "test"},
	}, nil, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
}

func TestPermissions_KeysNotBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		// 不含 read 授权：在 documentSecurity OR 语义下 collection 级的 read:any
		// 会对所有角色（含 keys）放行，无法检验 keys 不 bypass 文档权限。
		{Type: "create", Role: "users"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "Owned by alice"},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "user:alice"},
	}, alice)
	require.NoError(t, err)

	keysPrincipal := databases.Principal{Roles: []string{"keys"}}
	_, err = docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, keysPrincipal)
	require.ErrorIs(t, err, ErrPermissionDenied)
}

func TestPermissions_PlatformAdminBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "read", Role: "any"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}, true))

	alice := databases.Principal{Roles: []string{"users", "user:alice"}}
	created, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "Secret"},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
		{Type: "update", Role: "user:alice"},
		{Type: "delete", Role: "user:alice"},
	}, alice)
	require.NoError(t, err)

	adminPrincipal := databases.Principal{PlatformAdmin: true}
	got, err := docDB.GetDocument(ctx, projectID, "app", "docs", created.ID, adminPrincipal)
	require.NoError(t, err)
	require.Equal(t, "Secret", got.Data["title"])

	_, err = docDB.UpdateDocument(ctx, projectID, "app", "docs", databases.SimpleDocumentUpdate(databases.Document{
		ID:   created.ID,
		Data: map[string]any{"title": "Updated by admin"},
	}, nil), adminPrincipal)
	require.NoError(t, err)

	err = docDB.DeleteDocument(ctx, projectID, "app", "docs", created.ID, adminPrincipal)
	require.NoError(t, err)
}

func TestPermissions_SystemPrincipalBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "App DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "docs", "Docs", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "create", Role: "user:alice"},
		{Type: "read", Role: "user:alice"},
	}, true))

	_, err := docDB.CreateDocument(ctx, projectID, "app", "docs", databases.Document{
		Data: map[string]any{"title": "System created"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
}
