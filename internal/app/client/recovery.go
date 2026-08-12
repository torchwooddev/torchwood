package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/password"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateRecoveryCommand struct {
	ProjectID string
	Email     string
	URL       string
}

type UpdateRecoveryCommand struct {
	ProjectID string
	UserID    string
	Secret    string
	Password  string
}

func (a *Account) CreateRecovery(ctx context.Context, cmd CreateRecoveryCommand) error {
	if a.tokens == nil {
		return status.Error(codes.Unimplemented, "account recovery is not configured")
	}
	if a.mailer == nil {
		return status.Error(codes.Unimplemented, "email delivery is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	email := normalizeEmail(cmd.Email)
	if projectID == "" {
		return status.Error(codes.InvalidArgument, "project_id is required")
	}
	if email == "" {
		return status.Error(codes.InvalidArgument, "email is required")
	}
	if err := validateEmail(email); err != nil {
		return err
	}
	if err := validateRedirectURL(cmd.URL); err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid url: %v", err)
	}
	if err := a.validateProjectOAuthRedirectURLs(ctx, projectID, cmd.URL, cmd.URL); err != nil {
		return err
	}
	if err := a.ensureProjectReady(ctx, projectID); err != nil {
		return err
	}

	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.tokens.CheckSendRateLimit(ctx, projectID, email, clientInfo.IP); err != nil {
		return err
	}

	list, err := a.docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	if len(list.Documents) == 0 {
		return nil
	}
	userDoc := list.Documents[0]
	hash, _ := userDoc.Data["password_hash"].(string)
	if hash == "" {
		return nil
	}

	secret, expireAt, err := a.tokens.CreateRecoveryToken(ctx, projectID, userDoc.ID, email)
	if err != nil {
		return err
	}
	link := buildAccountActionURL(cmd.URL, userDoc.ID, secret)
	subject := "Reset your Torchwood password"
	body := fmt.Sprintf("Click the link below to reset your password:\n\n%s\n\nThis link expires at %s.", link, expireAt.Format("2006-01-02 15:04 MST"))
	if err := a.mailer.Send(ctx, email, subject, body); err != nil {
		return status.Errorf(codes.Internal, "failed to send recovery email: %v", err)
	}
	return nil
}

func (a *Account) UpdateRecovery(ctx context.Context, cmd UpdateRecoveryCommand) error {
	if a.tokens == nil {
		return status.Error(codes.Unimplemented, "account recovery is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	userID := strings.TrimSpace(cmd.UserID)
	secret := strings.TrimSpace(cmd.Secret)
	passwordRaw := cmd.Password
	if projectID == "" {
		return status.Error(codes.InvalidArgument, "project_id is required")
	}
	if userID == "" {
		return status.Error(codes.InvalidArgument, "user_id is required")
	}
	if secret == "" {
		return status.Error(codes.InvalidArgument, "secret is required")
	}
	if err := validatePasswordStrength(passwordRaw); err != nil {
		return err
	}
	if err := a.ensureProjectReady(ctx, projectID); err != nil {
		return err
	}
	if err := a.tokens.VerifyRecoveryToken(ctx, projectID, userID, secret); err != nil {
		return err
	}

	doc, err := a.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	if doc == nil {
		return status.Error(codes.NotFound, "user not found")
	}
	hash, err := password.Hash(passwordRaw)
	if err != nil {
		return err
	}
	// R05-P1-3：先撤会话再提交新密码；撤会话失败时密码保持原样，避免
	// "密码已改但旧会话仍存活"（重放/劫持窗口）。
	if err := a.sessions.DeleteSessionsByUser(ctx, projectID, userID); err != nil {
		return fmt.Errorf("delete sessions after password reset: %w", err)
	}
	if _, err := a.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userID,
		Data: map[string]any{"password_hash": hash},
	}, nil), databases.SystemPrincipal); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}
