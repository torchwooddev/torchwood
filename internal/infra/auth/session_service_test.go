package auth_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testSessionJWTSecret = "session-service-test-secret"

func testSessionJWTConfig() *config.AppConfig {
	return &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: testSessionJWTSecret},
		},
	}
}

type stubRoleResolver struct{}

func (stubRoleResolver) LoadUserRoles(_ context.Context, _, userID string) ([]string, error) {
	return []string{"users", "user:" + userID}, nil
}

func TestSessionService_RecordsClientInfo(t *testing.T) {
	t.Parallel()

	// Unit-level check: CreateSessionAndTokens reads ClientInfo from context.
	// Full integration is covered by account integration tests.
	svc := auth.NewSessionService(nil, nil, stubRoleResolver{}, nil)
	require.NotNil(t, svc)

	ctx := contexts.WithClientInfo(context.Background(), contexts.ClientInfo{
		IP:        "203.0.113.10",
		UserAgent: "TorchwoodTest/1.0",
	})
	info := contexts.ClientInfoFrom(ctx)
	require.Equal(t, "203.0.113.10", info.IP)
	require.Equal(t, "TorchwoodTest/1.0", info.UserAgent)
}

func TestSessionService_EnsureActiveSession_CorruptExpireAtFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{
		sessions: map[string]map[string]map[string]any{
			"proj-1": {
				"sess-bad": {"user_id": "user-1", "expire_at": "garbage"},
				"sess-ok":  {"user_id": "user-1", "expire_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano)},
			},
		},
	}
	svc := auth.NewSessionService(nil, docDB, nil, nil)

	err := svc.EnsureActiveSession(ctx, "proj-1", "sess-bad", "user-1")
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())

	require.NoError(t, svc.EnsureActiveSession(ctx, "proj-1", "sess-ok", "user-1"))
}

func TestSessionService_CreateSessionStoresHashedSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": {},
	}}
	svc := auth.NewSessionService(testSessionJWTConfig(), docDB, stubRoleResolver{}, nil)

	bundle, cookie, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "email")
	require.NoError(t, err)
	require.NotNil(t, bundle)
	require.NotEmpty(t, bundle.AccessToken)
	require.NotEmpty(t, bundle.RefreshToken)
	require.Len(t, docDB.sessions["proj-1"], 1)

	var sessionID string
	var data map[string]any
	for id, d := range docDB.sessions["proj-1"] {
		sessionID, data = id, d
	}
	hash, ok := data["secret_hash"].(string)
	require.True(t, ok)
	require.Len(t, hash, 64, "secret_hash 必须是 SHA-256 hex")
	_, decodeErr := hex.DecodeString(hash)
	require.NoError(t, decodeErr)
	require.NotContains(t, hash, "-", "secret_hash 不得是 UUID 原文")
	require.NotEqual(t, sessionID, hash)

	codec := auth.NewSessionCookieCodec(string(jwtparser.DeriveKey(testSessionJWTSecret, jwtparser.PurposeSessionCookie)))
	gotProject, gotSession, err := codec.Verify(cookie)
	require.NoError(t, err)
	require.Equal(t, "proj-1", gotProject)
	require.Equal(t, sessionID, gotSession)
	require.NotContains(t, cookie, hash)

	claims, parsed := jwtparser.Parse(jwtparser.DeriveKey(testSessionJWTSecret, jwtparser.PurposeEndUserJWT), bundle.AccessToken)
	require.True(t, parsed)
	require.Equal(t, sessionID, claims.SessionID)
	require.NotEqual(t, hash, claims.SessionID)
}

func TestSessionService_EnsureActiveSession_DualReadSecretHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	expire := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	plaintext := "550e8400-e29b-41d4-a716-446655440000"
	hashed := auth.HashOTP(plaintext)
	require.Len(t, hashed, 64)
	require.NotEqual(t, 64, len(plaintext))

	docDB := &stubDocDB{
		sessions: map[string]map[string]map[string]any{
			"proj-1": {
				"sess-hashed": {
					"user_id":     "user-1",
					"expire_at":   expire,
					"secret_hash": hashed,
				},
				"sess-plain": {
					"user_id":     "user-1",
					"expire_at":   expire,
					"secret_hash": plaintext,
				},
			},
		},
	}
	svc := auth.NewSessionService(nil, docDB, nil, nil)

	require.NoError(t, svc.EnsureActiveSession(ctx, "proj-1", "sess-hashed", "user-1"))
	require.NoError(t, svc.EnsureActiveSession(ctx, "proj-1", "sess-plain", "user-1"))
	require.Equal(t, hashed, docDB.sessions["proj-1"]["sess-hashed"]["secret_hash"])
	require.Equal(t, plaintext, docDB.sessions["proj-1"]["sess-plain"]["secret_hash"], "双读不得写回或踢掉明文存量")
}

