package client

import (
	"context"
	"fmt"
	"strings"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateMagicURLSessionCommand struct {
	ProjectID string
	Email     string
	URL       string
}

type UpdateMagicURLSessionCommand struct {
	ProjectID string
	UserID    string
	Secret    string
}

// CreateMagicURLSession 发送含一次性 secret 的登录链接邮件；用户不存在或为
// 占位邮箱时返回空 challenge（防枚举，与 recovery 行为一致）。
func (a *Account) CreateMagicURLSession(ctx context.Context, cmd CreateMagicURLSessionCommand) (*Challenge, error) {
	if a.tokens == nil {
		return nil, status.Error(codes.Unimplemented, "magic url is not configured")
	}
	if a.mailer == nil {
		return nil, status.Error(codes.Unimplemented, "email delivery is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	email := normalizeEmail(cmd.Email)
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validateRedirectURL(cmd.URL); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid url: %v", err)
	}
	if err := a.validateProjectOAuthRedirectURLs(ctx, projectID, cmd.URL, cmd.URL); err != nil {
		return nil, err
	}
	if err := a.ensureProjectReady(ctx, projectID); err != nil {
		return nil, err
	}

	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.tokens.CheckSendRateLimit(ctx, projectID, email, clientInfo.IP); err != nil {
		return nil, err
	}

	list, err := a.docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if len(list.Documents) == 0 {
		return &Challenge{}, nil
	}
	userDoc := list.Documents[0]
	// 匿名/phone/wechat 占位邮箱不允许 magic url 登录。
	if strings.HasSuffix(email, "@torchwood.local") {
		return &Challenge{}, nil
	}

	challengeID, secret, expireAt, err := a.tokens.CreateMagicURLToken(ctx, projectID, userDoc.ID, email)
	if err != nil {
		return nil, err
	}
	// secret 仅存在于邮件链接中；API 响应只回传不透明 challengeID。
	link := buildAccountActionURL(cmd.URL, userDoc.ID, secret)
	subject := "Sign in to Torchwood"
	body := fmt.Sprintf("Click the link below to sign in:\n\n%s\n\nThis link expires at %s.", link, expireAt.Format("2006-01-02 15:04 MST"))
	if err := a.mailer.Send(ctx, email, subject, body); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send magic url email: %v", err)
	}
	return &Challenge{ChallengeID: challengeID, ExpireAt: expireAt}, nil
}

// UpdateMagicURLSession 校验一次性 secret 后走 finishSignInWithProvider
// （MFA 钩子自动生效）。
func (a *Account) UpdateMagicURLSession(ctx context.Context, cmd UpdateMagicURLSessionCommand) (*User, *TokenBundle, string, *MFASignInChallenge, error) {
	if a.tokens == nil {
		return nil, nil, "", nil, status.Error(codes.Unimplemented, "magic url is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	userID := strings.TrimSpace(cmd.UserID)
	secret := strings.TrimSpace(cmd.Secret)
	if projectID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if userID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if secret == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "secret is required")
	}
	if err := a.ensureProjectReady(ctx, projectID); err != nil {
		return nil, nil, "", nil, err
	}
	if err := a.tokens.VerifyMagicURLToken(ctx, projectID, userID, secret); err != nil {
		return nil, nil, "", nil, err
	}

	doc, err := a.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if doc == nil {
		return nil, nil, "", nil, status.Error(codes.Unauthenticated, "user not found")
	}
	user := mapUserDoc(doc)
	if !users.CanAuthenticate(user.Status) {
		return nil, nil, "", nil, status.Error(codes.Unauthenticated, "user account is not active")
	}
	return a.finishSignInWithProvider(ctx, projectID, user, domainauth.ProviderMagicURL)
}
