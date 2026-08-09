package interceptor

import "strings"

// StorageServiceCreateFile is the gRPC method used for HTTP storage scope checks.
const StorageServiceCreateFile = "/torchwood.server.v1.StorageService/CreateFile"

// StorageServiceGetFile is the gRPC method used for HTTP storage read scope checks.
const StorageServiceGetFile = "/torchwood.server.v1.StorageService/GetFile"

// apiKeyScopeRule 是单个 gRPC 方法对应的 scope 资源名与读写方向（B2）。
type apiKeyScopeRule struct {
	resource string // scope 资源名（databases/users/teams/storage/projects/oauthproviders/apikeys/functions）
	op       string // "read" 或 "write"
}

// apiKeyScopeRules 显式映射全部 8 个 ACCESS_API_KEY 服务的方法（Health 是
// ACCESS_PUBLIC，不映射）。读方法 = List/Get/Count 类，其余一律 write。
// 新增 ACCESS_API_KEY 方法必须在此登记，否则 APIKeyScopeAllowed 对其 fail-closed。
var apiKeyScopeRules = map[string]apiKeyScopeRule{
	// DatabasesService
	"/torchwood.server.v1.DatabasesService/CreateDatabase":      {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/ListDatabases":       {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/GetDatabase":         {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/DeleteDatabase":      {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/CreateCollection":    {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/ListCollections":     {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/GetCollection":       {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/DeleteCollection":    {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/UpdateCollection":    {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/CreateAttribute":     {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/DeleteAttribute":     {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/CreateIndex":         {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/DeleteIndex":         {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/CreateDocument":      {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/ListDocuments":       {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/GetDocument":         {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/UpdateDocument":      {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/DeleteDocument":      {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/CountDocuments":      {"databases", "read"},
	"/torchwood.server.v1.DatabasesService/BulkUpdateDocuments": {"databases", "write"},
	"/torchwood.server.v1.DatabasesService/BulkDeleteDocuments": {"databases", "write"},
	// UsersService
	"/torchwood.server.v1.UsersService/CreateUser":         {"users", "write"},
	"/torchwood.server.v1.UsersService/ListUsers":          {"users", "read"},
	"/torchwood.server.v1.UsersService/GetUser":            {"users", "read"},
	"/torchwood.server.v1.UsersService/UpdateUser":         {"users", "write"},
	"/torchwood.server.v1.UsersService/UpdateUserPassword": {"users", "write"},
	"/torchwood.server.v1.UsersService/DeleteUser":         {"users", "write"},
	"/torchwood.server.v1.UsersService/ListUserSessions":   {"users", "read"},
	"/torchwood.server.v1.UsersService/DeleteUserSession":  {"users", "write"},
	"/torchwood.server.v1.UsersService/CreateUserToken":    {"users", "write"},
	// TeamsService
	"/torchwood.server.v1.TeamsService/CreateTeam":             {"teams", "write"},
	"/torchwood.server.v1.TeamsService/ListTeams":              {"teams", "read"},
	"/torchwood.server.v1.TeamsService/GetTeam":                {"teams", "read"},
	"/torchwood.server.v1.TeamsService/DeleteTeam":             {"teams", "write"},
	"/torchwood.server.v1.TeamsService/CreateMembership":       {"teams", "write"},
	"/torchwood.server.v1.TeamsService/ListMemberships":        {"teams", "read"},
	"/torchwood.server.v1.TeamsService/GetMembership":          {"teams", "read"},
	"/torchwood.server.v1.TeamsService/UpdateMembership":       {"teams", "write"},
	"/torchwood.server.v1.TeamsService/UpdateMembershipStatus": {"teams", "write"},
	"/torchwood.server.v1.TeamsService/DeleteMembership":       {"teams", "write"},
	// StorageService
	"/torchwood.server.v1.StorageService/CreateBucket":    {"storage", "write"},
	"/torchwood.server.v1.StorageService/UpdateBucket":    {"storage", "write"},
	"/torchwood.server.v1.StorageService/ListBuckets":     {"storage", "read"},
	"/torchwood.server.v1.StorageService/GetBucket":       {"storage", "read"},
	"/torchwood.server.v1.StorageService/DeleteBucket":    {"storage", "write"},
	"/torchwood.server.v1.StorageService/CreateFile":      {"storage", "write"},
	"/torchwood.server.v1.StorageService/ListFiles":       {"storage", "read"},
	"/torchwood.server.v1.StorageService/GetFile":         {"storage", "read"},
	"/torchwood.server.v1.StorageService/DeleteFile":      {"storage", "write"},
	"/torchwood.server.v1.StorageService/UpdateFile":      {"storage", "write"},
	"/torchwood.server.v1.StorageService/CreateFileToken": {"storage", "write"},
	"/torchwood.server.v1.StorageService/GetStorageUsage": {"storage", "read"},
	// ProjectsService
	"/torchwood.server.v1.ProjectsService/CreateProject": {"projects", "write"},
	"/torchwood.server.v1.ProjectsService/ListProjects":  {"projects", "read"},
	"/torchwood.server.v1.ProjectsService/GetProject":    {"projects", "read"},
	"/torchwood.server.v1.ProjectsService/UpdateProject": {"projects", "write"},
	// OAuthProvidersService
	"/torchwood.server.v1.OAuthProvidersService/ListOAuthProviders":  {"oauthproviders", "read"},
	"/torchwood.server.v1.OAuthProvidersService/UpsertOAuthProvider": {"oauthproviders", "write"},
	"/torchwood.server.v1.OAuthProvidersService/DeleteOAuthProvider": {"oauthproviders", "write"},
	// APIKeysService（IsAPIKeysServiceMethod 仍禁 API key 凭证调用）
	"/torchwood.server.v1.APIKeysService/CreateAPIKey": {"apikeys", "write"},
	"/torchwood.server.v1.APIKeysService/ListAPIKeys":  {"apikeys", "read"},
	"/torchwood.server.v1.APIKeysService/GetAPIKey":    {"apikeys", "read"},
	"/torchwood.server.v1.APIKeysService/DeleteAPIKey": {"apikeys", "write"},
	// FunctionsService
	"/torchwood.server.v1.FunctionsService/ListRuntimes":       {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/ListSpecifications": {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/CreateFunction":     {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/ListFunctions":      {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/GetFunction":        {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/UpdateFunction":     {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/DeleteFunction":     {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/CreateDeployment":   {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/ListDeployments":    {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/GetDeployment":      {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/DeleteDeployment":   {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/SetVariables":       {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/GetVariables":       {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/CreateExecution":    {"functions", "write"},
	"/torchwood.server.v1.FunctionsService/ListExecutions":     {"functions", "read"},
	"/torchwood.server.v1.FunctionsService/GetExecution":       {"functions", "read"},
}

// validAPIKeyScopes 是 API key scope 允许的格式全集：{*, all, 裸资源名,
// <resource>.read, <resource>.write}，与 apiKeyScopeRules 单一事实来源一致。
var validAPIKeyScopes = func() map[string]struct{} {
	scopes := map[string]struct{}{"*": {}, "all": {}}
	for _, r := range apiKeyScopeRules {
		scopes[r.resource] = struct{}{}
		scopes[r.resource+".read"] = struct{}{}
		scopes[r.resource+".write"] = struct{}{}
	}
	return scopes
}()

// APIKeyScopeAllowed reports whether scopes grant access to the given gRPC method.
// 匹配规则（B2）：裸资源名全量放行；<resource>.read 仅放行读方法；
// <resource>.write 仅放行写方法；* / all 全量放行。
// 通配符在未映射方法（resource 不存在）之后判断：新增的 ACCESS_API_KEY 服务
// 未登记时即使带 * / all 也拒绝（fail-closed）。
func APIKeyScopeAllowed(fullMethod string, scopes []string) bool {
	rule, ok := apiKeyScopeRules[fullMethod]
	if !ok {
		return false
	}
	for _, s := range scopes {
		if s == "*" || s == "all" {
			return true
		}
		if s == rule.resource {
			return true
		}
		if rule.op == "read" && s == rule.resource+".read" {
			return true
		}
		if rule.op == "write" && s == rule.resource+".write" {
			return true
		}
	}
	return false
}

// ValidAPIKeyScope reports whether a scope string is in the accepted format:
// {*, all, 裸资源名, <resource>.read, <resource>.write}（B2 scope 格式校验）。
func ValidAPIKeyScope(s string) bool {
	_, ok := validAPIKeyScopes[s]
	return ok
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
