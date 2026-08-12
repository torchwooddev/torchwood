# Round-2 遗留 Backlog 细化（下一轮修复任务书）

> 来源：`docs/review/round2/fix-plan.md`、`fix-report.md` §「需上级关注」、`docs/roadmap.md` §2.3 备注。
> 本文档是三项记录在案 backlog 的执行任务书，供下一轮接力 agent 按此实施。
> 每项均含：现状（带代码锚点）、目标、方案、改动清单、验收标准。

---

## B1. 邮箱变更完整 staging（G3-2 A 档，P1，R05-P1-2）

### 现状

改邮箱**未经新邮箱验证即生效**。Round-2 仅落地 B 档最小缓解：

- `internal/app/client/account.go:442-459`：`UpdateAccount` 中 email 变更直接写入 `updates["email"]` 并置 `email_verified=false`；
- `internal/app/client/account.go:502-513`：变更后向**旧邮箱**发安全通知（含撤销指引），新邮箱验证依赖用户另走既有 `CreateVerification` 流程；
- `internal/app/client/account.go:486-490`：敏感变更先撤全部会话再提交（G3-3 已修）。

风险残留：攻击者（或用户笔误）可把账号邮箱改到任意地址并立即生效，旧邮箱失主只能靠通知邮件事后补救。

### 目标（A 档）

邮箱变更走 staging：新邮箱验证通过前 `email` 保持旧值（旧邮箱仍可登录/找回），验证通过后才切换。

### 前置契约决策（需产品确认，建议按推荐执行）

`UpdateAccountRequest`（`proto/client/v1/account.proto:522-528`，现有字段 1-4）没有验证链接回调地址字段，A 档需要它。推荐：

- `UpdateAccountRequest` 增加 `string url = 5;`（改邮箱时必填，用于拼接验证链接，语义同 `CreateVerificationRequest.url`、`CreateRecoveryRequest.url`）；
- 新增 RPC `ConfirmEmailChange`（见下）。两者均为 proto 变更，需 `task generate-proto` 并遵守 `reserved`/optional/时间戳规范（`AGENTS.md` + `docs/developer/09-api-guide.md` §1.4）。

### 方案（推荐）

复用既有 account-token 机制，不引入新存储：

1. **新 purpose**：`internal/domain/auth/account_token.go` 增加 `AccountTokenPurposeEmailChange = "email_change"`；`internal/infra/auth/account_token_redis.go` 仿照 verification/recovery 增加 `CreateEmailChangeToken` / `VerifyEmailChangeToken`。注意：现有 `verifyToken` 只返回 error，email_change 需要取回 record 中的新邮箱（`createToken` 已把 email 写入 record），需新增返回 `(email string, error)` 的方法，消费仍走 GETDEL 原子。
2. **UpdateAccount 改 staging**（`internal/app/client/account.go`）：
   - email 变更时不再写 `updates["email"]`，改为写 `updates["pending_email"]` + 签发 email_change token + 向**新邮箱**发验证邮件（链接 = `cmd.url` + userID + secret），向**旧邮箱**的安全通知保留（B 档成果不回退）；
   - `url` 为空且要改邮箱时返回 `InvalidArgument`；
   - 撤会话时机：staging 阶段不撤，改到 ConfirmEmailChange 成功时撤（保持 G3-3 的「先撤会话后提交」语义）。
3. **新 RPC `ConfirmEmailChange`**：`proto/client/v1/account.proto` 增加
   `rpc ConfirmEmailChange(ConfirmEmailChangeRequest) returns (Account)`（REST 建议 `PUT /v1/account/email-change`，body `*`），请求含 `user_id`、`secret`（与 UpdateVerification 同形）；use-case 消费 token → 校验 record 的新邮箱未被他人占用（复用现有 ListDocuments email 查重）→ 写 `email=新值`、删 `pending_email`、`email_verified=true` → 先撤会话再提交。
   - 必须带 proto authz 注解（`collectMethodsByAccess` 启动期断言，见 `internal/infra/server/grpc.go:138`）；该方法应为已登录用户可调用（USER 级），注意 user_id 必须等于 principal.UserID。
4. **gRPC handler**：`internal/api/clientgrpc/account.go` 增加对应方法，薄薄一层透传。
5. **`mapUserDoc`**：`pending_email` 不暴露在响应里（属敏感暂存字段，确认现有映射不会泄漏）。

### 测试要求

- 「未验证前旧邮箱仍可登录/找回」集成测试（SignUp → UpdateAccount 改邮箱 → 旧邮箱登录成功、新邮箱登录失败 → ConfirmEmailChange → 新邮箱生效、旧邮箱失效）；
- token 一次性：Confirm 二次使用 → Unauthenticated；
- 新邮箱已被占用 → AlreadyExists；
- 单元测试覆盖 purpose 隔离（email_change token 不能当 verification 用，参照 `account_token_redis_test.go:110`）。

### 验收

`task generate-proto` 后 `go build ./...` 绿；`go vet` + `go test -short ./...` 绿；新集成测试在 CI（有 PG/Redis）全绿；`grpc_swagger_test.go` 一致性断言通过。

---

## B2. worker 重试计数持久化（G6 缓修项，P3，R07-P3-8）

### 现状

`cmd/worker/worker.go:24-25`：`maxProcessAttempts = 3`；`:150-171` `requeue` 用进程内存 `map[string]int` 计数，**worker 重启即清零**，瞬时失败任务实际可被重试超过上限（每次重启多出 ≤3 次）。超限兜底 `MarkExecutionFailed`（`internal/app/functions/executions.go:366-379`）保证不会无限重试。代码注释已标注已知限制与未来方案。

