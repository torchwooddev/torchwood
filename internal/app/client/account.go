package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/audit"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainidgen "github.com/torchwooddev/torchwood/internal/domain/idgen"
	"github.com/torchwooddev/torchwood/internal/domain/messaging"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"github.com/torchwooddev/torchwood/pkg/password"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Account struct {
	cfg            *config.AppConfig
	projectRepo    projects.Repository
	oauthProviders projects.OAuthProviderRepository
	docDB          databases.DocumentDB
	sessions       domainauth.SessionService
	otp            domainauth.OTPChallengeStore
	oauthState     domainauth.OAuthStateStore
	tokens         domainauth.AccountTokenStore
	loginThrottle  domainauth.LoginThrottle
	rotation       domainauth.RefreshRotationStore
	idGen          domainidgen.Generator
	mailer         messaging.Mailer
	sms            messaging.SMSSender
	rateLimiter    domainauth.RateLimiter
	roles          domainauth.UserRoleResolver
	mfa            domainauth.MFAService
	mfaChallenges  domainauth.MFAChallengeStore
	oneTimeTokens  domainauth.OneTimeTokenStore
	auditRepo      audit.Repository
}

func NewAccount(
	cfg *config.AppConfig,
	projectRepo projects.Repository,
	oauthProviders projects.OAuthProviderRepository,
	docDB databases.DocumentDB,
	sessions domainauth.SessionService,
	otp domainauth.OTPChallengeStore,
	oauthState domainauth.OAuthStateStore,
	tokens domainauth.AccountTokenStore,
	loginThrottle domainauth.LoginThrottle,
	rotation domainauth.RefreshRotationStore,
	idGen domainidgen.Generator,
	mailer messaging.Mailer,
	sms messaging.SMSSender,
	rateLimiter domainauth.RateLimiter,
	roles domainauth.UserRoleResolver,
	mfa domainauth.MFAService,
	mfaChallenges domainauth.MFAChallengeStore,
	oneTimeTokens domainauth.OneTimeTokenStore,
	auditRepo audit.Repository,
) *Account {
	return &Account{
		cfg:            cfg,
		projectRepo:    projectRepo,
		oauthProviders: oauthProviders,
		docDB:          docDB,
		sessions:       sessions,
		otp:            otp,
		oauthState:     oauthState,
		tokens:         tokens,
		loginThrottle:  loginThrottle,
		rotation:       rotation,
		idGen:          idGen,
		mailer:         mailer,
		sms:            sms,
		rateLimiter:    rateLimiter,
		roles:          roles,
		mfa:            mfa,
		mfaChallenges:  mfaChallenges,
		oneTimeTokens:  oneTimeTokens,
		auditRepo:      auditRepo,
	}
}

type SignUpCommand struct {
	ProjectID string
	Email     string
	Password  string
	Name      string
}

type SignInCommand struct {
	ProjectID string
	Email     string
	Password  string
}

type RefreshTokenCommand struct {
	ProjectID    string
	RefreshToken string
}

