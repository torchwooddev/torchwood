package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClientDatabases_DocumentCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	account := NewTestAccount(testConfig(), projectRepo, docDB)
	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "client-docs@torchwood.local",
		Password:  "User@123456",
		Name:      "Client Docs",
	})
	require.NoError(t, err)

	serverUC := appserver.NewDatabases(projectRepo, docDB)
	require.NoError(t, serverUC.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, serverUC.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		// 只授予集合级 create，读/写/删由文档级权限（documentSecurity OR 逻辑）决定，
		// 这样才能验证非属主被文档权限拒绝。
		{Type: "create", Role: "users"},
	}, true))

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Roles:     []string{"users", "user:" + user.ID},
	})
	clientUC := NewDatabases(projectRepo, docDB)

	created, err := clientUC.CreateDocument(userCtx, "app", "notes", "", map[string]any{
		"title": "Client note",
	}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	got, err := clientUC.GetDocument(userCtx, projectID, "app", "notes", created.ID)
	require.NoError(t, err)
	require.Equal(t, "Client note", got.Data["title"])

	otherCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    "other-user",
		Roles:     []string{"users", "user:other-user"},
	})
	_, err = clientUC.GetDocument(otherCtx, projectID, "app", "notes", created.ID)
	require.Error(t, err)

	updated, err := clientUC.UpdateDocument(userCtx, "app", "notes", created.ID, map[string]any{
		"title": "Updated note",
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Updated note", updated.Data["title"])

	list, total, _, err := clientUC.ListDocuments(userCtx, projectID, "app", "notes", databases.Query{})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)

	require.NoError(t, clientUC.DeleteDocument(userCtx, "app", "notes", created.ID))
}

// TestClientDatabases_UpsertDocument (T2): client UpsertDocument inserts with
// owner default permissions and updates the existing row on conflict columns.
func TestClientDatabases_UpsertDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	account := NewTestAccount(testConfig(), projectRepo, docDB)
	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "client-upsert@torchwood.local",
		Password:  "User@123456",
		Name:      "Client Upsert",
	})
	require.NoError(t, err)

	serverUC := appserver.NewDatabases(projectRepo, docDB)
	require.NoError(t, serverUC.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, serverUC.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, []databases.Permission{
		{Type: "create", Role: "users"},
		{Type: "update", Role: "users"},
		{Type: "read", Role: "users"},
	}, true))

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Roles:     []string{"users", "user:" + user.ID},
	})
	clientUC := NewDatabases(projectRepo, docDB)

	upserted, err := clientUC.UpsertDocument(userCtx, "app", "members", "m1", map[string]any{
		"email": "upsert@example.com",
		"name":  "First",
	}, []string{"email"}, nil)
	require.NoError(t, err)
	require.Equal(t, "m1", upserted.ID)
	require.Equal(t, "First", upserted.Data["name"])
	require.Contains(t, upserted.Permissions, databases.Permission{Type: "update", Role: "user:" + user.ID})

	updated, err := clientUC.UpsertDocument(userCtx, "app", "members", "m1", map[string]any{
		"email": "upsert@example.com",
		"name":  "Second",
	}, []string{"email"}, nil)
	require.NoError(t, err)
	require.Equal(t, "m1", updated.ID)
	require.Equal(t, "Second", updated.Data["name"])

	got, err := clientUC.GetDocument(userCtx, projectID, "app", "members", updated.ID)
	require.NoError(t, err)
	require.Equal(t, "Second", got.Data["name"])
}

func TestClientDatabases_GuestPublicRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	serverUC := appserver.NewDatabases(projectRepo, docDB)
	require.NoError(t, serverUC.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, serverUC.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "create", Role: "users"},
	}, true))

	clientUC := NewDatabases(projectRepo, docDB)
	_, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		Data: map[string]any{"title": "Public post"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	list, total, _, err := clientUC.ListDocuments(ctx, projectID, "app", "posts", databases.Query{})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)
	require.Equal(t, "Public post", list[0].Data["title"])

	lockedUC := appserver.NewDatabases(projectRepo, docDB)
	require.NoError(t, lockedUC.CreateCollection(ctx, projectID, "app", "private", "Private", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		{Type: "read", Role: "users"},
		{Type: "create", Role: "users"},
	}, true))
	created, err := docDB.CreateDocument(ctx, projectID, "app", "private", databases.Document{
		Data: map[string]any{"title": "Secret"},
	}, []databases.Permission{
		{Type: "read", Role: "user:owner"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	_, err = clientUC.GetDocument(ctx, projectID, "app", "private", created.ID)
	require.Error(t, err)
}

// TestClientDatabases_PrivateDocumentEnforced (B1): 用户集合 documentSecurity=true
// 下"私有文档"（ownerDocumentPermissions：read/update/delete:user:<id>）文档级优先：
// 匿名读拒、匿名列表不可见、他用户改删拒、owner 可读写删。
// 集合级配 read:any —— 旧 OR 语义下私有文档会全公开，本测试验证文档级覆盖。
func TestClientDatabases_PrivateDocumentEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	account := NewTestAccount(testConfig(), projectRepo, docDB)
	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "private-doc@torchwood.local",
		Password:  "User@123456",
		Name:      "Private Doc Owner",
	})
	require.NoError(t, err)

	serverUC := appserver.NewDatabases(projectRepo, docDB)
	require.NoError(t, serverUC.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, serverUC.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, []databases.Permission{
		// 集合级 read:any：B1 下被文档级权限覆盖（旧 OR 语义下私有文档全公开）。
		{Type: "read", Role: "any"},
		{Type: "create", Role: "users"},
	}, true))

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Roles:     []string{"users", "user:" + user.ID},
	})
	clientUC := NewDatabases(projectRepo, docDB)

	created, err := clientUC.CreateDocument(userCtx, "app", "notes", "", map[string]any{
		"title": "Private note",
	}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Len(t, created.Permissions, 3)

	// 匿名：GetDocument 拒绝，列表不可见。
	_, err = clientUC.GetDocument(ctx, projectID, "app", "notes", created.ID)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "anonymous read of private doc should be denied")

	anonList, anonTotal, _, err := clientUC.ListDocuments(ctx, projectID, "app", "notes", databases.Query{})
	require.NoError(t, err)
	require.Zero(t, anonTotal)
	require.Empty(t, anonList)

	// 他用户：读/改/删均拒绝。
	otherCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    "other-user",
		Roles:     []string{"users", "user:other-user"},
	})
	_, err = clientUC.GetDocument(otherCtx, projectID, "app", "notes", created.ID)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "other user read should be denied")

	_, err = clientUC.UpdateDocument(otherCtx, "app", "notes", created.ID, map[string]any{"title": "hacked"}, nil, nil)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "other user update should be denied")

	err = clientUC.DeleteDocument(otherCtx, "app", "notes", created.ID)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "other user delete should be denied")

	// owner：读/改/删均放行。
	got, err := clientUC.GetDocument(userCtx, projectID, "app", "notes", created.ID)
	require.NoError(t, err)
	require.Equal(t, "Private note", got.Data["title"])

	updated, err := clientUC.UpdateDocument(userCtx, "app", "notes", created.ID, map[string]any{
		"title": "Updated note",
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Updated note", updated.Data["title"])

	require.NoError(t, clientUC.DeleteDocument(userCtx, "app", "notes", created.ID))
}

func testConfig() *config.AppConfig {
	return &config.AppConfig{}
}
