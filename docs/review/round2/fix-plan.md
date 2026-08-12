# Torchwood Round-2 修复方案（基于 2026-08-12 修复后复审）

> 依据 `docs/review/round2/reports/` 12 份复审报告（01 安全认证 / 02 文档层 / 03 Server API /
> 04 Client-Console API / 05 Account 用例 / 06 Server-Console 用例 / 07 Storage-Functions /
> 08 CRUD / 09 基础设施 / 10 Proto / 11 Console / 12 SDK）汇总。
> 每条问题标注来源报告编号（如 R05-P1-2 = 05 报告新发现 P1 第 2 条；R06-F2-2 = 06 报告修复验证项）。
> 行号以复审时代码为准，修复时以实际代码为准（用 Grep 重新定位）。

## 0. 修复批次总览

| 批次 | 名称 | P0 | P1 | 建议顺序 | 依赖 |
|------|------|----|----|----------|------|
| G1 | CI 接入（buf lint / TS SDK / demo build） | 0 | 1 | **最先** | 无 |
| G2 | 权限收口（Functions 写方法等提权残留） | 1 | 2 | 1 | 无 |
| G3 | 认证与账户域收尾 | 0 | 5 | 1（与 G2 并行） | 需 generate-config |
| G4 | serverhttp 传输层（公共 httpAuth） | 0 | 2 | 2 | 无 |
| G5 | documentdb 与 crud 收尾 | 0 | 4 | 2（与 G4 并行） | 无 |
| G6 | Functions / Storage / Worker 收尾 | 0 | 3 | 3 | **依赖 G2**（共享 authz helper） |
| G7 | 基础设施收尾 | 0 | 2 | 3（与 G6 并行） | 无 |
| G8 | Console 前端收尾 | 0 | 5 | 3（与 G6/G7 并行） | 无 |
| G9 | SDK / CLI 收尾 | 0 | 0 | 4 | 无 |
| G10 | Proto / 契约收尾 | 0 | 4 | **最后** | 需 generate-proto；触碰 clientgrpc（与 G3 协调） |

> ⚠️ 并行执行约定：各批次文件范围基本无重叠（见 §11 冲突矩阵），可在独立 git 分支并行；
> 同一工作区串行执行时按批次顺序逐一提交。G6 必须先等 G2 合入（使用其共享 helper）。

---

## 1. G1 CI 接入（文件：.github/workflows/ci.yml、Taskfile.yml）

### G1-1 🟠 CI 未运行 buf lint / TS SDK 测试 / demo 构建（P1，R12-P1-1、R10-P1-2）
- 位置：`.github/workflows/ci.yml:63-88`（无 buf/TS 步骤）；`Taskfile.yml:140-144`（test 仅依赖 test-sdk-go）。
- 方案：
  1. backend job 或独立 job 增加 `buf lint`（与 `task generate-proto` 内已有 lint 对齐）；
     如仓库有 main 基线可加 `buf breaking --against`，无则仅 lint；
  2. 新增步骤：`cd sdk/typescript && npm ci && npm run test`（含契约测试 contract.test.ts）；
  3. 新增步骤：`task sdk-demo-build`（或等价 npm build）；
  4. `Taskfile.yml` 的 `test` 任务补上 TS SDK 测试依赖。
- 验证：push 后 CI 全绿；确认新增步骤真实执行（非静默跳过）。

---

## 2. G2 权限收口（文件：pkg/grpc/interceptor/admin_roles.go、internal/app/shared/、internal/app/functions/、internal/app/server/、internal/app/console/）

> 本批次是 Round-2 唯一 P0。原则：拦截器 denylist 补齐 + use-case 层纵深防御（fail-closed）。
> 角色模型对齐 Console `useAdminRole`：viewer 只读（仅 List/Get/Count）；member 可写业务资源；
> owner/admin（平台 admin）不受限。

### G2-1 🔴 Functions 写方法对 viewer/member admin 开放（P0，R06-P0）
- 位置：`pkg/grpc/interceptor/admin_roles.go:7-29`（仅登记 SetVariables）；
  `internal/app/functions/management.go:59-112`（CreateFunction）、`:129-166`（UpdateFunction）、
  `:168-189`（DeleteFunction）、`deployments.go`（CreateDeployment/DeleteDeployment）、
  `executions.go`（CreateExecution）。
- 方案：
  1. `adminRoleMethodRules` 补登 FunctionsService 全部写方法（Create/Update/DeleteFunction、
     CreateDeployment/DeleteDeployment、CreateExecution、DeleteExecution 等，逐一核对
     `proto/server/v1/functions.proto` 的 RPC 清单，不留遗漏）；
  2. use-case 层调用 `RequirePlatformAdmin`（见 G2-3 的共享 helper）做纵深防御；
  3. 补测试：viewer/member admin 调上述方法必须 PermissionDenied（参照 F2-2 既有测试模式）。

