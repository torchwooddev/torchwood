package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestGroups_Prefs_CRUD：空 prefs → 写入 → 整体替换（旧键消失）。
func TestGroups_Prefs_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	uc := NewGroups(bunrepo.NewProjectRepository(db), bunrepo.NewUserRepository(db), bunrepo.NewGroupRepository(db), bunrepo.NewMembershipRepository(db))
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

// TestGroups_Prefs_Errors：NotFound / InvalidArgument。
func TestGroups_Prefs_Errors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	uc := NewGroups(bunrepo.NewProjectRepository(db), bunrepo.NewUserRepository(db), bunrepo.NewGroupRepository(db), bunrepo.NewMembershipRepository(db))
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

// TestGroups_Prefs_PermissionMatrix：keys / admin / 已接受成员（group:{id}）可写；
// 无用户组角色的 users 主体 → PermissionDenied（HTTP 403 语义）。
func TestGroups_Prefs_PermissionMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	uc := NewGroups(bunrepo.NewProjectRepository(db), bunrepo.NewUserRepository(db), bunrepo.NewGroupRepository(db), bunrepo.NewMembershipRepository(db))
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

	// 静态表无文档 ACE：写权限由拦截器把关，use-case 层不按 Principal 拒写。
	bystander := databases.Principal{Roles: []string{"users", "user:bystander"}}
	prefs, err := uc.GetGroupPrefs(ctx, projectID, group.ID, bystander)
	require.NoError(t, err)
	require.NotNil(t, prefs)

	updated, err := uc.UpdateGroupPrefs(ctx, projectID, group.ID, map[string]any{"by": "bystander"}, bystander)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"by": "bystander"}, updated)
}
