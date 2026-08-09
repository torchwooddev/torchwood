package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mfaTestConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: "mfa-app-test-secret"},
		},
	}
}

// setupMFATestAccount 注册一个用户并返回带 principal 的 ctx 与 account。
func setupMFATestAccount(t *testing.T) (context.Context, *Account, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	t.Cleanup(cleanup)

	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	account := NewTestAccountWithRedis(mfaTestConfig(), projectRepo, docDB, rdb)

	user, _, _, mfa, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "mfa-user@torchwood.local",
		Password:  "User@123",
		Name:      "MFA User",
	})
	require.NoError(t, err)
	require.Nil(t, mfa)

	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Email:     user.Email,
		Roles:     []string{"users", "user:" + user.ID},
	})
	return userCtx, account, projectID, user.ID
}

func TestAccount_ListFactors(t *testing.T) {
	ctx, account, _, _ := setupMFATestAccount(t)

	factors, err := account.ListFactors(ctx)
	require.NoError(t, err)
	require.Empty(t, factors)
}

func TestAccount_CreateTOTPFactor_SecretEncrypted(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, plainSecret, otpauthURL, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)
	require.NotEmpty(t, plainSecret)
	require.Contains(t, otpauthURL, "otpauth://totp/")

	// 落库密文：非明文、带 enc:v1: 前缀。
	doc, err := account.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotNil(t, doc)
	rawFactors := doc.Data["factors"]
	require.NotNil(t, rawFactors)
	storedJSON := factorDocs(parseFactors(rawFactors))[0].(map[string]any)
	require.Contains(t, storedJSON["secret"], "enc:v1:")
	require.NotContains(t, storedJSON["secret"], plainSecret)

	// List 不含明文 secret。
	list, err := account.ListFactors(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, factor.ID, list[0].ID)
	require.Empty(t, list[0].Secret)
	require.Equal(t, auth.FactorStatusPending, list[0].Status)
}

func TestAccount_CreateTOTPFactor_RequiresJWTSecret(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	// 空 jwt secret 的配置。
	account := NewTestAccountWithRedis(&config.AppConfig{}, projectRepo, docDB, rdb)

	user, _, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "mfa-nosecret@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	userCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user.ID,
		Roles:     []string{"users", "user:" + user.ID},
	})
	_, _, _, err = account.CreateTOTPFactor(userCtx, projectID, user.ID, "")
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Internal, st.Code())
}

func TestAccount_VerifyTOTPFactor_Activate(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, plainSecret, _, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)

	code, err := totp.GenerateCode(plainSecret, time.Now())
	require.NoError(t, err)
	verified, err := account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, code)
	require.NoError(t, err)
	require.Equal(t, auth.FactorStatusVerified, verified.Status)

	// 已 verified 因子不可重复激活。
	_, err = account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, code)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestAccount_VerifyTOTPFactor_WrongCode(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, _, _, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)

	_, err = account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, "123456")
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())

	// 因子仍为 pending，可重试。
	factors, err := account.ListFactors(ctx)
	require.NoError(t, err)
	require.Equal(t, auth.FactorStatusPending, factors[0].Status)
}

func TestAccount_VerifyTOTPFactor_LockedAfterFailures(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, plainSecret, _, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err := account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, "000000")
		require.Error(t, err)
	}
	// 正确 code 也被锁定拒绝。
	code, err := totp.GenerateCode(plainSecret, time.Now())
	require.NoError(t, err)
	_, err = account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, code)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())
}

func TestAccount_DeleteFactor_RestoresDirectSignIn(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, plainSecret, _, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)
	code, err := totp.GenerateCode(plainSecret, time.Now())
	require.NoError(t, err)
	_, err = account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, code)
	require.NoError(t, err)

	// 删除后登录恢复直通（无 MFA 挑战）。
	require.NoError(t, account.DeleteFactor(ctx, projectID, userID, factor.ID))
	factors, err := account.ListFactors(ctx)
	require.NoError(t, err)
	require.Empty(t, factors)

	_, _, _, mfa, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "mfa-user@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.Nil(t, mfa)
}

