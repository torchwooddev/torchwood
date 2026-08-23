package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
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
	}, nil, nil, principal, &created.Version)
	require.NoError(t, err)
	require.Equal(t, float64(99), updated.Data["views"])
	require.Equal(t, int64(2), updated.Version)

	list, total, _, err := uc.ListDocuments(ctx, projectID, dbID, collID, databases.Query{
		Queries: []string{`equal("title","Hello Torchwood")`, `orderDesc("$createdAt")`},
	}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)

	count, err := uc.CountDocuments(ctx, projectID, dbID, collID, databases.Query{Queries: []string{`equal("title","Hello Torchwood")`}}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	require.NoError(t, uc.DeleteDocument(ctx, projectID, dbID, collID, created.ID, principal, &updated.Version))
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "members", "Members", []databases.Attribute{
		{ID: "email", Key: "email", Type: "string", Size: 256},
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, []databases.Index{
		{ID: "uq_email", Type: "unique", Attributes: []string{"email"}},
	}, nil, true))

	upserted, err := uc.UpsertDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "a@example.com",
		"name":  "Alice",
	}, []string{"email"}, databases.DefaultCollectionPermissions(), principal)
	require.NoError(t, err)
	require.Equal(t, "m1", upserted.ID)
	require.Equal(t, "Alice", upserted.Data["name"])

	updated, err := uc.UpsertDocument(ctx, projectID, "app", "members", "m1", map[string]any{
		"email": "a@example.com",
		"name":  "Alice Updated",
	}, []string{"email"}, databases.DefaultCollectionPermissions(), principal)
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	principal := databases.Principal{Roles: []string{"keys"}}

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	_, err := uc.UpsertDocument(ctx, projectID, "app", "posts", "", nil, []string{"title"}, nil, principal)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	_, err = uc.UpsertDocument(ctx, projectID, "app", "posts", "", map[string]any{"title": "t"}, nil, nil, principal)
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
