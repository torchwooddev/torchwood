package documentdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func namespaceExists(t *testing.T, ctx context.Context, db *clients.Database, name string) bool {
	t.Helper()
	var reg any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, name).Scan(&reg))
	return reg != nil
}

// TestDataPlaneSchema_DeleteDatabaseDefaultKeepsUsers：infra DeleteDatabase("default")
// 不得碰到 tw_<project>.users（§4.2 验收 1）。
func TestDataPlaneSchema_DeleteDatabaseDefaultKeepsUsers(t *testing.T) {
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
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "default", "default"))

	created, err := docDB.CreateDocument(ctx, projectID, ident.ProjectDataPlaneID, "users", databases.Document{
		ID:   "u-keep",
		Data: map[string]any{"email": "keep@torchwood.local", "name": "Keep"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	projectSchema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	defaultSchema, err := ident.SchemaName(projectID, "default")
	require.NoError(t, err)

	require.True(t, namespaceExists(t, ctx, db, projectSchema))
	require.True(t, namespaceExists(t, ctx, db, defaultSchema))

	require.NoError(t, docDB.DeleteDatabase(ctx, projectID, "default"))

	got, err := docDB.GetDocument(ctx, projectID, ident.ProjectDataPlaneID, "users", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "keep@torchwood.local", got.Data["email"])

	require.True(t, namespaceExists(t, ctx, db, projectSchema), "一段式 tw_<pid> 必须仍在")
	require.False(t, namespaceExists(t, ctx, db, defaultSchema), "tw_<pid>_default 应被 DROP")
}

// TestDataPlaneSchema_DeleteDatabaseSentinelRefused：infra DeleteDatabase("_")
// 失败且一段式 schema 仍在（§4.2 验收 2 / P0 DDL 分叉）。
func TestDataPlaneSchema_DeleteDatabaseSentinelRefused(t *testing.T) {
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

	projectSchema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	require.True(t, namespaceExists(t, ctx, db, projectSchema))

	err = docDB.DeleteDatabase(ctx, projectID, ident.ProjectDataPlaneID)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	require.True(t, namespaceExists(t, ctx, db, projectSchema), "一段式 tw_<pid> 必须仍在")
	coll, err := docDB.GetCollection(ctx, projectID, ident.ProjectDataPlaneID, "users")
	require.NoError(t, err)
	require.NotNil(t, coll)
}

func TestDataPlaneSchema_CreateCollectionWhitelist(t *testing.T) {
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

	err := docDB.CreateCollection(ctx, projectID, ident.ProjectDataPlaneID, "posts", "Posts", nil, nil, nil, true)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDataPlaneSchema_SystemCRUDHitsOneSegment(t *testing.T) {
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

	projectSchema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	var rel *string
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regclass(?)`, projectSchema+".users").Scan(&rel))
	require.NotNil(t, rel)

	created, err := docDB.CreateDocument(ctx, projectID, ident.ProjectDataPlaneID, "users", databases.Document{
		Data: map[string]any{"email": "seg@torchwood.local", "name": "Seg"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	got, err := docDB.GetDocument(ctx, projectID, ident.ProjectDataPlaneID, "users", created.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, "seg@torchwood.local", got.Data["email"])
}
