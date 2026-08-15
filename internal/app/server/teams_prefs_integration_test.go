package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// oldTeamsAttrsV2 模拟旧版本 spec 的 teams 集合属性（无 prefs）。
var oldTeamsAttrsV2 = []databases.Attribute{
	{ID: "teams_name", Key: "name", Type: "string", Size: 256},
	{ID: "teams_permissions", Key: "permissions", Type: "json"},
	{ID: "teams_total", Key: "total", Type: "integer", Default: 0},
}

// TestTeams_Prefs_CRUD：空 prefs → 写入 → 整体替换（旧键消失）。
func TestTeams_Prefs_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	uc := NewTeams(bunrepo.NewProjectRepository(db), docDB)
	team, err := uc.CreateTeam(ctx, projectID, "Design", nil)
	require.NoError(t, err)

	keys := databases.Principal{Roles: []string{"keys"}}

	prefs, err := uc.GetTeamPrefs(ctx, projectID, team.ID, keys)
	require.NoError(t, err)
	require.Empty(t, prefs, "从未设置时返回空对象")

	updated, err := uc.UpdateTeamPrefs(ctx, projectID, team.ID, map[string]any{"theme": "dark", "notifications": true}, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark", "notifications": true}, updated)

	got, err := uc.GetTeamPrefs(ctx, projectID, team.ID, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark", "notifications": true}, got)

	// 整体替换：旧键消失。
	updated, err = uc.UpdateTeamPrefs(ctx, projectID, team.ID, map[string]any{"locale": "zh-CN"}, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"locale": "zh-CN"}, updated)

	got, err = uc.GetTeamPrefs(ctx, projectID, team.ID, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"locale": "zh-CN"}, got)
}

// TestTeams_Prefs_SelfHealReconcile：存量项目（旧 spec teams 集合、不调
// EnsureSystemCollections）→ 直接 GetTeamPrefs/UpdateTeamPrefs 首请求触发 reconcile
// 并成功读写（覆盖验收标准 4）。
func TestTeams_Prefs_SelfHealReconcile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)

	// 模拟存量项目：只建 default 库元数据 + 旧 spec 的 teams 集合，绝不调
	// EnsureSystemCollections（否则 reconcile 提前发生，测不出自愈路径）。
	now := time.Now()
	_, err := db.NewInsert().Model(&model.DocumentDatabase{
		ID:        "default",
		ProjectID: projectID,
		Name:      "default",
		CreatedAt: now,
		UpdatedAt: now,
	}).Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "default", "teams", "teams", oldTeamsAttrsV2, nil, []databases.Permission{
		{Type: "create", Role: "keys"},
		{Type: "read", Role: "any"},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: "team:{id}"},
		{Type: "update", Role: "keys"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: "team:{id}"},
		{Type: "delete", Role: "keys"},
		{Type: "delete", Role: "admin"},
	}, true))

	teamID := "legacy-team-id"
	_, err = docDB.CreateDocument(ctx, projectID, "default", "teams", databases.Document{
		ID:   teamID,
		Data: map[string]any{"name": "Legacy Team", "total": 0},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	uc := NewTeams(bunrepo.NewProjectRepository(db), docDB)
	keys := databases.Principal{Roles: []string{"keys"}}

	// 首请求即触发 EnsureSystemCollections reconcile：读返回空对象而不是 42703。
	prefs, err := uc.GetTeamPrefs(ctx, projectID, teamID, keys)
	require.NoError(t, err)
	require.Empty(t, prefs)

	updated, err := uc.UpdateTeamPrefs(ctx, projectID, teamID, map[string]any{"theme": "dark"}, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark"}, updated)

	got, err := uc.GetTeamPrefs(ctx, projectID, teamID, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark"}, got)
}

// TestTeams_Prefs_Errors：NotFound / InvalidArgument。
func TestTeams_Prefs_Errors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	uc := NewTeams(bunrepo.NewProjectRepository(db), docDB)
	keys := databases.Principal{Roles: []string{"keys"}}

	_, err := uc.GetTeamPrefs(ctx, projectID, "no-such-team", keys)
	require.Equal(t, codes.NotFound, status.Code(err))

	_, err = uc.UpdateTeamPrefs(ctx, projectID, "no-such-team", map[string]any{"a": 1}, keys)
	require.Equal(t, codes.NotFound, status.Code(err))

	team, err := uc.CreateTeam(ctx, projectID, "Errors Team", nil)
	require.NoError(t, err)

	_, err = uc.UpdateTeamPrefs(ctx, projectID, team.ID, nil, keys)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "prefs is required")
}

// TestTeams_Prefs_PermissionMatrix：keys / admin / 已接受成员（team:{id}，系统集合
// OR 语义）可写；无团队角色的 users 主体 → PermissionDenied（HTTP 403 语义）。
func TestTeams_Prefs_PermissionMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	uc := NewTeams(bunrepo.NewProjectRepository(db), docDB)
	team, err := uc.CreateTeam(ctx, projectID, "Perm Team", nil)
	require.NoError(t, err)

	writeOK := []databases.Principal{
		{Roles: []string{"keys"}},
		{Roles: []string{"admin"}},
		{Roles: []string{"users", "user:member-1", "team:" + team.ID}},
	}
	for _, p := range writeOK {
		updated, err := uc.UpdateTeamPrefs(ctx, projectID, team.ID, map[string]any{"by": p.Roles[0]}, p)
		require.NoError(t, err, "principal %v 应可写 prefs", p.Roles)
		require.Equal(t, map[string]any{"by": p.Roles[0]}, updated)
	}

	// 无团队角色：读放行（read:any），写被拒 → PermissionDenied（MapDocumentDBError）。
	bystander := databases.Principal{Roles: []string{"users", "user:bystander"}}
	prefs, err := uc.GetTeamPrefs(ctx, projectID, team.ID, bystander)
	require.NoError(t, err)
	require.NotNil(t, prefs)

	_, err = uc.UpdateTeamPrefs(ctx, projectID, team.ID, map[string]any{"by": "bystander"}, bystander)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "无团队角色的 users 主体应被拒")
}