type User struct {
	ID            string
	Email         string
	Name          string
	Status        string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TokenBundle = domainauth.TokenBundle

type Session struct {
	ID        string
	UserID    string
	Provider  string
	UserAgent string
	IP        string
	ExpireAt  time.Time
	CreatedAt time.Time
	Current   bool
}

type UpdateAccountCommand struct {
	Name        string
	Email       string
	URL         string // 改邮箱时必填：新邮箱验证链接模板（语义同 CreateVerificationRequest.url）
	Password    string
	OldPassword string
}

type ConfirmEmailChangeCommand struct {
	ProjectID string
	UserID    string
	Secret    string
}

// SignUp 频控：每 IP 每小时最多 10 次。
const (
	signUpIPWindow = time.Hour
	signUpIPLimit  = 10
)

// dummyPasswordHash 是固定哑哈希，用户不存在时也执行一次 Verify，
// 保持 SignIn 两条失败路径的耗时一致。
var dummyPasswordHash = sync.OnceValue(func() string {
	h, err := password.Hash("torchwood-dummy-signin-password")
	if err != nil {
		return ""
	}
	return h
})

// init 预热 dummyPasswordHash，消除首次 SignIn 用户不存在路径的时序差异
// （R05-P2-9：包初始化时即完成 bcrypt 预热）。
func init() { dummyPasswordHash() }

func (a *Account) checkSignUpRateLimit(ctx context.Context, projectID, ip string) error {
	// nil 容忍：未装配限流器或拿不到客户端 IP 时不做限制。
	if a.rateLimiter == nil || ip == "" {
		return nil
	}
	return a.rateLimiter.Allow(ctx, "signup:ip:"+projectID+":"+ip, signUpIPLimit, signUpIPWindow)
}

func (a *Account) SignUp(ctx context.Context, cmd SignUpCommand) (*User, *TokenBundle, string, *MFASignInChallenge, error) {
	if cmd.ProjectID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	email := normalizeEmail(cmd.Email)
	if email == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if err := validateEmail(email); err != nil {
		return nil, nil, "", nil, err
	}
	if err := validatePasswordStrength(cmd.Password); err != nil {
		return nil, nil, "", nil, err
	}
	clientInfo := contexts.ClientInfoFrom(ctx)
	project, err := a.projectRepo.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if project == nil {
		return nil, nil, "", nil, status.Error(codes.NotFound, "project not found")
	}
	// 频控放在 project 校验之后并按 project 维度计数：无效 project 不污染
	// 频控键，不同 project 各自独立计数（R05-P3-11）。
	if err := a.checkSignUpRateLimit(ctx, project.ID, clientInfo.IP); err != nil {
		return nil, nil, "", nil, err
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, nil, "", nil, fmt.Errorf("ensure system collections: %w", err)
	}

	// Check email unique.
	list, err := a.docDB.ListDocuments(ctx, project.ID, databases.SystemDatabaseID, "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if len(list.Documents) > 0 {
		return nil, nil, "", nil, status.Error(codes.AlreadyExists, "email already registered")
	}

	hash, err := password.Hash(cmd.Password)
	if err != nil {
		return nil, nil, "", nil, err
	}

	userID, err := a.generateUserID(ctx, project.ID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	userDoc := databases.Document{
		ID: userID,
		Data: map[string]any{
			"email":          email,
			"password_hash":  hash,
			"name":           cmd.Name,
			"status":         users.StatusActive,
			"email_verified": false,
			"labels":         []any{},
			"prefs":          map[string]any{},
		},
	}
	userPerms := []databases.Permission{
		{Type: "read", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "delete", Role: "admin"},
	}
	if _, err := a.docDB.CreateDocument(ctx, project.ID, databases.SystemDatabaseID, "users", userDoc, userPerms, databases.SystemPrincipal); err != nil {
		if errors.Is(err, documentdb.ErrDuplicateKey) {
			return nil, nil, "", nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, nil, "", nil, fmt.Errorf("create user document: %w", err)
	}

	user := mapUserDoc(&userDoc)
	return a.finishSignIn(ctx, project.ID, user)
}

func (a *Account) generateUserID(ctx context.Context, projectID string) (string, error) {
	if a.idGen != nil {
		return a.idGen.NewID(ctx, projectID, domainidgen.ResourceUsers)
	}
	return idgen.UUID().String(), nil
}

func (a *Account) finishSignIn(ctx context.Context, projectID string, user *User) (*User, *TokenBundle, string, *MFASignInChallenge, error) {
	return a.finishSignInWithProvider(ctx, projectID, user, domainauth.ProviderEmail)
}

func (a *Account) SignIn(ctx context.Context, cmd SignInCommand) (*User, *TokenBundle, string, *MFASignInChallenge, error) {
	if cmd.ProjectID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	email := normalizeEmail(cmd.Email)
	if email == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if cmd.Password == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "password is required")
	}
	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.checkLoginThrottle(ctx, email, clientInfo.IP); err != nil {
		return nil, nil, "", nil, err
	}
	invalidCredentials := func() (*User, *TokenBundle, string, *MFASignInChallenge, error) {
		return nil, nil, "", nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}
	project, err := a.projectRepo.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if project == nil {
		return nil, nil, "", nil, status.Error(codes.NotFound, "project not found")
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, nil, "", nil, err
	}

	list, err := a.docDB.ListDocuments(ctx, project.ID, databases.SystemDatabaseID, "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if len(list.Documents) == 0 {
		// 用户不存在时对固定哑哈希执行一次 Verify，抹平"不存在"与"密码错误"
		// 两条路径的响应时序差异（防枚举）。
		_, _ = password.Verify(cmd.Password, dummyPasswordHash())
		// 未注册邮箱不记录失败计数：既不影响真实用户邮箱键，也不污染 IP 键
		// （R05-P1-5：未注册邮箱连续失败不得触发锁定）。
		return invalidCredentials()
	}
	userDoc := list.Documents[0]
	hash, _ := userDoc.Data["password_hash"].(string)
	if ok, _ := password.Verify(cmd.Password, hash); !ok {
		a.recordLoginFailure(ctx, email, clientInfo.IP)
		return invalidCredentials()
	}

	user := mapUserDoc(&userDoc)
	if !users.CanAuthenticate(user.Status) {
		return nil, nil, "", nil, status.Error(codes.Unauthenticated, "user account is not active")
	}
	a.resetLoginThrottle(ctx, email, clientInfo.IP)
	return a.finishSignIn(ctx, project.ID, user)
}

func (a *Account) checkLoginThrottle(ctx context.Context, email, ip string) error {
	if a.loginThrottle == nil {
		return nil
	}
	return a.loginThrottle.Check(ctx, domainauth.LoginNamespaceEndUser, email, ip)
}

func (a *Account) recordLoginFailure(ctx context.Context, email, ip string) {
	if a.loginThrottle == nil {
		return
	}
	_ = a.loginThrottle.RecordFailure(ctx, domainauth.LoginNamespaceEndUser, email, ip)
}

func (a *Account) resetLoginThrottle(ctx context.Context, email, ip string) {
	if a.loginThrottle == nil {
		return
	}
	_ = a.loginThrottle.Reset(ctx, domainauth.LoginNamespaceEndUser, email, ip)
}

func (a *Account) Me(ctx context.Context) (*User, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.UserID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "users", p.UserID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return mapUserDoc(doc), nil
}

func (a *Account) SignOut(ctx context.Context) error {
	p, ok := contexts.Principal(ctx)
	if !ok || p.SessionID == "" {
		return nil
	}
	return a.docDB.DeleteDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "sessions", p.SessionID, databases.DeleteOptions{}, databases.SystemPrincipal)
}