### G2-2 🟠 CreateUser / CreateBucket / UpdateProject / DeleteUserSession 未收口（P1，R06-P1）
- 位置：`internal/app/server/users.go:73-131`（CreateUser）、`:254-269`（DeleteUserSession）、
  `internal/app/storage/storage.go:61-97`（CreateBucket，**use-case 守卫由 G6 落地**，本批次只做拦截器规则）、
  `internal/app/server/projects.go:158-213`（UpdateProject）。
- 方案：
  1. 拦截器 `adminRoleMethodRules` 补登上述方法：viewer 一律拒；
     member 可按前端模型放行业务写（CreateUser/CreateBucket）——在规则表中明确每方法允许角色；
  2. UpdateProject：viewer 拒绝；member/owner/admin 可更新其绑定项目（保持现有语义，仅收 viewer）；
  3. use-case 层为 CreateUser/DeleteUserSession 加 `RequirePlatformAdmin` 或等价角色校验；
  4. 补 viewer 调写方法被拒的测试。

### G2-3 共享 authz helper 抽取（G6 依赖项）
- 位置：`internal/app/server/authz.go:14-22`（现有 `requirePlatformAdmin` 仅 server 包可用）。
- 方案：在 `internal/app/shared/`（如 `authz.go`）导出 `RequirePlatformAdmin(ctx) error`
  （基于 `contexts.Principal` 判断 `IsPlatformAdmin`）；`internal/app/server/authz.go` 改为
  薄包装或直接删除改用 shared；同步全部现有调用方。G6 将复用该 helper 于 storage/functions 包。

### G2-4 🟠 console Admins use-case 缺 ActorKind 纵深防御（P1，R04-P2-2）
- 位置：`internal/app/console/admins.go:54-178`。
- 方案：Create/Update/Delete 入口校验 `Principal.ActorKind == Admin`（List 可选）；
  对齐 handler 层 `requireAdminActor`（`internal/api/consolegrpc/admins.go:38-46`）。

### G2-5 🟢 CreateAPIKey 未校验 scope 不超出调用者（P3，R06-P3）
- 位置：`internal/app/server/apikeys.go:38-81`。
- 方案：`cmd.Scopes` 必须 ⊆ 调用者 `principal.Permissions`（或调用者为平台 admin 时放行），
  超出返回 PermissionDenied；补测试。

**验收**：`go vet ./pkg/grpc/interceptor/... ./internal/app/...`；`go test -short` 上述包全绿。

---

## 3. G3 认证与账户域收尾（文件：internal/app/client/、internal/infra/auth/、pkg/grpc/interceptor/、internal/api/clientgrpc/account.go、internal/pkg/config/config.proto）

> ⚠️ 本批次改 `config.proto`，需执行 `task generate-config`。
> 不触碰 `proto/`（DeleteFactor 契约变更归 G10）。

### G3-1 🟠 一次性 JWT 验证侧未消费，可重放（P1，R05-P1-1；测试缺口 R05-P2-8）
- 位置：`internal/app/client/jwt.go:41-67`（已登记 jti）、`internal/infra/auth/validator.go:62-67`（未消费）。
- 方案：JWT claims 中标记一次性（如 `one_time: true` 或专用 purpose）；`Validator.principalFromJWT`
  识别后调用 `OneTimeTokenStore.Consume`（GETDEL 原子），已消费/不存在返回 Unauthenticated；
  注意区分普通 access token 不受影响。补「同一 JWT 二次使用被拒」测试（R05-P2-8）。

### G3-2 🟠 改邮箱未验证即生效（P1，R05-P1-2）
- 位置：`internal/app/client/account.go:446-448`。
- 方案（分两档，优先实现 A）：
  - **A（完整 staging）**：新增 `pending_email` 暂存 + email-change 验证 token（复用
    `account_token_redis.go` 机制，新增 purpose）；验证通过后才写入 `email`。
    若需要新 RPC/字段（proto 变更）才能实现，则**不在本批次做**，转做 B 并在
    `docs/review/round2/fix-plan.md` 留下 backlog 备注。
  - **B（最小缓解，无 proto 变更）**：邮箱变更后向**旧邮箱**发送安全通知邮件（含撤销指引），
    向新邮箱发送验证邮件（复用现有 verification 流程）；`email_verified=false` 已有。
