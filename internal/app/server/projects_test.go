package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// platformAdminCtx 返回携带平台 admin principal 的上下文（M7 后 CreateProject
// 仅允许平台 admin，测试需显式注入）。
func platformAdminCtx(ctx context.Context) context.Context {
	return contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:         "admin-1",
		ActorKind:       shared.ActorKindAdmin,
		IsPlatformAdmin: true,
	})
}

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

	p, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{
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

func TestProjects_CreateProject_RequiresPlatformAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	projectsUC := NewProjects(repo, docDB, db)

	// API key 主体（ActorKind=service）被拒。
	apiKeyCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:     "key-1",
		ActorKind:   shared.ActorKindService,
		ProjectID:   "some-project",
		Roles:       []string{"keys"},
		Permissions: []string{"databases.write"},
	})
	_, err := projectsUC.CreateProject(apiKeyCtx, CreateProjectCommand{Name: "Hacked App"})
	require.Error(t, err)

	// 受限 admin（非平台 admin）被拒。
	viewerCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:   "admin-2",
		ActorKind: shared.ActorKindAdmin,
		UserID:    "admin-2",
		Roles:     []string{"viewer"},
	})
	_, err = projectsUC.CreateProject(viewerCtx, CreateProjectCommand{Name: "Viewer App"})
	require.Error(t, err)
}

func TestProjects_CreateProject_RejectsInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	projectsUC := NewProjects(repo, docDB, db)

	// 非白名单字符（下划线/非 ASCII）的项目名派生出的 ID 必须被拒。
	_, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{Name: "Bad_Name!"})
	require.Error(t, err)
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
