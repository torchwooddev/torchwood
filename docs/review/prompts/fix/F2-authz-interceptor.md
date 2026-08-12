# 修复任务 F2：鉴权拦截器与 Server API 提权收口

## 角色

你是资深 Go 安全工程师，负责修复 Torchwood 鉴权体系的三个 P0 越权漏洞与纵深防御补强。
方案详见 `docs/review/fix-plan.md` §2（F2 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`（认证中间件约定）、`docs/review/fix-plan.md` §2
- 审查报告（背景）：`docs/review/` 下的 01/03/06 报告

## 修复清单

1. **API Key 全量 scope 越权 console AdminsService**（P0）：
   - 现状：`pkg/grpc/interceptor/jwt.go:110-144` 仅对 `apiKeyMethods`（ACCESS_API_KEY 方法）
     执行 API Key scope 校验；`AdminsService` 属 `permissionMethods` 分支，API Key 凭证
     会带着 `Permissions`（key scopes）参与 `HasAnyPermission(["owner"])`，scope `*`/`all`
     直接命中；`internal/api/consolegrpc/admins.go:38-80` handler 无 ActorKind 二次校验。
   - 修复：
     a. `jwt.go` 的 `permissionMethods` 分支对 `principal.CredentialType == CredentialTypeAPIKey`
        直接返回 PermissionDenied（API Key 只允许经 `apiKeyMethods` 门禁）；
     b. 纵深防御：`AdminsService` 的 handler 或 use-case（`internal/app/console/admins.go`）
        增加 `ActorKind == Admin` 守卫；
     c. 补测试：`*`/`all` scope 的 API Key 调用 CreateAdmin/ListAdmins/UpdateAdmin/DeleteAdmin
        必须 PermissionDenied（参考 `pkg/grpc/interceptor/jwt_auth_test.go` 的既有测试模式）。
2. **Console 受限 admin（viewer/member）经 Server API 全面提权**（P0）：
   - 现状：`jwt.go:110-137` 对 admin 会话不区分 console 角色（owner/admin/member/viewer），
     use-case 层除 Projects 外不校验 `IsPlatformAdmin`；viewer/member 可 CreateAPIKey(scope=all)、
     CreateUserToken 冒充、执行全部 DDL。
   - 修复（最小收口）：
     a. 拦截器：admin 会话进入写方法前校验角色——将 `CreateAPIKey`、`CreateUserToken`、
        `UpdateUserPassword`、`DeleteUser`、databases schema 写方法（Create/Update/Delete
        Database/Collection/Attribute/Index）、functions `SetVariables`、oauth providers 写方法
        登记为「仅 owner/admin」方法（可在 `apikey_scope.go` 附近新增 adminRoleMethods 映射表，
        或复用 `permissionMethods` 的机制——注意该方法已按凭证类型分流，admin 走 roles 匹配：
        `HasAnyPermission(["owner","admin"])` 且 viewer/member 无 owner/admin 角色即可拒绝）；
     b. use-case 纵深防御：`internal/app/server/apikeys.go`（Create）、`users.go`
        （CreateUserToken/UpdateUserPassword/DeleteUser）、`databases.go`（DDL 写方法）
        在入口校验 `principal.IsPlatformAdmin`（对齐 `Projects.CreateProject` 模式）；
     c. 补集成测试或单测：viewer/member 角色调上述写方法必须 PermissionDenied
        （角色如何注入由你根据现有测试基建决定）。
3. **端用户可上传 Functions 部署代码包**（P0）：
   - 现状：`internal/api/serverhttp/functions_handler.go:173-193` `authorize()` 对
     API Key 做 scope 检查、对 admin 做项目校验，但对端用户 Bearer JWT/会话 cookie
     完全放行 → 任意注册用户可触发 Docker 构建并部署恶意代码窃取函数环境变量。
   - 修复：`authorize()` 增加分支——`CredentialType` 为 Token/Session 且 `ActorKind != Admin`
     时返回 PermissionDenied；补测试：端用户 JWT 上传 deployment 必须 403。
4. **P2 补强**：
   - `jwt.go:150-165` `extractCredential`：请求同时携带多种凭证（Authorization + cookie +
     x-api-key）时显式拒绝（返回错误，防凭证混淆），保持单一凭证原则。
   - 审计失败告警：`pkg/grpc/interceptor/audit.go:60-62` 写入失败静默丢弃 → 至少
     `logger.Warn`（本项与 F7 冲突表中归属 F2）。

## 约束

- **不要**抽取/重构 file_handler.go 与 functions_handler.go 的公共鉴权辅助（留给后续批次）
- 不修改 proto；不修改 `internal/app/client/` 与 `internal/infra/documentdb/`
- 保持现有代码风格；不引入新依赖；除必要外不新增注释
- 不运行需要本地基础设施的集成测试

## 验证

- `go vet ./pkg/grpc/interceptor/... ./internal/api/serverhttp/... ./internal/api/consolegrpc/... ./internal/app/console/... ./internal/app/server/...`
- `go test ./pkg/grpc/interceptor/...`（miniredis 单测可跑）
- `go build ./...`
- 新增拦截器测试覆盖：API Key × permission 方法（`*` scope）、端用户 × functions HTTP、
  admin 角色 × 写方法

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；列出依赖其他批次的
事项（如需要新的集成测试环境）。
