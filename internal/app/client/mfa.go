package client

import (
	"context"
	"fmt"
	"time"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MFASignInChallenge 是登录钩子返回的二次验证挑战；nil 表示无需挑战。
type MFASignInChallenge struct {
	Token   string
	Factors []domainauth.Factor
}

// mfaFactorActivateWindow 是 pending 因子必须激活的时限。
const mfaFactorActivateWindow = 10 * time.Minute

// mfaSignInChallenge 读取用户 verified 因子；存在时创建一次性挑战并返回，
// 不存在（或 MFA 未装配）返回 nil，调用方按原逻辑签发会话。
func (a *Account) mfaSignInChallenge(ctx context.Context, projectID string, user *User) (*MFASignInChallenge, error) {
	if a.mfaChallenges == nil || a.mfa == nil || user == nil {
		return nil, nil
	}
	doc, err := a.docDB.GetDocument(ctx, projectID, "default", "users", user.ID, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.Unauthenticated, "user not found")
	}
	factors := parseFactors(doc.Data["factors"])
	verified := make([]domainauth.Factor, 0, 1)
	for _, f := range factors {
		if f.Status == domainauth.FactorStatusVerified && f.Type == domainauth.FactorTypeTOTP {
			verified = append(verified, f)
		}
	}
	if len(verified) == 0 {
		return nil, nil
	}
	token, _, err := a.mfaChallenges.Create(ctx, projectID, user.ID)
	if err != nil {
		return nil, err
	}
	return &MFASignInChallenge{Token: token, Factors: verified}, nil
}

func (a *Account) ListFactors(ctx context.Context) ([]domainauth.Factor, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	factors, err := a.loadFactors(ctx, p)
	if err != nil {
		return nil, err
	}
	// 读路径不回显明文 secret（ListFactors 仅返回元数据）。
	for i := range factors {
		factors[i].Secret = ""
	}
	return factors, nil
}

func (a *Account) CreateTOTPFactor(ctx context.Context, projectID, userID, email string) (*domainauth.Factor, string, string, error) {
	if a.mfa == nil {
		return nil, "", "", status.Error(codes.Unimplemented, "mfa is not configured")
	}
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, "", "", err
	}
	if projectID == "" {
		projectID = p.ProjectID
	}
	if projectID != p.ProjectID || userID != p.UserID {
		return nil, "", "", status.Error(codes.PermissionDenied, "cannot create mfa factor for another user")
	}

	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "users", p.UserID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, "", "", err
	}
	if doc == nil {
		return nil, "", "", status.Error(codes.NotFound, "user not found")
	}
	if email == "" {
		email = normalizeEmail(stringValue(doc.Data["email"]))
	}
	if email == "" {
		return nil, "", "", status.Error(codes.FailedPrecondition, "user email cannot be used for mfa")
	}

	project, err := a.projectRepo.GetProject(ctx, p.ProjectID)
	if err != nil {
		return nil, "", "", err
	}
	if project == nil {
		return nil, "", "", status.Error(codes.NotFound, "project not found")
	}
	issuer := project.Name
	if issuer == "" {
		issuer = p.ProjectID
	}

	factors, err := a.loadFactors(ctx, p)
	if err != nil {
		return nil, "", "", err
	}
	// 清理同类型过期 pending 因子（激活时限外），避免堆积。
	now := time.Now()
	kept := factors[:0]
	for _, f := range factors {
		if f.Type == domainauth.FactorTypeTOTP && f.Status == domainauth.FactorStatusPending &&
			!f.CreatedAt.IsZero() && now.Sub(f.CreatedAt) > mfaFactorActivateWindow {
			continue
		}
		kept = append(kept, f)
	}
	factors = kept

	factor, plainSecret, otpauthURL, err := a.mfa.CreateTOTPFactor(ctx, issuer, p.UserID, email)
	if err != nil {
		return nil, "", "", err
	}
	factors = append(factors, *factor)
	if _, err := a.saveFactors(ctx, p, factors); err != nil {
		return nil, "", "", err
	}
	return factor, plainSecret, otpauthURL, nil
}

func (a *Account) VerifyTOTPFactor(ctx context.Context, projectID, userID, factorID, code string) (*domainauth.Factor, error) {
	if a.mfa == nil {
		return nil, status.Error(codes.Unimplemented, "mfa is not configured")
	}
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		projectID = p.ProjectID
	}
	if projectID != p.ProjectID || userID != p.UserID {
		return nil, status.Error(codes.PermissionDenied, "cannot verify mfa factor for another user")
	}
	if factorID == "" {
		return nil, status.Error(codes.InvalidArgument, "factor_id is required")
	}
	if code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	factors, err := a.loadFactors(ctx, p)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range factors {
		if factors[i].ID == factorID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, status.Error(codes.NotFound, "mfa factor not found")
	}
	factor := &factors[idx]
	if factor.Status != domainauth.FactorStatusPending {
		return nil, status.Error(codes.InvalidArgument, "mfa factor is already verified")
	}
	if !factor.CreatedAt.IsZero() && time.Since(factor.CreatedAt) > mfaFactorActivateWindow {
		// 过期 pending 因子：删除并拒绝。
		factors = append(factors[:idx], factors[idx+1:]...)
		if _, err := a.saveFactors(ctx, p, factors); err != nil {
			return nil, err
		}
		return nil, status.Error(codes.Unauthenticated, "mfa factor activation expired")
	}

	if err := a.mfa.VerifyTOTPFactor(ctx, factor, code); err != nil {
		return nil, err
	}
	factor.Status = domainauth.FactorStatusVerified
	factors[idx] = *factor
	if _, err := a.saveFactors(ctx, p, factors); err != nil {
		return nil, err
	}
	factor.Secret = ""
	return factor, nil
}

