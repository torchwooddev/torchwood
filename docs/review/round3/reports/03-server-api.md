# Round 3 审查报告：03 Server API 传输层

> 范围：`internal/api/servergrpc/`、`internal/api/serverhttp/`；交叉 `proto/server/v1/*.proto`、`pkg/grpc/interceptor`（`method_auth` / 凭证语义）、`internal/infra/server/grpc_swagger_test.go`。
> 对照：`docs/review/prompts/03-server-api.md`、`docs/developer/09-api-guide.md` §1.4、Round 2 `docs/review/round2/reports/03-server-api.md`（只作背景，不抄结论）。
> 验证：只读审查；本轮子代理无 shell，**未执行** `go vet` / `go test`。建议本地补跑：
> `go vet ./internal/api/servergrpc/... ./internal/api/serverhttp/...`
> `go test ./internal/api/servergrpc/... ./internal/api/serverhttp/... -short`（集成测试已被 `-short` 跳过）。

---

## 摘要

传输层整体仍保持「handler 只做编解码 + 项目上下文 + 调用 use-case」的分层。Round 2 指出的 HTTP 公共鉴权、多凭证拒绝、同 key 多值、Preview 畸形尺寸、公开 bucket ID 校验、GetVariables 注释等问题均已落地。B3 自定义动词（`:count` / `:bulkUpdate` / `:bulkDelete` / `:runtimes` / `:specifications`）与 proto、gateway、swagger 一致。

本轮发现 **0 条 P0**、**1 条 P1**：Functions 代码包 HTTP 上传鉴权通过后未把 Principal 注入 `context`，`CreateDeployment` 的 `RequireServerWriteActor` 会把合法请求打成 401。其余为分页 token 丢失、GetBucket 手拼 DSL、optional presence 未转发等 P2/P3。无越权/注入类 P0。

**Verdict：有条件通过（CONDITIONAL）**——无 P0，但 P1 未修前不建议关闭本模块。

---

## 已核实健康

| 项 | 结论 | 证据 |
|----|------|------|
| handler 不写业务 | ✅ 总体成立 | `servergrpc/*.go` 以 `projectID` + map + 调 use-case 为主；`CreateExecution` 仅在传输层加 gRPC 超时余量（`functions.go:265-275`），可接受 |
| `method_auth` / `service_auth` 齐全 | ✅ | 各 `proto/server/v1/*.proto` 均有 `service_auth.default_access: ACCESS_API_KEY`（Health 为 `ACCESS_PUBLIC`）；启动期 `collectMethodsByAccess` 缺注解即失败（`internal/infra/server/grpc.go:176-210`） |
| swagger `x-torchwood-access` 与注解一致 | ✅ | `internal/infra/server/grpc_swagger_test.go:65-165` 断言顶层/operation 扩展等于 `method_auth` |
| API Key scope 覆盖 ACCESS_API_KEY 方法 | ✅ | `pkg/grpc/interceptor/apikey_scope.go:25-116` + `AssertAPIKeyScopeCoverage` fail-closed（`:196-221`） |
| 同 key 多值凭证头拒绝 | ✅ | gRPC `credentialMetadataValue`（`jwt.go:207-218`）；HTTP `auth.go:33-35`（`X-Api-Key` / `Authorization`）与 `:44-46`（多条 `TORCHWOOD_session_*`）；单测 `auth_test.go:62-102` |
| 多凭证并存拒绝（HTTP 对齐 gRPC） | ✅ | `auth.go:52-64`；`auth_test.go:18-58`、`133-160` |
| HTTP 公共 `httpAuth` | ✅ | `internal/api/serverhttp/auth.go`；`FileHandler`/`FunctionsHandler` 复用 |
| Functions HTTP 拒绝端用户 | ✅ | `functions_handler.go:182-186`；`functions_handler_test.go:179-237` |
| File HTTP 读写 scope 与 gRPC 一致 | ✅ | GET → `StorageServiceGetFile`，其余 → `StorageServiceCreateFile`（`file_handler.go:780-787`） |
| 自定义动词路径 | ✅ | proto `documents:count` / `:bulkUpdate` / `:bulkDelete`、`functions:runtimes` / `:specifications`；gateway `databases.pb.gw.go:1721-1761`、`functions.pb.gw.go:695-715`；swagger 同步；旧字面量段不在生成路由中 |
| admin 缺 `X-Torchwood-Project` fail-closed | ✅（handler 层） | admin JWT 不带 `ProjectID`（`validator.go:150-159`）；拦截器空 ProjectID 放行以便 `ProjectsService`（`jwt.go:135-141`，`ValidateAdminProjectAccess` 在 `ProjectID==""` 时 return nil）；**项目作用域 handler** 一律 `projectID==""` → `Unauthenticated`（如 `users.go:39-41`、`functions.go:31-33`、`file_handler.go:141-146`、`auth.go` + `projectID()`） |
| Preview 先读 header 再决定是否全量 | ✅ | `file_handler.go:599-618`、`:674-681`；`file_handler_preview_test.go:60-79` 覆盖 8193 宽 PNG |
| 解码失败 → 400 | ✅ | `file_handler.go:612-613`、`:692-696`；`file_handler_preview_test.go:82-103` |
| 公开 bucket ID 校验 + `BuildEqual` | ✅ | `file_handler.go:551-557`、`:735-737`；`file_handler_dsl_test.go` |
| 私有文件 `Cache-Control: private, no-store` | ✅ | `file_handler.go:511-515`、`:663-666` |
| GetVariables 注释与脱敏一致 | ✅ | `proto/server/v1/functions.proto:160-164` |
| GetProject / GetUser / GetTeam / GetAPIKey nil → NotFound | ✅ | `projects.go:69-73`、`users.go:88-90`、`teams.go:79-81`、`apikeys.go:87-89` |
| OAuth 回调 open redirect | ✅（传输层） | handler 仅转 `HandleOAuth2Callback` 的 URL；缺参回 `/?error=...`（`oauth_handler.go:44-46`）；success/failure 在 use-case `validateRedirectURL` + 项目白名单 |
| OAuth / API Key secret 不回显 | ✅ | `oauth_providers.go:82` 仅 `HasClientSecret`；API Key 明文只在 Create 响应 |
| 敏感字段不经 Users 映射泄漏 | ✅ | `mapUserDoc`（`users.go:193-228`）只取 email/name/status 等，不含 `password_hash` |

