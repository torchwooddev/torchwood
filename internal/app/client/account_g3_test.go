package client

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	inframessaging "github.com/torchwooddev/torchwood/internal/infra/messaging"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// failableSessionService 包装真实 SessionService，DeleteSessionsByUser 可按
// 标志注入故障（R05-P1-3 故障注入测试用）。
type failableSessionService struct {
	real domainauth.SessionService
	fail bool
}

func (f *failableSessionService) CreateSessionAndTokens(ctx context.Context, projectID, userID, email, provider string) (*domainauth.TokenBundle, string, error) {
	return f.real.CreateSessionAndTokens(ctx, projectID, userID, email, provider)
}

func (f *failableSessionService) IssueTokens(ctx context.Context, projectID, userID, email, sessionID string) (*domainauth.TokenBundle, string, error) {
	return f.real.IssueTokens(ctx, projectID, userID, email, sessionID)
}

func (f *failableSessionService) IssueTokensWithRefreshID(ctx context.Context, projectID, userID, email, sessionID, refreshTokenID string) (*domainauth.TokenBundle, string, error) {
	return f.real.IssueTokensWithRefreshID(ctx, projectID, userID, email, sessionID, refreshTokenID)
}

func (f *failableSessionService) EnsureActiveSession(ctx context.Context, projectID, sessionID, userID string) error {
	return f.real.EnsureActiveSession(ctx, projectID, sessionID, userID)
}

func (f *failableSessionService) DeleteSessionsByUser(ctx context.Context, projectID, userID string) error {
	if f.fail {
		return status.Error(codes.Internal, "injected session revocation failure")
	}
	return f.real.DeleteSessionsByUser(ctx, projectID, userID)
}

// setupG3Account 组装带 miniredis + 可选 mailer + 可注入故障的 session
// service 的 Account（真实 Postgres docDB，集成测试，-short 跳过）。
func setupG3Account(t *testing.T) (context.Context, *Account, string, *failableSessionService, *miniredis.Miniredis, *CaptureMailer) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	t.Cleanup(cleanup)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: "g3-account-test-secret"},
		},
	}
	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	roles := NewUserRoles(docDB)
	rotation := auth.NewRedisRefreshRotationStore(rdb)
	realSessions := auth.NewSessionService(cfg, docDB, roles, rotation)
	sessions := &failableSessionService{real: realSessions}
	mailer := &CaptureMailer{}
	account := NewAccount(
		cfg,
		projectRepo,
		nil,
		docDB,
		sessions,
		auth.NewRedisOTPChallengeStore(rdb, cfg),
		auth.NewRedisOAuthStateStore(rdb),
		auth.NewRedisAccountTokenStore(rdb),
		auth.NewRedisLoginThrottle(rdb),
		rotation,
		nil,
		mailer,
		inframessaging.NewSMSService(cfg),
		auth.NewRedisRateLimiter(rdb),
		roles,
		auth.NewTOTPService(cfg, rdb),
		auth.NewRedisMFAChallengeStore(rdb),
		auth.NewRedisOneTimeTokenStore(rdb),
		nil,
	)
	return ctx, account, projectID, sessions, mr, mailer
}

func signUpG3User(t *testing.T, ctx context.Context, account *Account, projectID, email string) (*User, string) {
	t.Helper()
	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     email,
		Password:  "User@123",
		Name:      "G3 User",
	})
	require.NoError(t, err)
	return user, user.ID
}

// TestAccount_EmailChangeNotifiesOldEmail（R05-P1-2，B 档）：邮箱变更生效后
// 必须向旧邮箱发送安全通知（含撤销指引）；新邮箱收到验证邮件由既有
// CreateVerification 流程覆盖（email_verified=false 已置位）。
func TestAccount_EmailChangeNotifiesOldEmail(t *testing.T) {
	ctx, account, projectID, _, _, mailer := setupG3Account(t)

	user, userID := signUpG3User(t, ctx, account, projectID, "old-mail@torchwood.local")
	authCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    userID,
		Email:     user.Email,
		Roles:     []string{"users", "user:" + userID},
	})

	updated, err := account.UpdateAccount(authCtx, UpdateAccountCommand{
		Email:       "new-mail@torchwood.local",
		OldPassword: "User@123",
	})
	require.NoError(t, err)
	require.Equal(t, "new-mail@torchwood.local", updated.Email)
	require.False(t, updated.EmailVerified, "新邮箱未验证前 email_verified 必须为 false")

	// 旧邮箱收到安全通知。
	require.Equal(t, []string{"old-mail@torchwood.local"}, mailer.To)
	require.Contains(t, mailer.Subjects[0], "email address was changed")
	require.Contains(t, mailer.Bodies[0], "did not make this change")
}

