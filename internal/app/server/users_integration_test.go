package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/groups"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/password"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type documentRoles struct{}

func (documentRoles) LoadUserRoles(ctx context.Context, projectID, userID string) ([]string, error) {
	return []string{"users", "user:" + userID}, nil
}

func newUsersUC(ctx context.Context, t *testing.T) (*Users, databases.DocumentDB, string, func()) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	cfg := &config.AppConfig{}
	sessions := auth.NewSessionService(cfg, docDB, documentRoles{}, nil)
	uc := NewUsers(bunrepo.NewProjectRepository(db), docDB, sessions, db)
	return uc, docDB, projectID, cleanup
}

func TestServerUsers_CreateAndUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	uc, docDB, projectID, cleanup := newUsersUC(ctx, t)
	defer cleanup()
	_ = docDB

	// 创建用户。
	doc, err := uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "ServerUser@Test.Torchwood.local",
		Password: "Pass@123",
		Name:     "Server User",
		Labels:   []any{"vip", "beta"},
		Prefs:    map[string]any{"theme": "dark"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, doc.ID)
	require.Equal(t, "serveruser@test.torchwood.local", doc.Data["email"])
	require.Equal(t, users.StatusActive, doc.Data["status"])
	require.Equal(t, false, doc.Data["email_verified"])
	require.Equal(t, []any{"vip", "beta"}, doc.Data["labels"])
	require.Equal(t, map[string]any{"theme": "dark"}, doc.Data["prefs"])

	// 密码已哈希存储，可被 SignIn 验证。
	hash, _ := doc.Data["password_hash"].(string)
	ok, _ := password.Verify("Pass@123", hash)
	require.True(t, ok)

	// 重复 email。
	_, err = uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "serveruser@test.torchwood.local",
		Password: "Pass@123",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.AlreadyExists, st.Code())

	// 弱密码。
	_, err = uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "weak@torchwood.local",
		Password: "short",
	})
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 更新 status（此前被 protected 字段过滤，回归验证）。
	updated, err := uc.UpdateUser(ctx, projectID, doc.ID, map[string]any{"status": users.StatusBlocked}, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Equal(t, users.StatusBlocked, updated.Data["status"])

	// 更新 email → 自动重置 email_verified，并校验唯一性。
	updated, err = uc.UpdateUser(ctx, projectID, doc.ID, map[string]any{"email": "new-email@torchwood.local"}, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Equal(t, "new-email@torchwood.local", updated.Data["email"])
	require.Equal(t, false, updated.Data["email_verified"])

	// email_verified 可单独设置。
	updated, err = uc.UpdateUser(ctx, projectID, doc.ID, map[string]any{"email_verified": true}, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Equal(t, true, updated.Data["email_verified"])

	// password_hash 仍受保护，不允许直接写。
	_, err = uc.UpdateUser(ctx, projectID, doc.ID, map[string]any{"password_hash": "evil"}, databases.Principal{Roles: []string{"keys"}})
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestServerUsers_PasswordResetAndSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	uc, _, projectID, cleanup := newUsersUC(ctx, t)
	defer cleanup()

	doc, err := uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "reset-test@torchwood.local",
		Password: "Pass@123",
		Name:     "Reset Test",
	})
	require.NoError(t, err)

	// 重置密码。
	_, err = uc.UpdateUserPassword(ctx, projectID, doc.ID, "NewPass@456")
	require.NoError(t, err)

	// DB 中当前哈希：旧密码失效、新密码可验证。
	after, err := uc.GetUser(ctx, projectID, doc.ID, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	hash, _ := after.Data["password_hash"].(string)
	ok, _ := password.Verify("Pass@123", hash)
	require.False(t, ok)
	ok, _ = password.Verify("NewPass@456", hash)
	require.True(t, ok)

	// 弱密码拒绝。
	_, err = uc.UpdateUserPassword(ctx, projectID, doc.ID, "weak")
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 会话管理：先造两个会话，再列表/删除。
	s1, _, err := uc.sessions.CreateSessionAndTokens(ctx, projectID, doc.ID, "reset-test@torchwood.local", "email")
	require.NoError(t, err)
	_ = s1
	s2, _, err := uc.sessions.CreateSessionAndTokens(ctx, projectID, doc.ID, "reset-test@torchwood.local", "server_token")
	require.NoError(t, err)
	_ = s2

	sessionsList, err := uc.ListUserSessions(ctx, projectID, doc.ID)
	require.NoError(t, err)
	require.Len(t, sessionsList, 2)

	// 删除一个会话。
	target := sessionsList[0].ID
	require.NoError(t, uc.DeleteUserSession(ctx, projectID, doc.ID, target))
	sessionsList, err = uc.ListUserSessions(ctx, projectID, doc.ID)
	require.NoError(t, err)
	require.Len(t, sessionsList, 1)
	for _, s := range sessionsList {
		require.NotEqual(t, target, s.ID)
	}

	// 删除他人会话 → NotFound。
	other, err := uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "other@torchwood.local",
		Password: "Pass@123",
	})
	require.NoError(t, err)
	otherSession, _, err := uc.sessions.CreateSessionAndTokens(ctx, projectID, other.ID, "other@torchwood.local", "email")
	require.NoError(t, err)
	_ = otherSession
	err = uc.DeleteUserSession(ctx, projectID, other.ID, target)
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())

	// 不存在的用户 → NotFound。
	_, err = uc.ListUserSessions(ctx, projectID, "missing-user")
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestServerUsers_DeleteUser_CascadeBeyondDefaultPage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// 需要本地 Postgres（CI 兜底）：级联删除必须循环分页，
	// 超过默认 50 条页上限的会话/成员不得残留（F4-1）。
	ctx := platformAdminCtx(context.Background())
	uc, docDB, projectID, cleanup := newUsersUC(ctx, t)
	defer cleanup()

	doc, err := uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "cascade-61@torchwood.local",
		Password: "Pass@123",
		Name:     "Cascade 61",
	})
	require.NoError(t, err)

	// 直插 61 条 sessions（> 默认 50 条页上限）。
	for i := 0; i < 61; i++ {
		_, err := docDB.CreateDocument(ctx, projectID, "default", "sessions", databases.Document{
			ID: idgen.UUID().String(),
			Data: map[string]any{
				"user_id":    doc.ID,
				"provider":   "email",
				"expire_at":  time.Now().Add(time.Hour).Format(time.RFC3339Nano),
				"user_agent": "cascade-test",
			},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}
	// 直插 61 条 memberships。
	for i := 0; i < 61; i++ {
		_, err := docDB.CreateDocument(ctx, projectID, "default", "memberships", databases.Document{
			ID: idgen.UUID().String(),
			Data: map[string]any{
				"group_id":  idgen.UUID().String(),
				"user_id":   doc.ID,
				"status":    groups.StatusAccepted,
				"roles":     []string{groups.RoleMember},
				"joined_at": time.Now().Format(time.RFC3339Nano),
			},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
	}

	require.NoError(t, uc.DeleteUser(ctx, projectID, doc.ID, databases.Principal{Roles: []string{"keys"}}))

	// 用户文档与全部级联文档均已清理。
	me, err := docDB.GetDocument(ctx, projectID, "default", "users", doc.ID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Nil(t, me)
	sessions, err := docDB.ListDocuments(ctx, projectID, "default", "sessions", databases.Query{
		Queries: []string{query.BuildEqual("user_id", doc.ID)},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Zero(t, sessions.TotalCount)
	memberships, err := docDB.ListDocuments(ctx, projectID, "default", "memberships", databases.Query{
		Queries: []string{query.BuildEqual("user_id", doc.ID)},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Zero(t, memberships.TotalCount)
}

func TestServerUsers_CreateUserToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := platformAdminCtx(context.Background())
	uc, docDB, projectID, cleanup := newUsersUC(ctx, t)
	defer cleanup()

	doc, err := uc.CreateUser(ctx, projectID, CreateUserCommand{
		Email:    "token-test@torchwood.local",
		Password: "Pass@123",
		Name:     "Token Test",
	})
	require.NoError(t, err)

	// 模拟登录签发 token。
	bundle, err := uc.CreateUserToken(ctx, projectID, doc.ID)
	require.NoError(t, err)
	require.NotEmpty(t, bundle.AccessToken)
	require.NotEmpty(t, bundle.RefreshToken)

	// 签发后产生一条该用户的会话（可被管理员列出/删除）。
	sessionsList, err := uc.ListUserSessions(ctx, projectID, doc.ID)
	require.NoError(t, err)
	require.Len(t, sessionsList, 1)

	// 以用户身份可读取自己的文档。
	tokenPrincipal := databases.Principal{Roles: []string{"users", "user:" + doc.ID}}
	me, err := docDB.GetDocument(ctx, projectID, "default", "users", doc.ID, tokenPrincipal)
	require.NoError(t, err)
	require.NotNil(t, me)

	// blocked 用户不可模拟登录。
	_, err = uc.UpdateUser(ctx, projectID, doc.ID, map[string]any{"status": users.StatusBlocked}, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	_, err = uc.CreateUserToken(ctx, projectID, doc.ID)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}
