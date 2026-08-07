package client

import (
	"context"
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
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
	user, _, _, err := account.SignUp(ctx, SignUpCommand{
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

func testConfig() *config.AppConfig {
	return &config.AppConfig{}
}
