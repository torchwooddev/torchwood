package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/domain/users"
	"github.com/torchwoodio/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 匿名会话按客户端 IP 频控：默认每 IP 每小时 20 次，防止无限刷用户文档与会话。
const (
	anonymousSessionIPWindow = time.Hour
	anonymousSessionIPLimit  = 20
)

type CreateAnonymousSessionCommand struct {
	ProjectID string
}

func (a *Account) CreateAnonymousSession(ctx context.Context, cmd CreateAnonymousSessionCommand) (*User, *TokenBundle, string, error) {
	projectID := strings.TrimSpace(cmd.ProjectID)
	if projectID == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "project_id is required")
	}
	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.checkAnonymousSessionRateLimit(ctx, clientInfo.IP); err != nil {
		return nil, nil, "", err
	}
	if err := a.ensureProjectReady(ctx, projectID); err != nil {
		return nil, nil, "", err
	}

	userID, err := a.generateUserID(ctx, projectID)
	if err != nil {
		return nil, nil, "", err
	}
	email := anonymousEmail(userID)
	userDoc := databases.Document{
		ID: userID,
		Data: map[string]any{
			"email":          email,
			"password_hash":  "",
			"name":           "Anonymous",
			"status":         users.StatusActive,
			"email_verified": false,
			"labels":         []any{"anonymous"},
			"prefs":          map[string]any{},
		},
	}
	if _, err := a.docDB.CreateDocument(ctx, projectID, "default", "users", userDoc, userDocumentPermissions(userID), databases.SystemPrincipal); err != nil {
		return nil, nil, "", err
	}
	user := mapUserDoc(&userDoc)
	return a.finishSignInWithProvider(ctx, projectID, user, domainauth.ProviderAnonymous)
}

func (a *Account) checkAnonymousSessionRateLimit(ctx context.Context, ip string) error {
	// nil 容忍：未装配限流器或拿不到客户端 IP 时不做限制。
	if a.rateLimiter == nil || ip == "" {
		return nil
	}
	return a.rateLimiter.Allow(ctx, "anonymous:ip:"+ip, anonymousSessionIPLimit, anonymousSessionIPWindow)
}

func anonymousEmail(userID string) string {
	shortID := userID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("anon_%s@torchwood.local", shortID)
}