- 验证：A 档需「未验证前旧邮箱仍可登录/找回」测试；B 档需通知邮件发送测试。
- **本批次实施档位：B**。理由：A 档的 email-change 验证链接需要调用方提供验证 URL
  （`UpdateAccountRequest` 无 url 字段，proto 变更归 G10）；不引入新 proto 字段的话只能
  硬编码 publicBaseURL 路径，属产品契约决策，超出本批次范围。
  **Backlog（A 档，依赖 G10 proto 变更）**：`UpdateAccountRequest` 增加 `url` 字段后，
  实现 `pending_email` 暂存 + 新 purpose（`email_change`）+ 验证通过才写入 `email`，
  未验证前旧邮箱保持可登录/找回。

### G3-3 🟠 邮箱/密码变更后撤会话失败无回滚（P1，R05-P1-3）
- 位置：`internal/app/client/account.go:474-489`、`internal/app/client/recovery.go:129-136`。
- 方案：先撤会话再提交资料变更，或变更提交后撤会话失败时重试一次仍失败则回滚资料
  （恢复旧 email/password_hash）；两路径（UpdateAccount 与 recovery）统一处理。补故障注入测试。

### G3-4 🟠 登录节流对未注册邮箱计数（P1，R05-P1-5 = F1-9.6 未修项）
- 位置：`internal/app/client/account.go:277-280`、`internal/infra/auth/login_throttle_redis.go:44-49`。
- 方案：仅当用户存在时 `RecordFailure`；未注册邮箱走哑哈希 Verify 后返回统一错误，不计数。
  补「未注册邮箱连续失败不触发锁定」测试。

### G3-5 🟠 会话数量无上限（P1，R05-P1-6 = F1-9.7 未修项）
- 位置：`internal/infra/auth/session_service.go:45-70`、`internal/pkg/config/config.proto`。
- 方案：`config.proto` 增加 `security.sessions.max_per_user`（默认如 50，0=不限）；
  执行 `task generate-config`；`CreateSessionAndTokens` 超限删除最旧（按 expire_at/created_at）
  会话后再创建。补配置解析与淘汰测试。

### G3-6 🟠 审计落库无超时（P1，R01-F7-6 未修项）
- 位置：`pkg/grpc/interceptor/audit.go:70`（`a.repo.Insert(context.Background(), entry)`）。
- 方案：`context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)` 或异步落库
  （带缓冲 channel + worker，溢出丢弃并 Warn）；失败不得阻塞/影响 RPC 响应。补超时测试。

### G3-7 🟡 DeleteSessionsByUser 循环删除非事务（P2，R05-P2-7）
- 位置：`internal/infra/auth/session_service.go:156-179`。
- 方案：包 `RunInTx`（复用 documentdb `clients.InTx` 模式）或改用批量删除；失败时返回
  已删除数量并 Warn。与 G3-3 协调同文件改动。

### G3-8 🟡 gRPC metadata 同 key 多值未拒绝（P2，R01-P2-2）
- 位置：`pkg/grpc/interceptor/jwt.go:228-234`（`firstMetadataValue`）。
- 方案：凭证类 key（authorization/x-api-key/cookie）`len(values) > 1` 时返回
  "multiple credentials provided"；补测试。

### G3-9 🟡 dummyPasswordHash 首次调用时序差异（P2，R05-P2-9）
- 位置：`internal/app/client/account.go:151-159`。
- 方案：包初始化（`func init()` 或 provider 构造时）预热一次 `dummyPasswordHash()`。

### G3-10 🟡 clientgrpc handler 零测试 + 两处小修（P1 测试缺口 R04-P1-1；P2/P3 R04-P2-4、R04-P3-2）
- 方案：
  1. 为 `internal/api/clientgrpc` 建 `account_test.go`：mock use-case，覆盖 Magic URL 响应
     仅含 challengeId、DeleteSessions keepCurrent 透传、错误码映射；
  2. 移除 `SignOut` 重复的 Principal 校验（`account.go:60-67`），或改为统一 `requirePrincipal`；
  3. `DeleteSession` handler 补 `session_id == ""` → InvalidArgument（`account.go:114-119`）。
- 注意：本文件 G10 还会改 `DeleteFactor`（见 §10 G10-4），并行时各改各的方法，合序在后者优先。

### G3-11 🟢 杂项（P3）
- `internal/app/client/mfa_test.go:108-110`：`TestAccount_CreateTOTPFactor_RequiresJWTSecret`
  补 `if testing.Short() { t.Skip(...) }`（R04-P3-1、R05-P2-10）。
- `internal/app/client/account.go:183-186`：SignUp 频控移到 project 校验之后，
  或频控键加 project_id 维度（R05-P3-11）。

**验收**：`task generate-config` 无异常 diff；`go vet ./internal/app/client/... ./internal/infra/auth/... ./pkg/grpc/interceptor/... ./internal/api/clientgrpc/...`；
`go test -short` 上述包全绿（含新增测试）。

---

