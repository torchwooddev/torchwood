package bunrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainfunctions "github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

func seedFunctionRow(t *testing.T, ctx context.Context, repo domainfunctions.FunctionRepo, projectID string) *domainfunctions.Function {
	t.Helper()
	now := time.Now()
	fn := &domainfunctions.Function{
		ID:             "fn_1",
		ProjectID:      projectID,
		Name:           "hello",
		Runtime:        "node-18.0",
		Entrypoint:     "index.main",
		TimeoutSeconds: 15,
		Spec:           "shared-1x",
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	require.NoError(t, repo.CreateFunction(ctx, fn))
	return fn
}

func seedDeploymentRow(t *testing.T, ctx context.Context, repo domainfunctions.FunctionRepo, fn *domainfunctions.Function, status string) *domainfunctions.Deployment {
	t.Helper()
	now := time.Now()
	dep := &domainfunctions.Deployment{
		ID:         "dep_1",
		FunctionID: fn.ID,
		ProjectID:  fn.ProjectID,
		Size:       1024,
		Status:     status,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(t, repo.CreateDeployment(ctx, dep))
	return dep
}

func TestFunctionRepository_CRUDAndRelations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	repo := bunrepo.NewFunctionRepository(db)

	// 空列表。
	list, err := repo.ListFunctions(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, list)

	fn := seedFunctionRow(t, ctx, repo, projectID)

	got, err := repo.GetFunction(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Equal(t, "hello", got.Name)

	// 跨项目不可见。
	missing, err := repo.GetFunction(ctx, "other-project", fn.ID)
	require.NoError(t, err)
	require.Nil(t, missing)

	// 更新。
	fn.Name = "renamed"
	fn.UpdatedAt = time.Now()
	require.NoError(t, repo.UpdateFunction(ctx, fn))
	got, err = repo.GetFunction(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)

	// deployment + variables + executions。
	dep := seedDeploymentRow(t, ctx, repo, fn, domainfunctions.DeploymentStatusReady)

	require.NoError(t, repo.SetVariables(ctx, projectID, fn.ID, map[string]string{"A": "1", "B": "2"}))
	vars, err := repo.GetVariables(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"A": "1", "B": "2"}, vars)

	// SetVariables 全量替换。
	require.NoError(t, repo.SetVariables(ctx, projectID, fn.ID, map[string]string{"C": "3"}))
	vars, err = repo.GetVariables(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"C": "3"}, vars)

	rec := &domainfunctions.ExecutionRecord{
		ID:           "exe_1",
		FunctionID:   fn.ID,
		ProjectID:    projectID,
		DeploymentID: dep.ID,
		Status:       domainfunctions.ExecutionStatusQueued,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, repo.CreateExecution(ctx, rec))
	gotRec, err := repo.GetExecution(ctx, projectID, fn.ID, "exe_1")
	require.NoError(t, err)
	require.Equal(t, domainfunctions.ExecutionStatusQueued, gotRec.Status)

	// 删除部署 → 执行记录级联删除。
	require.NoError(t, repo.DeleteDeployment(ctx, fn.ID, dep.ID))
	gotRec, err = repo.GetExecution(ctx, projectID, fn.ID, "exe_1")
	require.NoError(t, err)
	require.Nil(t, gotRec, "FK 级联删除 execution")

	// 删除函数 → 部署/变量级联删除。
	require.NoError(t, repo.DeleteFunction(ctx, projectID, fn.ID))
	got, err = repo.GetFunction(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Nil(t, got)
	vars, err = repo.GetVariables(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Empty(t, vars)
	deps, err := repo.ListDeployments(ctx, projectID, fn.ID)
	require.NoError(t, err)
	require.Empty(t, deps)
}

func TestFunctionRepository_PruneOldExecutions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	repo := bunrepo.NewFunctionRepository(db)
	fn := seedFunctionRow(t, ctx, repo, projectID)
	dep := seedDeploymentRow(t, ctx, repo, fn, domainfunctions.DeploymentStatusReady)

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.CreateExecution(ctx, &domainfunctions.ExecutionRecord{
			ID:           "exe_" + string(rune('a'+i)),
			FunctionID:   fn.ID,
			ProjectID:    projectID,
			DeploymentID: dep.ID,
			Status:       domainfunctions.ExecutionStatusCompleted,
			CreatedAt:    time.Now().Add(time.Duration(i) * time.Second),
			UpdatedAt:    time.Now(),
		}))
	}
	require.NoError(t, repo.PruneOldExecutions(ctx, fn.ID, 3))
	recs, err := repo.ListExecutions(ctx, projectID, fn.ID, 100)
	require.NoError(t, err)
	require.Len(t, recs, 3, "仅保留最近 3 条")
}

func TestFunctionRepository_RecoverOrphanExecutions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	repo := bunrepo.NewFunctionRepository(db)
	fn := seedFunctionRow(t, ctx, repo, projectID)
	dep := seedDeploymentRow(t, ctx, repo, fn, domainfunctions.DeploymentStatusReady)

	stale := &domainfunctions.ExecutionRecord{
		ID:           "exe_stale",
		FunctionID:   fn.ID,
		ProjectID:    projectID,
		DeploymentID: dep.ID,
		Status:       domainfunctions.ExecutionStatusRunning,
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		UpdatedAt:    time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, repo.CreateExecution(ctx, stale))
	fresh := &domainfunctions.ExecutionRecord{
		ID:           "exe_fresh",
		FunctionID:   fn.ID,
		ProjectID:    projectID,
		DeploymentID: dep.ID,
		Status:       domainfunctions.ExecutionStatusQueued,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, repo.CreateExecution(ctx, fresh))

	n, err := repo.RecoverOrphanExecutions(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := repo.GetExecution(ctx, projectID, fn.ID, "exe_stale")
	require.NoError(t, err)
	require.Equal(t, domainfunctions.ExecutionStatusFailed, got.Status)
	require.Equal(t, "worker restarted", got.Error)

	got, err = repo.GetExecution(ctx, projectID, fn.ID, "exe_fresh")
	require.NoError(t, err)
	require.Equal(t, domainfunctions.ExecutionStatusQueued, got.Status, "未过期的记录不受影响")
}