---

## 🔴 P0 严重

无。

---

## 🟠 P1 高

### 1. Functions 代码包 HTTP 上传未注入 Principal，合法请求会被 use-case 打成 401

- 位置：`internal/api/serverhttp/functions_handler.go:93-157`（尤其 `:148-152`）；对照 `internal/app/functions/deployments.go:32-35`、`internal/app/shared/authz.go:43-47`
- 问题：`upload` 在 `authorize` 成功后直接用 `r.Context()` 调 `CreateDeployment`。HTTP 自定义路由不经过 gRPC `UnaryAuthMiddleware`，context 里没有 Principal。`RequireServerWriteActor` 在 `contexts.Principal` 缺失时返回 `Unauthenticated`。
- 现有测试只覆盖 `authorize()` 与鉴权失败早退（`functions_handler_test.go:179-301`、`auth_test.go:148-160`），从未走到 `CreateDeployment`，因此漏检。
- 影响：`POST /v1/server/functions/{functionId}/deployments/code` 对 admin / 带 `functions.write` 的 API Key 都会在鉴权通过后失败；控制台/SDK 走该 multipart 路径时部署不可用。gRPC `CreateDeployment` 不受影响。
- 修复建议：`authorize` 成功后 `ctx = contexts.WithPrincipal(r.Context(), principal)` 再调 use-case；补一条不依赖真实 Docker 的单测，断言 mock `CreateDeployment` 收到的 ctx 含 admin/API Key Principal。

---

## 🟡 P2 中

### 2. `GetBucket` 手拼 DSL，未用 `query.BuildEqual`

- 位置：`internal/api/servergrpc/storage.go:81-84`
- 问题：`equal("$id","` + `req.GetId()` + `")`。同模块 `ListFiles` 已用 `query.BuildEqual`（`storage.go:160`），公开 bucket HTTP 路径也已转义。`GetBucketRequest.id` 无格式校验。
- 影响：持有 `storage.read` 的调用方可构造引号打断 `equal`，导致解析失败或非预期过滤。查询仍限定在调用方项目内，**不是跨项目越权**，但是与已修复的 DSL 注入防线不一致。
- 修复建议：改为 `query.BuildEqual("$id", req.GetId())`；或新增 `Storage.GetBucket` 走 `GetDocument`，避免用 List 冒充 Get。

### 3. 多个 List RPC 丢弃 next_page_token，分页无法续翻

- 位置：
  - `internal/api/servergrpc/users.go:61-76`（use-case 返回 token，handler `_` 丢弃）
  - `internal/api/servergrpc/teams.go:52-67`、`:155-170`
  - `internal/api/servergrpc/storage.go:58-73`、`:161-172`
