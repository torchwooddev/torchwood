# 复审报告（Round 3）：07 - Storage / Functions / Worker

> 审查范围：`internal/app/storage/*`、`internal/app/functions/*`、`internal/infra/storage/*`、
> `internal/infra/functions/*`、`internal/infra/messaging/*`、`internal/infra/queue/*`、
> `cmd/worker/*`；交叉阅读 `internal/api/serverhttp/functions_handler.go`、
> `internal/api/serverhttp/file_handler.go`、`internal/api/serverhttp/auth.go`、
> `internal/infra/bun/bunrepo/function_repo.go`、`internal/api/servergrpc/functions.go`。
>
> 审查基准：当前工作区代码；对照 `docs/review/prompts/07-storage-functions-worker.md`、
> `docs/implementation-storage-chunked-upload.md`、`docs/implementation-functions-executor.md`、
> Round 2 报告 `docs/review/round2/reports/07-storage-functions-worker.md`、
> backlog B2（`docs/review/round2/backlog-next-round.md`）。
> 执行方式：只读静态审查；**未修改源代码**；未跑 Docker 集成测试；本子代理无 shell，
> 未执行 `go vet` / `go test`（相关单测已通读，含 `cmd/worker/requeue_test.go`、
> `consume_test.go`、`internal/infra/functions/docker_test.go`）。

---

## 摘要

Round 2 在本模块内登记的 P1/P2（zip 实际字节预算、CreateFile 顺序、complete/abort 互斥、
SetVariables 的 `project_id`、构建日志 Scanner 缓冲、data 必须为 JSON object）**均已落地**，
且有对应单测。B2（worker 重试计数写入 payload）**已实现并通过单测**，旧消息兼容、
超限 `MarkExecutionFailed` 路径正确。

本轮唯一新的 **P1** 是 G12 落地不完整：HTTP multipart 部署上传在 handler 层完成
API key / admin 鉴权后，**没有把 Principal 注入 `context`**，导致
`CreateDeployment` 的 `RequireServerWriteActor` 对 Console / 大包上传一律
`Unauthenticated`。gRPC ≤1MiB 路径不受影响。未发现任意对象读写、容器逃逸或
可被攻击者触发的数据损坏；Redis List 崩溃丢任务仍是已声明的 MVP 设计权衡，不升 P0。

---

## 1. 已核实健康

