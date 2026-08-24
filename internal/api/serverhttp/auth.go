package serverhttp

import (
	"context"
	"net/http"
	"strings"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuthValidator 是 HTTP 鉴权所需的最小凭证校验面（infra/auth.Validator 满足；
// 接口化便于单测且消除 api→infra 直依赖，仿 realtime/handler.go CredentialValidator 模式）。
type AuthValidator interface {
	Authenticate(ctx context.Context, req shared.AuthnRequest) (*shared.Principal, error)
	ValidateAdminProjectAccess(ctx context.Context, principal *shared.Principal) error
}

// httpAuth 是 serverhttp 各 handler 共享的 HTTP 认证/鉴权原语，与 gRPC /
// Realtime 共用 Validator.Authenticate。
type httpAuth struct {
	validator AuthValidator
}

func newHTTPAuth(validator AuthValidator) *httpAuth {
	return &httpAuth{validator: validator}
}

// authenticate 解析并校验请求凭证，返回认证主体；多凭证并存、同一凭证头
// 多值或凭证缺失返回 401。
func (a *httpAuth) authenticate(r *http.Request) (*shared.Principal, error) {
	return a.validator.Authenticate(r.Context(), authnRequestFromHTTP(r))
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
		if !domainauth.APIKeyScopeAllowed(apiKeyScope(r), principal.Permissions) {
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

// authnRequestFromHTTP 从 HTTP 请求组装共用凭证面（避免 import infra/auth，
// 逻辑与 infra/auth.AuthnRequestFromHTTP 一致）。
func authnRequestFromHTTP(r *http.Request) shared.AuthnRequest {
	if r == nil {
		return shared.AuthnRequest{}
	}
	return shared.AuthnRequest{
		Authorization: r.Header.Values("Authorization"),
		APIKey:        r.Header.Values("X-Api-Key"),
		CookieHeaders: r.Header.Values("Cookie"),
	}
}
