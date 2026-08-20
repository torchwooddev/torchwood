package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// oldGroupsAttrsV2 模拟旧版本 spec 的 groups 集合属性（无 prefs）。
var oldGroupsAttrsV2 = []databases.Attribute{
	{ID: "groups_name", Key: "name", Type: "string", Size: 256},
	{ID: "groups_permissions", Key: "permissions", Type: "json"},
	{ID: "groups_total", Key: "total", Type: "integer", Default: 0},
}

// TestGroups_Prefs_CRUD：空 prefs → 写入 → 整体替换（旧键消失）。
func TestGroups_Prefs_CRUD(t *testing.T) {
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

	uc := NewGroups(bunrepo.NewProjectRepository(db), docDB)
	group, err := uc.CreateGroup(ctx, projectID, "Design", nil)
	require.NoError(t, err)

	keys := databases.Principal{Roles: []string{"keys"}}

	prefs, err := uc.GetGroupPrefs(ctx, projectID, group.ID, keys)
	require.NoError(t, err)
	require.Empty(t, prefs, "从未设置时返回空对象")

	updated, err := uc.UpdateGroupPrefs(ctx, projectID, group.ID, map[string]any{"theme": "dark", "notifications": true}, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark", "notifications": true}, updated)

	got, err := uc.GetGroupPrefs(ctx, projectID, group.ID, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark", "notifications": true}, got)

	// 整体替换：旧键消失。
	updated, err = uc.UpdateGroupPrefs(ctx, projectID, group.ID, map[string]any{"locale": "zh-CN"}, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"locale": "zh-CN"}, updated)

	got, err = uc.GetGroupPrefs(ctx, projectID, group.ID, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"locale": "zh-CN"}, got)
}

// TestGroups_Prefs_SelfHealReconcile：存量项目（旧 spec groups 集合、不调
// EnsureSystemCollections）→ 直接 GetGroupPrefs/UpdateGroupPrefs 首请求触发 reconcile
// 并成功读写（覆盖验收标准 4）。
func TestGroups_Prefs_SelfHealReconcile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)

	// 模拟存量项目：只建项目数据面 catalog sentinel + 旧 spec 的 groups 集合，绝不调
	// EnsureSystemCollections（否则 reconcile 提前发生，测不出自愈路径）。
	testutil.InsertCatalogDatabase(ctx, db, projectID, ident.ProjectDataPlaneID, "(project)")
	require.NoError(t, docDB.CreateCollection(ctx, projectID, ident.ProjectDataPlaneID, "groups", "groups", oldGroupsAttrsV2, nil, []databases.Permission{
		{Type: "create", Role: "keys"},
		{Type: "read", Role: "any"},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: "group:{id}"},
		{Type: "update", Role: "keys"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: "group:{id}"},
		{Type: "delete", Role: "keys"},
		{Type: "delete", Role: "admin"},
	}, true))

	groupID := "legacy-group-id"
	_, err := docDB.CreateDocument(ctx, projectID, ident.ProjectDataPlaneID, "groups", databases.Document{
		ID:   groupID,
		Data: map[string]any{"name": "Legacy Group", "total": 0},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	uc := NewGroups(bunrepo.NewProjectRepository(db), docDB)
	keys := databases.Principal{Roles: []string{"keys"}}

	// 首请求即触发 EnsureSystemCollections reconcile：读返回空对象而不是 42703。
	prefs, err := uc.GetGroupPrefs(ctx, projectID, groupID, keys)
	require.NoError(t, err)
	require.Empty(t, prefs)

	updated, err := uc.UpdateGroupPrefs(ctx, projectID, groupID, map[string]any{"theme": "dark"}, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark"}, updated)

	got, err := uc.GetGroupPrefs(ctx, projectID, groupID, keys)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"theme": "dark"}, got)
}

// TestGroups_Prefs_Errors：NotFound / InvalidArgument。
func TestGroups_Prefs_Errors(t *testing.T) {
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

	uc := NewGroups(bunrepo.NewProjectRepository(db), docDB)
	keys := databases.Principal{Roles: []string{"keys"}}

	_, err := uc.GetGroupPrefs(ctx, projectID, "no-such-group", keys)
	require.Equal(t, codes.NotFound, status.Code(err))

	_, err = uc.UpdateGroupPrefs(ctx, projectID, "no-such-group", map[string]any{"a": 1}, keys)
	require.Equal(t, codes.NotFound, status.Code(err))

	group, err := uc.CreateGroup(ctx, projectID, "Errors Group", nil)
	require.NoError(t, err)

	_, err = uc.UpdateGroupPrefs(ctx, projectID, group.ID, nil, keys)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "prefs is required")
}

// TestGroups_Prefs_PermissionMatrix：keys / admin / 已接受成员（group:{id}，系统集合
// OR 语义）可写；无用户组角色的 users 主体 → PermissionDenied（HTTP 403 语义）。
func TestGroups_Prefs_PermissionMatrix(t *testing.T) {
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

	uc := NewGroups(bunrepo.NewProjectRepository(db), docDB)
	group, err := uc.CreateGroup(ctx, projectID, "Perm Group", nil)
	require.NoError(t, err)

	writeOK := []databases.Principal{
		{Roles: []string{"keys"}},
		{Roles: []string{"admin"}},
		{Roles: []string{"users", "user:member-1", "group:" + group.ID}},
	}
	for _, p := range writeOK {
		updated, err := uc.UpdateGroupPrefs(ctx, projectID, group.ID, map[string]any{"by": p.Roles[0]}, p)
		require.NoError(t, err, "principal %v 应可写 prefs", p.Roles)
		require.Equal(t, map[string]any{"by": p.Roles[0]}, updated)
	}

	// 无用户组角色：读放行（read:any），写被拒 → PermissionDenied（MapDocumentDBError）。
	bystander := databases.Principal{Roles: []string{"users", "user:bystander"}}
	prefs, err := uc.GetGroupPrefs(ctx, projectID, group.ID, bystander)
	require.NoError(t, err)
	require.NotNil(t, prefs)

	_, err = uc.UpdateGroupPrefs(ctx, projectID, group.ID, map[string]any{"by": "bystander"}, bystander)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "无用户组角色的 users 主体应被拒")
}
