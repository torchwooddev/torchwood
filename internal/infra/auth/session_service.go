package auth

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultSessionTTL = 7 * 24 * time.Hour

// defaultMaxSessionsPerUser 是 security.sessions.max_per_user 未配置（0）时的
// 单用户会话上限默认值。
const defaultMaxSessionsPerUser = 50

// SessionService implements domainauth.SessionService.
type SessionService struct {
	cfg          *config.AppConfig
	docDB        databases.DocumentDB
	sessionCodec *SessionCookieCodec
	roles        domainauth.UserRoleResolver
	rotation     domainauth.RefreshRotationStore
}

func NewSessionService(
	cfg *config.AppConfig,
	docDB databases.DocumentDB,
	roles domainauth.UserRoleResolver,
	rotation domainauth.RefreshRotationStore,
) *SessionService {
	return &SessionService{
		cfg:          cfg,
		docDB:        docDB,
		sessionCodec: NewSessionCookieCodec(string(jwtparser.DeriveKey(cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeSessionCookie))),
		roles:        roles,
		rotation:     rotation,
	}
}

func (s *SessionService) CreateSessionAndTokens(ctx context.Context, projectID, userID, email, provider string) (*domainauth.TokenBundle, string, error) {
	if provider == "" {
		provider = domainauth.ProviderEmail
	}
	client := contexts.ClientInfoFrom(ctx)

	// R05-P1-6：会话数量上限（security.sessions.max_per_user，未配置/0 = 默认
	// 50；-1 = 不限）。超限时先淘汰最旧会话（按 expire_at 升序）再创建，
	// 保证并发登录不越界。
	if max := s.maxSessionsPerUser(); max > 0 {
		if err := s.evictOldestSessions(ctx, projectID, userID, max); err != nil {
			return nil, "", err
		}
	}

	expireAt := time.Now().Add(defaultSessionTTL)
	sessionID := idgen.UUID().String()
	// UUID 高熵，可用无盐 SHA-256（HashOTP）。
	sessionSecret := idgen.UUID().String()
	sessionDoc := databases.Document{
		ID: sessionID,
		Data: map[string]any{
			"user_id":     userID,
			"secret_hash": HashOTP(sessionSecret),
			"provider":    provider,
			"expire_at":   expireAt.Format(time.RFC3339Nano),
			"user_agent":  client.UserAgent,
			"ip":          client.IP,
		},
	}
	sessionPerms := sessionPermissions(userID)
	if _, err := s.docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", sessionDoc, sessionPerms, databases.SystemPrincipal); err != nil {
		return nil, "", err
	}
	return s.IssueTokens(ctx, projectID, userID, email, sessionID)
}

func (s *SessionService) IssueTokens(ctx context.Context, projectID, userID, email, sessionID string) (*domainauth.TokenBundle, string, error) {
	return s.IssueTokensWithRefreshID(ctx, projectID, userID, email, sessionID, idgen.UUID().String())
}

func (s *SessionService) IssueTokensWithRefreshID(ctx context.Context, projectID, userID, email, sessionID, refreshTokenID string) (*domainauth.TokenBundle, string, error) {
	accessTTL := 15 * time.Minute
	if d, err := time.ParseDuration(s.cfg.GetSecurity().GetJwt().GetAccessTtl()); err == nil {
		accessTTL = d
	}
	refreshTTL := defaultSessionTTL
	if d, err := time.ParseDuration(s.cfg.GetSecurity().GetJwt().GetRefreshTtl()); err == nil {
		refreshTTL = d
	}

	now := time.Now()
	baseRoles, err := s.roles.LoadUserRoles(ctx, projectID, userID)
	if err != nil {
		return nil, "", err
	}
	accessClaims := jwtparser.Claims{
		TokenID:   idgen.UUID().String(),
		UserID:    userID,
		Username:  email,
		ActorKind: "end_user",
		ProjectID: projectID,
		SessionID: sessionID,
		TokenType: jwtparser.TokenTypeAccess,
		Roles:     baseRoles,
		ExpiresAt: now.Add(accessTTL).Unix(),
		IssuedAt:  now.Unix(),
	}
	endUserKey := jwtparser.DeriveKey(s.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeEndUserJWT)
	accessToken, err := jwtparser.Generate(endUserKey, accessClaims)
	if err != nil {
		return nil, "", err
	}
	refreshClaims := accessClaims
	refreshClaims.TokenID = refreshTokenID
	refreshClaims.TokenType = jwtparser.TokenTypeRefresh
	refreshClaims.ExpiresAt = now.Add(refreshTTL).Unix()
	refreshToken, err := jwtparser.Generate(endUserKey, refreshClaims)
	if err != nil {
		return nil, "", err
	}

	if s.rotation != nil {
		if err := s.rotation.Register(ctx, domainauth.RefreshRotationKey(projectID, sessionID), refreshTokenID, refreshTTL); err != nil {
			return nil, "", err
		}
	}

	cookie := s.sessionCodec.Sign(projectID, sessionID)
	return &domainauth.TokenBundle{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ExpiresAt:      accessClaims.ExpiresAt,
		RefreshTokenID: refreshTokenID,
	}, cookie, nil
}

