package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/domain/projects"
	"github.com/torchwoodio/torchwood/internal/domain/users"
	infraauth "github.com/torchwoodio/torchwood/internal/infra/auth"
	"github.com/torchwoodio/torchwood/pkg/idgen"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateOAuth2LinkSessionCommand struct {
	ProjectID string
	Provider  string
	Success   string
	Failure   string
}

type CreateOAuth2LinkTokenSessionCommand struct {
	ProjectID string
	Provider  string
	Code      string
	State     string
}

type CreateOAuth2SessionCommand struct {
	ProjectID string
	Provider  string
	Success   string
	Failure   string
}

type CreateOAuth2TokenSessionCommand struct {
	ProjectID string
	Provider  string
	Success   string
	Failure   string
	Code      string
	State     string
}

type OAuth2CallbackResult struct {
	ProjectID     string
	SuccessURL    string
	FailureURL    string
	User          *User
	Tokens        *TokenBundle
	SessionCookie string
	RedirectURL   string
}

func (a *Account) CreateOAuth2Session(ctx context.Context, cmd CreateOAuth2SessionCommand) (string, error) {
	if a.oauthState == nil {
		return "", status.Error(codes.Unimplemented, "oauth2 is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	provider := normalizeOAuthProvider(cmd.Provider)
	if projectID == "" {
		return "", status.Error(codes.InvalidArgument, "project_id is required")
	}
	if provider == "" {
		return "", status.Error(codes.InvalidArgument, "provider is required")
	}
	return a.createOAuth2Session(ctx, createOAuth2SessionParams{
		projectID: projectID,
		provider:  provider,
		success:   cmd.Success,
		failure:   cmd.Failure,
	})
}

func (a *Account) CreateOAuth2LinkSession(ctx context.Context, cmd CreateOAuth2LinkSessionCommand) (string, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return "", err
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	if projectID == "" {
		projectID = p.ProjectID
	}
	if projectID != p.ProjectID {
		return "", status.Error(codes.PermissionDenied, "cannot link oauth provider for another project")
	}
	return a.createOAuth2Session(ctx, createOAuth2SessionParams{
		projectID:  projectID,
		provider:   cmd.Provider,
		success:    cmd.Success,
		failure:    cmd.Failure,
		linkUserID: p.UserID,
	})
}

func (a *Account) CreateOAuth2LinkTokenSession(ctx context.Context, cmd CreateOAuth2LinkTokenSessionCommand) (*User, error) {
	if _, err := a.requireUser(ctx); err != nil {
		return nil, err
	}
	result, err := a.completeOAuth2Code(ctx, completeOAuth2CodeCommand{
		ProjectID: cmd.ProjectID,
		Provider:  cmd.Provider,
		Code:      cmd.Code,
		State:     cmd.State,
	})
	if err != nil {
		return nil, err
	}
	if result.User == nil {
		return nil, status.Error(codes.Internal, "oauth link did not return user")
	}
	return result.User, nil
}

type createOAuth2SessionParams struct {
	projectID  string
	provider   string
	success    string
	failure    string
	linkUserID string
}

func (a *Account) createOAuth2Session(ctx context.Context, params createOAuth2SessionParams) (string, error) {
	if a.oauthState == nil {
		return "", status.Error(codes.Unimplemented, "oauth2 is not configured")
	}
	provider := normalizeOAuthProvider(params.provider)
	if provider == "" {
		return "", status.Error(codes.InvalidArgument, "provider is required")
	}
	if provider == domainauth.ProviderWeChatMiniProgram {
		return "", status.Error(codes.InvalidArgument, "use CreateWeChatMiniProgramSession for wechat_miniprogram")
	}
	if err := validateRedirectURL(params.success); err != nil {
		return "", status.Errorf(codes.InvalidArgument, "invalid success url: %v", err)
	}
	if err := validateRedirectURL(params.failure); err != nil {
		return "", status.Errorf(codes.InvalidArgument, "invalid failure url: %v", err)
	}
	if err := a.validateProjectOAuthRedirectURLs(ctx, params.projectID, params.success, params.failure); err != nil {
		return "", err
	}
	if err := a.ensureProjectReady(ctx, params.projectID); err != nil {
		return "", err
	}
	oauthCfg, err := a.loadOAuthProvider(ctx, params.projectID, provider)
	if err != nil {
		return "", err
	}
	stateID := idgen.UUID().String()
	verifier := ""
	challenge := ""
	if usesWeChatPKCE(provider) {
		verifier = oauth2.GenerateVerifier()
		challenge = oauth2.S256ChallengeFromVerifier(verifier)
	}
	if err := a.oauthState.Save(ctx, domainauth.OAuthState{
		StateID:      stateID,
		ProjectID:    params.projectID,
		Provider:     provider,
		SuccessURL:   params.success,
		FailureURL:   params.failure,
		PKCEVerifier: verifier,
		LinkUserID:   params.linkUserID,
	}, 0); err != nil {
		return "", err
	}
	authClient, err := infraauth.NewOAuthAuthenticator(provider, oauthCfg.ClientID, oauthCfg.ClientSecret, a.oauthCallbackURL(provider), oauthCfg.Scopes)
	if err != nil {
		return "", status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return authClient.AuthorizeURL(stateID, challenge), nil
}

func (a *Account) CreateOAuth2TokenSession(ctx context.Context, cmd CreateOAuth2TokenSessionCommand) (*User, *TokenBundle, string, error) {
	result, err := a.completeOAuth2Code(ctx, completeOAuth2CodeCommand{
		ProjectID: cmd.ProjectID,
		Provider:  cmd.Provider,
		Code:      cmd.Code,
		State:     cmd.State,
	})
	if err != nil {
		return nil, nil, "", err
	}
	return result.User, result.Tokens, result.SessionCookie, nil
}

func (a *Account) HandleOAuth2Callback(ctx context.Context, provider, code, state string) (*OAuth2CallbackResult, error) {
	result, err := a.completeOAuth2Code(ctx, completeOAuth2CodeCommand{
		Provider: provider,
		Code:     code,
		State:    state,
	})
	if err != nil {
		// completeOAuth2Code 在多数失败分支返回 nil result，这里必须兜底。
		failureURL := "/"
		var successURL string
		if result != nil {
			if result.FailureURL != "" {
				failureURL = result.FailureURL
			}
			successURL = result.SuccessURL
		}
		return &OAuth2CallbackResult{
			SuccessURL:  successURL,
			FailureURL:  failureURL,
			RedirectURL: appendQuery(failureURL, "error", status.Convert(err).Message()),
		}, err
	}
	redirect := appendOAuthSPAFragment(result.SuccessURL, result.User.ID, result.Tokens)
	return &OAuth2CallbackResult{
		ProjectID:     result.ProjectID,
		SuccessURL:    result.SuccessURL,
		FailureURL:    result.FailureURL,
		User:          result.User,
		Tokens:        result.Tokens,
		SessionCookie: result.SessionCookie,
		RedirectURL:   redirect,
	}, nil
}

type completeOAuth2CodeCommand struct {
	ProjectID string
	Provider  string
	Code      string
	State     string
}

type completeOAuth2CodeResult struct {
	ProjectID     string
	SuccessURL    string
	FailureURL    string
	User          *User
	Tokens        *TokenBundle
	SessionCookie string
}

func (a *Account) completeOAuth2Code(ctx context.Context, cmd completeOAuth2CodeCommand) (*completeOAuth2CodeResult, error) {
	if a.oauthState == nil {
		return nil, status.Error(codes.Unimplemented, "oauth2 is not configured")
	}
	provider := normalizeOAuthProvider(cmd.Provider)
	code := strings.TrimSpace(cmd.Code)
	stateID := strings.TrimSpace(cmd.State)
	if provider == "" {
		return nil, status.Error(codes.InvalidArgument, "provider is required")
	}
	if code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}
	if stateID == "" {
		return nil, status.Error(codes.InvalidArgument, "state is required")
	}

	oauthState, err := a.oauthState.Consume(ctx, stateID)
	if err != nil {
		return nil, err
	}

	if oauthState.Provider != provider {
		return nil, status.Error(codes.Unauthenticated, "oauth provider mismatch")
	}
	projectID := oauthState.ProjectID
	if cmd.ProjectID != "" && cmd.ProjectID != projectID {
		return nil, status.Error(codes.Unauthenticated, "oauth project mismatch")
	}

	project, err := a.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, err
	}

	oauthCfg, err := a.loadOAuthProvider(ctx, projectID, provider)
	if err != nil {
		return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, err
	}

	authClient, err := infraauth.NewOAuthAuthenticator(provider, oauthCfg.ClientID, oauthCfg.ClientSecret, a.oauthCallbackURL(provider), oauthCfg.Scopes)
	if err != nil {
		return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	profile, err := authClient.Exchange(ctx, code, oauthState.PKCEVerifier)
	if err != nil {
		return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, status.Errorf(codes.Unauthenticated, "oauth code exchange failed: %v", err)
	}

	if oauthState.LinkUserID != "" {
		if err := a.linkOAuthIdentity(ctx, projectID, oauthState.LinkUserID, provider, profile); err != nil {
			return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, err
		}
		doc, err := a.docDB.GetDocument(ctx, projectID, "default", "users", oauthState.LinkUserID, databases.SystemPrincipal)
		if err != nil {
			return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, err
		}
		if doc == nil {
			return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, status.Error(codes.NotFound, "user not found")
		}
		return &completeOAuth2CodeResult{
			ProjectID:  projectID,
			SuccessURL: oauthState.SuccessURL,
			FailureURL: oauthState.FailureURL,
			User:       mapUserDoc(doc),
		}, nil
	}

	user, err := a.resolveOAuthUser(ctx, projectID, provider, profile)
	if err != nil {
		return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, err
	}
	if !users.CanAuthenticate(user.Status) {
		return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, status.Error(codes.Unauthenticated, "user account is not active")
	}

	user, tokens, cookie, err := a.finishSignInWithProvider(ctx, projectID, user, provider)
	if err != nil {
		return &completeOAuth2CodeResult{SuccessURL: oauthState.SuccessURL, FailureURL: oauthState.FailureURL}, err
	}
	return &completeOAuth2CodeResult{
		ProjectID:     projectID,
		SuccessURL:    oauthState.SuccessURL,
		FailureURL:    oauthState.FailureURL,
		User:          user,
		Tokens:        tokens,
		SessionCookie: cookie,
	}, nil
}

func (a *Account) loadOAuthProvider(ctx context.Context, projectID, provider string) (*domainOAuthProvider, error) {
	if a.oauthProviders == nil {
		return nil, status.Error(codes.FailedPrecondition, "oauth provider repository is not configured")
	}
	cfg, err := a.oauthProviders.GetOAuthProvider(ctx, projectID, provider)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "oauth provider is not enabled for this project")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "oauth provider credentials are missing")
	}
	return &domainOAuthProvider{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Scopes:       cfg.Scopes,
	}, nil
}

