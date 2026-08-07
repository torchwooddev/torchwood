package server

import (
	"context"
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestDatabases_DocumentCRUD covers P1 Sprint 1 document API use cases.
func TestDatabases_DocumentCRUD(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
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

	created, err := uc.CreateDocument(ctx, projectID, dbID, collID, "", map[string]any{
		"title": "Hello Torchwood",
		"views": 1,
	}, databases.DefaultCollectionPermissions(), principal)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "Hello Torchwood", created.Data["title"])

	got, err := uc.GetDocument(ctx, projectID, dbID, collID, created.ID, principal)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	updated, err := uc.UpdateDocument(ctx, projectID, dbID, collID, created.ID, map[string]any{
		"views": 99,
	}, nil, nil, principal)
	require.NoError(t, err)
	require.Equal(t, float64(99), updated.Data["views"])

	list, total, _, err := uc.ListDocuments(ctx, projectID, dbID, collID, databases.Query{
		Queries: []string{`equal("title","Hello Torchwood")`, `orderDesc("$createdAt")`},
	}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)

	count, err := uc.CountDocuments(ctx, projectID, dbID, collID, []string{`equal("title","Hello Torchwood")`}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	require.NoError(t, uc.DeleteDocument(ctx, projectID, dbID, collID, created.ID, principal))
	_, err = uc.GetDocument(ctx, projectID, dbID, collID, created.ID, principal)
	require.Error(t, err)
}
