# Round 3 全量只读审核：04 - Client/Console API 传输层

> 审查范围：`internal/api/clientgrpc/`、`internal/api/consolegrpc/`，交叉 `proto/client/v1/`、`proto/console/v1/`，以及用例层/拦截器只读对照。
> 对照：`docs/review/round2/reports/04-client-console-api.md`、`docs/review/prompts/04-client-console-api.md`。
> **未修改源代码**。本轮为静态通读；未跑集成测试（需本地 Postgres/Redis）。

---

## 摘要

Round 2 指出的传输层主风险大多已落地：`clientgrpc` 补了 Account handler 单测；`SignOut` 不再重复校验 Principal；`DeleteSession` 空 `session_id` 在 handler 返回 `InvalidArgument`；`Admins` 写操作的 `ActorKind` 守卫已下沉到 use-case。本轮特别关注的五条安全/契约路径（ConfirmEmailChange 免登录、B3 `:count` 自定义动词、setup 一次性 token、cookie/SameSite、匿名权限边界）在传输层均核实为按设计工作，未见越权或凭据绕过。

新发现以契约与映射缺陷为主：`ListTeams` 丢弃 `NextPageToken`（P1）；`UpdateAccount` 丢掉 proto3 optional presence（P2）；未配置 setup token 仍返回 `FailedPrecondition`（Round 2 P2-1 残留）。无 P0。

| Round 2 项 | 本轮结论 | 证据 |
|------------|----------|------|
| P1-1 `clientgrpc` 零测试 | ✅ 部分修复 | 新增 `internal/api/clientgrpc/account_test.go`（Magic URL secret 不回传、`keep_current`、ConfirmEmailChange、错误码、SignOut 幂等）。`databases.go` / `teams.go` 仍无 handler 测试。 |
| P2-1 setup token 未配置返回 400 | ❌ 未改 | `internal/app/console/setup.go:122-123` 仍为 `codes.FailedPrecondition`。 |
| P2-2 Admins use-case 缺 ActorKind | ✅ 已修复（写路径） | `internal/app/console/admins.go:74-79/116-119/165-168` 的 Create/Update/Delete 调用 `RequireAdminActor`。List 仍只靠 handler。 |
| P2-3 Go SDK 缺 Account 方法 | ⏭ 超出本模块 | 属 12-sdk-cli；本模块不重复开单。 |
| P2-4 SignOut 重复 Principal 校验 | ✅ 已修复 | `internal/api/clientgrpc/account.go:60-65` 已删除重复检查，注释写明 use-case 幂等。 |
| P3-2 DeleteSession 空 id | ✅ 已修复 | `internal/api/clientgrpc/account.go:125-129` handler 层 `InvalidArgument`。 |

---

## 已核实健康

### ConfirmEmailChange 为 public RPC，不误做认证假设、不泄露 pending_email

- Proto：`proto/client/v1/account.proto:131-145` 标注 `ACCESS_PUBLIC`，REST `PUT /v1/account/email-change`。
- Handler：`internal/api/clientgrpc/account.go:101-111` 只透传 `project_id` / `user_id` / `secret`，**不**调用 `requirePrincipal`，也不把 context 里的 UserID 覆盖请求体。
- 响应：`mapUser`（`account.go:532-545`）只映射 `id/email/name/status/email_verified/timestamps`；`Account` 消息无 `pending_email` 字段，传输层无法回传暂存邮箱。
- 用例对照：`internal/app/client/account.go:551-616` 以 token（GETDEL）为唯一凭证；错误 secret → `Unauthenticated`，与 recovery 同模型。
- 测试：`account_test.go:375-433` 覆盖免登录错误 secret、staging（email 保持旧值、只写 `pending_email`）、确认后清空、token 一次性。用例层 `account_g3_test.go:457-481` 另证无 principal 也能确认成功。

### B3 自定义动词 `documents:count` 映射正确，不再占用 `document_id`

- Proto：`proto/client/v1/databases.proto:104-114` 为 `GET .../documents:count`，`ACCESS_PUBLIC`。
- 生成网关：`genproto/client/v1/databases.pb.gw.go:876` 的 Count 模式 verb 为 `"count"`；Get/Update/Delete 仍走 `{document_id}` 段。`GET .../documents/count` 与 `GET .../documents:count` 不再冲突。
- Handler：`internal/api/clientgrpc/databases.go:142-153` 将 `ListDocumentsRequest` 的 `queries` / `project_id` 透传 `CountDocuments`。`clientgrpc` 内已无 `clientDocumentIDReserved`。
- 用例对照：`internal/app/client/databases_reserved_test.go` 断言 `document_id="count"` 可 CRUD。

### Console setup/bootstrap：一次性 token 可用，完成后关闭

