package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	domainidgen "github.com/torchwoodio/torchwood/internal/domain/idgen"
	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/domain/messaging"
	"github.com/torchwoodio/torchwood/internal/domain/projects"
	"github.com/torchwoodio/torchwood/internal/domain/shared"
	"github.com/torchwoodio/torchwood/internal/domain/users"
	"github.com/torchwoodio/torchwood/internal/infra/auth"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	"github.com/torchwoodio/torchwood/internal/pkg/config"
	"github.com/torchwoodio/torchwood/internal/pkg/contexts"
	"github.com/torchwoodio/torchwood/pkg/idgen"
	"github.com/torchwoodio/torchwood/pkg/jwtparser"
	"github.com/torchwoodio/torchwood/pkg/password"
	"github.com/torchwoodio/torchwood/pkg/query"
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
	Password    string
	OldPassword string
}

func (a *Account) SignUp(ctx context.Context, cmd SignUpCommand) (*User, *TokenBundle, string, error) {
	if cmd.ProjectID == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "project_id is required")
	}
	email := normalizeEmail(cmd.Email)
	if email == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "email is required")
	}
	if err := validatePasswordStrength(cmd.Password); err != nil {
		return nil, nil, "", err
	}
	project, err := a.projectRepo.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, nil, "", err
	}
	if project == nil {
		return nil, nil, "", status.Error(codes.NotFound, "project not found")
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, nil, "", fmt.Errorf("ensure system collections: %w", err)
	}

	// Check email unique.
	list, err := a.docDB.ListDocuments(ctx, project.ID, "default", "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, "", err
	}
	if len(list.Documents) > 0 {
		return nil, nil, "", status.Error(codes.AlreadyExists, "email already registered")
	}

	hash, err := password.Hash(cmd.Password)
	if err != nil {
		return nil, nil, "", err
	}

	userID, err := a.generateUserID(ctx, project.ID)
	if err != nil {
		return nil, nil, "", err
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
		{Type: "update", Role: "keys"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: fmt.Sprintf("user:%s", userID)},
		{Type: "delete", Role: "keys"},
		{Type: "delete", Role: "admin"},
	}
	if _, err := a.docDB.CreateDocument(ctx, project.ID, "default", "users", userDoc, userPerms, databases.SystemPrincipal); err != nil {
		if errors.Is(err, documentdb.ErrDuplicateKey) {
			return nil, nil, "", status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, nil, "", fmt.Errorf("create user document: %w", err)
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

func (a *Account) finishSignIn(ctx context.Context, projectID string, user *User) (*User, *TokenBundle, string, error) {
	return a.finishSignInWithProvider(ctx, projectID, user, domainauth.ProviderEmail)
}

func (a *Account) SignIn(ctx context.Context, cmd SignInCommand) (*User, *TokenBundle, string, error) {
	if cmd.ProjectID == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "project_id is required")
	}
	email := normalizeEmail(cmd.Email)
	if email == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "email is required")
	}
	if cmd.Password == "" {
		return nil, nil, "", status.Error(codes.InvalidArgument, "password is required")
	}
	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.checkLoginThrottle(ctx, email, clientInfo.IP); err != nil {
		return nil, nil, "", err
	}
	invalidCredentials := func() (*User, *TokenBundle, string, error) {
		a.recordLoginFailure(ctx, email, clientInfo.IP)
		return nil, nil, "", status.Error(codes.Unauthenticated, "invalid credentials")
	}
	project, err := a.projectRepo.GetProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, nil, "", err
	}
	if project == nil {
		return nil, nil, "", status.Error(codes.NotFound, "project not found")
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, nil, "", err
	}

	list, err := a.docDB.ListDocuments(ctx, project.ID, "default", "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, "", err
	}
	if len(list.Documents) == 0 {
		return invalidCredentials()
	}
	userDoc := list.Documents[0]
	hash, _ := userDoc.Data["password_hash"].(string)
	if ok, _ := password.Verify(cmd.Password, hash); !ok {
		return invalidCredentials()
	}

	user := mapUserDoc(&userDoc)
	if !users.CanAuthenticate(user.Status) {
		return nil, nil, "", status.Error(codes.Unauthenticated, "user account is not active")
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
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "users", p.UserID, databases.Principal{Roles: p.Roles})
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
	return a.docDB.DeleteDocument(ctx, p.ProjectID, "default", "sessions", p.SessionID, databases.Principal{Roles: p.Roles})
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
		_ = a.docDB.DeleteDocument(ctx, projectID, "default", "sessions", claims.SessionID, databases.SystemPrincipal)
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
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "users", p.UserID, databases.Principal{Roles: p.Roles})
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
	if email := normalizeEmail(cmd.Email); email != "" && email != stringValue(doc.Data["email"]) {
		list, err := a.docDB.ListDocuments(ctx, p.ProjectID, "default", "users", databases.Query{
			Queries:  []string{query.BuildEqual("email", email)},
			PageSize: 1,
		}, databases.SystemPrincipal)
		if err != nil {
			return nil, err
		}
		if len(list.Documents) > 0 && list.Documents[0].ID != p.UserID {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		updates["email"] = email
		updates["email_verified"] = false
	}
	if cmd.Password != "" {
		if err := validatePasswordStrength(cmd.Password); err != nil {
			return nil, err
		}
		if cmd.OldPassword == "" {
			return nil, status.Error(codes.InvalidArgument, "old_password is required")
		}
		hash, _ := doc.Data["password_hash"].(string)
		if ok, _ := password.Verify(cmd.OldPassword, hash); !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid old password")
		}
		newHash, err := password.Hash(cmd.Password)
		if err != nil {
			return nil, err
		}
		updates["password_hash"] = newHash
	}
	if len(updates) == 0 {
		return mapUserDoc(doc), nil
	}

	updated, err := a.docDB.UpdateDocument(ctx, p.ProjectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   p.UserID,
		Data: updates,
	}, nil), databases.Principal{Roles: p.Roles})
	if err != nil {
		if errors.Is(err, documentdb.ErrDuplicateKey) {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, fmt.Errorf("update account: %w", err)
	}
	if _, passwordChanged := updates["password_hash"]; passwordChanged {
		if err := a.sessions.DeleteSessionsByUser(ctx, p.ProjectID, p.UserID); err != nil {
			return nil, fmt.Errorf("delete sessions after password change: %w", err)
		}
	}
	return mapUserDoc(&updated), nil
}

