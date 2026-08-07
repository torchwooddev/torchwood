package interceptor

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Validator validates a raw credential and returns the authenticated Principal.
type Validator interface {
	ValidateToken(ctx context.Context, token string) (*shared.Principal, error)
	ValidateCredential(ctx context.Context, raw string, credentialType shared.CredentialType) (*shared.Principal, error)
	ValidateAdminProjectAccess(ctx context.Context, principal *shared.Principal) error
}

type AuthInterceptor struct {
	validator         Validator
	publicMethods     map[string]struct{}
	apiKeyMethods     map[string]struct{}
	permissionMethods map[string][]string
	logger            *slog.Logger
}

func NewAuthInterceptor(validator Validator, publicMethods, apiKeyMethods []string, permissionMethods map[string][]string) (*AuthInterceptor, error) {
	if validator == nil {
		return nil, errors.New("validator cannot be nil")
	}
	i := &AuthInterceptor{
		validator:         validator,
		publicMethods:     make(map[string]struct{}),
		apiKeyMethods:     make(map[string]struct{}),
		permissionMethods: permissionMethods,
		logger:            slog.Default(),
	}
	if i.permissionMethods == nil {
		i.permissionMethods = map[string][]string{}
	}
	for _, m := range publicMethods {
		i.publicMethods[m] = struct{}{}
	}
	for _, m := range apiKeyMethods {
		i.apiKeyMethods[m] = struct{}{}
	}
	return i, nil
}

// WithLogger 替换认证失败留痕所用的 logger（默认 slog.Default()），返回自身便于链式调用。
func (i *AuthInterceptor) WithLogger(l *slog.Logger) *AuthInterceptor {
	if l != nil {
		i.logger = l
	}
	return i
}

// logAuthFailure 在认证/鉴权拒绝路径输出结构化告警日志，只记录方法名、
// 拒绝原因类别与凭证类型，绝不记录 token 本体。
func (i *AuthInterceptor) logAuthFailure(ctx context.Context, method, reason string, credentialType shared.CredentialType) {
	ci := contexts.ClientInfoFrom(ctx)
	i.logger.WarnContext(ctx, "grpc auth rejected",
		slog.String("method", method),
		slog.String("reason", reason),
		slog.String("credential_type", string(credentialType)),
		slog.String("ip", ci.IP),
		slog.String("user_agent", ci.UserAgent),
	)
}

func (i *AuthInterceptor) UnaryAuthMiddleware(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if _, ok := i.publicMethods[info.FullMethod]; ok {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if credentialType, token, err := extractCredential(md); err == nil {
				if principal, err := i.validator.ValidateCredential(ctx, token, credentialType); err == nil && principal != nil {
					ctx = contexts.WithPrincipal(ctx, principal)
				}
			}
		}
		return handler(ctx, req)
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		i.logAuthFailure(ctx, info.FullMethod, "metadata_missing", "")
		return nil, status.Error(codes.Unauthenticated, "metadata is not provided")
	}

	credentialType, token, err := extractCredential(md)
	if err != nil {
		i.logAuthFailure(ctx, info.FullMethod, "credential_missing", "")
		return nil, status.Error(codes.Unauthenticated, "authentication credential is not provided")
	}

	principal, err := i.validator.ValidateCredential(ctx, token, credentialType)
	if err != nil {
		i.logAuthFailure(ctx, info.FullMethod, "credential_invalid", credentialType)
		return nil, err
	}
	if principal == nil {
		i.logAuthFailure(ctx, info.FullMethod, "credential_invalid", credentialType)
		return nil, status.Error(codes.Unauthenticated, "invalid or expired credential")
	}

	if _, isAPIKeyMethod := i.apiKeyMethods[info.FullMethod]; isAPIKeyMethod {
		if principal.CredentialType != shared.CredentialTypeAPIKey && principal.ActorKind != shared.ActorKindAdmin {
			i.logAuthFailure(ctx, info.FullMethod, "credential_type_not_allowed", credentialType)
			return nil, status.Error(codes.Unauthenticated, "developer API requires x-api-key header or admin session")
		}
		if principal.CredentialType == shared.CredentialTypeAPIKey {
			// API key 凭证禁止调用 APIKeys 服务，防止泄露的 key 自铸新 key 造成永久提权。
			if IsAPIKeysServiceMethod(info.FullMethod) {
				i.logAuthFailure(ctx, info.FullMethod, "apikey_self_management_denied", credentialType)
				return nil, status.Error(codes.PermissionDenied, "api keys cannot manage api keys")
			}
			if !APIKeyScopeAllowed(info.FullMethod, principal.Permissions) {
				i.logAuthFailure(ctx, info.FullMethod, "apikey_scope_missing", credentialType)
				return nil, status.Error(codes.PermissionDenied, "api key missing required scope")
			}
		}
	}

	// Allow admin console sessions to target a specific project via header.
	if principal.ActorKind == shared.ActorKindAdmin {
		if projectID := firstMetadataValue(md, "X-Torchwood-Project"); projectID != "" {
			principal.ProjectID = projectID
		}
		if err := i.validator.ValidateAdminProjectAccess(ctx, principal); err != nil {
			i.logAuthFailure(ctx, info.FullMethod, "admin_project_access_denied", credentialType)
			return nil, err
		}
	}

	if perms := i.permissionMethods[info.FullMethod]; len(perms) > 0 {
		if !principal.HasAnyPermission(perms) {
			i.logAuthFailure(ctx, info.FullMethod, "permission_denied", credentialType)
			return nil, status.Error(codes.PermissionDenied, "missing required permission")
		}
	}

	ctx = contexts.WithPrincipal(ctx, principal)
	return handler(ctx, req)
}

func extractCredential(md metadata.MD) (shared.CredentialType, string, error) {
	if raw := firstMetadataValue(md, "authorization"); raw != "" {
		if credentialType, token, ok := ParseAuthorizationHeader(raw); ok {
			return credentialType, token, nil
		}
	}
	if raw := firstMetadataValue(md, "cookie"); raw != "" {
		if _, token, ok := parseSessionCookie(raw); ok {
			return shared.CredentialTypeSession, token, nil
		}
	}
	if raw := firstMetadataValue(md, "x-api-key"); raw != "" {
		return shared.CredentialTypeAPIKey, raw, nil
	}
	return "", "", errors.New("no credential")
}

// ParseAuthorizationHeader 解析 Authorization 头，支持 Bearer / Session / ApiKey 三种 scheme；
// scheme 无法识别或格式不合法时返回 ok=false，调用方应拒绝而不是把整串当 token。
func ParseAuthorizationHeader(raw string) (shared.CredentialType, string, bool) {
	parts := strings.Fields(raw)
	if len(parts) != 2 {
		return "", "", false
	}
	switch strings.ToLower(parts[0]) {
	case "bearer":
		return shared.CredentialTypeToken, parts[1], true
	case "session":
		return shared.CredentialTypeSession, parts[1], true
	case "apikey", "api-key":
		return shared.CredentialTypeAPIKey, parts[1], true
	}
	return "", "", false
}

func parseSessionCookie(raw string) (projectID, token string, ok bool) {
	for _, part := range strings.Split(raw, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || value == "" {
			continue
		}
		if name == "TORCHWOOD_session_console" {
			return "console", value, true
		}
		if strings.HasPrefix(name, "TORCHWOOD_session_") {
			return strings.TrimPrefix(name, "TORCHWOOD_session_"), value, true
		}
	}
	return "", "", false
}

func firstMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
