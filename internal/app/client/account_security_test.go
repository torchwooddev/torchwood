package client

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func setupAccountSecurity(t *testing.T, withRedis bool) (context.Context, *Account, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	t.Cleanup(cleanup)

	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	var account *Account
	if withRedis {
		mr, err := miniredis.Run()
		require.NoError(t, err)
		t.Cleanup(mr.Close)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		account = NewTestAccountWithRedis(securityTestConfig(), projectRepo, docDB, rdb)
	} else {
		account = NewTestAccount(securityTestConfig(), projectRepo, docDB)
	}
	return ctx, account, projectID, ""
}

func securityTestConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: "account-security-test-secret"},
		},
	}
}

func TestAccount_UpdateEmailRequiresOldPasswordAndRevokesSessions(t *testing.T) {
	ctx, account, projectID, _ := setupAccountSecurity(t, false)

	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "email-change@torchwood.local",
		Password:  "User@123",
		Name:      "Email Change",
	})
	require.NoError(t, err)

	authCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Email:     user.Email,
		Roles:     []string{"users", "user:" + user.ID},
	})

	// 无旧密码 → 拒绝。
	_, err = account.UpdateAccount(authCtx, UpdateAccountCommand{Email: "new-email@torchwood.local"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 旧密码错误 → 拒绝。
	_, err = account.UpdateAccount(authCtx, UpdateAccountCommand{Email: "new-email@torchwood.local", OldPassword: "wrong"})
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())

	// 旧密码正确 → 成功，且撤销全部会话（含当前会话）。
	updated, err := account.UpdateAccount(authCtx, UpdateAccountCommand{Email: "new-email@torchwood.local", OldPassword: "User@123"})
	require.NoError(t, err)
	require.Equal(t, "new-email@torchwood.local", updated.Email)

	sessions, err := account.ListSessions(authCtx)
	require.NoError(t, err)
	require.Empty(t, sessions)

	// 新邮箱 + 旧密码可登录。
	_, tokens, _, _, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "new-email@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)
}

func TestAccount_AnonymousUpgradeSetsPasswordWithoutOldPassword(t *testing.T) {
	ctx, account, projectID, _ := setupAccountSecurity(t, false)

	anon, _, _, _, err := account.CreateAnonymousSession(ctx, CreateAnonymousSessionCommand{ProjectID: projectID})
	require.NoError(t, err)

	authCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    anon.ID,
		Email:     anon.Email,
		Roles:     []string{"users", "user:" + anon.ID},
	})

	// 匿名用户（password_hash 为空）升级：直接设置邮箱+密码，跳过 old_password。
	updated, err := account.UpdateAccount(authCtx, UpdateAccountCommand{
		Email:    "upgraded@example.com",
		Password: "NewPass@123",
	})
	require.NoError(t, err)
	require.Equal(t, "upgraded@example.com", updated.Email)

	// 升级后可用新邮箱+密码登录（会话已被撤销）。
	_, tokens, _, _, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "upgraded@example.com",
		Password:  "NewPass@123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)
}

func TestAccount_UpdatePrefsLimits(t *testing.T) {
	ctx, account, projectID, _ := setupAccountSecurity(t, false)

	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "prefs-limit@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	authCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Email:     user.Email,
		Roles:     []string{"users", "user:" + user.ID},
	})

	// 超过 64KB → 拒绝。
	huge := map[string]any{"blob": strings.Repeat("x", 64*1024)}
	_, err = account.UpdatePrefs(authCtx, huge)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 嵌套深度 > 20 → 拒绝。
	deep := map[string]any{}
	cur := deep
	for i := 0; i < 21; i++ {
		next := map[string]any{}
		cur["n"] = next
		cur = next
	}
	_, err = account.UpdatePrefs(authCtx, deep)
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 正常 prefs 可用。
	got, err := account.UpdatePrefs(authCtx, map[string]any{"theme": "dark"})
	require.NoError(t, err)
	require.Equal(t, "dark", got["theme"])
}

func TestAccount_SignUpRateLimit(t *testing.T) {
	ctx, account, projectID, _ := setupAccountSecurity(t, true)

	clientCtx := contexts.WithClientInfo(ctx, contexts.ClientInfo{IP: "203.0.113.7"})
	for i := 0; i < signUpIPLimit; i++ {
		_, _, _, _, err := account.SignUp(clientCtx, SignUpCommand{
			ProjectID: projectID,
			Email:     fmt.Sprintf("signup-%d@torchwood.local", i),
			Password:  "User@123",
		})
		require.NoError(t, err)
	}
	_, _, _, _, err := account.SignUp(clientCtx, SignUpCommand{
		ProjectID: projectID,
		Email:     "signup-over@torchwood.local",
		Password:  "User@123",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())

	// 其他 IP 不受影响。
	otherCtx := contexts.WithClientInfo(ctx, contexts.ClientInfo{IP: "203.0.113.8"})
	_, _, _, _, err = account.SignUp(otherCtx, SignUpCommand{
		ProjectID: projectID,
		Email:     "signup-other@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
}

func TestAccount_EmailValidation(t *testing.T) {
	ctx, account, projectID, _ := setupAccountSecurity(t, false)

	_, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "not-an-email",
		Password:  "User@123",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 超过 254 字符 → 拒绝。
	long := strings.Repeat("a", 245) + "@example.com"
	_, _, _, _, err = account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     long,
		Password:  "User@123",
	})
	require.Error(t, err)
	st, _ = status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