- `GetSetupStatus` / `SignUp` 保持 `ACCESS_PUBLIC`（`proto/console/v1/auth.proto:52-77`）；关闭逻辑在 use-case，不依赖拦截器。
- Handler：`internal/api/consolegrpc/auth.go:59-83` 透传 `setup_token`，并返回 `setup_token_required`；成功后复用 `setSessionCookies`。
- Use-case：`internal/app/console/setup.go:119-144` 先常量时间比较 token，再 `WithBootstrapLock` 串行化「admins 为空」检查；已有 admin 则 `FailedPrecondition: setup already completed`。
- 集成测试：`auth_integration_test.go:93-152` 首次 sign-up 下发 owner + cookie + 一次性 API Key secret；二次 sign-up HTTP 400 且文案含 `setup already completed`；`setup-status` 翻转为 `needs_setup=false`。
- 未配置 token 仍拒绝创建（入口默认关闭），见残留 P2-1。

### Cookie 路径、清除对称、SameSite=Lax（CSRF）

- 签发：`internal/api/consolegrpc/cookies.go:34-39`  
  - `TORCHWOOD_session_console`：`Path=/`、HttpOnly、SameSite=Lax  
  - `TORCHWOOD_console_refresh`：`Path=/v1/console/auth`、HttpOnly、SameSite=Lax  
  - `Secure` 随 `auth.SecureCookies()`（HTTPS public URL）。
- 清除：`cookies.go:44-49` Path 与签发一致，`Max-Age=-1`。
- 注释（`cookies.go:1-11`）说明变更类走 cookie 的跨站 POST 不会携带 Lax cookie，故不另做 CSRF token。Admins 的 PATCH/DELETE 同样不会被跨站表单带上 Lax cookie（表单只能 GET/POST）。
- 测试：`auth_test.go:136-242` 覆盖 Path/HttpOnly/SameSite、TLS 时 Secure、body 优先于 cookie、SignOut 对称清除。

### 匿名用户权限边界（传输层不另开后门）

- `CreateAnonymousSession` 为 `ACCESS_PUBLIC`（`account.proto:288-298`）；handler（`account.go:263-271`）只透传 `project_id`。
- 用例写入 `labels=["anonymous"]`，JWT 角色为 `users` + `user:{id}` + `label:anonymous`（`user_roles.go:21-35`）。因此匿名会话与实名用户一样通过 `ACCESS_PERMISSION ["users"]`——这是 Appwrite 风格的「匿名即已认证用户」，不是传输层漏鉴权。
- 未带凭证的文档读：`resolveReadPrincipal` 使用 `GuestPrincipal`（`guests`）；已登录匿名用户按自身 roles 过 `_perms`。
- 系统集合：写全拒；`users/sessions/identities` 读全拒；`teams/buckets` 仅当集合 `read:any` 时匿名可读（`system_collections_readonly_test.go`）。
- 升级实名：`UpdateAccount` 在 `password_hash` 为空时跳过 `old_password`（`account.go:489-498`），属用例状态机，handler 未放宽。
- 匿名会话 IP 频控（20/小时）在用例层；handler 无额外假设。

### 其它传输层健康项

- 当前用户标识一律来自拦截器注入的 Principal（Account 的 MFA/JWT、Teams 的 `dbPrincipal`、Databases 写路径的 `resolveProject`），请求体没有 `{userId}` 可冒充他人。
- `DeleteSessions` 的 `keep_current` 经 query 绑定，handler 透传；测试覆盖 true/false。
- Magic URL 响应只有 `challenge_id` + `expire_at`，secret 仅在邮件中。
- `AdminsService` handler `requireAdminActor` + 拦截器拒绝 API Key；写操作 use-case 再守一层。
- Client 文档读 `project_id` 与 Principal 不一致时，用例返回 `project_id mismatch`（`databases.go:54-57`），handler 的 `resolveProjectID` 不会单独越权跨项目写。

---

## 🔴 P0 严重

无。

---

## 🟠 P1 高

### P1-1 `ListTeams` 丢弃 use-case 返回的 `NextPageToken`

- **位置**：`internal/api/clientgrpc/teams.go:33-49`
- **问题描述**：use-case `Teams.ListTeams` 返回 `(docs, total, nextPageToken, err)`，handler 用 `_` 丢掉第三返回值，`ListResponseMeta` 只填 `PageSize`/`TotalCount`，不设 `NextPageToken`。对照同目录 `ListDocuments`（`databases.go:69-72`）正确回传 `next`。客户端即使传 `page_token`/`page_size` 也无法续页。
- **影响**：团队数量超过默认/请求页大小时列表被截断且无法翻页，属功能缺陷。
- **修复建议**：与 `ListDocuments` 对齐，接收 next token 并写入 `Meta.NextPageToken`；补一条 handler/用例联调测试断言第二页非空且无重叠。

---

## 🟡 P2 中

### P2-1（残留）未配置 setup token 时 SignUp 返回 `FailedPrecondition`（HTTP 400）

