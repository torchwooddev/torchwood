# Round 2 复审报告：04 - Client/Console API 传输层

## 1. 修复验证结论表

| 修复项 | 结论 | 证据与说明 |
|--------|------|------------|
| **F1-1** Magic URL 登录 secret 回传响应体 | ✅ 已修复 | `internal/app/client/magic_url.go:80-91`：`CreateMagicURLSession` 调用 `tokens.CreateMagicURLToken` 得到 `challengeID, secret, expireAt`，但只返回 `&Challenge{ChallengeID: challengeID, ExpireAt: expireAt}`；`secret` 仅用于 `buildAccountActionURL` 拼入邮件链接。`internal/api/clientgrpc/account.go:406-419` handler 只透传 `ChallengeId/ExpireAt`。`internal/app/client/email_otp.go:27-30` 的 `Challenge` 结构仅含 `ChallengeID string` 与 `ExpireAt time.Time`，不含 secret。测试 `internal/app/client/magic_url_test.go:132-176` 明确断言响应 `challengeID` 不等于邮件链接中的 `secret`，且用 `challengeID` 当 secret 无法登录。 |
| **F2-1** API Key 全量 scope 越权 console AdminsService | ✅ 已修复 | `pkg/grpc/interceptor/jwt.go:144-155`：`permissionMethods` 分支对 `principal.CredentialType == CredentialTypeAPIKey` 直接返回 `PermissionDenied`。`internal/api/consolegrpc/admins.go:38-46` 增加 `requireAdminActor`，要求 `p.ActorKind == shared.ActorKindAdmin`，并用于 `List/Create/Update/DeleteAdmin`。测试 `pkg/grpc/interceptor/jwt_auth_test.go:139-171` 对 `*`/`all` scope 调用四个 AdminsService 方法均断言 `PermissionDenied`，已运行通过。 |
| **F7-1** Console 首个管理员引导 SignUp 入口 | ✅ 已修复 | `internal/pkg/config/config.proto:59-61` 新增 `setup_token`；`internal/pkg/config/bind.go:16` 已绑定 `security.setup_token`。`internal/app/console/setup.go:117-125` 在 `SignUp` 入口先判 `s.setupToken == ""` 返回失败，再常量时间比较请求 token。`internal/app/console/setup.go:135-151` 使用 `adminRepo.WithBootstrapLock(bootstrapLockKey, ...)` 串行化首次性检查。`proto/console/v1/auth.proto:75-77` 保持 `SignUp` 为 `ACCESS_PUBLIC`，校验下沉到 use-case。`internal/app/console/setup_test.go:315-325/327-336` 分别断言未配置 token 与 token 错误均被拒绝。`console/src/routes/Login.tsx:26-27/163-173` 前端已根据 `setup_token_required` 展示 setup token 输入框并透传。 |
| **F8-2** DeleteSessions keepCurrent 无法传递 | ✅ 已修复（采用 query 绑定方案） | `proto/client/v1/account.proto:123-127`：`DeleteSessions` 使用 `delete: "/v1/account/sessions"`，无 `body`，将 `keep_current` 绑定为查询参数。`genproto/client/v1/account.pb.gw.go:270-301` 生成代码调用 `runtime.PopulateQueryParameters(&protoReq, req.Form, filter...)` 把 query 参数映射到 `DeleteSessionsRequest`。`internal/api/clientgrpc/account.go:121-126` handler 读取 `req.GetKeepCurrent()` 并透传给 `client.DeleteSessions`。`internal/app/client/account.go:537-555` 用例正确实现 `keepCurrent` 跳过当前会话。`sdk/typescript/src/client/account.ts:103-108` 以 `query: { keep_current: String(keepCurrent) }` 发送。`sdk/typescript/src/__tests__/account.test.ts:53-72` 新增测试断言 URL 查询参数为 `keep_current=true`。`genproto/client/v1/account.swagger.json:484-490` 同步显示 `keepCurrent` query 参数。 |