| 项 | 结论 | 证据（文件路径:行号） | 说明 |
|----|------|----------------------|------|
| **F5-1** Function ID / zip 路径穿越 | ✅ 保持 | `management.go:19`、`deployments.go:177-209`；`security_test.go:16-97` | 小写字符集 + `filepath.Base` + `assertZipDir`；测试覆盖 `../../etc/passwd`。 |
| **F5-2** 跨项目 IDOR | ✅ 保持 | `deployments.go:126-169`；`function_repo.go:83-133` | Get/Delete 均带 `project_id`；use-case 前置 `GetFunction`。 |
| **F5-3** 解压 0644 + USER 非 root | ✅ 代码保持 | `docker.go:403`、`456`、`462` | 仍未在本机跑 Docker 集成测试。 |
| **F5-4** GetVariables 脱敏 | ✅ 保持并加强 | `variables.go:12-13`、`58-66`、`70-87` | Get/Set 响应均掩码；`variables_test.go` 覆盖掩码保留旧值。 |
| **F5-5** 构建失败日志 | ✅ 保持 | `docker.go:206-211`、`535-582` | Scanner 上限已提到 4MiB（见下）。 |
| **F5-6** TW_DATA 32KB | ✅ 保持 | `executions.go:21-22`、`86-106`；`docker.go:40-41`、`228-235` | app + executor 双层预算。 |
| **F5-7a/c/d/e/g** | ✅ 保持 | `variables.go:25-31`；`deployments.go:73-91`；`docker.go:97-163`；`management.go:19`；`function_repo.go:228-240` | 信号量满清 pending；网络失败不缓存；ID 仅小写；孤儿对账不含 queued。 |
| **F5-7f + B2** 重试计数 | ✅ **B2 已落地** | `cmd/worker/worker.go:24-25`、`147-192`；`executions.go:54-63`、`370-383`；`requeue_test.go` 全文；`consume_test.go:124-238` | 见 §1.1。 |
| **F6-1** complete 锁 1h + 回滚确认 | ✅ 保持 | `redis_upload_session.go:26-28`、`168-185`；`uploads.go:176-195`、`235-242` | 锁后重新 `Get`；回滚要 `IsLockOwner`。 |
| **F6-2** Preview 像素防线 | ✅ 保持 | `file_handler.go:586-618`、`674-686` | `DecodeConfig` + 8192 边长上限。 |
| **F6-3a** DeleteBucket 清文档 | ✅ 保持 | `storage.go:160-200`；`cleanup_integration_test.go:61-115` | 已显式断言 files 文档 NotFound。 |
| **F6-3b/c/d/e/f/g** | ✅ 保持 | `uploads.go:142-144`；`object.go:46-49`；`uploads.go:69`、`312-327`；`storage.go:432-478`；`file_handler.go:510-515`、`548-557` | EnsureBucket、默认 bucket 小写、owner 绑定、HMAC 派生密钥、Cache-Control、DSL `BuildEqual`。 |
| **R07-P1-1 zip bomb 实际字节** | ✅ **本轮确认已修** | `docker.go:44-77`、`408-426`；`docker_test.go:93-259`、`321-342` | `budgetWriter` 按实际写入计数；伪造声明大小 / 总预算 / 单条预算 / symlink 均有测试，超限 `RemoveAll(destDir)`。 |
| **R07-P1-2 CreateFile 顺序** | ✅ **本轮确认已修** | `storage.go:240-253` | `EnsureBucket` 先于 `CreateDocument`；Put 失败仍回滚文档。 |
| **R07-P2-3 SetVariables project_id** | ✅ **本轮确认已修** | `function_repo.go:136-162` | DELETE/INSERT 均带 `project_id`。 |
| **R07-P2-4/5 complete/abort 竞态** | ✅ **本轮确认已修** | `uploads.go:186-195`、`269-299` | 锁内重读会话；Abort 抢不到锁则 `FailedPrecondition`。 |
| **R07-P2-6 Scanner 512KB** | ✅ **本轮确认已修** | `docker.go:38`、`542`；`docker_test.go:304-319` | 单行上限 4MiB，超限明确报错。 |
| **R07-P3-7 data 必须 object** | ✅ **本轮确认已修** | `executions.go:89-96`；`executions_test.go:190-220` | 数组/标量/`null` 一律拒绝。 |
| **G12 gRPC 写路径** | ✅ 守卫与 scope 正确 | `authz.go:43-54`；`management.go:57` 等 7 处；`apikey_scope.go:107`；`jwt.go:121-134`；`authz_test.go:32-82` | API key（`functions.write`）与 admin 会话过 use-case 守卫；端用户拒绝；gRPC 拦截器管 scope 与 owner/admin 角色。 |
| **File token HMAC** | ✅ 健康 | `storage.go:408-478`；`file_token_test.go:36-72` | `DeriveKey(..., PurposeFileToken)`；过期/篡改 401；绑定 project/bucket/file；旧主密钥签名被拒。 |
| **公开 bucket 匿名读** | ✅ 健康 | `file_handler.go:546-560` | `?project=` + `GuestPrincipal` + `read:any` 兜底；非法 bucketID 先拒。 |
| **Docker 资源 / 镜像白名单 / secret 日志** | ✅ 基线健康 | `docker.go:249-270`、`449-466` | cap-drop ALL、no-new-privileges、read-only、pids=512、spec 映射 CPU/mem；`FROM` 仅 node:18-alpine / python:3.11-alpine（由入口文件决定，用户 Dockerfile 会被覆盖）；env 不进日志，Get/SetVariables 掩码。 |
| **zip slip / symlink / RemoveAll 范围** | ✅ 健康 | `docker.go:371-395`、`408-425` | Clean + 前缀校验；拒绝 symlink；`RemoveAll` 仅作用于 `MkdirTemp` 的 destDir；`Build` 还有 `defer os.RemoveAll(buildDir)`。 |

### 1.1 B2 核实（必须项）

`requeue` 已删除进程内 `map[string]int`，改为解析 payload 的 `attempt`、`+1` 后再 marshal 回队：