func TestSessionService_IssueTokensAfterLegacyPlaintextSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plaintext := "550e8400-e29b-41d4-a716-446655440000"
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": {
			"sess-legacy": {
				"user_id":     "user-1",
				"expire_at":   time.Now().Add(time.Hour).Format(time.RFC3339Nano),
				"secret_hash": plaintext,
			},
		},
	}}
	svc := auth.NewSessionService(testSessionJWTConfig(), docDB, stubRoleResolver{}, nil)

	require.NoError(t, svc.EnsureActiveSession(ctx, "proj-1", "sess-legacy", "user-1"))
	bundle, cookie, err := svc.IssueTokens(ctx, "proj-1", "user-1", "user@example.com", "sess-legacy")
	require.NoError(t, err)
	require.NotEmpty(t, bundle.AccessToken)
	require.NotEmpty(t, bundle.RefreshToken)

	codec := auth.NewSessionCookieCodec(string(jwtparser.DeriveKey(testSessionJWTSecret, jwtparser.PurposeSessionCookie)))
	gotProject, gotSession, err := codec.Verify(cookie)
	require.NoError(t, err)
	require.Equal(t, "proj-1", gotProject)
	require.Equal(t, "sess-legacy", gotSession)
	require.Equal(t, plaintext, docDB.sessions["proj-1"]["sess-legacy"]["secret_hash"])
}

func TestProviderConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, "email", domainauth.ProviderEmail)
	require.Equal(t, "wechat_web", domainauth.ProviderWeChatWeb)
}

// g3SessionData 构造会话文档数据（expire_at 为 RFC3339Nano，字符串序=时间序）。
func g3SessionData(expireAt time.Time) map[string]any {
	return map[string]any{
		"user_id":     "user-1",
		"expire_at":   expireAt.Format(time.RFC3339Nano),
		"provider":    "email",
		"secret_hash": "secret",
	}
}

// TestSessionService_EvictsOldestSessionsOverLimit（R05-P1-6）：max_per_user=2
// 且已有 3 个会话时，CreateSessionAndTokens 必须先淘汰最旧 2 个再创建。
func TestSessionService_EvictsOldestSessionsOverLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": {
			"sess-oldest": g3SessionData(now.Add(-3 * time.Hour)),
			"sess-middle": g3SessionData(now.Add(-2 * time.Hour)),
			"sess-newest": g3SessionData(now.Add(-time.Hour)),
		},
	}}
	cfg := &config.AppConfig{Security: &config.Security{Sessions: &config.Security_Sessions{MaxPerUser: 2}}}
	svc := auth.NewSessionService(cfg, docDB, stubRoleResolver{}, nil)

	_, _, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "")
	require.NoError(t, err)

	// 3 个既有 + 1 新建 → 淘汰最旧 2 个 → 剩 2 个。
	require.Len(t, docDB.sessions["proj-1"], 2)
	_, ok := docDB.sessions["proj-1"]["sess-oldest"]
	require.False(t, ok, "最旧会话必须被淘汰")
	_, ok = docDB.sessions["proj-1"]["sess-middle"]
	require.False(t, ok, "次旧会话必须被淘汰")
	_, ok = docDB.sessions["proj-1"]["sess-newest"]
	require.True(t, ok, "最新会话必须保留")
}

// TestSessionService_NoEvictionUnderLimit（R05-P1-6）：未超限时不淘汰。
func TestSessionService_NoEvictionUnderLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": {
			"sess-a": g3SessionData(time.Now().Add(-time.Hour)),
		},
	}}
	cfg := &config.AppConfig{Security: &config.Security{Sessions: &config.Security_Sessions{MaxPerUser: 5}}}
	svc := auth.NewSessionService(cfg, docDB, stubRoleResolver{}, nil)

	_, _, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "")
	require.NoError(t, err)
	require.Len(t, docDB.sessions["proj-1"], 2)
	_, ok := docDB.sessions["proj-1"]["sess-a"]
	require.True(t, ok)
}

// g3Sessions 构造 count 个会话文档，编号越大 expire_at 越新（sess-0 最旧）。
func g3Sessions(now time.Time, count int) map[string]map[string]any {
	sessions := make(map[string]map[string]any, count)
	for i := 0; i < count; i++ {
		sessions[fmt.Sprintf("sess-%d", i)] = g3SessionData(now.Add(-time.Duration(count-i) * time.Hour))
	}
	return sessions
}

