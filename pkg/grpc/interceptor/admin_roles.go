package interceptor

// adminRoleMethodRules 登记仅限 console owner/admin 角色调用的 Server API
// 写方法（安全评审 M7）：受限 admin（member/viewer）会话一律拒绝；
// API key 与端用户凭证不在此表处理（分别走 apiKeyMethods scope 门禁与
// use-case 层 IsPlatformAdmin 守门）。
var adminRoleMethodRules = map[string][]string{
	// APIKeysService
	"/torchwood.server.v1.APIKeysService/CreateAPIKey": {"owner", "admin"},
	// UsersService
	"/torchwood.server.v1.UsersService/CreateUserToken":    {"owner", "admin"},
	"/torchwood.server.v1.UsersService/UpdateUserPassword": {"owner", "admin"},
	"/torchwood.server.v1.UsersService/DeleteUser":         {"owner", "admin"},
	// DatabasesService（schema DDL 写方法）
	"/torchwood.server.v1.DatabasesService/CreateDatabase":   {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteDatabase":   {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/CreateCollection": {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/UpdateCollection": {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteCollection": {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/CreateAttribute":  {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteAttribute":  {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/CreateIndex":      {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteIndex":      {"owner", "admin"},
	// FunctionsService
	"/torchwood.server.v1.FunctionsService/SetVariables": {"owner", "admin"},
	// OAuthProvidersService
	"/torchwood.server.v1.OAuthProvidersService/UpsertOAuthProvider": {"owner", "admin"},
	"/torchwood.server.v1.OAuthProvidersService/DeleteOAuthProvider": {"owner", "admin"},
}