## 4. G4 serverhttp 传输层（文件：internal/api/serverhttp/）

### G4-1 🟠 抽取公共 httpAuth + 统一多凭证拒绝（P1，R01-P1-1、R03-P2-2、R03-P2-3 = F2-4-b 未修项）
- 位置：`internal/api/serverhttp/file_handler.go:738-805`、`functions_handler.go:173-239`。
- 方案：新增 `internal/api/serverhttp/auth.go`，抽取 `authenticate/authorize/projectID`
  公共实现；多凭证并存（X-Api-Key / Authorization / session cookie 任意两种）返回 401，
  与 gRPC `extractCredential` 语义一致；两个 handler 复用。保留现有测试并补多凭证拒绝用例。

### G4-2 🟠 Preview 源文件整体读内存 + 缺畸形尺寸单测（P1，R03-P1-1）
- 位置：`internal/api/serverhttp/file_handler.go:645-653`。
- 方案：先读有限 header（如 `io.LimitReader(src, 512KB)`）交给 `image.DecodeConfig`，
  尺寸超限直接拒绝、不再读全量；通过后才受限读取全文件。补纯单元测试：构造 IHDR
  width=8193 的合法 PNG 头字节流，断言 400 且未读全量。

### G4-3 🟡 公开 bucket 匿名路径 bucketID 未校验格式（P2，R03-P2-4）
- 位置：`internal/api/serverhttp/file_handler.go:545-553`。
- 方案：resolve 前校验 bucketID（`idgen.ID(bucketID).IsValid()` + 字符集/长度），非法返回 400。

### G4-4 🟢 Preview 解码失败错误码（P3，R03-P3-6）
- 位置：`internal/api/serverhttp/file_handler.go:655-667`。
- 方案：非图片/损坏图片返回 400/415 而非 500。

**验收**：`go vet ./internal/api/serverhttp/...`；`go test -short ./internal/api/serverhttp/...` 全绿。

---

## 5. G5 documentdb 与 crud 收尾（文件：internal/infra/documentdb/、pkg/crud/、internal/api/servergrpc/databases.go）

### G5-1 🟠 DeleteIndex DDL 与元数据未事务化（P1，R02-P1-1）
- 位置：`internal/infra/documentdb/postgres.go:263-279`。
- 方案：整体包 `p.db.RunInTx`（对齐 DeleteAttribute/DeleteCollection）；补删索引后同名重建测试。

### G5-2 🟠 CreateDatabase 元数据未接入事务（P1，R02-P1-2）
- 位置：`internal/infra/documentdb/postgres.go:83-104`。
- 方案：整体包 `RunInTx`，INSERT 改 `p.conn(txCtx).NewInsert()`；补失败回滚测试。

### G5-3 🟠 ListCollections 未使用 p.conn(ctx)（P1，R02-P1-3）
- 位置：`internal/infra/documentdb/postgres.go:227-238`。
- 方案：COUNT 与主查询均改 `p.conn(ctx)`；顺手排查同文件其他裸 `p.db.New*` 调用点。

### G5-4 🟠 UpdateDocument 仅改 permissions 不刷新审计列（P1，R02-P1-4）
- 位置：`internal/infra/documentdb/postgres.go:746-770`。
- 方案：仅权限变更分支也 SET `_updated_at = NOW()`、`_updated_by`；补测试。

### G5-5 🟡 Upsert advisory lock 键序列化碰撞（P2，R02-P2-2）
- 位置：`internal/infra/documentdb/postgres.go:664-672`。
- 方案：冲突值序列化改长度前缀或 JSON 编码后再 hashtext；补含 `\x00` 值不串扰测试（可为纯函数单测）。

### G5-6 🟡 ListDocuments 默认排序缺 `_id` tiebreaker（P2，R08-P2-4）
- 位置：`internal/infra/documentdb/postgres.go:1912`。
- 方案：`ORDER BY d._created_at DESC, d._id DESC`；补同 _created_at 多行分页稳定性测试。

### G5-7 🟡 pkg/crud contains/notcontains 未转义 LIKE（P2，R08-P2-2）
- 位置：`pkg/crud/filter.go:306-312`。
- 方案：加 `escapeLikePattern` + `ESCAPE '\'`（对齐 `postgres.go:655-659`）；补含 `%`/`_` 值测试。

### G5-8 🟢 小项（P3）
- `internal/api/servergrpc/databases.go:122-132`：CreateCollection 移除尾随 GetCollection 重查，
  直接返回 adapter 结果（R02-P3-1）。
- `internal/infra/documentdb/postgres_test.go:681-773`：补 `page_size > maxQueryLimit` clamp 用例（R08-P3-7）。
- R02-P2-1（Count/List 非原子快照）：代码不改，在 `docs/developer/06-databases.md` 补行为说明。

