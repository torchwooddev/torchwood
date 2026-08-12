# 复审报告（Round 2）：07 - Storage / Functions / Worker

> 审查范围：`internal/app/storage/*`、`internal/app/functions/*`、`internal/infra/storage/*`、
> `internal/infra/functions/*`、`internal/infra/messaging/*`、`internal/infra/queue/*`、
> `cmd/worker/*`、`internal/api/serverhttp/file_handler.go`、`internal/api/serverhttp/functions_handler.go`、
> `pkg/idgen/id.go`、`.github/workflows/ci.yml`。
>
> 审查基准：当前工作区 HEAD；fix-plan 中部分行号锚点已漂移，以下证据按当前代码重新定位。
> 静态检查命令：`go vet ./internal/app/storage/... ./internal/app/functions/... ./internal/infra/storage/... ./internal/infra/functions/... ./internal/infra/messaging/... ./internal/infra/queue/... ./cmd/worker/... ./pkg/idgen/... ./internal/api/serverhttp/... ./internal/infra/bun/bunrepo/...`（通过）。
> 单元测试命令：`go test -short ./<上述路径>/...`（全部通过；含 Redis 的测试使用 miniredis，含 DB 的集成测试在 `-short` 下跳过，由 CI 兜底）。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 证据（文件路径:行号） | 说明 |
|--------|------|----------------------|------|
| **F5-1** Function ID 路径穿越 → 任意文件写入 | ✅ 已修复 | `internal/app/functions/management.go:17`、`management.go:60-68`；`internal/app/functions/deployments.go:169-173`、`185-193`、`195-200`；`internal/app/functions/security_test.go:16-49`、`51-88` | `CreateFunction` 增加字符集/长度/保留字校验；`zipPath` 对 `projectID/functionID/deploymentID` 均取 `filepath.Base`；`writeZip/removeZip` 通过 `assertZipDir` 断言路径在根目录内；测试覆盖 `../../etc/passwd`、保留字等恶意 ID 以及逃逸路径拒绝。 |
| **F5-2** GetDeployment/DeleteDeployment 跨项目 IDOR | ✅ 已修复 | `internal/app/functions/deployments.go:121-136`、`141-162`；`internal/domain/functions/repo.go:11-20`；`internal/infra/bun/bunrepo/function_repo.go:80-93`、`119-124`；`security_test.go:90-132` | Use-case 前置 `GetFunction(projectID, functionID)`；repo 端口签名已增加 `projectID`；`GetDeployment/DeleteDeployment` SQL 均带 `fd.project_id = ?`；测试覆盖跨项目访问返回 `NotFound` 且不得生效。 |
| **F5-3** Docker 解压 0600 + USER 非 root → EACCES | ⚠️ 代码已修复，真实 Docker 运行未本地验证 | `internal/infra/functions/docker.go:355`（`0o644`）；`docker.go:392-396`（`USER node`）、`401`（`USER 1000`）；`docker_integration_test.go:87-110` | 解压写入改为 `0o644`；Node/Python Dockerfile 均切非 root 用户；`TestDockerExecutor_BuildAndRunNode` 存在但依赖本地 Docker，未在本地执行（环境无 Docker）。 |
| **F5-4** GetVariables 明文返回全部 secret | ✅ 已修复 | `internal/app/functions/variables.go:11-12`、`37-55`；`security_test.go:134-152` | `GetVariables` 对所有非空值返回 `"******"`；`SetVariables` 返回原值（值仅在此处可见）；测试断言脱敏。 |
| **F5-5** docker build 失败被吞 + 构建日志丢弃 | ✅ 已修复 | `internal/infra/functions/docker.go:163-175`、`477-528`；`deployments.go:95-104`；`docker_test.go:11-73` | `Build` 读取构建输出并扫描 `{"error":...}`/`{"errorDetail":...}`；`buildError` 组合错误与尾部日志；`buildDeployment` 将错误写入 `dep.Error`；测试覆盖成功流、错误 JSON、长日志尾部保留。 |
| **F5-6** TW_DATA 64KB 超 execve 32KiB 硬限制 | ✅ 已修复 | `internal/app/functions/executions.go:20-22`、`77-92`；`internal/infra/functions/docker.go:40`、`191-198`；`security_test.go:170-184` | `maxExecutionDataBytes` 收紧至 32KB；`CreateExecution` 校验 `envSize(vars)+len(data) ≤ 32KB`；executor 再次以 32KB 合并预算兜底；测试覆盖合并超限拒绝。 |
| **F5-7a** SetVariables 未校验 function 存在 | ✅ 已修复 | `internal/app/functions/variables.go:15-22`；`security_test.go:154-160` | `SetVariables` 先 `GetFunction`，不存在返回 `NotFound`。 |
| **F5-7b** worker 构建无超时 | ✅ 已修复 | `internal/app/functions/executions.go:25`、`256-264` | `workerRebuildTimeout = 5min`，`ProcessExecution` 补构建使用 `context.WithTimeout`。 |
| **F5-7c** buildDeployment 信号量满泄漏 pending | ✅ 已修复 | `internal/app/functions/deployments.go:78-87`、`68-72`；`security_test.go:186-215` | 信号量满时删除 deployment 行与本地 zip；`CreateDeployment` 在 `buildDeployment` 返回错误后再次清理；测试断言 pending 行与 zip 均被清理。 |
| **F5-7d** ensureNetwork sync.Once 粘住瞬时错误 | ✅ 已修复 | `internal/infra/functions/docker.go:61-64`、`98-127` | 改为 `netMu`+`netReady` 仅缓存成功，失败不缓存、下次调用可重试。 |
| **F5-7e** CreateFunction ID 字符集同时解决镜像名非法 | ✅ 已修复 | `internal/app/functions/management.go:17`、`60-68`；`internal/infra/functions/docker.go:90-96` | ID 正则仅允许 `[a-zA-Z0-9_-]` 且以字母数字开头；镜像名 `func-{functionID}-{deploymentID}` 因此合法。 |
| **F5-7f** worker 消费失败丢任务 | ✅ 已修复 | `cmd/worker/worker.go:113-146`、`151-183` | 非 `ErrInvalidQueuePayload` 的瞬时失败会重抛回队，最多 `maxProcessAttempts=3` 次；超限调用 `MarkExecutionFailed` 兜底标记 `failed`。 |
| **F5-7g** 孤儿对账状态倒挂 | ✅ 已修复 | `internal/infra/bun/bunrepo/function_repo.go:219-229` | `RecoverOrphanExecutions` 仅 `status IN ('building','running')`，`queued` 仍留队列不被误标。 |
| **F6-1** complete 互斥锁 TTL 短于长 Compose | ✅ 已修复 | `internal/infra/storage/redis_upload_session.go:28`、`168-172`；`internal/app/storage/uploads.go:176-184`、`223-233` | `completeLockTTL` 提升至 1h；回滚删对象前通过 `IsLockOwner` 与 `Get` 双重确认“自己仍是锁持有者且会话仍存在”。 |
| **F6-2** Preview 解码无像素级防线 | ✅ 已修复 | `internal/api/serverhttp/file_handler.go:583`、`641-662`、`677-686`；`file_handler_integration_test.go:597-646` | 解码前先 `image.DecodeConfig` 读取宽高，超过 `maxPreviewSourceDimension=8192` 直接拒绝；缩放后流式编码回 `ResponseWriter`。 |
| **F6-3a** DeleteBucket 不删 files 元数据 | ✅ 已修复 | `internal/app/storage/storage.go:152-193`；`cleanup_integration_test.go:59-94` | `DeleteBucket` 按 `bucket_id` 分页循环删除文件文档与对象，再按前缀清理残留分片，最后删 bucket 文档。 |
| **F6-3b** UploadChunk 缺 EnsureBucket | ✅ 已修复 | `internal/app/storage/uploads.go:142-144` | `UploadChunk` 在写分片前调用 `store.EnsureBucket`；`CompleteUpload` 也调用。 |
| **F6-3c** 默认 bucket 名大小写不一致 | ✅ 已修复 | `internal/domain/storage/object.go:47-49`；`internal/app/storage/storage.go:499-504`；`internal/infra/storage/minio.go:45-49` | 统一使用常量 `DefaultBucketName = "torchwood-files"`，全小写。 |
| **F6-3d** upload session 无 owner 绑定 | ✅ 已修复 | `internal/domain/storage/upload_session.go:14`；`internal/app/storage/uploads.go:69`、`123-125`、`292-303`；`file_token_test.go:80-103` | 会话记录 `OwnerUserID`；`UploadChunk/CompleteUpload/AbortUpload` 通过 `checkUploadOwner` 校验；测试覆盖 owner/非 owner/keys 豁免。 |
| **F6-3e** file token 与 JWT 共用密钥 | ✅ 已修复 | `internal/app/storage/storage.go:461-463`、`423-428`、`435-457`；`file_token_test.go:36-72` | `fileTokenKey` 使用 `jwtparser.DeriveKey(master, PurposeFileToken)` 派生专用密钥；旧行为（主密钥原文签名）的 token 被拒绝。 |
| **F6-3f** 私有文件下载无 Cache-Control | ✅ 已修复 | `internal/api/serverhttp/file_handler.go:509-513` | 非公开路径返回 `Cache-Control: private, no-store`；公开 bucket 匿名路径返回 `public, max-age=86400`。 |
| **F6-3g** 公开 bucket 匿名路径 bucketID 拼 DSL 未转义 | ✅ 已修复 | `internal/api/serverhttp/file_handler.go:548`；`file_handler_dsl_test.go:10-29` | 使用 `query.BuildEqual("$id", bucketID)` 构造 DSL；测试断言恶意 bucketID 不会逃逸出单个 equal 条件。 |
| **F10-1** CI backend job 必失败于 minio 健康检查 | ⚠️ 已修复，未实际跑 CI | `.github/workflows/ci.yml:32-42`、`60-61`、`67-68` | minio 增加 `command: server /data`；健康检查改为 `bash /dev/tcp/127.0.0.1/9000`；新增 Docker 测试开关 `TORCHWOOD_RUN_DOCKER_TESTS=1` 与镜像预拉取。由于无 GitHub Actions 环境，未实际验证 CI 全绿。 |

