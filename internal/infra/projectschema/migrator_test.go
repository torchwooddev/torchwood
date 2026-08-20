package projectschema_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

func TestApply_IdempotentCatalogAndOAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	require.NoError(t, projectschema.Apply(ctx, db, projectID))
	require.NoError(t, projectschema.Apply(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	var version int64
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT MAX(version) FROM "+quoted+".schema_migrations").Scan(&version))
	require.Equal(t, int64(3), version)

	var dirty bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT COALESCE(bool_or(dirty), false) FROM "+quoted+".schema_migrations").Scan(&dirty))
	require.False(t, dirty)

	for _, table := range []string{"document_databases", "document_collections", "document_attributes", "document_indexes", "project_oauth_providers", "functions", "function_deployments", "function_variables", "function_executions"} {
		var reg any
		require.NoError(t, db.DB.QueryRowContext(ctx,
			`SELECT to_regclass(?)`, quoted+"."+table).Scan(&reg), table)
		require.NotNil(t, reg, "expected %s.%s", quoted, table)
	}

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	var ns any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, schema).Scan(&ns))
	require.NotNil(t, ns)
}

func TestApply_RejectsInvalidProjectID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	require.Error(t, projectschema.Apply(ctx, db, "_"))
	require.Error(t, projectschema.Apply(ctx, db, "Default"))
	require.Error(t, projectschema.Apply(ctx, db, ""))
}

func TestEnsureAll_AppliesListedProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	p1, _, c1 := testutil.CreateTestProject(ctx, db)
	defer c1()
	p2, _, c2 := testutil.CreateTestProject(ctx, db)
	defer c2()

	require.NoError(t, projectschema.EnsureAll(ctx, db, []string{p1, p2}))
	require.Error(t, projectschema.EnsureAll(ctx, db, []string{p1, "_"}))
}
