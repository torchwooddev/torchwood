# Round 2 复审报告：06 - Server/Console 用例层

> 复审范围：`internal/app/server/`、`internal/app/console/`、`internal/app/shared/` 及交叉引用。  
> 基线：`docs/review/prompts/06-server-console-use-cases.md`、`docs/review/fix-plan.md` §F2-2 / F4 / F5-4 / F7-1。  
> 验证命令：`go vet ./internal/app/server/... ./internal/app/console/... ./internal/app/shared/... ./internal/app/functions/... ./internal/api/servergrpc/... ./pkg/grpc/interceptor/...`（通过）；`go test -short ./internal/app/server/... ./internal/app/console/... ./internal/app/shared/... ./internal/app/functions/... ./internal/api/servergrpc/... ./pkg/grpc/interceptor/...`（全部通过）。  
> 注：集成测试依赖 Postgres/Redis，按约束未运行；相关 >50 条级联用例已审阅源码。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 证据（文件路径:行号） | 说明 |
|--------|------|----------------------|------|
| **F2-2** Console 受限 admin 经 Server API 提权 | ⚠️ 部分修复 | `pkg/grpc/interceptor/admin_roles.go:7-29` 登记了 CreateAPIKey、CreateUserToken、UpdateUserPassword、DeleteUser、Databases DDL、SetVariables、OAuth Upsert/Delete 的 owner/admin 限制；`internal/app/server/authz.go:14-22` 提供 use-case 层 `requirePlatformAdmin`；DeleteUser/UpdateUserPassword/CreateUserToken/CreateAPIKey/DDL 均调用该守卫。 | 列出的关键写方法已收口；但 **Functions 全部写方法（除 SetVariables）、CreateUser、CreateBucket、UpdateProject、DeleteUserSession 等仍未被拦截器或 use-case 限制**，viewer/member admin 仍可调用，修复不完整。 |
| **F4-1** 用户/团队级联删除被 50 条截断 | ✅ 已修复 | `internal/app/server/users.go:320-325` 将级联包入 `RunInTx`；`users.go:418-436` `cascadeListAll` 显式 `PageSize: 1000` 并循环至 `NextPageToken == ""`；`teams.go:467-469` `listMembershipDocs` 同样使用该循环。 | DeleteUser 级联清理 sessions/identities/memberships，DeleteTeam 级联清理 memberships，均不再受默认 50 条分页截断。 |
| **F4-1** >50 条集成测试 | ✅ 已补 | `internal/app/server/users_integration_test.go:192-254` `TestServerUsers_DeleteUser_CascadeBeyondDefaultPage` 直插 61 条 sessions 与 61 条 memberships 验证全部清理。 | 因需本地 Postgres，未实际运行，但测试用例已存在。 |
| **F4-2** DeleteDatabase/DeleteCollection 不清理元数据 | — 归属 F3-5 | `docs/review/fix-plan.md:173-178` 已明确并入 F3-5，由动态文档层负责；本模块不逐条验证。 | 已在 fix-plan 中标注归属，避免重复记录。 |
| **F4-3** 团队 last-owner 保护 | ✅ 已修复 | `internal/app/server/teams.go:473-500` `guardLastOwner` 统计 accepted 且含 owner 角色的其他成员；`teams.go:364-380` DeleteMembership 与 `teams.go:280-307` UpdateMembership 均调用；`internal/app/client/teams.go:140-161` DeleteMembership 最终同样进入 server 用例。 | server 与 client 自退路径均受保护。 |
| **F4-4** UpdateUser 改邮箱不查重 | ✅ 已修复 | `internal/app/server/users.go:156-170` 改邮箱前先 ListDocuments 查重并排除自身 userID；`internal/app/shared/docdb_errors.go:28` 与 `:44` 将 23505 映射为 `AlreadyExists`。 | 重复邮箱返回 `codes.AlreadyExists`，并发唯一索引冲突亦有兜底。 |
| **F4-5** GetProject 返回 nil,nil | ✅ 已修复 | `internal/api/servergrpc/projects.go:69-73` `p == nil` 时返回 `codes.NotFound`。 | 与 users.go GetUser 模式对齐。 |
| **F4-6** 补强项（事务/审计/角色下沉） | ⚠️ 部分修复 | DeleteUser 包事务 ✅ (`users.go:320-325`)；DeleteTeam 未包事务 ❌ (`teams.go:165-182`)；CreateUserToken 加审计日志 ✅ (`users.go:297-304`)，生命周期为默认会话 TTL 未额外收紧 ⚠️；console admins 角色校验下沉 ✅ (`internal/app/console/admins.go:73-198`)。 | 主要项已处理，DeleteTeam 事务与 token 生命周期仍待补强。 |
| **F5-4** GetVariables 明文返回 secret | ⚠️ 部分修复 | `internal/app/functions/variables.go:49-53` 对非空值掩码为 `"******"`；`variables.go:15-35` SetVariables 返回原值。 | 后端已脱敏；**Console 前端未适配**：`console/src/routes/functions/pages.tsx:450-467` 用 GetVariables 返回值直接填充表单，保存时会将掩码写回。 |
| **F7-1** Console 首个管理员引导可被抢占 | ✅ 已修复 | `internal/app/console/setup.go:120-125` 未配置 setup token 或校验失败均拒绝；`setup.go:135-151` 使用 `WithBootstrapLock` + `ListAdmins` 串行化首个 admin 创建；`internal/pkg/config/config.proto:59-61` 新增 `security.setup_token`；`internal/pkg/config/bind.go:16` 绑定 `TORCHWOOD_SECURITY_SETUP_TOKEN`。 | 配置、绑定、use-case 并发保护均到位；`setup_test.go` 覆盖 token 缺失/错误/二次注册/rollback 场景。 |