func (a *Account) RefreshToken(ctx context.Context, cmd RefreshTokenCommand) (*TokenBundle, string, error) {
	if cmd.RefreshToken == "" {
		return nil, "", status.Error(codes.InvalidArgument, "refresh_token is required")
	}
	claims, ok := jwtparser.Parse(jwtparser.DeriveKey(a.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeEndUserJWT), cmd.RefreshToken)
	if !ok {
		return nil, "", status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if claims.TokenType != jwtparser.TokenTypeRefresh || claims.ActorKind != "end_user" {
		return nil, "", status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	projectID := claims.ProjectID
	if cmd.ProjectID != "" && cmd.ProjectID != projectID {
		return nil, "", status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if claims.SessionID == "" || claims.UserID == "" {
		return nil, "", status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if err := a.sessions.EnsureActiveSession(ctx, projectID, claims.SessionID, claims.UserID); err != nil {
		return nil, "", err
	}
	if err := a.ensureUserCanAuthenticate(ctx, projectID, claims.UserID); err != nil {
		return nil, "", err
	}
	if a.rotation == nil {
		return a.sessions.IssueTokens(ctx, projectID, claims.UserID, claims.Username, claims.SessionID)
	}

	refreshTTL := 7 * 24 * time.Hour
	if d, err := time.ParseDuration(a.cfg.GetSecurity().GetJwt().GetRefreshTtl()); err == nil {
		refreshTTL = d
	}
	rotationKey := domainauth.RefreshRotationKey(projectID, claims.SessionID)
	newRefreshTokenID := idgen.UUID().String()
	result, err := a.rotation.Rotate(ctx, rotationKey, claims.TokenID, newRefreshTokenID, refreshTTL)
	if err != nil {
		return nil, "", err
	}
	switch result {
	case domainauth.RotateOK:
		return a.sessions.IssueTokensWithRefreshID(ctx, projectID, claims.UserID, claims.Username, claims.SessionID, newRefreshTokenID)
	case domainauth.RotateMismatch:
		// 旧 refresh token 被再次使用：判定为重用，删除会话使该会话全部 token 立即失效。
		_ = a.docDB.DeleteDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", claims.SessionID, databases.DeleteOptions{}, databases.SystemPrincipal)
		return nil, "", status.Error(codes.Unauthenticated, "refresh token reuse detected")
	default: // RotateMissing
		return nil, "", status.Error(codes.Unauthenticated, "session expired")
	}
}

func (a *Account) UpdateAccount(ctx context.Context, cmd UpdateAccountCommand) (*User, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "users", p.UserID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	updates := map[string]any{}
	if cmd.Name != "" {
		updates["name"] = cmd.Name
	}
	hash, _ := doc.Data["password_hash"].(string)
	oldEmail := normalizeEmail(stringValue(doc.Data["email"]))
	emailChanging := false
	if email := normalizeEmail(cmd.Email); email != "" && email != oldEmail {
		if err := validateEmail(email); err != nil {
			return nil, err
		}
		// 邮箱变更走 staging（R05-P1-2，A 档）：新邮箱验证通过前 email 保持
		// 旧值（旧邮箱仍可登录/找回），仅写入 pending_email + 签发 email_change
		// token + 向新邮箱发验证邮件；验证通过（ConfirmEmailChange）才切换。
		if cmd.URL == "" {
			return nil, status.Error(codes.InvalidArgument, "url is required when changing email")
		}
		if err := validateRedirectURL(cmd.URL); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid url: %v", err)
		}
		if err := a.validateProjectOAuthRedirectURLs(ctx, p.ProjectID, cmd.URL, cmd.URL); err != nil {
			return nil, err
		}
		list, err := a.docDB.ListDocuments(ctx, p.ProjectID, databases.SystemDatabaseID, "users", databases.Query{
			Queries:  []string{query.BuildEqual("email", email)},
			PageSize: 1,
		}, databases.SystemPrincipal)
		if err != nil {
			return nil, err
		}
		if len(list.Documents) > 0 && list.Documents[0].ID != p.UserID {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		updates["pending_email"] = email
		emailChanging = true
	}
	if cmd.Password != "" {
		if err := validatePasswordStrength(cmd.Password); err != nil {
			return nil, err
		}
		newHash, err := password.Hash(cmd.Password)
		if err != nil {
			return nil, err
		}
		updates["password_hash"] = newHash
	}
	// 敏感变更（改邮箱/改密码）二次验证：非匿名用户（已有密码）必须提供旧密码；
	// 匿名用户（password_hash 为空）升级为实名/设置密码时跳过。
	if (emailChanging || updates["password_hash"] != nil) && hash != "" {
		if cmd.OldPassword == "" {
			return nil, status.Error(codes.InvalidArgument, "old_password is required")
		}
		if ok, _ := password.Verify(cmd.OldPassword, hash); !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid old password")
		}
	}
	if len(updates) == 0 {
		return mapUserDoc(doc), nil
	}

	// 先撤会话、后提交：撤会话失败即返回，无"密码已改但旧会话仍存活"窗口。
	// 邮箱变更走 staging：pending 阶段不撤会话（旧邮箱仍可登录），撤会话
	// 时机推迟到 ConfirmEmailChange 成功时（G3-3 语义在确认路径保持）。
	if _, passwordChanged := updates["password_hash"]; passwordChanged {
		if err := a.sessions.DeleteSessionsByUser(ctx, p.ProjectID, p.UserID); err != nil {
			return nil, fmt.Errorf("delete sessions after account change: %w", err)
		}
	}

	// R05-P1-2（A 档 staging）：先签发 email_change token 并向**新邮箱**发送
	// 验证邮件（失败则变更不落库，无副作用），提交后才向**旧邮箱**发安全通知
	//（B 档成果保留），让被劫持者第一时间察觉并止损。
	if emailChanging {
		if a.tokens == nil || a.mailer == nil {
			return nil, status.Error(codes.Unimplemented, "email delivery is not configured")
		}
		// Round3 H6-3：与 verification/recovery/magic 对齐，签发前走发送频控
		//（60s cooldown + IP 窗口），防改邮箱邮件轰炸。
		clientInfo := contexts.ClientInfoFrom(ctx)
		if err := a.tokens.CheckSendRateLimit(ctx, p.ProjectID, normalizeEmail(cmd.Email), clientInfo.IP); err != nil {
			return nil, err
		}
		secret, expireAt, err := a.tokens.CreateEmailChangeToken(ctx, p.ProjectID, p.UserID, normalizeEmail(cmd.Email))
		if err != nil {
			return nil, err
		}
		link := buildAccountActionURL(cmd.URL, p.UserID, secret)
		subject := "Confirm your Torchwood email change"
		body := fmt.Sprintf("Click the link below to confirm your new email address:\n\n%s\n\nThis link expires at %s.", link, expireAt.Format("2006-01-02 15:04 MST"))
		if err := a.mailer.Send(ctx, normalizeEmail(cmd.Email), subject, body); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to send email change confirmation: %v", err)
		}
	}

	updated, err := a.docDB.UpdateDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   p.UserID,
		Data: updates,
	}, nil), databases.Principal{Roles: p.Roles})
	if err != nil {
		if errors.Is(err, documentdb.ErrDuplicateKey) {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, fmt.Errorf("update account: %w", err)
	}
	if emailChanging && oldEmail != "" && a.mailer != nil {
		subject := "Your Torchwood email address is being changed"
		body := fmt.Sprintf("Your Torchwood account email is pending change to %s. The change takes effect only after confirmation from the new address.\n\nIf you did not make this change, sign in to your account and update your email or contact support immediately.", normalizeEmail(cmd.Email))
		if err := a.mailer.Send(ctx, oldEmail, subject, body); err != nil {
			slog.Warn("email change notification failed", "user_id", p.UserID, "error", err)
		}
	}
	return mapUserDoc(&updated), nil
}

