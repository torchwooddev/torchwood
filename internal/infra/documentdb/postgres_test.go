package documentdb

import (
	"context"
	"testing"
	"time"

	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/infra/bun/model"
	"github.com/torchwoodio/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
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

	docDB := NewPostgresDocumentDB(db)
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
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "posts", databases.SimpleDocumentUpdate(databases.Document{
		ID: got.ID,
		Data: map[string]any{
			"views": 100,
		},
	}, nil), databases.Principal{Roles: []string{"any"}})
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
	count, err := docDB.CountDocuments(ctx, projectID, "app", "posts", []string{`equal("title","Hello World")`}, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	// Delete.
	require.NoError(t, docDB.DeleteDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}}))
	got2, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Nil(t, got2)
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

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	created, err := docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		Data: map[string]any{
			"email": "perm@torchwood.local",
			"name":  "Permission Test",
		},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	// User without permission cannot read.
	list, err := docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// User with permission can read.
	list, err = docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)

	// System roles bypass permissions.
	list, err = docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(list.Documents), 1)

	// Get without permission is denied.
	_, err = docDB.GetDocument(ctx, projectID, "default", "users", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	// Get with permission succeeds.
	got, err := docDB.GetDocument(ctx, projectID, "default", "users", created.ID, databases.Principal{Roles: []string{"user:alice"}})
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

	docDB := NewPostgresDocumentDB(db)

	projectA, internalA, cleanupA := testutil.CreateTestProject(ctx, db)
	defer cleanupA()
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectA, internalA))

	// Second project must use a unique name (projects.name is unique).
	projectB := &model.Project{
		ID:        "test-project-b",
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

	collA, err := docDB.GetCollection(ctx, projectA, "default", "users")
	require.NoError(t, err)
	require.NotNil(t, collA)

	collB, err := docDB.GetCollection(ctx, projectB.ID, "default", "users")
	require.NoError(t, err)
	require.NotNil(t, collB)
}