---

## 2. 新发现问题

### 🔴 P0：F2-2 修复不完整——受限 admin 仍可调用 Functions 写方法

- **位置**：
  - 用例层：`internal/app/functions/management.go:59-112` (CreateFunction)、`:129-166` (UpdateFunction)、`:168-189` (DeleteFunction)；`internal/app/functions/deployments.go` CreateDeployment/DeleteDeployment；`internal/app/functions/executions.go` CreateExecution。
  - 传输层：`internal/api/servergrpc/functions.go:61-85`、`:118-149`、`:151-161`、`:163-178`、`:208-218`、`:249-282`。
  - 拦截器：`pkg/grpc/interceptor/admin_roles.go:7-29` 仅登记了 `SetVariables`，未覆盖其他 Functions 写方法。
- **问题描述**：FunctionsService 写方法（除 SetVariables）既不在 `adminRoleMethodRules` 中，use-case 层也未调用 `requirePlatformAdmin`。viewer/member admin 会话在通过 `ValidateAdminProjectAccess` 后，可直接创建/更新/删除函数、部署与执行。
- **影响**：受限 admin 获得超出其角色的函数管理能力，与 F2-2 “viewer/member 仅放行 List/Get/Count” 的目标不符，构成提权。
- **修复建议**：将 FunctionsService 全部写方法加入 `adminRoleMethodRules`（或在前端角色模型中允许 member 写业务资源则统一在 use-case 层按角色细粒度鉴权），并在 use-case 层增加纵深防御守卫。

### 🟠 P1：Console 变量页未适配 GetVariables 脱敏，保存时会覆写真实值

- **位置**：`console/src/routes/functions/pages.tsx:450-467`（用 GetVariables 结果初始化表单状态）、`:483-489`（保存 mutation）、`:530-533` 与 `:655-661`（输入框双向绑定）、`:691`（保存按钮仅 platformAdmin 可见）。
- **问题描述**：后端 `GetVariables` 已返回掩码 `"******"`，但前端直接把该值回填到输入框；`SetVariables` 是全量替换，保存时会将 `"******"` 作为真实值写回数据库。
- **影响**：owner/admin 在不知情的情况下保存变量，导致原 secret 被掩码字符串覆盖，功能损坏。
- **修复建议**：GetVariables 后在前端将 value 显示为空占位符或提示“仅在设置时可见”；保存时只提交用户编辑过的 key/value，未编辑项应从本地状态中剔除，避免误传掩码。

### 🟠 P1：F2-2 残留——CreateUser / CreateBucket / UpdateProject / DeleteUserSession 对 viewer/member 未收口