// TestAccount_UpdateAccount_SessionRevocationFailureLeavesCredentials（R05-P1-3）：
// 撤会话失败时资料变更不得提交——旧密码仍可登录、邮箱不变。
func TestAccount_UpdateAccount_SessionRevocationFailureLeavesCredentials(t *testing.T) {
	ctx, account, projectID, sessions, _, _ := setupG3Account(t)

	user, userID := signUpG3User(t, ctx, account, projectID, "revoke-fail@torchwood.local")
	authCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    userID,
		Email:     user.Email,
		Roles:     []string{"users", "user:" + userID},
	})

	sessions.fail = true
	_, err := account.UpdateAccount(authCtx, UpdateAccountCommand{
		Password:    "NewPass@123",
		OldPassword: "User@123",
	})
	require.Error(t, err)

	// 旧密码仍可登录（password_hash 未变），邮箱未变。
	sessions.fail = false
	_, tokens, _, _, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "revoke-fail@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)

	// 邮箱变更同样不提交。
	sessions.fail = true
	_, err = account.UpdateAccount(authCtx, UpdateAccountCommand{
		Email:       "never-applied@torchwood.local",
		OldPassword: "User@123",
	})
	require.Error(t, err)
	sessions.fail = false
	_, _, _, _, err = account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "revoke-fail@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
}

// TestAccount_UpdateRecovery_SessionRevocationFailureLeavesOldPassword（R05-P1-3）：
// 找回密码路径撤会话失败时旧密码保持有效。
func TestAccount_UpdateRecovery_SessionRevocationFailureLeavesOldPassword(t *testing.T) {
	ctx, account, projectID, sessions, _, _ := setupG3Account(t)

	user, userID := signUpG3User(t, ctx, account, projectID, "recovery-revoke-fail@torchwood.local")

	secret, _, err := account.tokens.CreateRecoveryToken(ctx, projectID, userID, user.Email)
	require.NoError(t, err)

	sessions.fail = true
	err = account.UpdateRecovery(ctx, UpdateRecoveryCommand{
		ProjectID: projectID,
		UserID:    userID,
		Secret:    secret,
		Password:  "NewPass@456",
	})
	require.Error(t, err)

	// 旧密码仍可登录。
	sessions.fail = false
	_, tokens, _, _, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "recovery-revoke-fail@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)
}

// TestAccount_UnregisteredEmailFailuresDoNotTriggerLockout（R05-P1-5）：
// 未注册邮箱连续失败不计数——同 IP 下已注册用户登录不受影响。
func TestAccount_UnregisteredEmailFailuresDoNotTriggerLockout(t *testing.T) {
	ctx, account, projectID, _, _, _ := setupG3Account(t)

	signUpG3User(t, ctx, account, projectID, "registered-user@torchwood.local")

	attackerCtx := contexts.WithClientInfo(ctx, contexts.ClientInfo{IP: "198.51.100.77"})
	for i := 0; i < 12; i++ {
		_, _, _, _, err := account.SignIn(attackerCtx, SignInCommand{
			ProjectID: projectID,
			Email:     "no-such-user@torchwood.local",
			Password:  "WrongPass@1",
		})
		require.Error(t, err)
		st, _ := status.FromError(err)
		require.Equal(t, codes.Unauthenticated, st.Code(), "未注册邮箱失败必须是统一 Unauthenticated，而非 ResourceExhausted")
	}

	// 同一 IP 下已注册用户仍可正常登录（未计数 → 未锁定）。
	_, tokens, _, _, err := account.SignIn(attackerCtx, SignInCommand{
		ProjectID: projectID,
		Email:     "registered-user@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokens.AccessToken)
}