---

## 2. 新发现问题

### 🔴 P0 严重

**无。** 本轮审查未发现在 Storage / Functions / Worker 模块内存在任意文件读写、容器逃逸、任务丢失、跨租户提权等 P0 级问题。

### 🟠 P1 高

1. **`extractZip` 信任 zip 头声明的 `UncompressedSize64`，实际解压字节未做硬限制（zip bomb 风险）**
   - 位置：`internal/infra/functions/docker.go:331-336`、`355-360`
   - 描述：单条目/总大小上限基于 `f.UncompressedSize64` 累加，而实际 `io.Copy` 写入文件的字节未被计量。攻击者可构造头声明很小、实际解压极大的 zip，绕过 `maxZipEntryBytes/maxZipTotalBytes`，耗尽 worker 磁盘/内存。
   - 影响：Docker 构建阶段 DoS，可能导致 worker OOM 或磁盘满。
   - 建议：给 `dst` 包装一个带剩余预算的 `io.Writer`，按实际写入字节拦截；或改用 `io.CopyN` 并在超额时返回错误。

2. **`CreateFile` 先写文件元数据文档，再 EnsureBucket/Put 对象；EnsureBucket 失败会产生孤儿文档**
   - 位置：`internal/app/storage/storage.go:233-240`
   - 描述：第 233 行创建 `files` 文档后，第 237 行才调用 `store.EnsureBucket`。若底层 bucket 创建失败（权限/网络），错误返回但文件文档已残留。
   - 影响：元数据与对象不一致，用户可见不存在文件；后续清理需依赖后台任务。
   - 建议：将 `EnsureBucket` 提前到创建文档之前；或在 EnsureBucket/Put 失败时回滚删除已创建的文档（当前仅 Put 失败回滚）。