**辅助验证结果**：
- `go vet ./internal/api/clientgrpc/... ./internal/api/consolegrpc/... ./pkg/grpc/interceptor/... ./internal/app/console/...` 通过。
- `go test -short ./internal/api/consolegrpc/... ./pkg/grpc/interceptor/... ./internal/app/console/...` 通过。
- `go test -short ./internal/app/client/...` 因 `internal/app/client/mfa_test.go:110` 的 `TestAccount_CreateTOTPFactor_RequiresJWTSecret` 未检查 `testing.Short()` 且本地无 Postgres 而失败；其余环境无关单测通过。`pkg/grpc/interceptor` 中 F2-1/F2-2/F2-4 相关用例均绿。

---

## 2. 新发现问题

### 🟠 P1

**P1-1 `internal/api/clientgrpc` 传输层无任何单元测试**
- **位置**：`internal/api/clientgrpc/` 目录下无 `*_test.go`。
- **问题描述**：AccountService、DatabasesService、TeamsService 三个 handler 文件均没有直接测试。F1-1/F8-2 等修复的正确性依赖 `internal/app/client` 与 `sdk/typescript` 的测试间接覆盖，handler 层的字段映射、错误透传、Principal 提取路径缺乏回归保护。
- **影响**：后续 proto 字段类型再次变更（如 F8-2 的 `expires_at` 从 int64 改为 Timestamp）时，handler 映射错误难以被本模块测试捕获。
- **修复建议**：为 `AccountService` 关键方法（Magic URL 响应字段、DeleteSessions keepCurrent 透传、SignOut/TOTP 等 Principal 路径）增加基于 mock use-case 的 handler 单元测试，覆盖正常与错误映射。

---

### 🟡 P2

**P2-1 F7-1 未配置 setup token 时返回 `FailedPrecondition`（HTTP 400），与任务书要求的 403/404 不符**
- **位置**：`internal/app/console/setup.go:120-122`。
- **问题描述**：代码返回 `codes.FailedPrecondition`：
  ```go
  if s.setupToken == "" {
      return nil, status.Error(codes.FailedPrecondition, "setup token is not configured...")
  }
  ```
  任务书明确要求"未设置 setup token 时 SignUp 是否拒绝（403/404）"。`PermissionDenied` 才映射为 HTTP 403，`NotFound` 映射为 404；`FailedPrecondition` 映射为 400。
- **影响**：引导安全入口的状态码与审计/自动化预期不一致，可能被前端或安全扫描误判为"请求格式错误"而非"入口关闭"。
- **修复建议**：将未配置 setup token 的拒绝改为 `codes.PermissionDenied`（推荐，语义为"引导入口未启用"）或 `codes.NotFound`，并在 `console/src/api/auth.ts` 与前端错误提示中同步。

**P2-2 F2-1 纵深防御未下沉到 use-case 层**
- **位置**：`internal/app/console/admins.go:54-178`。
- **问题描述**：fix-plan F2-1 方案要求 "`AdminsService` handler/use-case 增加 `ActorKind == Admin` 纵深防御守卫"。当前仅在 handler 层 `internal/api/consolegrpc/admins.go:38-46` 增加了 `requireAdminActor`，而 `internal/app/console/admins.go` 的 `List/Create/Update/Delete` 均未校验 `ActorKind`，只做了业务级 role/last-owner 保护。
- **影响**：如果未来出现绕过 handler 的 gRPC 内部调用（如其他服务直接引用 `console.Admins`）、测试桩构造错误或 handler 被误改，use-case 层缺少 fail-closed 守卫。
- **修复建议**：在 `Admins` 用例的写操作上增加 `contexts.Principal` 提取并断言 `ActorKind == Admin`，或至少给 `Create/Update/Delete` 增加守卫；`List` 可视风险决策。

**P2-3 F8-2 修复未同步到 Go SDK**
- **位置**：`sdk/go/client/account.go:1-80`。
- **问题描述**：Go SDK `AccountService` 目前仅有 `SignUp/SignIn/RefreshToken/Me/SignOut`，缺少 `ListSessions/DeleteSession/DeleteSessions/CreateJWT/CreateMagicURLSession` 等方法，`DeleteSessions` 的 `keepCurrent` 参数更无从谈起。
- **影响**：Go SDK 与 TS SDK/REST 契约不一致，使用 Go SDK 的 Agent/自动化无法调用 Client Account 的会话管理、Magic URL、JWT 等功能。
- **修复建议**：补齐 Go SDK `AccountService` 缺失方法，并为 `DeleteSessions` 增加 `keepCurrent bool` 参数与单元测试。