**验收**：`go vet ./internal/infra/documentdb/... ./pkg/crud/... ./internal/api/servergrpc/...`；
`go test -short` 相关包全绿；集成测试由 CI 兜底。

---

## 6. G6 Functions / Storage / Worker 收尾（文件：internal/app/functions/、internal/app/storage/、internal/infra/functions/docker.go、internal/infra/bun/bunrepo/function_repo.go、cmd/worker/）

> ⚠️ **依赖 G2 合入**（使用 `internal/app/shared` 的 `RequirePlatformAdmin`）。

### G6-1 🟠 extractZip 实际解压字节无硬限制（zip bomb 绕过）（P1，R07-P1-1）
- 位置：`internal/infra/functions/docker.go:331-336,355-360`。
- 方案：写入侧包带预算的 `io.Writer`（按实际字节计数，超额报错并清理半成品），
  不再仅信任 `UncompressedSize64`；补伪造声明大小的恶意 zip 测试（纯函数可测）。

### G6-2 🟠 CreateFile 顺序导致孤儿元数据（P1，R07-P1-2）
- 位置：`internal/app/storage/storage.go:233-240`。
- 方案：`EnsureBucket` 提前到创建文档之前；EnsureBucket/Put 失败均回滚删除已建文档；补测试。

### G6-3 🟠 function ID 允许大写 → Docker 镜像名非法（P1，R08-P1-1）
- 位置：`internal/app/functions/management.go:17`、`internal/infra/functions/docker.go:90-96`。
- 方案：`functionIDPattern` 收紧为 `^[a-z0-9][a-z0-9_-]{0,63}$`；`security_test.go` 同步
  （`Fn-1` 改为非法用例）；`imageName` 对 functionID 做 `strings.ToLower` 兜底兼容历史数据。

### G6-4 🟠 CreateBucket use-case 守卫（P1，R06-P1 的 storage 部分）
- 位置：`internal/app/storage/storage.go:61-97`。
- 方案：调用 `appshared.RequirePlatformAdmin(ctx)`（G2 提供）或按 G2 规则表对齐的角色校验。

### G6-5 🟡 function_repo 写路径补 project_id 过滤（P2，R07-P2-3、R08-P2-3）
- 位置：`internal/infra/bun/bunrepo/function_repo.go:135-138`（SetVariables DELETE）、
  `:60-64`（UpdateFunction）、`:113-117`（UpdateDeployment）。
- 方案：三处 WHERE 均加 `project_id = ?`（UpdateDeployment 同时带 function_id）；补跨项目不可见测试。

### G6-6 🟡 分片上传并发竞态（P2，R07-P2-4、R07-P2-5）
- 位置：`internal/app/storage/uploads.go:158-195`（CompleteUpload 锁前快照）、`:259-286`（AbortUpload 无锁）。
- 方案：`LockComplete` 成功后重新 `Get` 会话再判缺片；`AbortUpload` 尝试获取 complete 锁，
  获取失败返回 FailedPrecondition 提示重试。补并发测试（可用 miniredis）。

### G6-7 🟡 readBuildOutput Scanner 512KB 上限丢日志（P2，R07-P2-6）
- 位置：`internal/infra/functions/docker.go:481`。
- 方案：上限提升（如 4MB）或超长行降级为 `io.Reader` 截断读取；补超长行测试。

### G6-8 🟢 小项（P3）
- `internal/app/functions/executions.go:80-82`：`data` 校验必须是 JSON object（R07-P3-7）。
- `internal/app/functions/mocks_test.go:110-119`：mock DeleteDeployment 补 projectID 断言（R08-P3-5）。
- `cmd/worker/worker.go:38-39,151-164`：重试计数持久化（ExecutionRecord 或 Redis）——可做可缓，
  不做则在代码注释标注已知限制（R07-P3-8）。
- `internal/app/storage/cleanup_integration_test.go`：DeleteBucket 测试补「files 文档已删」显式断言（R07 结论 3）。
- `pkg/idgen` 语义：在 `AGENTS.md` 或 `pkg/idgen/id.go` 注释明确 `IsValid` 仅判非空、
  字符集校验归各 use-case（R08-P3-6）。

**验收**：`go vet ./internal/app/functions/... ./internal/app/storage/... ./internal/infra/functions/... ./internal/infra/bun/bunrepo/... ./cmd/worker/...`；
`go test -short` 相关包全绿；Docker/MinIO 集成由 CI 兜底（G1 后 CI 可用）。

---

## 7. G7 基础设施收尾（文件：internal/infra/clients/、internal/infra/idgen/、internal/infra/health/、cmd/server/）

