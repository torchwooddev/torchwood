package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestProjects_CreateProject_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	projectsUC := NewProjects(repo, docDB, db)

	p, err := projectsUC.CreateProject(ctx, CreateProjectCommand{
		Name:        "Transactional App",
		Description: "integration test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, p.ID)
	t.Cleanup(func() {
		_, _ = db.NewDelete().Model((*model.Project)(nil)).Where("id = ?", p.ID).Exec(ctx)
	})

	coll, err := docDB.GetCollection(ctx, p.ID, "default", "users")
	require.NoError(t, err)
	require.NotNil(t, coll)
}

func TestProjects_CreateProject_RollsBackOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)

	projectID := fmt.Sprintf("rollback-%d", time.Now().UnixNano())
	p := &projects.Project{
		ID:        projectID,
		Name:      fmt.Sprintf("Rollback Test %d", time.Now().UnixNano()),
		Status:    "active",
		Settings:  map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := repo.CreateProject(txCtx, p); err != nil {
			return err
		}
		if err := docDB.EnsureSystemCollections(txCtx, p.ID, p.InternalID); err != nil {
			return err
		}
		return fmt.Errorf("simulated failure")
	})
	require.Error(t, err)

	got, err := repo.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Nil(t, got)

	exists, err := db.NewSelect().Model((*model.DocumentDatabase)(nil)).
		Where("project_id = ? AND id = ?", projectID, "default").Exists(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}