```147:169:cmd/worker/worker.go
// requeue 将瞬时失败的任务重抛回队：解析 payload 内嵌 attempt 计数并 +1，
// ... 旧消息无 attempt 字段（json.Unmarshal 视为 0）时首次重试
// 即为 attempt=1。
func requeue(payload []byte) (next []byte, ok bool) {
	var m retryMessage
	...
	m.Attempt++
	if m.Attempt > maxProcessAttempts {
		return nil, false
	}
```

- 超限走 `failPayload` → `MarkExecutionFailed`（`worker.go:140-142`、`183-192`；`executions.go:370-383`）。
- `queueMessage` / `retryMessage` 字段对齐，`data` 往返保留（`executions.go:57-63`）。
- 单测：递增、旧格式 attempt=0→1、连续 3 次回队后第 4 次超限、坏 JSON 不重试
  （`requeue_test.go:13-73`）；consume 循环端到端 `[]int{1,2,3}` 后标 `failed`
  （`consume_test.go:124-188`）。
- 验收注释已更新，不再写「进程内存、重启清零」。

**B2 verdict：通过。**

### 1.2 Redis 队列丢失（按要求如实评估，不升 P0）

实现是 Redis List `LPUSH` + `BRPOP`（`internal/infra/queue/redis_queue.go:21-40`），
**无 ack / 可见性超时 / 死信**。这是 `docs/implementation-functions-executor.md` §5.5 / §8
明确的 MVP 偏离：

| 故障 | 实际后果 | 兜底 |
|------|----------|------|
| Redis 持久化开启后重启 | 队列消息还在，worker 继续消费 | 无需对账 |
| Redis 无持久化崩溃 / flush | **queued 消息丢失**；DB 记录停在 `queued` | F5-7g 后 `RecoverOrphanExecutions` **故意不标 queued**（`function_repo.go:234-238`），因此会**永久停在 queued**，而不是 1h 后标 failed |
| Worker 进程崩溃（已 BRPOP、执行中） | 消息已出队；记录可能停在 building/running | 启动 1h 后标 failed（`worker.go:59-67`） |
| Worker 关机时 `ProcessExecutionPayload` 因 ctx 取消失败 | `consume` 直接 return，不回队（`worker.go:125-128`） | 同上，1h 对账 |

这是已知设计权衡，不是新的 P0「任务丢失漏洞」。残留可观测性问题记为 P2-3。

---

## 2. 新发现问题

### 🔴 P0 严重

**无。** 未发现任意对象读写、容器逃逸、可被外部触发的队列投毒导致数据损坏或跨租户提权。

### 🟠 P1 高

1. **G12 不完整：HTTP multipart 部署上传未注入 Principal，Console / 大包路径恒 401**
   - 位置：`internal/api/serverhttp/functions_handler.go:93-152`；
     `internal/app/functions/deployments.go:31-35`；
     `internal/app/shared/authz.go:43-47`
   - 描述：`upload` 用 `h.authorize(r)` 做完 API key `functions.write` / admin 项目校验后，
     仍把**裸的** `r.Context()` 传给 `CreateDeployment`。G12 把 7 个写方法守卫改成
     `RequireServerWriteActor`，该方法只认 `contexts.Principal(ctx)`。
     Gateway（`grpc_gateway.go:75-78`）对自定义 handler **没有** `WithPrincipal` 中间件；
     gRPC 拦截器的注入（`jwt.go:157`）覆盖不到这条 HTTP 路由。
     单测只覆盖 `authorize()`（`functions_handler_test.go:239-301`），从未走到
     `CreateDeployment`，所以绿测掩盖了整条上传链。
   - 影响：
     - Console `uploadDeployment`（`console/src/api/functions.ts:127-138`，
       `POST /server/functions/{id}/deployments/code`）是管理后台的**唯一**部署通道，当前会
       一律 `Unauthenticated`。
     - API key CI/CD 走 multipart（>1MiB，gRPC 通道上限）同样失败。
     - gRPC `CreateDeployment`（≤1MiB，CLI/SDK）仍可用，因此这是「大包 + Console」回归，
       不是全部 Functions 写死。
   - 建议：`authorize` 成功后
     `ctx = contexts.WithPrincipal(r.Context(), principal)` 再调 use-case；
     补一条带 mock Functions 的 handler 单测，断言 API key / admin 能进到
     `CreateDeployment`，端用户仍 403。修复时一并处理下面 P2-1 的角色门禁。

