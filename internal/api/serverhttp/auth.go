package serverhttp

import (
	"net/http"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// httpAuth 是 serverhttp 各 handler 共享的 HTTP 认证/鉴权原语，凭证语义与
// gRPC 认证拦截器的 extractCredential 对齐：X-Api-Key / Authorization /
// session cookie 三种凭证并存（任意两种及以上）一律 401 Unauthenticated
// （多凭证拒绝，防代理/头混淆绕过）。
type httpAuth struct {
	validator *auth.Validator
}

func newHTTPAuth(validator *auth.Validator) *httpAuth {
	return &httpAuth{validator: validator}
}

// authenticate 解析并校验请求凭证，返回认证主体；多凭证并存、同一凭证头
// 多值或凭证缺失返回 401。
func (a *httpAuth) authenticate(r *http.Request) (*shared.Principal, error) {
	ctx := r.Context()

	// 同一凭证 key 多值与 gRPC credentialMetadataValue 语义对齐：拒绝，
	// 防止多值头部导致的解析歧义。
	if len(r.Header.Values("X-Api-Key")) > 1 || len(r.Header.Values("Authorization")) > 1 {
		return nil, status.Error(codes.Unauthenticated, "multiple credentials provided")
	}
	apiKey := r.Header.Get("X-Api-Key")
	authz := r.Header.Get("Authorization")
	var sessionCookies []string
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, "TORCHWOOD_session_") {
			sessionCookies = append(sessionCookies, c.Value)
		}
	}
	if len(sessionCookies) > 1 {
		return nil, status.Error(codes.Unauthenticated, "multiple credentials provided")
	}
	sessionCookie := ""
	if len(sessionCookies) == 1 {
		sessionCookie = sessionCookies[0]
	}

	credentialCount := 0
	if apiKey != "" {
		credentialCount++
	}
	if authz != "" {
		credentialCount++
	}
	if sessionCookie != "" {
		credentialCount++
	}
	if credentialCount > 1 {
		return nil, status.Error(codes.Unauthenticated, "multiple credentials provided")
	}

	switch {
	case apiKey != "":
		return a.validator.ValidateCredential(ctx, apiKey, shared.CredentialTypeAPIKey)
	case authz != "":
		// 与 gRPC 认证拦截器走同一解析逻辑：支持 Bearer / Session / ApiKey scheme，
		// scheme 不识别时直接拒绝，而不是把整串当 token 校验。
		credentialType, token, ok := interceptor.ParseAuthorizationHeader(authz)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization header")
		}
		return a.validator.ValidateCredential(ctx, token, credentialType)
	case sessionCookie != "":
		return a.validator.ValidateCredential(ctx, sessionCookie, shared.CredentialTypeSession)
	default:
		return nil, status.Error(codes.Unauthenticated, "authentication credential is not provided")
	}
}

// authorize 对已认证主体做方法级授权：
//   - API key 必须持有 apiKeyScope(r) 返回的 scope（按方法区分读写权限）；
//   - admin 主体可经 X-Torchwood-Project 指定项目，并校验项目访问权。
func (a *httpAuth) authorize(r *http.Request, apiKeyScope func(*http.Request) string) (*shared.Principal, error) {
	ctx := r.Context()
	principal, err := a.authenticate(r)
	if err != nil {
		return nil, err
	}
	if principal.CredentialType == shared.CredentialTypeAPIKey {
		if !interceptor.APIKeyScopeAllowed(apiKeyScope(r), principal.Permissions) {
			return nil, status.Error(codes.PermissionDenied, "api key missing required scope")
		}
	}
	if principal.ActorKind == shared.ActorKindAdmin {
		if projectID := strings.TrimSpace(r.Header.Get("X-Torchwood-Project")); projectID != "" {
			principal.ProjectID = projectID
		}
		if err := a.validator.ValidateAdminProjectAccess(ctx, principal); err != nil {
			return nil, err
		}
	}
	return principal, nil
}

// projectID 解析请求上下文中的项目 ID（admin 会话可经 X-Torchwood-Project 覆盖）。
func (a *httpAuth) projectID(r *http.Request, p *shared.Principal) string {
	if p == nil {
		return ""
	}
	switch p.CredentialType {
	case shared.CredentialTypeAPIKey:
		return p.ProjectID
	case shared.CredentialTypeToken, shared.CredentialTypeSession:
		if p.ActorKind == shared.ActorKindAdmin {
			if pid := strings.TrimSpace(r.Header.Get("X-Torchwood-Project")); pid != "" {
				return pid
			}
		}
		return p.ProjectID
	default:
		return p.ProjectID
	}
}