func (a *Account) DeleteFactor(ctx context.Context, projectID, userID, factorID string) error {
	p, err := a.requireUser(ctx)
	if err != nil {
		return err
	}
	if projectID == "" {
		projectID = p.ProjectID
	}
	if projectID != p.ProjectID || userID != p.UserID {
		return status.Error(codes.PermissionDenied, "cannot delete mfa factor for another user")
	}
	if factorID == "" {
		return status.Error(codes.InvalidArgument, "factor_id is required")
	}

	factors, err := a.loadFactors(ctx, p)
	if err != nil {
		return err
	}
	idx := -1
	for i := range factors {
		if factors[i].ID == factorID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return status.Error(codes.NotFound, "mfa factor not found")
	}
	factors = append(factors[:idx], factors[idx+1:]...)
	_, err = a.saveFactors(ctx, p, factors)
	return err
}

// CompleteMFASession 消费一次性挑战 token、校验 TOTP code 并签发会话。
func (a *Account) CompleteMFASession(ctx context.Context, projectID, challengeToken, factorID, code string) (*User, *TokenBundle, string, error) {
	if a.mfaChallenges == nil || a.mfa == nil {
		return nil, nil, "", status.Error(codes.Unimplemented, "mfa is not configured")
	}
	challengeProjectID, userID, err := a.mfaChallenges.Consume(ctx, challengeToken)
	if err != nil {
		return nil, nil, "", err
	}
	if projectID == "" {
		projectID = challengeProjectID
	}
	if projectID != challengeProjectID {
		return nil, nil, "", status.Error(codes.Unauthenticated, "invalid or expired challenge")
	}
	if factorID == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "factor_id is required")
	}
	if code == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "code is required")
	}

	doc, err := a.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, "", err
	}
	if doc == nil {
		return nil, nil, "", status.Error(codes.Unauthenticated, "user not found")
	}
	factors := parseFactors(doc.Data["factors"])
	var factor *domainauth.Factor
	for i := range factors {
		if factors[i].ID == factorID && factors[i].Status == domainauth.FactorStatusVerified {
			factor = &factors[i]
			break
		}
	}
	if factor == nil {
		return nil, nil, "", status.Error(codes.Unauthenticated, "mfa factor is not verified")
	}
	if err := a.mfa.ValidateTOTP(ctx, factor, code); err != nil {
		return nil, nil, "", err
	}

	user := mapUserDoc(doc)
	if !users.CanAuthenticate(stringValue(doc.Data["status"])) {
		return nil, nil, "", status.Error(codes.Unauthenticated, "user account is not active")
	}
	tokens, cookie, err := a.sessions.CreateSessionAndTokens(ctx, projectID, userID, user.Email, domainauth.ProviderMFA)
	if err != nil {
		return nil, nil, "", err
	}
	return user, tokens, cookie, nil
}

// loadFactors 读取用户文档中的 factors JSON 数组。
func (a *Account) loadFactors(ctx context.Context, p *shared.Principal) ([]domainauth.Factor, error) {
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "users", p.UserID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return parseFactors(doc.Data["factors"]), nil
}

// saveFactors 把因子列表写回用户文档（读-改-写；UpdateDocument 失败时调用方
// 必须放弃内存状态，由本函数原样返回错误）。
func (a *Account) saveFactors(ctx context.Context, p *shared.Principal, factors []domainauth.Factor) (*databases.Document, error) {
	updated, err := a.docDB.UpdateDocument(ctx, p.ProjectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   p.UserID,
		Data: map[string]any{"factors": factorDocs(factors)},
	}, nil), databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, fmt.Errorf("update mfa factors: %w", err)
	}
	return &updated, nil
}

func parseFactors(raw any) []domainauth.Factor {
	var out []domainauth.Factor
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return out
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		f := domainauth.Factor{
			ID:     stringValue(m["id"]),
			Type:   stringValue(m["type"]),
			Secret: stringValue(m["secret"]),
			Status: stringValue(m["status"]),
		}
		if createdAt, ok := m["created_at"].(string); ok {
			if t, err := auth.ParseSessionTime(createdAt); err == nil {
				f.CreatedAt = t
			}
		}
		if f.ID == "" || f.Type == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func factorDocs(factors []domainauth.Factor) []any {
	out := make([]any, 0, len(factors))
	for _, f := range factors {
		out = append(out, map[string]any{
			"id":         f.ID,
			"type":       f.Type,
			"secret":     f.Secret,
			"status":     f.Status,
			"created_at": f.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}