func (s *SessionService) EnsureActiveSession(ctx context.Context, projectID, sessionID, userID string) error {
	sessionDoc, err := s.docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", sessionID, databases.SystemPrincipal)
	if err != nil {
		return status.Error(codes.Unauthenticated, "session lookup failed")
	}
	if sessionDoc == nil {
		return status.Error(codes.Unauthenticated, "session not found or revoked")
	}
	if uid, _ := sessionDoc.Data["user_id"].(string); uid != userID {
		return status.Error(codes.Unauthenticated, "invalid session")
	}
	if expireAtRaw, ok := sessionDoc.Data["expire_at"]; ok {
		expireAt, err := parseSessionTime(expireAtRaw)
		if err != nil {
			// Fail closed: unparsable expiry is treated as expired.
			return status.Error(codes.Unauthenticated, "session expired")
		}
		if expireAt.Before(time.Now()) {
			return status.Error(codes.Unauthenticated, "session expired")
		}
	}
	return nil
}

// DeleteSessionsByUser removes every session document owned by the user.
// 循环分页收集全部会话 ID 后以 BulkDeleteDocuments 批量删除（documentdb 侧
// 包事务，中途失败整体回滚，替代逐条删除的部分成功行为——R05-P2-7）。
func (s *SessionService) DeleteSessionsByUser(ctx context.Context, projectID, userID string) error {
	pageToken := ""
	var ids []string
	for {
		list, err := s.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Query{
			Queries:   []string{query.BuildEqual("user_id", userID)},
			PageSize:  1000,
			PageToken: pageToken,
		}, databases.SystemPrincipal)
		if err != nil {
			return err
		}
		for i := range list.Documents {
			ids = append(ids, list.Documents[i].ID)
		}
		if list.NextPageToken == "" {
			break
		}
		pageToken = list.NextPageToken
	}
	if len(ids) == 0 {
		return nil
	}
	deleted, err := s.docDB.BulkDeleteDocuments(ctx, projectID, databases.SystemDatabaseID, "sessions", ids, databases.SystemPrincipal)
	if err != nil {
		slog.Warn("delete sessions by user failed", "project_id", projectID, "user_id", userID, "deleted", deleted, "error", err)
		return err
	}
	return nil
}

// maxSessionsPerUser 返回单用户会话上限：未配置（0 值）回退默认 50；
// -1 表示不限（返回 0，调用方跳过淘汰）。
func (s *SessionService) maxSessionsPerUser() int {
	if s.cfg == nil || s.cfg.GetSecurity() == nil || s.cfg.GetSecurity().GetSessions() == nil {
		return defaultMaxSessionsPerUser
	}
	switch v := s.cfg.GetSecurity().GetSessions().GetMaxPerUser(); {
	case v < 0:
		return 0 // -1 = 不限
	case v == 0:
		return defaultMaxSessionsPerUser
	default:
		return int(v)
	}
}

// evictOldestSessions 当会话数达到上限时，淘汰最旧（expire_at 最早）的
// （总数-max+1）个会话，为本次新建腾出位置。
func (s *SessionService) evictOldestSessions(ctx context.Context, projectID, userID string, max int) error {
	pageToken := ""
	var ids []string
	for {
		list, err := s.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Query{
			Queries:   []string{query.BuildEqual("user_id", userID), query.BuildFilter("orderAsc", "expire_at")},
			PageSize:  1000,
			PageToken: pageToken,
		}, databases.SystemPrincipal)
		if err != nil {
			return err
		}
		for i := range list.Documents {
			ids = append(ids, list.Documents[i].ID)
		}
		if list.NextPageToken == "" {
			break
		}
		pageToken = list.NextPageToken
	}
	if len(ids) < max {
		return nil
	}
	evict := ids[:len(ids)-max+1]
	if _, err := s.docDB.BulkDeleteDocuments(ctx, projectID, databases.SystemDatabaseID, "sessions", evict, databases.SystemPrincipal); err != nil {
		slog.Warn("evict old sessions failed", "project_id", projectID, "user_id", userID, "error", err)
		return err
	}
	return nil
}

func sessionPermissions(userID string) []databases.Permission {
	// sessions 集合的 keys 只读；update/delete 仅限 owner（user:<id>）与 admin
	// （安全评审 C1 第 3 层 / M2：任意 scope 的 API key 不得改删会话）。
	return []databases.Permission{
		{Type: "read", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "delete", Role: "admin"},
	}
}

func parseSessionTime(v any) (time.Time, error) {
	return ParseSessionTime(v)
}

// sha256HexLen 是 SHA-256 十六进制编码长度。
const sha256HexLen = 64

// sessionSecretLooksHashed 判定 stored 是否为 64 字符 hex（视为已哈希）。
func sessionSecretLooksHashed(stored string) bool {
	if len(stored) != sha256HexLen {
		return false
	}
	_, err := hex.DecodeString(stored)
	return err == nil
}

// canonicalizeSessionSecretHash 双读 secret_hash：64 字符 hex 原样返回，否则按明文做 HashOTP。
func canonicalizeSessionSecretHash(stored string) string {
	if stored == "" || sessionSecretLooksHashed(stored) {
		return stored
	}
	return HashOTP(stored)
}

// ParseSessionTime decodes session expire_at values from document storage.
func ParseSessionTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		return time.Parse(time.RFC3339Nano, t)
	}
	return time.Time{}, fmt.Errorf("unsupported time type")
}