type domainOAuthProvider struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
}

func (a *Account) oauthCallbackURL(provider string) string {
	base := strings.TrimRight(a.publicBaseURL(), "/")
	return fmt.Sprintf("%s/v1/account/oauth2/%s/callback", base, provider)
}

func (a *Account) publicBaseURL() string {
	if u := strings.TrimSpace(a.cfg.GetServer().GetHttp().GetPublicUrl()); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:9099"
}

func normalizeOAuthProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case domainauth.ProviderGoogle:
		return domainauth.ProviderGoogle
	case domainauth.ProviderGitHub:
		return domainauth.ProviderGitHub
	case domainauth.ProviderWeChatWeb:
		return domainauth.ProviderWeChatWeb
	case domainauth.ProviderWeChatMP:
		return domainauth.ProviderWeChatMP
	case domainauth.ProviderWeChatMiniProgram:
		return domainauth.ProviderWeChatMiniProgram
	case domainauth.ProviderWeChatApp:
		return domainauth.ProviderWeChatApp
	default:
		return ""
	}
}

func validateRedirectURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("url host is required")
	}
	return nil
}

func appendQuery(rawURL, key, value string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func appendOAuthSPAFragment(rawURL, userID string, tokens *TokenBundle) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	vals := url.Values{}
	if userID != "" {
		vals.Set("userId", userID)
	}
	if tokens != nil {
		vals.Set("access_token", tokens.AccessToken)
	}
	u.Fragment = vals.Encode()
	return u.String()
}

func (a *Account) ensureProjectReady(ctx context.Context, projectID string) error {
	project, err := a.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if project == nil {
		return status.Error(codes.NotFound, "project not found")
	}
	return a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID)
}

func (a *Account) validateProjectOAuthRedirectURLs(ctx context.Context, projectID, successURL, failureURL string) error {
	project, err := a.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if project == nil {
		return status.Error(codes.NotFound, "project not found")
	}
	allowed := projects.OAuthAllowedRedirectURLs(project.Settings)
	if len(allowed) == 0 {
		allowed = projects.DefaultOAuthRedirectAllowlist(a.publicBaseURL())
	}
	if !projects.MatchRedirectURL(successURL, allowed) {
		return status.Error(codes.InvalidArgument, "success url is not allowed for this project")
	}
	if !projects.MatchRedirectURL(failureURL, allowed) {
		return status.Error(codes.InvalidArgument, "failure url is not allowed for this project")
	}
	return nil
}