### 🟡 P2 中

1. **HTTP 部署上传绕过 `adminRoleMethodRules`（viewer/member 可部署）——G12 角色门禁缺口**
   - 位置：`functions_handler.go:175-188`；对照 `pkg/grpc/interceptor/jwt.go:129-134`、
     `admin_roles.go:38`
   - 描述：gRPC 路径把 Functions 写方法限制为 owner/admin。HTTP `authorize` 只拒绝端用户，
     任何通过 `ValidateAdminProjectAccess` 的 admin 会话（含 viewer/member）都会放行。
     当前被 P1 挡住，P1 修好后即暴露。Console 部署走的正是这条 HTTP 路径。
   - 影响：只读角色可上传 zip 并触发 Docker 构建（资源消耗 + 覆盖部署）。
   - 建议：HTTP 侧对 `FunctionsServiceCreateDeployment` 复用 `adminRoleMethodRules`
     （或抽公共函数），与拦截器同一张表。

2. **异步执行信号量满被标 `failed` 且不回队**
   - 位置：`internal/app/functions/executions.go:298-307`
   - 描述：`ProcessExecution` 抢不到 `runSemaphore` 时写 `failed` / `too many concurrent executions` 并 `return nil`。Worker 视为成功消费，B2 重试不会发生。同步路径返回 `ResourceExhausted` 是合理的；异步路径把瞬时过载当成终态。
   - 影响：并发高峰下异步任务被永久失败，需调用方重提。
   - 建议：异步路径返回可重试错误（或重新入队）而不是标 failed；或至少延迟后再试。

3. **Redis 丢失后 queued 执行永久悬挂（F5-7g 的可观测性回退）**
   - 位置：`function_repo.go:228-240`；`domain/functions/repo.go:29-31`
   - 描述：对账刻意跳过 `queued`，依赖「消息还在 Redis」。无 AOF/RDB 的 Redis 崩溃后，记录停在 `queued`，Console 会一直转圈，没有 1h 后标 failed 的兜底。
   - 影响：运维可见性差；不是新的数据损坏。属已声明权衡的残留面。
   - 建议：对「`queued` 且 `updated_at` 超过 TTL（如 24h）且队列为空/消息不在」做二次对账；或文档明确要求 Redis 持久化。

4. **末片大小未按声明 `size` 校验，文档 size 可与对象不符**
   - 位置：`internal/app/storage/uploads.go:132-137`、`222-230`
   - 描述：非末片强制 `== chunkSize`，末片只要求 `1..chunkSize`，没有
     `size == session.Size - chunkSize*(partCount-1)`。设计文档验收标准 4 写明
     「保证 sum(parts)==size」。客户端可声明 32MiB 却只上传 16MiB+1B，complete 仍成功，
     文档 `size` 为声明值。
   - 影响：用量统计、预览 50MiB 判断、客户端 Content-Length 预期失真；对象内容仍是实际上传字节，不属于静默损坏。
   - 建议：末片必须等于余数（size 整除 chunkSize 时等于 chunkSize）。

5. **`ListFiles` 忽略 `bucketID` 参数，过滤全靠调用方**
   - 位置：`internal/app/storage/storage.go:302-319`
   - 描述：方法签名收 `bucketID` 但函数体不用。gRPC handler 有
     `query.BuildEqual("bucket_id", ...)`（`servergrpc/storage.go:160`），
     `DeleteBucket` 也自己拼了 filter。直接调 use-case 且 `Query` 为空会列出项目内
     调用方可读的**全部**文件。
   - 影响：抽象泄漏；未来新调用方容易漏过滤。Server API 当前路径安全。
   - 建议：use-case 内强制追加 `bucket_id` equal，与 handler 去重。

