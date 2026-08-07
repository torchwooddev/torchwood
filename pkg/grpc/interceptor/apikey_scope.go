package interceptor

import "strings"

// StorageServiceCreateFile is the gRPC method used for HTTP storage scope checks.
const StorageServiceCreateFile = "/torchwood.server.v1.StorageService/CreateFile"

// APIKeyScopeAllowed reports whether scopes grant access to the given gRPC method.
func APIKeyScopeAllowed(fullMethod string, scopes []string) bool {
	resource := apiKeyScopeResource(fullMethod)
	// Fail closed: methods without a mapped resource are denied, so a newly
	// added ACCESS_API_KEY service cannot silently bypass scope checks.
	if resource == "" {
		return false
	}
	if len(scopes) == 0 {
		// An API key with no scopes has no access to resource-scoped methods.
		return false
	}
	for _, s := range scopes {
		if s == "*" || s == "all" {
			return true
		}
	}
	for _, s := range scopes {
		if s == resource || strings.HasPrefix(s, resource+".") {
			return true
		}
	}
	return false
}

func apiKeyScopeResource(fullMethod string) string {
	parts := strings.Split(fullMethod, "/")
	if len(parts) < 2 {
		return ""
	}
	// fullMethod 形如 /package.Service/Method，取最后一个点段作为服务短名做精确匹配，
	// 避免子串匹配把 OAuthProviders 误归入 projects scope。
	svc := parts[len(parts)-2]
	if i := strings.LastIndex(svc, "."); i >= 0 {
		svc = svc[i+1:]
	}
	switch strings.TrimSuffix(svc, "Service") {
	case "Projects":
		return "projects"
	case "OAuthProviders":
		return "oauthproviders"
	case "APIKeys":
		return "apikeys"
	case "Users":
		return "users"
	case "Teams":
		return "teams"
	case "Storage":
		return "storage"
	case "Databases":
		return "databases"
	case "Health":
		return "health"
	default:
		return ""
	}
}

// IsAPIKeysServiceMethod reports whether fullMethod belongs to the APIKeys service.
// API key 凭证不允许调用这些方法：泄露的 key 若能自铸新 key，等同于永久提权。
// admin console session 不受此限制。
func IsAPIKeysServiceMethod(fullMethod string) bool {
	parts := strings.Split(fullMethod, "/")
	if len(parts) < 2 {
		return false
	}
	return strings.Contains(parts[len(parts)-2], "APIKeys")
}