- **位置**：
  - `internal/app/server/users.go:73-131` CreateUser 未调用 `requirePlatformAdmin`，且内部使用 `databases.SystemPrincipal` 写库。
  - `internal/app/server/users.go:254-269` DeleteUserSession 未调用 `requirePlatformAdmin`，内部使用 `SystemPrincipal` 删文档。
  - `internal/app/storage/storage.go:61-97` CreateBucket 使用 `SystemPrincipal` 写 buckets 文档，无平台 admin 守卫。
  - `internal/app/server/projects.go:158-213` UpdateProject 明确允许“非平台 admin 更新其绑定项目”。
  - `pkg/grpc/interceptor/admin_roles.go:7-29` 未覆盖上述方法。
- **问题描述**：拦截器采用 denylist，未在上述写方法处限制 viewer/member。use-case 层也未统一鉴权，导致受限 admin 可操作这些资源。
- **影响**：与前端 `useAdminRole` 中 viewer 只读、member 仅写业务资源的模型不一致，viewer 可直接创建用户/bucket/修改项目，member 可执行平台级敏感操作。
- **修复建议**：将 admin 写权限收口改为 allowlist（viewer 仅 List/Get/Count，member 按前端模型开放业务写方法），或在 use-case 层统一按 `principal.Role` 与 `IsPlatformAdmin` 鉴权。

### 🟡 P2：DeleteTeam 级联删除未包事务

- **位置**：`internal/app/server/teams.go:165-182`。
- **问题描述**：DeleteTeam 先循环删除 memberships，再删除 team 文档，但未包裹在 `RunInTx` 中；中途失败会留下孤儿 membership 或 team 元数据。
- **影响**：数据一致性风险，与 DeleteUser 的事务处理不一致。
- **修复建议**：参照 DeleteUser，在 `Teams` 中注入 `*clients.Database`，将 membership 清理与 team 删除包入 `RunInTx`。

### 🟡 P2：proto 注释与 GetVariables 脱敏实现不一致

- **位置**：`proto/server/v1/functions.proto:158-160`。
- **问题描述**：注释仍声明“GetVariables 响应回显”，但后端实现已改为掩码 `"******"`。
- **影响**：SDK/前端开发者按旧注释集成时可能错误地假设明文可见。
- **修复建议**：更新注释为“GetVariables 返回掩码值，真实值仅 SetVariables 请求/响应中可见一次”。

### 🟢 P3：API Key 创建时未校验 scope 不超出调用者 scope

- **位置**：`internal/app/server/apikeys.go:38-81`。
- **问题描述**：CreateAPIKey 校验 scope 格式，但未校验 `cmd.Scopes` 是否全部包含于调用者自身 `principal.Permissions`。
- **影响**：F2-2 已阻止 API Key 调用 CreateAPIKey，故当前仅 owner/admin 会话可创建 key；但若未来开放 member 创建 key，可能产生超范围 key。
- **修复建议**：在 use-case 中比较 `cmd.Scopes` 与 `principal.Permissions`，拒绝超出调用者权限的 scope。

---

## 3. 模块总体结论

- **修复完成度估计**：约 **75%**。核心 P0 项（F4-1 级联删除、F4-3 last-owner、F4-4 邮箱查重、F4-5 GetProject NotFound、F7-1 setup token）已完整修复；F2-2 与 F5-4 后端修复到位但**覆盖不完整/前端未适配**，F4-6 事务与生命周期仍有缺口。
- **剩余风险 Top 3**：
  1. **Functions 写方法对 viewer/member admin 开放**（P0），是最直接的提权残留。
  2. **Console 变量页会误把掩码 `"******"` 当真实值保存**（P1），已造成数据损坏风险。
  3. **CreateUser / CreateBucket / UpdateProject / DeleteUserSession 等写方法未纳入 F2-2 权限模型**（P1），与前端角色权限不一致。
- **是否建议关闭审查**：**不建议关闭**。需先补齐 F2-2 的完整方法覆盖（建议改为 allowlist 或统一 use-case 层角色守卫），修复 Console 变量页脱敏适配，并将 DeleteTeam 纳入事务后，方可认为本模块审查可关闭。