### 🟡 P2 中

3. **`SetVariables` 删除旧变量时未加 `project_id` 过滤，依赖 function ID 全局唯一性**
   - 位置：`internal/infra/bun/bunrepo/function_repo.go:135-138`
   - 描述：`DELETE function_variables WHERE function_id = ?` 缺少 `project_id = ?`。当前 `functions` 表以 `id` 为全局主键，因此不会立即跨项目误删；但该假设是隐性的，一旦未来改为项目内唯一或出现数据导入冲突，会直接导致跨项目变量被覆盖/删除。
   - 影响：数据完整性风险、与未来模型变更冲突。
   - 建议：删除条件增加 `AND project_id = ?`，与 `GetVariables` 保持一致。

4. **`CompleteUpload` 在加锁前使用会话快照判断缺片，无法看到加锁期间新上传的分片**
   - 位置：`internal/app/storage/uploads.go:158-195`
   - 描述：`session` 在 `LockComplete` 之前获取，`missing` 检查基于该快照。若客户端在 `CompleteUpload` 调用期间并发上传缺失分片，即使最终分片齐全，本次 complete 也会返回 `missing chunks`。
   - 影响：并发续传场景下可能出现假性失败，需客户端重试。
   - 建议：加锁成功后重新 `Get` 会话再判断缺片；或在 `MarkChunk` 处与 complete 锁协调。

