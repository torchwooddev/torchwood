package interceptor

// adminRoleMethodRules 登记仅限 console owner/admin 角色调用的 Server API
// 写方法（安全评审 M7）：受限 admin（member/viewer）会话一律拒绝；
// API key 与端用户凭证不在此表处理（分别走 apiKeyMethods scope 门禁与
// use-case 层 IsPlatformAdmin 守门）。
//
// 角色模型对齐 Console useAdminRole：viewer 只读（仅 List/Get/Count）；
// member 可写业务资源；owner/admin（平台 admin）不受限。各方法允许角色：
//   - FunctionsService 写方法（含变量）：仅 owner/admin（平台级敏感写）；
//   - CreateUser/CreateBucket/UpdateProject：member/owner/admin（业务写，
//     viewer 只读）；
//   - DeleteUserSession：仅 owner/admin（管理员操作）。
var adminRoleMethodRules = map[string][]string{
	// APIKeysService（仅 owner/admin）
	"/torchwood.server.v1.APIKeysService/CreateAPIKey": {"owner", "admin"},
	// UsersService
	"/torchwood.server.v1.UsersService/CreateUser":         {"member", "owner", "admin"},
	"/torchwood.server.v1.UsersService/CreateUserToken":    {"owner", "admin"},
	"/torchwood.server.v1.UsersService/UpdateUserPassword": {"owner", "admin"},
	"/torchwood.server.v1.UsersService/DeleteUser":         {"owner", "admin"},
	"/torchwood.server.v1.UsersService/DeleteUserSession":  {"owner", "admin"},
	// DatabasesService（schema DDL 写方法，仅 owner/admin）
	"/torchwood.server.v1.DatabasesService/CreateDatabase":   {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteDatabase":   {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/CreateCollection": {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/UpdateCollection": {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteCollection": {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/CreateAttribute":  {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteAttribute":  {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/CreateIndex":      {"owner", "admin"},
	"/torchwood.server.v1.DatabasesService/DeleteIndex":      {"owner", "admin"},
	// FunctionsService 全部写方法（对照 proto/server/v1/functions.proto RPC
	// 清单逐一登记；GetVariables 返回掩码值安全可放行，其余读方法 viewer 可读）
	"/torchwood.server.v1.FunctionsService/CreateFunction":   {"owner", "admin"},
	"/torchwood.server.v1.FunctionsService/UpdateFunction":   {"owner", "admin"},
	"/torchwood.server.v1.FunctionsService/DeleteFunction":   {"owner", "admin"},
	"/torchwood.server.v1.FunctionsService/CreateDeployment": {"owner", "admin"},
	"/torchwood.server.v1.FunctionsService/DeleteDeployment": {"owner", "admin"},
	"/torchwood.server.v1.FunctionsService/SetVariables":     {"owner", "admin"},
	"/torchwood.server.v1.FunctionsService/CreateExecution":  {"owner", "admin"},
	// OAuthProvidersService（仅 owner/admin）
	"/torchwood.server.v1.OAuthProvidersService/UpsertOAuthProvider": {"owner", "admin"},
	"/torchwood.server.v1.OAuthProvidersService/DeleteOAuthProvider": {"owner", "admin"},
	// StorageService（业务写，member 可创建 bucket）
	"/torchwood.server.v1.StorageService/CreateBucket": {"member", "owner", "admin"},
	// ProjectsService（member/owner/admin 可更新其绑定项目，仅收 viewer）
	"/torchwood.server.v1.ProjectsService/UpdateProject": {"member", "owner", "admin"},
}