func (a *Account) ListSessions(ctx context.Context) ([]Session, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return nil, err
	}
	list, err := a.docDB.ListDocuments(ctx, p.ProjectID, "default", "sessions", databases.Query{
		Queries: []string{query.BuildEqual("user_id", p.UserID)},
	}, databases.Principal{Roles: p.Roles})
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(list.Documents))
	for i := range list.Documents {
		s := mapSessionDoc(&list.Documents[i])
		s.Current = s.ID == p.SessionID
		out = append(out, s)
	}
	return out, nil
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
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "users", p.UserID, databases.Principal{Roles: p.Roles})
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
	updated, err := a.docDB.UpdateDocument(ctx, p.ProjectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
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

func (a *Account) deleteUserSession(ctx context.Context, p *shared.Principal, sessionID string) error {
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "sessions", sessionID, databases.Principal{Roles: p.Roles})
	if err != nil {
		return err
	}
	if doc == nil {
		return status.Error(codes.NotFound, "session not found")
	}
	if uid, _ := doc.Data["user_id"].(string); uid != p.UserID {
		return status.Error(codes.PermissionDenied, "cannot delete another user's session")
	}
	return a.docDB.DeleteDocument(ctx, p.ProjectID, "default", "sessions", sessionID, databases.Principal{Roles: p.Roles})
}

func (a *Account) requireUser(ctx context.Context) (*shared.Principal, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.UserID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return p, nil
}

func (a *Account) ensureUserCanAuthenticate(ctx context.Context, projectID, userID string) error {
	doc, err := a.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
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