// ConfirmEmailChange 消费 email_change 一次性 token（GETDEL 原子）并校验新
// 邮箱未被他人占用，通过后切换 email、清除 pending_email、置 email_verified，
// 并先撤全部会话再提交（G3-3 语义）。免登录（ACCESS_PUBLIC）：点邮件链接即完成，
// 与 recovery 同一安全模型（随机 secret + TTL + 一次性消费）。
func (a *Account) ConfirmEmailChange(ctx context.Context, cmd ConfirmEmailChangeCommand) (*User, error) {
	if a.tokens == nil {
		return nil, status.Error(codes.Unimplemented, "account verification is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	userID := strings.TrimSpace(cmd.UserID)
	secret := strings.TrimSpace(cmd.Secret)
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if secret == "" {
		return nil, status.Error(codes.InvalidArgument, "secret is required")
	}
	if err := a.ensureProjectReady(ctx, projectID); err != nil {
		return nil, err
	}
	newEmail, err := a.tokens.VerifyEmailChangeToken(ctx, projectID, userID, secret)
	if err != nil {
		return nil, err
	}
	// 新邮箱在 token 有效期内可能已被他人注册（并发创建）：复用 ListDocuments
	// 查重，被占用则 AlreadyExists（token 已被原子消费，不可重试）。
	list, err := a.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", newEmail)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if len(list.Documents) > 0 && list.Documents[0].ID != userID {
		return nil, status.Error(codes.AlreadyExists, "email already registered")
	}
	doc, err := a.docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "users", userID, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	// 先撤会话、后提交：撤会话失败即返回，无"邮箱已改但旧会话仍存活"窗口。
	if err := a.sessions.DeleteSessionsByUser(ctx, projectID, userID); err != nil {
		return nil, fmt.Errorf("delete sessions after email change: %w", err)
	}
	updated, err := a.docDB.UpdateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.SimpleDocumentUpdate(databases.Document{
		ID: userID,
		Data: map[string]any{
			"email":          newEmail,
			"pending_email":  nil,
			"email_verified": true,
		},
	}, nil), databases.SystemPrincipal)
	if err != nil {
		// 查重与写入之间存在竞态窗口（email 唯一索引兜底）。
		if errors.Is(err, documentdb.ErrDuplicateKey) {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, fmt.Errorf("confirm email change: %w", err)
	}
	return mapUserDoc(&updated), nil
}

// ListSessions 循环分页拉取全部会话（PageSize=1000，直至 NextPageToken 空），
// 避免 ListDocuments 默认 50 条截断。
func (a *Account) ListSessions(ctx context.Context) ([]Session, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, 16)
	pageToken := ""
	for {
		list, err := a.docDB.ListDocuments(ctx, p.ProjectID, databases.SystemDatabaseID, "sessions", databases.Query{
			Queries:   []string{query.BuildEqual("user_id", p.UserID)},
			PageSize:  1000,
			PageToken: pageToken,
		}, databases.Principal{Roles: p.Roles})
		if err != nil {
			return nil, err
		}
		for i := range list.Documents {
			s := mapSessionDoc(&list.Documents[i])
			s.Current = s.ID == p.SessionID
			out = append(out, s)
		}
		if list.NextPageToken == "" {
			return out, nil
		}
		pageToken = list.NextPageToken
	}
}