5. **`AbortUpload` 未获取 complete 锁，可能与正在执行的 `CompleteUpload` 竞态**
   - 位置：`internal/app/storage/uploads.go:259-286`
   - 描述：`AbortUpload` 直接删除会话与分片对象，不检查是否有人正在 complete。若 complete 已执行完 `Compose` 但未创建文档，`AbortUpload` 删除分片后 complete 再创建文档，会导致最终文件存在但无源分片（可接受），或 `Delete session` 与 complete 的 `Delete session` 冲突。
   - 影响：竞态下状态短暂不一致。
   - 建议：`AbortUpload` 尝试获取 complete 锁（或返回 `FailedPrecondition` 提示稍后再试）。

6. **`readBuildOutput` 的 Scanner buffer 固定 512KB，超长 JSON 行会报错并丢失日志**
   - 位置：`internal/infra/functions/docker.go:481`
   - 描述：`scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)`，若 daemon 输出单行超过 512KB，`scanner.Err()` 非 nil，可能导致构建错误信息不可读。
   - 影响：极端大错误行时日志截断/丢失。
   - 建议：使用更大上限的 Scanner，或在超长行时切换到 `io.Reader` 兜底读取。

### 🟢 P3 低

7. **`CreateExecution` 仅校验 `data` 是合法 JSON，不限制为对象**
   - 位置：`internal/app/functions/executions.go:80-82`
   - 描述：`json.Valid` 接受数组、字符串、数字等。函数运行时通常期望对象，传入数组/字符串可能导致运行时错误而非 early reject。
   - 影响：轻微，属于输入语义约束。
   - 建议：若业务约定 data 必须为 JSON object，增加类型检查。

8. **worker 重试计数器存储在进程内存，重启后重置**
   - 位置：`cmd/worker/worker.go:38-39`、`151-164`
   - 描述：`attempts map[string]int` 是单进程状态，worker 重启后之前已重试 2 次的任务会重新获得 3 次机会。
   - 影响：最坏情况下延长失败任务标记 `failed` 的时间。
   - 建议：可将重试次数写入执行记录（`ExecutionRecord`）或 Redis，作为持久化兜底。

---

## 3. 模块总体结论

- **修复完成度估计**：约 **90%**。F5/F6 代码层修复已全部落地，端口签名、调用方、测试均同步；F10 CI 配置已按 fix-plan 调整。
- **剩余风险 Top 3**：
  1. **F5-3 / F10-1 的真实运行验证尚未完成**：`TestDockerExecutor_BuildAndRunNode` 与 CI backend job 是否真绿，取决于 GitHub Actions 的 Docker-in-Docker 环境，本地无法确认。
  2. **zip bomb 实际解压字节未硬限制**（新发现 P1）：`extractZip` 仍依赖 zip 头声明大小，是 worker 资源耗尽的主要残留风险。
  3. **CreateFile 元数据/对象创建顺序**（新发现 P1）：EnsureBucket 后置可能在失败时留下孤儿文件文档。
- **是否建议关闭本模块审查**：**不建议完全关闭**。建议先满足以下前提再关闭：
  1. CI backend job 至少一次全绿，且确认 `TestDockerExecutor_BuildAndRunNode` 实际执行过（未因 DinD 不可用而被静默跳过）；
  2. 修复新发现的 P1/P2 项（至少 P1 的 zip bomb 实际字节限制与 CreateFile 顺序）；
  3. 补一个 `DeleteBucket` 删除文件文档的单元/集成断言（当前 `TestDeleteBucket_RemovesOrphanChunks` 只验证了分片对象清理，未显式断言 `files` 集合中相关文档被删）。