### 目标

重试计数跨重启不丢，超限判定精确。

### 方案（推荐 (a)，(b) 为可选彻底档）

- **(a) payload 内嵌 attempt（推荐，无 schema 变更）**：重抛回队前改写 payload——unmarshal → `attempt+1` → marshal 再 `Enqueue`。`requeue` 从 payload 读计数而非内存 map；超限（attempt > maxProcessAttempts）走 `failPayload`。队列消息本身是唯一事实来源，重启/多 worker 副本天然正确。注意保持 payload 字段兼容（旧消息无 attempt 字段视为 0），并确认 `ProcessExecutionPayload`（`executions.go:355-364`）对多余字段容忍（`json.Unmarshal` 默认容忍，OK）。
- **(b) ExecutionRecord 加 `retry_count` 字段（可选）**：每次失败原子自增。需确认 executions 集合的存储适配器（bun 静态表还是 documentdb 动态集合）再评估迁移成本；若做，与 (a) 二选一，不要双写两套计数。

### 测试要求

- 单测：payload attempt 递增、超限不再回队并调用 MarkExecutionFailed、旧格式 payload（无 attempt）兼容；
- 现有 worker 测试（`cmd/worker` 包）全绿。

### 验收

`go vet` + `go test -short ./cmd/worker/... ./internal/app/functions/...` 绿；同步删除/更新 `worker.go:150-157` 的「已知限制」注释为新的机制说明。

---

## B3. REST 保留字自定义动词迁移（R10-P1-3，P1，breaking change）

### 现状

字面量路由与 `{id}` 通配路由冲突，目前靠服务端创建校验兜底：

- `proto/server/v1/databases.proto:141-149`：`documents/count`、`documents/bulk`、`documents/bulk/delete`；
- `proto/server/v1/functions.proto:68-71`：`functions/runtimes`、`functions/specifications`；
- 兜底校验：`internal/app/server/databases.go:21-27`（拒绝 id=`count`/`bulk`）、`internal/app/functions/management.go:19-25`（拒绝 id=`runtimes`/`specifications`）；
- 兜底只能挡新数据：**历史已存在的保留字 id 资源在 REST 下的 Get/Update/Delete 会被字面量路由遮蔽**（`docs/review/round2/reports/10-proto-codegen.md` §3）。

### 目标

迁移为自定义动词风格，根除路由冲突，并给出历史数据处理方案。

### 方案

1. **proto 迁移**：
   - databases：`get: ".../documents/count"` → `get: ".../documents:count"`；`patch: ".../documents/bulk"` → `patch: ".../documents:bulkUpdate"`；`post: ".../documents/bulk/delete"` → `post: ".../documents:bulkDelete"`（最终动词命名遵循 `docs/developer/09-api-guide.md` §1.4 的 OpenAPI 建模约定，如已有惯例以惯例为准）；
   - functions：`/v1/server/functions/runtimes` → `/v1/server/functions:runtimes`，`/v1/server/functions/specifications` → `/v1/server/functions:specifications`；
   - 删除 proto 中「保留字说明」注释（databases.proto:115-116、:288-289；functions.proto:64-65、:187-188），改为说明自定义动词；
   - `task generate-proto`。
2. **解除服务端兜底校验**：`databases.go:21-27` 与 `management.go:19-25` 的保留字拒绝逻辑移除（迁移后 `count`/`bulk`/`runtimes`/`specifications` 成为合法 id）；对应测试 `internal/app/server/databases_reserved_test.go` 改为「保留字 id 可正常创建并读写」。
3. **数据清理**：检查并提供迁移说明——若任何环境已存在保留字 id 的文档/函数（此前能被创建说明是校验上线前的数据），需在升级说明中提示先重命名/删除；在 `docs/roadmap.md:152-157` 的 backlog 备注更新为「已迁移 + 升级指引」。
4. **影响面同步**：
   - SDK：`sdk/go`、`sdk/typescript` 中涉及这些路径的封装同步更新，各自测试跑绿；
   - Console 前端与 CLI：确认无硬编码旧路径（`cmd/client` 走 sdk/go InvokeJSON，一般无感）；
   - 文档：`docs/developer/09-api-guide.md`、`docs/roadmap.md`（§2.3 表与 backlog 备注）、`docs/developer/12-sdk.md` 补 breaking change 声明（旧 REST 路径废弃）；
   - `internal/infra/server/grpc_swagger_test.go` 的 swagger/method_auth 一致性断言必须保持绿。
5. **兼容性决策（需产品确认）**：是否在过渡期同时保留旧字面量路由（双路由）。推荐**不保留**（这正是要根除的冲突），直接 breaking + 升级说明。

### 验收

`task generate-proto` 无异常 diff、`buf lint` 通过；`go build ./...` + `go vet` + `go test -short ./...` 全绿；REST 层集成测试（`internal/api/` 相关）在 CI 绿；`id="count"` 的文档创建后 REST Get/Update/Delete 语义正确的集成测试通过。

---

## 执行约束（对三项通用）

- 最小改动、不引入新依赖；proto 变更遵守 `reserved`/optional 规范；provider 变更跑 `task wire-all`；config proto 变更跑 `task generate-config`。
- 本地无 PG/Redis/MinIO/Docker：单元测试（`-short`）必须全绿，集成测试交给 CI，推送后必须监视 CI 至全绿才算完成。
- 修改 Console 代码需先 `task console-build` 再 `task build`。
- 完成后将结果（含每项 ✅/❌、关键决策、CI run 链接）写入 `docs/review/round2/backlog-fix-report.md`。