### G7-1 🟠 连接池零值陷阱（P1，R09-P1-1 = F7-6f 未修项）
- 位置：`internal/infra/clients/database.go:76-84`。
- 方案：pool 字段 ≤0 时落安全默认（如 max_open=4*GOMAXPROCS、max_idle=max_open）并 Warn；
  补零值/负值/正常值测试。

### G7-2 🟠 SQL 日志脱敏绕过（P1，R09-P1-2）
- 位置：`internal/infra/clients/dbhook.go:82,89-94`。
- 方案：正则扩展覆盖 `INSERT ... (col) VALUES (...)` 形式；敏感列名单加 `setup_token`；
  补 INSERT 场景脱敏单测。

### G7-3 🟡 JWT 弱密钥子串绕过 + 测试（P2，R01-P2-3、R01-P3-4）
- 位置：`cmd/server/provides.go:71-91`。
- 方案：含已知弱子串（`change-me` 等）即拒绝（不仅是 Warn）；新增 `provides_test.go`
  覆盖空值/短值/精确弱值/子串弱值/正常值。

### G7-4 🟡 random ID Redis 集合容量上限（P2，R09-P2-2）
- 位置：`internal/infra/idgen/random_redis.go:15,31`。
- 方案：`SCARD` 超阈值（如 100 万）拒绝生成并 Warn，或按时间窗口分片 key；补测试（miniredis）。

### G7-5 🟡 health 缓存并发打穿（P2，R09-P2-3）
- 位置：`internal/infra/health/checks.go:87-114`。
- 方案：缓存失效加 singleflight/互斥；TTL 可配置化（可选）。

### G7-6 🟢 小项（P2/P3）
- `cmd/server/provides.go:103-107`：注释说明 Lynx 逆序停止的实际顺序与理由（R09-P2-4）。
- `cmd/server/main.go:47-50`：`cleanup()` 包 `context.WithTimeout`（如 10s）+ 起止日志（R09-P3-1）。
- `internal/infra/idgen/service.go:150-155`：显式失败为既定设计——在
  `docs/developer/13-operations.md` 补运维说明（R09-P2-1）。

**验收**：`go vet ./internal/infra/... ./cmd/server/...`；`go test` 无外部依赖包全绿。

---

## 8. G8 Console 前端收尾（文件：console/src/）

### G8-1 🟠 路由级权限守卫（P1，R11-P1-1 = F9-3-i 残留）
- 位置：`console/src/App.tsx:75-131`。
- 方案：新增 `RequireRole`（基于 `useAdminRole`）：viewer 访问写路由（*/new、*/edit 等）
  重定向或 NotFound；与按钮 gating 对齐。

### G8-2 🟠 变量页掩码覆写真实 secret（P1，R06-P1）
- 位置：`console/src/routes/functions/pages.tsx:450-467,483-489,530-533,655-661`。
- 方案：GetVariables 回填时掩码值显示为占位（空 + placeholder「已设置，仅设置时可见」）；
  保存只提交用户实际编辑/新增的 key，未触碰项不提交（SetVariables 全量替换语义下，
  未编辑项需保留原值——改为：提交时过滤掉值仍为掩码且未修改的项是不行的，
  因为 SetVariables 全量替换会删除它们。**正确做法**：保持全量提交，但掩码且未编辑的项
  不纳入提交会丢 key，因此改为「掩码项未编辑则整行保持不提交」不可行——最终方案：
  页面改为逐 key 编辑模式，或后端 SetVariables 支持「掩码值=保持不变」约定。
  **实施时先与后端确认**：推荐后端约定——SetVariables 请求中值等于掩码串 `******` 的 key
  保留旧值不覆盖（G8 只改前端时需后端配合则标注跨端依赖，交由 G8 一并小改
  `internal/app/functions/variables.go`）。
- 验证：编辑单 key 保存后其他 key 原值不变。

### G8-3 🟠 双 toast 残留（P1，R11-P1-3、R11-P1-4、R11-P2-8）
- 位置：`console/src/routes/storage/pages.tsx:519-520`、`console/src/routes/Login.tsx:67-70`、
  各 `handleBulkDelete`。
- 方案：API 客户端支持按请求标记跳过全局 toast；Login、generateShare、批量操作使用该标记，
  错误由页面统一展示。

### G8-4 🟠 路由级 React.lazy + ErrorBoundary（P1，R11-P1-5 = F9-3-j 未修项）
- 位置：`console/src/App.tsx:1-147`。
- 方案：页面组件改 `React.lazy` + `<Suspense>`；路由出口/每个 lazy 段加 ErrorBoundary。

### G8-5 🟠 API Key 详情页删除按钮角色 gating（P1，R11-P1-2）
- 位置：`console/src/routes/api-keys/pages.tsx:258`。
- 方案：`isPlatformAdmin(role)` 条件渲染，与列表页一致。

