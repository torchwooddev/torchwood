package client

import (
	"context"
	"strings"
	"time"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
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

func (a *Account) CreateAnonymousSession(ctx context.Context, cmd CreateAnonymousSessionCommand) (*User, *TokenBundle, string, *MFASignInChallenge, error) {
	projectID := strings.TrimSpace(cmd.ProjectID)
	if projectID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.checkAnonymousSessionRateLimit(ctx, clientInfo.IP); err != nil {
		return nil, nil, "", nil, err
	}
	if err := a.requireProject(ctx, projectID); err != nil {
		return nil, nil, "", nil, err
	}

	userID, err := a.generateUserID(ctx, projectID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	registered, err := users.Register(users.RegisterInput{
		ID:        userID,
		Email:     users.AnonymousEmail(userID),
		Name:      "Anonymous",
		Anonymous: true,
	})
	if err != nil {
		return nil, nil, "", nil, mapUserError(err)
	}
	if err := a.usersRepo.Insert(ctx, projectID, registered); err != nil {
		return nil, nil, "", nil, err
	}
	return a.finishSignInWithProvider(ctx, projectID, accountUser(registered), domainauth.ProviderAnonymous)
}

func (a *Account) checkAnonymousSessionRateLimit(ctx context.Context, ip string) error {
	// nil 容忍：未装配限流器或拿不到客户端 IP 时不做限制。
	if a.rateLimiter == nil || ip == "" {
		return nil
	}
	return a.rateLimiter.Allow(ctx, "anonymous:ip:"+ip, anonymousSessionIPLimit, anonymousSessionIPWindow)
}