**P2-4 `clientgrpc/account.go` 的 `SignOut` 重复校验 Principal**
- **位置**：`internal/api/clientgrpc/account.go:60-67`。
- **问题描述**：
  ```go
  if _, ok := contexts.Principal(ctx); !ok {
      return nil, status.Error(codes.Unauthenticated, "unauthenticated")
  }
  ```
  拦截器已对 `ACCESS_PERMISSION` 方法完成认证，handler 再检查一次属于重复防御。与 AGENTS.md "handler 从 context 取 Principal，不自行解析凭据" 的约定不冲突，但增加了不必要的代码路径，且返回码与拦截器不一致时可能引起困惑。
- **影响**：低；但破坏了 handler "只做传输编排" 的边界清晰性。
- **修复建议**：移除该重复检查，或改为断言 `requirePrincipal` 并统一使用其返回的 Principal。

---

### 🟢 P3

**P3-1 `TestAccount_CreateTOTPFactor_RequiresJWTSecret` 未遵循 `testing.Short()` 约定**
- **位置**：`internal/app/client/mfa_test.go:108-110`。
- **问题描述**：该测试未像同文件其他集成测试一样在开头检查 `if testing.Short() { t.Skip(...) }`，直接调用 `testutil.SetupTestDB(t)`，后者在缺少 `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE` 时直接 `t.Fatalf`。
- **影响**：在本地无 Postgres 环境执行 `go test -short ./internal/app/client/...` 时，整个包因该用例失败而退出，掩盖其他短测试的真实结果。
- **修复建议**：在该测试开头补 `if testing.Short() { t.Skip("skipping integration test") }`。

**P3-2 `clientgrpc/account.go` 的 `DeleteSession` 未在 handler 层校验 `session_id` 非空**
- **位置**：`internal/api/clientgrpc/account.go:114-119`。
- **问题描述**：handler 直接将 `req.GetSessionId()` 透传给 use-case，未做 `session_id == ""` 的前置校验。虽然 use-case 层 `internal/app/client/account.go:523-535` 已校验，但 handler 层缺失导致 gRPC 状态码由 use-case 决定，路径不一致。
- **影响**：低；错误映射统一性略差。
- **修复建议**：在 handler 层增加空值校验并返回 `InvalidArgument`，与其他 handler 的输入校验风格保持一致。

**P3-3 `console/src/routes/Login.tsx` 探测 setup-status 失败时退回到登录态**
- **位置**：`console/src/routes/Login.tsx:50-54`。
- **问题描述**：`getSetupStatus()` catch 分支将 `setupState` 设为 `"login"`，此时若服务端实际未初始化且无 setup token 配置，登录表单会展示而非引导表单，用户只能反复收到登录失败。
- **影响**：仅影响网络异常或后端不可达时的 UX，不构成安全绕过；setup token 校验仍在服务端。
- **修复建议**：探测失败时展示明确错误提示或重试按钮，而非静默切换到登录态。

---

## 3. 模块总体结论

- **修复完成度估计**：约 85%。F1-1、F2-1、F7-1、F8-2 的核心安全修复均已落地，相关测试（Go 用例层、拦截器、TS SDK）基本覆盖关键行为。
- **剩余风险 Top 3**：
  1. **传输层 handler 零测试**：`internal/api/clientgrpc` 无直接测试，proto/字段类型再变更时回归风险高。
  2. **Go SDK 与 REST/proto 契约脱节**：Account 会话管理、Magic URL、JWT 等方法缺失，影响 Agent-Native 定位下的 Go 客户端。
  3. **F7-1 状态码偏差与 use-case 纵深防御不完整**：未配置 setup token 返回 400 而非预期的 403/404；`console.Admins` use-case 层缺少 `ActorKind` 兜底。
- **是否建议关闭本模块审查**：**不建议立即关闭**。建议在补齐 `internal/api/clientgrpc` 基础单元测试、修复 P2-1/P2-2/P2-3 后再做一次轻量复审；P3 项可随日常迭代处理。