### G8-6 🟡 小项（P2/P3）
- 新建页父资源校验：`CollectionNewPage`/`DatabaseNewPage` 等查询父资源，不存在渲染 NotFound（R11-P2-9）。
- `invalidateQueries` 用完整 key `["buckets", projectId]` 等（R11-P2-10）。
- chunked-uploader `run` 用 ref/reducer 消除 stale closure（R11-P2-7）。
- `UserEditPage` 保存后用响应重建表单（R11-P3-11）。
- `Login.tsx:50-54` setup-status 探测失败显示错误 + 重试按钮（R04-P3-3）。
- 补最小单元测试：`useAdminRole`、批量删除汇总、变量页提交过滤（R11-P2-6）。

**验收**：`npx tsc --noEmit`（console/）；`pnpm lint`；新增测试通过；`task console-build`。

---

## 9. G9 SDK / CLI 收尾（文件：sdk/go/、sdk/typescript/、cmd/client/）

### G9-1 🟡 Go SDK Account 缺失方法（P2，R04-P2-3）
- 位置：`sdk/go/client/account.go:1-80`。
- 方案：补 `ListSessions/DeleteSession/DeleteSessions(keepCurrent)/CreateJWT/CreateMagicURLSession`
  等 Client Account 方法（对照 `proto/client/v1/account.proto` RPC 清单）；补 bufconn 测试。

### G9-2 🟡 Go SDK 新增方法错误路径测试（P2，R12-P2-2）
- 位置：`sdk/go/server/services_test.go:395-465`。
- 方案：每个 F8-4 新增方法补至少一个错误用例（NotFound/PermissionDenied 透传）。

### G9-3 🟡 TS 契约测试校验 HTTP 绑定（P2，R12-P2-3）
- 位置：`sdk/typescript/src/__tests__/contract.test.ts:286-313`。
- 方案：从 swagger JSON 读取每个 operation 的 method/path/parameters，与 SDK 方法实际请求
  （mock fetch 捕获）结构化比对；至少覆盖 Create/Update/Delete 写方法。

### G9-4 🟡 TS `AuthResult.tokens` 改可选（P2，R12-P2-6）
- 位置：`sdk/typescript/src/types.ts:32-38`。
- 方案：`tokens?: TokenBundle`，JSDoc 注明 `mfa_required=true` 时无 tokens；检查 SDK 内引用点。

### G9-5 🟢 小项（P2/P3）
- `sdk/go/client/token_test.go`：补 `~/tokens.json` 展开测试（R12-P2-5）；
  `expandHome` 注释说明仅支持 `~` 与 `~/`（R12-P3-7）。
- `cmd/client/cmd/functions.go:211,225,432`：help 文案区分「gRPC 通道 8MiB」与
  「大代码包走 multipart（50MiB）」（R12-P3-8）。
- TS SDK 方法名映射表补进 `sdk/typescript` README 或 `docs/developer/12-sdk.md`（R10-P3-10）。

**验收**：`go test ./sdk/go/... ./cmd/client/...`；`cd sdk/typescript && npx tsc --noEmit && npm run test`；`task sdk-demo-build`。

---

## 10. G10 Proto / 契约收尾（文件：proto/**、internal/infra/server/grpc.go、pkg/grpc/interceptor/apikey_scope.go、internal/api/clientgrpc/account.go、genproto 重新生成）

> ⚠️ 需 `task generate-proto` 重新生成；**最后批次、独立分支**。
> 本批次触碰 `internal/api/clientgrpc/account.go` 的 `DeleteFactor`（与 G3-10 同文件，先确保 G3 已合入）。

### G10-1 🟠 method_auth 覆盖率不足（P1，R10-P1-1 = F11-4-1 未修项）
- 方案：至少为敏感方法显式补方法级 `method_auth`：SetVariables/GetVariables、CreateFileToken、
  CreateUserToken、APIKeysService 全部、AdminsService 全部；随后逐步覆盖其余 RPC
  （以 `protoregistry` 遍历输出清单核对，143 个 RPC 全覆盖或记录在案）。

### G10-2 🟠 API Key scope 硬编码无一致性断言（P1，R10-P1-5 = F11-4-2 未修项）
- 位置：`pkg/grpc/interceptor/apikey_scope.go:20-111`、`internal/infra/server/grpc.go:116-119`。
- 方案：启动期断言 `apiKeyScopeRules` 覆盖集合 == 全部 `ACCESS_API_KEY` 方法集合
  （不一致直接 panic，fail-closed）；可选增强：`MethodAuth` 扩展 scope 字段由 proto 推导（可后置）。

