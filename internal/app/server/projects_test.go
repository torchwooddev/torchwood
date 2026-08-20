package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	p, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{
		ID:          "txapp",
		Name:        "Transactional App",
		Description: "integration test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, p.ID)
	t.Cleanup(func() {
		_ = projectsUC.DeleteProject(ctx, p.ID)
	})

	coll, err := docDB.GetCollection(ctx, p.ID, databases.SystemDatabaseID, "users")
	require.NoError(t, err)
	require.NotNil(t, coll)

	def, err := docDB.GetDatabase(ctx, p.ID, "default")
	require.NoError(t, err)
	require.NotNil(t, def, "CreateProject 应建第一业务库 default")

	sentinel, err := docDB.GetDatabase(ctx, p.ID, databases.SystemDatabaseID)
	require.NoError(t, err)
	require.NotNil(t, sentinel)

	var publicCatalog int
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM public.document_collections WHERE project_id = ?`, p.ID).Scan(&publicCatalog))
	require.Zero(t, publicCatalog, "PR4: catalog 不得再写 public")

	cat := testutil.CatalogIdent(p.ID)
	projectCatalog, err := db.NewSelect().Model((*model.DocumentCollection)(nil)).
		ModelTableExpr("?.document_collections AS dc", cat).
		Where("project_id = ? AND is_system = TRUE", p.ID).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(databases.SystemCollectionIDs), projectCatalog)
}

func TestProjects_CreateProject_RequiresPlatformAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	// API key 主体（ActorKind=service）被拒。
	apiKeyCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:     "key-1",
		ActorKind:   shared.ActorKindService,
		ProjectID:   "some-project",
		Roles:       []string{"keys"},
		Permissions: []string{"databases.write"},
	})
	_, err := projectsUC.CreateProject(apiKeyCtx, CreateProjectCommand{ID: "hacked", Name: "Hacked App"})
	require.Error(t, err)

	// 受限 admin（非平台 admin）被拒。
	viewerCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:   "admin-2",
		ActorKind: shared.ActorKindAdmin,
		UserID:    "admin-2",
		Roles:     []string{"viewer"},
	})
	_, err = projectsUC.CreateProject(viewerCtx, CreateProjectCommand{ID: "viewer", Name: "Viewer App"})
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
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	for _, id := range []string{"", "Bad_Name", "my-shop", "1shop", "MyShop"} {
		_, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{ID: id, Name: "App"})
		require.Error(t, err, id)
	}
}

func TestProjects_CreateProject_RollsBackOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)

	projectID := fmt.Sprintf("rb%x", time.Now().UnixNano())
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

	var ns any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, "tw_"+projectID).Scan(&ns))
	require.Nil(t, ns)
}

// createTestProject 直接经 repo 落库一个项目（UpdateProject 不需要 docDB）。
func createTestProject(t *testing.T, repo projects.Repository, id, name string) *projects.Project {
	t.Helper()
	p := &projects.Project{
		ID:        id,
		Name:      name,
		Status:    "active",
		Settings:  map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateProject(context.Background(), p))
	return p
}

// restrictedAdminCtx 返回携带非平台 admin principal 的上下文（绑定 projectID）。
func restrictedAdminCtx(ctx context.Context, projectID string) context.Context {
	return contexts.WithPrincipal(ctx, &shared.Principal{
		ActorID:         "admin-2",
		ActorKind:       shared.ActorKindAdmin,
		IsPlatformAdmin: false,
		ProjectID:       projectID,
	})
}

func strPtr(s string) *string { return &s }

func TestProjects_UpdateProject_PlatformAdminSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	created := createTestProject(t, repo, "alpha", "Alpha App")
	time.Sleep(2 * time.Millisecond) // 保证 updated_at 严格递增

	got, err := projectsUC.UpdateProject(platformAdminCtx(ctx), UpdateProjectCommand{
		ProjectID:   "alpha",
		Name:        strPtr("Alpha Renamed"),
		Description: strPtr("new description"),
	})
	require.NoError(t, err)
	require.Equal(t, "Alpha Renamed", got.Name)
	require.Equal(t, "new description", got.Description)
	require.True(t, got.UpdatedAt.After(created.UpdatedAt), "updated_at 必须单调递增")

	// 断言 repo 层落库。
	persisted, err := repo.GetProject(ctx, "alpha")
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.Equal(t, "Alpha Renamed", persisted.Name)
	require.Equal(t, "new description", persisted.Description)
	require.True(t, persisted.UpdatedAt.After(created.UpdatedAt))
}

func TestProjects_UpdateProject_RestrictedAdminOwnProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	createTestProject(t, repo, "own", "Own App")
	got, err := projectsUC.UpdateProject(restrictedAdminCtx(ctx, "own"), UpdateProjectCommand{
		ProjectID:   "own",
		Description: strPtr("updated by restricted admin"),
	})
	require.NoError(t, err)
	require.Equal(t, "updated by restricted admin", got.Description)
}

func TestProjects_UpdateProject_RestrictedAdminOtherProjectNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	createTestProject(t, repo, "own", "Own App")
	createTestProject(t, repo, "other", "Other App")

	// 非平台 admin 越权更新他人项目 → NotFound（防存在性探测，与 GetProject 一致）。
	_, err := projectsUC.UpdateProject(restrictedAdminCtx(ctx, "own"), UpdateProjectCommand{
		ProjectID: "other",
		Name:      strPtr("Hacked"),
	})
	require.Equal(t, codes.NotFound, status.Code(err))

	// 无绑定项目（ProjectID 为空）同样拒绝。
	_, err = projectsUC.UpdateProject(restrictedAdminCtx(ctx, ""), UpdateProjectCommand{
		ProjectID: "own",
		Name:      strPtr("Hacked"),
	})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestProjects_UpdateProject_ProjectNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	_, err := projectsUC.UpdateProject(platformAdminCtx(ctx), UpdateProjectCommand{
		ProjectID: "missing",
		Name:      strPtr("Nope"),
	})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestProjects_UpdateProject_NothingToUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	// 前置检查：name 与 description 均未提供 → InvalidArgument（先于取数/越权）。
	_, err := projectsUC.UpdateProject(platformAdminCtx(ctx), UpdateProjectCommand{ProjectID: "alpha"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "nothing to update")
}

func TestProjects_UpdateProject_EmptyID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	_, err := projectsUC.UpdateProject(platformAdminCtx(ctx), UpdateProjectCommand{
		Name: strPtr("Whatever"),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestProjects_UpdateProject_BlankNameRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	createTestProject(t, repo, "alpha", "Alpha App")

	// 有意收紧：编辑场景拒绝空白名（严格于 CreateProject 的空名回落默认 id）。
	_, err := projectsUC.UpdateProject(platformAdminCtx(ctx), UpdateProjectCommand{
		ProjectID: "alpha",
		Name:      strPtr("   "),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "name is required")
}

func TestProjects_UpdateProject_NameCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	createTestProject(t, repo, "alpha", "Alpha App")
	createTestProject(t, repo, "beta", "Beta App")

	// 改名撞名 → InvalidArgument（不落库，避免依赖 DB unique violation → 500）。
	_, err := projectsUC.UpdateProject(platformAdminCtx(ctx), UpdateProjectCommand{
		ProjectID: "beta",
		Name:      strPtr("Alpha App"),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "project name already exists")

	persisted, err := repo.GetProject(ctx, "beta")
	require.NoError(t, err)
	require.Equal(t, "Beta App", persisted.Name)
}

func TestProjects_CreateProject_RejectsLongDescription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	// 口径 a：CreateProject 与 UpdateProject 对 description 施加同一上限 512。
	_, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{
		ID:          "longdesc",
		Name:        "Long Description App",
		Description: strings.Repeat("x", 513),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "description")

	// 恰好 512 字符放行。
	p, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{
		ID:          "boundary",
		Name:        "Boundary App",
		Description: strings.Repeat("x", 512),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = projectsUC.DeleteProject(ctx, p.ID)
	})
}

func TestProjects_DeleteProject_DropsSchemas(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	repo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	projectsUC := NewProjects(repo, docDB, db)

	p, err := projectsUC.CreateProject(platformAdminCtx(ctx), CreateProjectCommand{
		ID:              "delme",
		Name:            "Delete Me",
		FirstDatabaseID: "app",
	})
	require.NoError(t, err)

	_, err = docDB.CreateDocument(ctx, p.ID, databases.SystemDatabaseID, "users", databases.Document{
		Data: map[string]any{"email": "gone@torchwood.local", "name": "Gone"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	require.NoError(t, projectsUC.DeleteProject(ctx, p.ID))

	got, err := repo.GetProject(ctx, p.ID)
	require.NoError(t, err)
	require.Nil(t, got)

	var projectNS, appNS any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, "tw_delme").Scan(&projectNS))
	require.Nil(t, projectNS)
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regnamespace(?)`, "tw_delme_app").Scan(&appNS))
	require.Nil(t, appNS)
}
