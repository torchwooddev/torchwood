package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestDefaultDatabase_NoSystemCollections 覆盖 PR3：系统集合不寄居 default。
// catalog 系统行只在 database_id='_'；ListCollections("default") 不含 7 个系统
// 集合；业务库（含 default）可建普通 users（is_system=false）。use-case 仍禁
// Create/Delete default。
func TestDefaultDatabase_NoSystemCollections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := platformAdminCtx(context.Background())
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)
	uc := NewDatabases(repo, docDB)

	p, err := projectsUC.CreateProject(ctx, CreateProjectCommand{
		ID:   "pr3def",
		Name: "PR3 Default",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = projectsUC.DeleteProject(context.Background(), p.ID) })

	cat := testutil.CatalogIdent(p.ID)
	defaultSystem, err := db.NewSelect().Model((*model.DocumentCollection)(nil)).
		ModelTableExpr("?.document_collections AS dc", cat).
		Where("project_id = ? AND database_id = ? AND is_system = TRUE", p.ID, "default").
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, defaultSystem, "零行 database_id='default' AND is_system")

	sentinelSystem, err := db.NewSelect().Model((*model.DocumentCollection)(nil)).
		ModelTableExpr("?.document_collections AS dc", cat).
		Where("project_id = ? AND database_id = ? AND is_system = TRUE", p.ID, databases.SystemDatabaseID).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(databases.SystemCollectionIDs), sentinelSystem)

	cols, _, _, err := uc.ListCollections(ctx, p.ID, "default", databases.ListQuery{})
	require.NoError(t, err)
	gotIDs := map[string]bool{}
	for _, c := range cols {
		require.False(t, c.IsSystem, "ListCollections(default) 不得返回 is_system 集合")
		gotIDs[c.ID] = true
	}
	for _, id := range databases.SystemCollectionIDs {
		require.False(t, gotIDs[id], "ListCollections(default) 不得含系统集合 %s", id)
	}

	coll, err := uc.GetCollection(ctx, p.ID, "default", "users")
	require.NoError(t, err)
	require.Nil(t, coll, "未自建时 default.users 不存在")

	require.NoError(t, uc.CreateCollection(ctx, p.ID, "default", "users", "Users", []databases.Attribute{
		{ID: "name", Key: "name", Type: "string", Size: 256},
	}, nil, nil, true))
	created, err := uc.GetCollection(ctx, p.ID, "default", "users")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.False(t, created.IsSystem)

	sysUsers, err := docDB.GetCollection(ctx, p.ID, databases.SystemDatabaseID, "users")
	require.NoError(t, err)
	require.NotNil(t, sysUsers)
	require.True(t, sysUsers.IsSystem)

	err = uc.CreateDatabase(ctx, p.ID, "default", "default")
	require.Equal(t, codes.InvalidArgument, status.Code(err), "use-case 仍禁 Create default")
	err = uc.DeleteDatabase(ctx, p.ID, "default")
	require.Equal(t, codes.InvalidArgument, status.Code(err), "use-case 仍禁 Delete default")
}