### G10-3 🟠 契约一致性补强（P1，R10-P1-4、R10-P1-6）
- `x-torchwood-access`：生成后脚本或启动期断言 swagger 扩展与 `collectMethodsByAccess` 一致。
- `proto/server/v1/databases.proto:197` `UpdateCollectionRequest.name` 与
  `proto/console/v1/admins.proto:120` `UpdateAdminRequest.role` 改 `optional string`；
  同步更新 handler/use-case 的空值语义（未提供=不修改）。

### G10-4 🟠 DeleteFactor 契约缺 code 字段（P1，R05-P1-4）
- 位置：`proto/client/v1/account.proto:590`（DeleteFactorRequest）、
  `internal/api/clientgrpc/account.go:376-386`。
- 方案：`DeleteFactorRequest` 增加 `string code = 2;`（可选，未传时 verified 因子仍拒绝）；
  handler 透传；重新生成；TS SDK `deleteFactor` 同步加参（与 G9 协调，SDK 改动可放本批次）。

### G10-5 🟡 注释与文档同步（P2）
- `proto/server/v1/functions.proto:158-160`：GetVariables 注释改为「返回掩码值，
  真实值仅 SetVariables 请求/响应中可见一次」（R03-P3-5、R06-P2）。
- `proto/client/v1/account.proto:123-127`：DeleteSessions 注释声明 REST 经 query 传
  `keep_current`（R12-P2-4，维持 query 方案不改 body）。
- `proto/shared/v1/openapi.proto`：dead code——内容并入 `docs/developer/09-api-guide.md`
  后删除该文件（R10-P2-7）。
- 时间戳 int64→RFC3339 breaking change：在 `docs/`（如 developer/12-sdk.md 或 CHANGELOG）
  显式声明（R10-P2-8）。
- 「删除字段一律 reserved」写入 `AGENTS.md` 或 09-api-guide 的 proto 规范节（R10-P2-9）。
- REST 保留字自定义动词迁移（`:count`/`:bulkUpdate`）：**本批次不做**，在
  `docs/roadmap.md` 或 fix-plan 记录为 backlog，提示清理历史保留字 id 数据（R10-P1-3）。

**验收**：`task generate-proto` 后 `go build ./...`；`buf lint` 通过；启动期断言生效；
`go test -short` 相关包全绿。

---

## 11. 文件冲突矩阵

| 文件 | 批次 | 冲突风险 |
|------|------|----------|
| internal/api/clientgrpc/account.go | G3（SignOut/DeleteSession/测试）、G10（DeleteFactor） | **G10 在 G3 合入后执行** |
| internal/app/storage/storage.go | G6（G6-2/G6-4） | 唯一（G2 不碰，CreateBucket 守卫归 G6） |
| internal/app/functions/variables.go | G2（守卫）、G8-2（掩码约定，若需后端配合）、G6（无） | G8-2 若改后端需与 G2 合序 |
| pkg/grpc/interceptor/jwt.go / audit.go | G3 | 唯一 |
| pkg/grpc/interceptor/admin_roles.go | G2 | 唯一 |
| pkg/grpc/interceptor/apikey_scope.go | G10 | 唯一 |
| internal/pkg/config/config.proto | G3 | 需 generate-config |
| proto/** | G10 | 需 generate-proto |
| .github/workflows/ci.yml、Taskfile.yml | G1 | 唯一 |
| cmd/server/provides.go | G7 | 唯一 |
| internal/infra/documentdb/postgres.go | G5 | 唯一 |
| internal/infra/functions/docker.go | G6 | 唯一 |
| internal/infra/bun/bunrepo/function_repo.go | G6 | 唯一 |
| console/src/** | G8 | 唯一 |
| sdk/**、cmd/client/** | G9（DeleteFactor SDK 同步归 G10） | G9 与 G10 合序 |

## 12. 回归验证清单（全部批次完成后）

1. `task generate-all`（proto/config/wire）无异常 diff。
2. `task test`（需本地基础设施）：新增测试全绿，总体不少于修复前。
3. `task lint`、`task build`、`task console-build`、`task sdk-demo-build`。
4. 手工安全冒烟：
   - viewer admin 调 CreateFunction/CreateDeployment/CreateUser → PermissionDenied；
   - 一次性 JWT 二次使用 → Unauthenticated；
   - 未注册邮箱连续 10 次失败登录 → 目标已注册邮箱不受影响；
   - HTTP 请求同时携带 X-Api-Key + Cookie → 401；
   - Console 变量页编辑单 key 保存 → 其他 key 原值不变；
   - 构造声明 1KB 实际 100MB 的 zip 部署包 → 拒绝且不耗尽磁盘。
5. CI 全绿（含 buf lint、TS SDK 测试、demo 构建、docker 集成测试）。