6. **Messaging 适配器：SMTP 头注入面 + HTTP 无超时（roadmap P2 质量）**
   - 位置：`internal/infra/messaging/mailer.go:39-47`、`113-124`；
     `sms.go:32-33`、`50-81`
   - 描述：`buildMessage` 把 `to`/`subject`/`from` 直接拼进头，未剥离 CR/LF；
     `Send` 忽略 `ctx`。SMS 使用无超时的 `http.DefaultClient`。dev 模式把 OTP/短信打到 stdout。
   - 影响：若调用方传入未校验的收件人或主题，存在头注入；Twilio 调用可能挂死。
     属适配器质量，不是本模块新的认证绕过。
   - 建议：净化头字段；给 SMTP/HTTP 设超时；dev 日志打码 OTP。

### 🟢 P3 低

7. **`Functions.Execute` 遗留入口无鉴权、无部署绑定**
   - 位置：`internal/app/functions/functions.go:35-58`
   - 描述：旧 `ExecuteCommand` 直接调 executor，仓库内无 handler 引用。死代码，但签名仍公开。
   - 建议：删除或标 deprecated，避免被误接。

8. **`normalizeMimeType` 大小写 / 分号空格不归一**
   - 位置：`internal/app/storage/storage.go:495-505`
   - 描述：只精确匹配小写 `text/html` 等；`Text/HTML` 或 `text/html ; charset=utf-8` 会原样入库。下载侧有 `nosniff` + CSP sandbox，风险低。
   - 建议：`TrimSpace` + `ToLower` 后再判断。

9. **`UpdateExecution` / `PruneOldExecutions` 仍只按主键 / function_id**
   - 位置：`function_repo.go:222-226`、`247-257`
   - 描述：与已修好的 SetVariables 相比，这两处仍依赖 execution/function ID 全局唯一。当前 schema 成立。
   - 建议：写路径补 `project_id`，与 GetExecution 对齐。

10. **重试无退避；`failPayload` 解析失败会把 payload 打进日志**
    - 位置：`cmd/worker/worker.go:136-139`、`186`
    - 描述：瞬时错误立即 LPUSH 到队头，可能打满 DB；坏消息日志含 `data` 字段。
    - 建议：指数退避；日志只打 execution_id。

11. **`resolveReadContext` 注释与实现优先级不符**
    - 位置：`file_handler.go:520-536`
    - 描述：注释写 token「优先级最高」，实现却先走凭证。有效凭证 + 他人 token 时走凭证 ACL。
    - 建议：改注释，或若产品要「有 token 即按 token」则调换顺序。

---

## 3. 模块总体结论

- **安全边界**：对象 key 由服务端 UUID/项目 ID 拼接、文件名不进路径；zip slip/bomb/symlink
  与 `RemoveAll` 范围已收紧；Docker `FROM` 白名单 + 非 root + 资源上限 + 不把 secret 打进日志
  达到 MVP 基线。File token 与 JWT 密钥分离、公开 bucket 走 Guest + `read:any`，逻辑正确。
- **资源管理**：分片 complete/abort 互斥与 1h 锁、孤儿分片 48h cleaner、构建信号量满清 pending、
  容器 `defer Remove` 均保持。镜像只在 DeleteDeployment/DeleteFunction 时删除，长期部署会胀本地
  Docker 缓存（已知单机假设）。
- **队列**：B2 达到验收；Redis List 无 ack 是文档化权衡。不要把「Redis 崩溃丢 queued」写成 P0。
- **G12**：gRPC + scope 已按方案 B 放行 API key；**HTTP multipart 是破的**，与
  「CLI / SDK / serverhttp 函数写路径全部恢复」的声明不符。

**最需优先修复的 3 项：**

1. HTTP 部署上传注入 Principal（P1），并补角色门禁（P2-1）——否则 Console 无法部署。
2. 异步执行信号量满应可重试（P2-2），避免高峰误杀任务。
3. 末片按声明 size 校验（P2-4），避免文档与对象大小漂移。

**是否建议关闭本模块审查：** 不建议。P1（G12 HTTP 路径）修复并补 handler 单测后，
本模块可视为 Round 3 闭环；Docker 集成测试 / CI backend 全绿仍是 Round 2 遗留的运行时验证项，
不阻塞本报告结论。

**模块 verdict：CONDITIONAL PASS**（存储与 worker 核心安全项健康；Functions HTTP 部署鉴权回归为 P1，修完即可过）。