// TestSessionService_DefaultLimitAppliedWhenUnset（G11-5）：未配置
// （max_per_user=0）回退默认 50——51 个会话（50 旧 + 1 新建）时淘汰 1 个最旧，
// 总上限保持 50，不再视为不限。
func TestSessionService_DefaultLimitAppliedWhenUnset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": g3Sessions(time.Now(), 50),
	}}
	svc := auth.NewSessionService(&config.AppConfig{}, docDB, stubRoleResolver{}, nil)

	_, _, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "")
	require.NoError(t, err)
	require.Len(t, docDB.sessions["proj-1"], 50, "未配置时必须按默认 50 淘汰")
	_, ok := docDB.sessions["proj-1"]["sess-0"]
	require.False(t, ok, "最旧会话必须被淘汰")
}

// TestSessionService_ExplicitZeroUsesDefaultLimit（G11-5）：显式配置 0 与未配置
// 语义一致（默认 50）。
func TestSessionService_ExplicitZeroUsesDefaultLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": g3Sessions(time.Now(), 51),
	}}
	cfg := &config.AppConfig{Security: &config.Security{Sessions: &config.Security_Sessions{MaxPerUser: 0}}}
	svc := auth.NewSessionService(cfg, docDB, stubRoleResolver{}, nil)

	_, _, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "")
	require.NoError(t, err)
	require.Len(t, docDB.sessions["proj-1"], 50, "显式 0 必须回退默认 50")
}

// TestSessionService_ExplicitMinusOneUnlimited（G11-5）：显式 -1 = 不限，
// 任何数量会话都不淘汰。
func TestSessionService_ExplicitMinusOneUnlimited(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": g3Sessions(time.Now(), 55),
	}}
	cfg := &config.AppConfig{Security: &config.Security{Sessions: &config.Security_Sessions{MaxPerUser: -1}}}
	svc := auth.NewSessionService(cfg, docDB, stubRoleResolver{}, nil)

	_, _, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "")
	require.NoError(t, err)
	require.Len(t, docDB.sessions["proj-1"], 56, "-1 = 不限，不得淘汰")
}

// TestSessionService_ExplicitLimitEvictsOldest（G11-5）：显式 10——15 个既有
// 会话 + 1 新建 → 淘汰最旧 6 个 → 剩 10。
func TestSessionService_ExplicitLimitEvictsOldest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": g3Sessions(time.Now(), 15),
	}}
	cfg := &config.AppConfig{Security: &config.Security{Sessions: &config.Security_Sessions{MaxPerUser: 10}}}
	svc := auth.NewSessionService(cfg, docDB, stubRoleResolver{}, nil)

	_, _, err := svc.CreateSessionAndTokens(ctx, "proj-1", "user-1", "user@example.com", "")
	require.NoError(t, err)
	require.Len(t, docDB.sessions["proj-1"], 10, "显式 10 必须淘汰至上限")
	_, ok := docDB.sessions["proj-1"]["sess-0"]
	require.False(t, ok, "最旧会话必须被淘汰")
	_, ok = docDB.sessions["proj-1"]["sess-14"]
	require.True(t, ok, "最新会话必须保留")
}

// TestSessionService_DeleteSessionsByUser_BulkDelete（R05-P2-7）：批量删除
// 替代逐条循环——只删目标用户会话，其他用户会话保留。
func TestSessionService_DeleteSessionsByUser_BulkDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": {
			"sess-1": g3SessionData(time.Now()),
			"sess-2": g3SessionData(time.Now()),
			"sess-3": {
				"user_id":   "user-2",
				"expire_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano),
			},
		},
	}}
	svc := auth.NewSessionService(nil, docDB, nil, nil)

	require.NoError(t, svc.DeleteSessionsByUser(ctx, "proj-1", "user-1"))
	require.Len(t, docDB.sessions["proj-1"], 1)
	_, ok := docDB.sessions["proj-1"]["sess-3"]
	require.True(t, ok, "其他用户的会话不得被误删")
}

// TestSessionService_DeleteSessionsByUser_BulkDeleteFailure（R05-P2-7）：
// 批量删除失败必须返回错误（调用方据此不提交凭据变更）。
func TestSessionService_DeleteSessionsByUser_BulkDeleteFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	docDB := &stubDocDB{sessions: map[string]map[string]map[string]any{
		"proj-1": {"sess-1": g3SessionData(time.Now())},
	}}
	docDB.failBulkDelete = true
	svc := auth.NewSessionService(nil, docDB, nil, nil)

	err := svc.DeleteSessionsByUser(ctx, "proj-1", "user-1")
	require.Error(t, err)
	require.Len(t, docDB.sessions["proj-1"], 1, "失败时不得部分删除")
}