- 对照：`ListDocuments` / `ListCollections` 正确写入 `Meta.NextPageToken`（`databases.go:153-156`、`:360-363`）；`ListProjects` 用 `pkg/crud` 编解码（`projects.go:37-57`）。
- 另：`ListAPIKeys`（`apikeys.go:59-76`）、`ListFunctions`（`functions.go:87-104`）、`ListOAuthProviders`（`oauth_providers.go:25-41`）、`ListDeployments` / `ListExecutions` 完全忽略 `ListRequest` 的 page 字段。
- 影响：用户/团队/文件等超过默认页后客户端拿不到续翻 token，Agent/Console 只能看到第一页。
- 修复建议：与 `ListDocuments` 对齐，把 use-case 的 next token 写入 `ListResponseMeta`；尚无分页的列表要么实现，要么在 proto 注释标明「全量返回、忽略 page_*」。

### 4. `UpdateUser` 未按 proto3 optional presence 转发空串

- 位置：`internal/api/servergrpc/users.go:99-114`；契约 `proto/server/v1/users.proto:113-122`（「未设置 = 不修改；设置（含空串）= 更新/清空」）
- 问题：`status` / `name` / `email` 用 `GetX() != ""`，无法区分「未传」与「显式空串」。`email_verified` 正确使用指针（`:115-117`）。`labels`/`prefs` 对空 Struct `{}` 仍写入 `updates`（`:103-108`），与「空对象 = 不修改」也不一致。
- 影响：无法清空 `name`；PATCH 带空 `labels`/`prefs` 可能误清空。use-case 本身接受空 `name`。
- 修复建议：`if req.Name != nil { updates["name"] = req.GetName() }`（status/email 同理）；labels/prefs 仅在 `Fields` 非空或显式约定时写入。

同类（降一档、不单列）：`UpdateFile` proto 写「支持清空 name/mime_type」（`storage.proto:181-185`），但 handler `GetName()`（`storage.go:204-211`）+ use-case 空串不改（`internal/app/storage/storage.go:351-356`）双边都不支持清空。

### 5. 公开 Health Check 回传依赖原始错误串

- 位置：`internal/api/servergrpc/health.go:23-32` 原样返回 `checkers.Details`；`internal/infra/health/checks.go:165-167` `st.Error = err.Error()`；单测断言 `"connection refused"`（`health_test.go:48`）
- 问题：`ACCESS_PUBLIC` 的 `/v1/health`、`/v1/server/health` 对匿名调用者暴露下游拨号失败原文。
- 影响：信息泄露面（地址、拒绝原因）；不构成越权。
- 修复建议：对外只返回 `ok`/`unavailable` + 依赖名；原文仅写日志。`/healthz/readiness` 已承担 503，不必在业务 Health 上回传内部错误。

### 6. `httpError` 对非 status 错误把 `err.Error()` 回给客户端

- 位置：`internal/api/serverhttp/file_handler.go:834-838`
- 问题：非 gRPC status 一律 `codes.Internal` + 原始 message。use-case 存在 `fmt.Errorf("create file document: %w", err)` / `fmt.Errorf("upload file: %w", err)`（`internal/app/storage/storage.go:246-252`），未映射时可能带出适配层细节。
- 影响：HTTP 上传/下载错误体可能泄漏内部路径或驱动错误。
- 修复建议：非 status 错误对外固定 `"internal error"`，原文只打日志。

### 7. HTTP 将「任意两条 `TORCHWOOD_session_*`」视为多凭证，比 gRPC 更严且可能误伤

- 位置：`internal/api/serverhttp/auth.go:39-46`；对照 `pkg/grpc/interceptor/jwt.go:238-251`（`parseSessionCookie` 取**第一条**匹配 cookie，console 优先）
- 问题：同浏览器同时存在 `TORCHWOOD_session_console` 与 `TORCHWOOD_session_<project>` 时，HTTP 文件/函数入口 401；gRPC 则选用第一条。
- 影响：admin 在同 origin 测端用户登录后，Console 预览/上传文件会突然 401。属一致性/可用性，不是绕过。
- 修复建议：与 gRPC 对齐——仅拒绝**同一 cookie 名**多值；多种 session cookie 时优先 `TORCHWOOD_session_console`，或按凭证类型互斥（console session vs 端用户 session）写清策略并两边共用。

---

## 🟢 P3 低

### 8. Users / Teams / Storage / API Keys / OAuth 传输层几乎无单测

- 位置：`internal/api/servergrpc/` 仅有 `projects_test.go`、`functions_test.go`、`health_test.go`、`databases_audit_test.go`（后者为集成测试，`-short` 跳过）
- 问题：P1（Principal 注入）和 P2（分页 token、optional presence、GetBucket DSL）均无 handler 级回归网。
- 修复建议：至少为 `GetBucket` 转义、`UpdateUser` presence、`ListUsers` next token、Functions HTTP `WithPrincipal` 补纯单测。