// TestAccount_SignInRequiresMFA 登录钩子：有 verified 因子时返回 mfa_required，
// 且不产生会话文档；CompleteMFASession 后会话可用；challenge 一次性。
func TestAccount_VerifyTOTPFactor_ExpiredPending(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, _, _, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)

	// 把因子 created_at 改成 11 分钟前，模拟激活超时。
	old := factor.CreatedAt.Add(-11 * time.Minute)
	doc, err := account.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	require.NoError(t, err)
	factors := parseFactors(doc.Data["factors"])
	for i := range factors {
		if factors[i].ID == factor.ID {
			factors[i].CreatedAt = old
		}
	}
	_, err = account.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userID,
		Data: map[string]any{"factors": factorDocs(factors)},
	}, nil), databases.SystemPrincipal)
	require.NoError(t, err)

	code, err := totp.GenerateCode("whatever", time.Now())
	require.NoError(t, err)
	_, err = account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, code)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())

	// 过期因子已被删除。
	list, err := account.ListFactors(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestAccount_SignInRequiresMFA(t *testing.T) {
	ctx, account, projectID, userID := setupMFATestAccount(t)

	factor, plainSecret, _, err := account.CreateTOTPFactor(ctx, projectID, userID, "")
	require.NoError(t, err)
	code, err := totp.GenerateCode(plainSecret, time.Now())
	require.NoError(t, err)
	_, err = account.VerifyTOTPFactor(ctx, projectID, userID, factor.ID, code)
	require.NoError(t, err)

	// 登出当前会话（SignUp 产生的），确保登录前无活跃会话。
	sessions, err := account.ListSessions(ctx)
	require.NoError(t, err)
	for _, s := range sessions {
		require.NoError(t, account.DeleteSession(ctx, s.ID))
	}

	// SignIn 触发 MFA 挑战：不签发会话。
	user, tokens, cookie, mfa, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "mfa-user@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.NotNil(t, mfa)
	require.NotEmpty(t, mfa.Token)
	require.Len(t, mfa.Factors, 1)
	require.Nil(t, tokens)
	require.Empty(t, cookie)
	require.Equal(t, userID, user.ID)

	// 无新会话文档。
	sessionsAfter, err := account.ListSessions(ctx)
	require.NoError(t, err)
	require.Empty(t, sessionsAfter)

	// 错误 code 拒绝。
	_, _, _, err = account.CompleteMFASession(ctx, projectID, mfa.Token, factor.ID, "123456")
	require.Error(t, err)

	// 错误 code 后 challenge 已被消耗：正确 code 也无法再使用同一 token。
	goodCode, err := totp.GenerateCode(plainSecret, time.Now())
	require.NoError(t, err)
	_, _, _, err = account.CompleteMFASession(ctx, projectID, mfa.Token, factor.ID, goodCode)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
	require.Contains(t, strings.ToLower(err.Error()), "challenge")

	// 重新登录获取新挑战，用正确 code 完成挑战 → 签发会话。
	_, _, _, mfa2, err := account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "mfa-user@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)
	require.NotNil(t, mfa2)
	goodCode2, err := totp.GenerateCode(plainSecret, time.Now())
	require.NoError(t, err)
	user2, tokens2, cookie2, err := account.CompleteMFASession(ctx, projectID, mfa2.Token, factor.ID, goodCode2)
	require.NoError(t, err)
	require.Equal(t, userID, user2.ID)
	require.NotEmpty(t, tokens2.AccessToken)
	require.NotEmpty(t, cookie2)

	// 会话可用。
	meCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    user2.ID,
		Email:     user2.Email,
		Roles:     []string{"users", "user:" + user2.ID},
	})
	me, err := account.Me(meCtx)
	require.NoError(t, err)
	require.Equal(t, userID, me.ID)
}