func (a *Account) DeleteSession(ctx context.Context, sessionID string) error {
	p, err := a.requireUser(ctx)
	if err != nil {
		return err
	}
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := a.deleteUserSession(ctx, p, sessionID); err != nil {
		return err
	}
	return nil
}

func (a *Account) DeleteSessions(ctx context.Context, keepCurrent bool) error {
	p, err := a.requireUser(ctx)
	if err != nil {
		return err
	}
	sessions, err := a.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if keepCurrent && s.ID == p.SessionID {
			continue
		}
		if err := a.deleteUserSession(ctx, p, s.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *Account) GetPrefs(ctx context.Context) (map[string]any, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "users", p.UserID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	if prefs, ok := doc.Data["prefs"].(map[string]any); ok {
		return prefs, nil
	}
	return map[string]any{}, nil
}

func (a *Account) UpdatePrefs(ctx context.Context, prefs map[string]any) (map[string]any, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if prefs == nil {
		return nil, status.Error(codes.InvalidArgument, "prefs is required")
	}
	if err := validatePrefs(prefs); err != nil {
		return nil, err
	}
	updated, err := a.docDB.UpdateDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   p.UserID,
		Data: map[string]any{"prefs": prefs},
	}, nil), databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, fmt.Errorf("update prefs: %w", err)
	}
	if out, ok := updated.Data["prefs"].(map[string]any); ok {
		return out, nil
	}
	return map[string]any{}, nil
}