### 9. `X-Torchwood-Project` 同 key 多值未拒绝

- 位置：gRPC `firstMetadataValue`（`jwt.go:254-260`）；HTTP `r.Header.Get`（`auth.go:99`、`:119`）
- 问题：凭证头已 fail-closed 多值，项目头仍取第一个。
- 影响：代理合并重复项目头时语义依赖顺序；不能单独构成越权。
- 修复建议：与 `credentialMetadataValue` 一样，`len(values)>1` 则 `Unauthenticated`/`InvalidArgument`。

### 10. gRPC 把「多凭证」等 extract 错误压成同一句文案

- 位置：`pkg/grpc/interceptor/jwt.go:94-98`
- 问题：`multiple credentials provided` / `invalid authorization header` / `no credential` 对外都变成 `"authentication credential is not provided"`。HTTP `authenticate` 保留具体原因（`auth.go:34`、`:74`、`:80`）。
- 影响：排障与客户端提示不一致；安全上仍是 401。
- 修复建议：保留 `Unauthenticated`，按错误种类给出与 HTTP 对齐的短消息。

### 11. multipart 简单上传忽略 permissions / metadata / 自定义文件名

- 位置：`internal/api/serverhttp/file_handler.go:451-458`
- 问题：分片创建会话支持 `metadata`/`permissions`（`:183-228`），简单 `POST .../files` 只用 part 的 filename 与 Content-Type。
- 影响：功能缺口，不是安全问题（默认走 owner 权限）。
- 修复建议：读取表单字段 `permissions`/`metadata`/`name`，或在文档中写明仅分片/gRPC 支持。

### 12. `CreateDatabase` / `CreateCollection` / `CreateAttribute` 响应不回读持久化时间戳

- 位置：`databases.go:60`、`:127-133`、`:247-255`
- 问题：刻意避免尾随 Get（注释 R02-P3-1），但 `created_at`/`updated_at`/`attributes` 为空，与随后 Get 不一致。
- 影响：客户端若依赖创建响应里的时间戳会看到零值。
- 修复建议：adapter 返回写入后的模型，或文档标明创建响应不含审计时间。

### 13. 公开匿名读的 `?project=` 无 ID 格式校验

- 位置：`file_handler.go:547-560`（bucketID 已校验，project 未校验）
- 问题：超长/怪异 project 查询串会进入 `resolveProject`/`ListBuckets`。
- 影响：探测与日志噪音；注入已被 `BuildEqual` + bucketID 校验挡住。
- 修复建议：与 `isValidBucketID` 同样约束 project 参数。

### 14. gRPC `CreateFile` 整文件进 `bytes`（受 8MiB recv 限制）

- 位置：`storage.go:129-143`；`internal/infra/server/grpc.go:100` `MaxRecvMsgSize(8<<20)`
- 问题：大文件应走 HTTP multipart / 分片。当前有上限，不会 OOM，但 API 易误导。
- 修复建议：proto/OpenAPI 标明「小文件捷径，>8MiB 用 `/v1/storage/...`」。

---

## 模块结论

**校验充分性**：项目上下文缺失在项目作用域 RPC/HTTP 上 fail-closed；ID/枚举/密码强度等多在 use-case。传输层缺口是 **optional presence** 与 **GetBucket DSL**，不是普遍「信任客户端伪造 project_id」。

**鉴权一致性**：gRPC 拦截器 + proto 注解 + scope 表 + swagger 测试形成闭环。HTTP 已与 gRPC 对齐多凭证/同 key 多值；Functions HTTP 另拒端用户，与 `ACCESS_API_KEY` 一致。**唯一实质性断裂**是 Functions 上传未把已鉴权 Principal 放进 context。

**最需优先修复的 3 项**：

1. **P1** Functions HTTP `WithPrincipal`（否则代码包上传路径不可用）。
2. **P2** ListUsers/ListTeams/ListFiles 等补回 `next_page_token`（Agent 分页正确性）。
3. **P2** `GetBucket` 改 `BuildEqual` / GetDocument（与已修 DSL 防线对齐）。

**相对 Round 2**：原 P1 Preview 内存/单测、P2 `httpAuth`/多凭证/bucketID、P3 GetVariables 注释与 Preview 500，均已关闭。本轮不应再把这些当未修项。

**是否建议关闭本模块审查**：**不建议关闭**。无 P0，但 P1 会让官方 multipart 部署入口失效；修完并补单测后再做一次聚焦复核即可关闭。