- **位置**：`internal/app/console/setup.go:122-123`（handler `auth.go:70-73` 原样透传）
- **问题描述**：Round 2 P2-1 未改。`setupToken == ""` 时返回 `FailedPrecondition`（grpc-gateway → 400），任务书期望 403/404。错误 token 已正确用 `PermissionDenied`（403）。
- **影响**：引导入口关闭被客户端/扫描当成「请求格式错误」，与「入口未启用」语义不符。安全上入口仍是关的。
- **修复建议**：未配置改为 `codes.PermissionDenied`（或 `NotFound`），并同步前端/集成测试对二次 sign-up「已完成」与「未配置」的状态码预期。

### P2-2 `UpdateAccount` 丢掉 proto3 optional presence，无法按契约清空 name

- **位置**：`internal/api/clientgrpc/account.go:87-94`；对照 `proto/client/v1/account.proto:538-541`、`internal/app/client/account.go:140-146/444-446`
- **问题描述**：proto 写明 `name`/`email`「未设置=不修改；设置（含空串）=更新/清空」。handler 用 `req.GetName()`/`GetEmail()` 压成普通 `string`，`UpdateAccountCommand.Name` 也无 presence。用例 `if cmd.Name != ""` 把空串当「不改」。同模块 `UpdateAdmin` 已正确用 `req.Role != nil`（`admins.go:89-91`）。
- **影响**：客户端无法将 name 清空；与 OpenAPI/proto 契约不一致。email 空串同样无法表达「清除」，但改邮箱本就走 staging，实际风险低于 name。
- **修复建议**：Command 改为 `*string`（或单独 `NameSet bool`）；handler 在 `req.Name != nil` 时才赋值；用例对「已设置的空串」写入空 name。

### P2-3 `DatabasesService` / `TeamsService` handler 仍无直接单测

- **位置**：`internal/api/clientgrpc/databases.go`、`internal/api/clientgrpc/teams.go`（目录无对应 `*_test.go`）
- **问题描述**：Round 2 P1-1 在 Account 上已补齐，但 CountDocuments 的 `:count` 映射、`resolveProjectID` 回退顺序、`ListTeams` 分页字段等仍只靠用例/生成代码间接覆盖。
- **影响**：本轮 P1-1 这类纯 handler 映射错误不会被本包测试抓住。
- **修复建议**：用 mock/轻量桩测 `CountDocuments` 路径与 query 透传、`ListTeams` 的 `NextPageToken`、非法 permissions → `InvalidArgument`。

---

## 🟢 P3 低

### P3-1 `Admins.List` use-case 仍无 `ActorKind` 守卫

- **位置**：`internal/app/console/admins.go:55-61`（handler `admins.go:48-51` 有 `requireAdminActor`）
- **问题描述**：Round 2 P2-2 要求写路径下沉，Create/Update/Delete 已做；List 仍只在 handler 拦截。读路径风险低于写，但与「use-case fail-closed」不完全一致。
- **影响**：仅当有人绕过 handler 直接调 `Admins.List` 时才会暴露邮箱列表。
- **修复建议**：List 同样调用 `RequireAdminActor`，或在注释中明确「读路径仅 handler 守卫」的取舍。

### P3-2 cookie 注释「变更类端点均为 POST」不精确

- **位置**：`internal/api/consolegrpc/cookies.go:5-6`
- **问题描述**：Console 变更还有 `PATCH /v1/console/admins/{id}` 与 `DELETE /v1/console/admins/{id}`。SameSite=Lax 对跨站非 GET 仍不带 cookie，结论成立，但注释容易误导后续加 GET 副作用接口。
- **影响**：文档准确性；当前 CSRF 模型仍然成立。
- **修复建议**：改为「非安全方法 + Lax；若新增带副作用的 GET 须另做 CSRF」。

---

## 模块结论

- **鉴权一致性**：Client 用户标识来自 Principal；ConfirmEmailChange / recovery / Magic URL 确认类 public RPC 以 secret 为唯一凭证，handler 未错误要求登录态。Console Admins 写路径 handler + use-case 双守卫；API Key 不能走 permission 方法。匿名会话是带 `users` 角色的已认证用户，文档 guest 读与系统集合隔离在用例/文档层，传输层未放宽。
- **校验充分性**：Account 关键路径（空 session_id、prefs 必填、Magic URL/ConfirmEmailChange 错误码）有 handler 测试。分页与 optional presence 仍有漏洞（P1-1、P2-2）。setup 关闭逻辑正确，仅状态码与审计预期不一致（P2-1）。
- **Cookie / CSRF**：HttpOnly + SameSite=Lax + refresh 限 `/v1/console/auth`，签发/清除对称，测试覆盖完整。
- **最需优先修复的 3 项**：
  1. **P1-1** `ListTeams` 回传 `NextPageToken`
  2. **P2-2** `UpdateAccount` 保留 proto3 optional presence
  3. **P2-1** 未配置 setup token 改为 403/404 语义
- **是否建议关闭本模块审查**：**可以关闭安全主路径**（无 P0，本轮关注的五条专项均核实健康）。分页与 optional 属契约缺陷，建议随日常迭代修，不必再开一轮全量传输层复审。