// prefs 大小与嵌套深度上限。
const (
	maxPrefsBytes = 64 * 1024
	maxPrefsDepth = 20
)

func validatePrefs(prefs map[string]any) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return status.Error(codes.InvalidArgument, "invalid prefs")
	}
	if len(raw) > maxPrefsBytes {
		return status.Error(codes.InvalidArgument, "prefs exceed size limit")
	}
	if prefsDepth(prefs, 0) > maxPrefsDepth {
		return status.Error(codes.InvalidArgument, "prefs nesting is too deep")
	}
	return nil
}

func prefsDepth(v any, depth int) int {
	switch t := v.(type) {
	case map[string]any:
		depth++
		for _, child := range t {
			if d := prefsDepth(child, depth); d > depth {
				depth = d
			}
		}
	case []any:
		depth++
		for _, child := range t {
			if d := prefsDepth(child, depth); d > depth {
				depth = d
			}
		}
	}
	return depth
}

func (a *Account) deleteUserSession(ctx context.Context, p *shared.Principal, sessionID string) error {
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "sessions", sessionID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return err
	}
	if doc == nil {
		return status.Error(codes.NotFound, "session not found")
	}
	if uid, _ := doc.Data["user_id"].(string); uid != p.UserID {
		return status.Error(codes.PermissionDenied, "cannot delete another user's session")
	}
	return a.docDB.DeleteDocument(ctx, p.ProjectID, databases.SystemDatabaseID, "sessions", sessionID, databases.DeleteOptions{}, databases.SystemPrincipal)
}

func (a *Account) requireUser(ctx context.Context) (*shared.Principal, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.UserID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return p, nil
}

func (a *Account) ensureUserCanAuthenticate(ctx context.Context, projectID, userID string) error {
	doc, err := a.docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "users", userID, databases.SystemPrincipal)
	if err != nil {
		return status.Error(codes.Unauthenticated, "user lookup failed")
	}
	if doc == nil {
		return status.Error(codes.Unauthenticated, "user not found")
	}
	if !users.CanAuthenticate(stringValue(doc.Data["status"])) {
		return status.Error(codes.Unauthenticated, "user account is not active")
	}
	return nil
}

func mapSessionDoc(doc *databases.Document) Session {
	if doc == nil {
		return Session{}
	}
	s := Session{
		ID:        doc.ID,
		UserID:    stringValue(doc.Data["user_id"]),
		Provider:  stringValue(doc.Data["provider"]),
		UserAgent: stringValue(doc.Data["user_agent"]),
		IP:        stringValue(doc.Data["ip"]),
		CreatedAt: doc.CreatedAt,
	}
	if expireAtRaw, ok := doc.Data["expire_at"]; ok {
		if expireAt, err := auth.ParseSessionTime(expireAtRaw); err == nil {
			s.ExpireAt = expireAt
		}
	}
	return s
}

func mapUserDoc(doc *databases.Document) *User {
	if doc == nil {
		return nil
	}
	return &User{
		ID:            doc.ID,
		Email:         stringValue(doc.Data["email"]),
		Name:          stringValue(doc.Data["name"]),
		Status:        stringValue(doc.Data["status"]),
		EmailVerified: boolValue(doc.Data["email_verified"]),
		CreatedAt:     doc.CreatedAt,
		UpdatedAt:     doc.UpdatedAt,
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}
