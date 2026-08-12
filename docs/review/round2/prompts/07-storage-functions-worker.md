# 复审任务（Round 2）：07 - Storage / Functions / Worker

## 背景
- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。
- Round 2 核心目标：验证 F5/F6/F10 修复是否真实落地、是否完整、是否引入回归，并重新评估剩余风险。
- 本 prompt 覆盖 fix 批次：F5（Functions 安全修复）、F6（Storage 修复）、F10（CI 修复，重点验证其对 Docker 集成测试验证的影响）。
- 复审结论将纳入 `docs/review/round2/reports/`（由执行 agent 自行创建），供下一轮决策使用。

## 角色
你是资深 Go 后端代码审查专家（对象存储、容器执行、消息队列领域）。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Storage / Functions / Worker」模块做一次**只读**审查。**不得修改任何代码**，只输出审查报告。同时你是修复验证者，需对照 fix-plan 逐条核实。

## 第一步：建立基线
- 读 `docs/review/prompts/07-storage-functions-worker.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 **F5 全部、F6 全部、F10 全部** 章节：这是本模块 Round 1 结论与修复方案；同时读 §0 总览表与 §12 文件冲突矩阵，确认批次依赖与冲突协调结果。
- 重点关注 fix-plan 中的依赖声明：F5 依赖 F2（handler 鉴权），F5 又依赖 F10（CI 修复后才能验证 Docker 集成测试）；F6 与 F5 可并行；F10 需在 F5 之前完成。
- 可用 `git log --oneline 1288705..HEAD -- internal/app/storage internal/app/functions internal/infra/storage internal/infra/functions internal/infra/messaging internal/infra/queue cmd/worker internal/api/serverhttp pkg/idgen .github/workflows` 与 `git show <commit>` 查看修复的实际改动。
- 若 fix-plan 锚点行号已漂移，使用 `git blame`/当前代码重新定位，在结论表中给出实际 `文件路径:行号`，并简要说明原锚点位置。
- 建立基线时同步扫描测试文件：fix-plan 中承诺的新增测试（如恶意 function ID 拒绝测试、并发 complete 测试、Docker 构建失败日志断言）必须能在对应 `*_test.go` 中找到。

## 必读上下文
- 仓库根目录：`D:\Codes\qiulin\torchwood`；先读 `AGENTS.md`、`docs/implementation-storage-chunked-upload.md`、`docs/implementation-functions-executor.md`。
- 架构分层：`internal/app/storage` + `internal/app/functions` 为用例层；`internal/infra/storage`（MinIO/S3）、`internal/infra/functions`（Docker build/run）、`internal/infra/messaging`、`internal/infra/queue` 为适配器；`cmd/worker` 消费执行队列（BRPOP、N=4 并发、孤儿对账）。
- 能力边界：S3 上传/下载/预览缩略图、公开 bucket、HMAC file token（1h 默认/7d 上限）、分片上传（≤16MiB/part、≤10000 parts、24h TTL、ComposeObject 合并）、Docker 构建与同步/异步执行、execution 历史。
- 关键约定：端口在 domain、适配器在 infra；gRPC 方法必须带 proto authz 注解；列表查询复用 `pkg/crud` 或 AIP-132/158/160 抽象，动态文档优先使用 `pkg/query`。
- 复审范围沿用 Round 1：`internal/app/storage/*`、`internal/app/functions/*`、`internal/infra/storage/*`、`internal/infra/functions/*`、`internal/infra/messaging/*`、`internal/infra/queue/*`、`cmd/worker/*`、`internal/api/serverhttp/file_handler.go`、`internal/api/serverhttp/functions_handler.go`、`pkg/idgen/id.go`、`.github/workflows/ci.yml`。

## 复审重点 A：修复验证（逐条核实）
对 fix-plan 中本模块的每一个修复项逐条核实：
1. **F5-1 Function ID 路径穿越导致任意文件写入** — `internal/app/functions/management.go:47-49`、`pkg/idgen/id.go:20-22`、`internal/app/functions/deployments.go:144-157`
2. **F5-2 GetDeployment/DeleteDeployment 跨项目 IDOR** — `internal/app/functions/management.go:114-141`、`internal/infra/bun/bunrepo/function_repo.go:80-93,118-124`、`internal/domain/functions/repo.go:17,20`
3. **F5-3 Docker 解压 0600 + USER 非 root 导致 EACCES** — `internal/infra/functions/docker.go:328`、`docker.go:365-375`
4. **F5-4 GetVariables 明文返回全部 secret** — `internal/app/functions/variables.go:30-35`、`internal/api/servergrpc/functions.go:237-247`
5. **F5-5 docker build 失败被吞且构建日志丢弃** — `internal/infra/functions/docker.go:149-157`
6. **F5-6 TW_DATA 64KB 超 execve 32KiB 硬限制** — `internal/app/functions/executions.go:20`、`internal/infra/functions/docker.go:185`
7. **F5-7a SetVariables 未校验 function 存在** — `internal/app/functions/variables.go:12-28`
8. **F5-7b worker 构建无超时** — `internal/app/functions/executions.go:254`
9. **F5-7c buildDeployment 信号量满泄漏 pending** — `internal/app/functions/deployments.go:61-80`
10. **F5-7d ensureNetwork sync.Once 粘住瞬时错误** — `internal/infra/functions/docker.go:96-112`
11. **F5-7e CreateFunction ID 字符集同时解决镜像名非法** — `internal/app/functions/management.go:47-49`、`pkg/idgen/id.go:20-22`
12. **F5-7f worker 消费失败丢任务** — `cmd/worker/worker.go:120-125`
13. **F5-7g 孤儿对账状态倒挂** — `internal/infra/bun/bunrepo/function_repo.go:217-234`
14. **F6-1 complete 互斥锁 TTL 短于长 Compose** — `internal/infra/storage/redis_upload_session.go:24`、`internal/app/storage/uploads.go:166-199`
15. **F6-2 Preview 解码无像素级防线** — `internal/api/serverhttp/file_handler.go:571`、`file_handler.go:624-635`
16. **F6-3a DeleteBucket 不删 files 元数据** — `internal/app/storage/storage.go:150-184`
17. **F6-3b UploadChunk 缺 EnsureBucket** — `internal/app/storage/uploads.go:138-141`
18. **F6-3c 默认 bucket 名大小写不一致** — `internal/app/storage/storage.go:484-490`、`internal/infra/storage/minio.go:45-49`
19. **F6-3d upload session 无 owner 绑定** — `internal/app/storage/uploads.go:111-146`
20. **F6-3e file token 与 JWT 共用密钥** — `internal/app/storage/storage.go:414,427`
21. **F6-3f 私有文件下载无 Cache-Control** — `internal/api/serverhttp/file_handler.go:497-507`
22. **F6-3g 公开 bucket 匿名路径 bucketID 拼 DSL 未转义** — `internal/api/serverhttp/file_handler.go:538-541`
23. **F10-1 CI backend job 必失败于 minio 健康检查** — `.github/workflows/ci.yml:38`

逐条检查：修复是否已落地；修复是否正确完整（有无绕过路径、边界遗漏，例如只改了入口 A 没改入口 B、校验可加在错误层、并发场景仍可乘）；修复是否引入新问题（接口/行为变化是否同步到全部调用方与前端/SDK）；承诺的测试是否真实存在且断言的是真实行为（不是恰好通过的假断言）。

## 复审重点 B：回归与新问题排查
- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归）。特别关注：
  - function repo 端口签名若增加 projectID，调用方（use-case、servergrpc、worker）是否全部同步；
  - storage file token 密钥独立后，既有 token 是否失效或兼容；
  - F5-1 的 ID 字符集校验是否影响既有 function 的读取与更新路径。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（路径穿越、zip bomb/slip、容器逃逸、信息泄露、凭据处理）、正确性（错误吞掉、部分失败处理、panic）、并发（队列消费竞态、complete 互斥、cleaner 与正常流程竞争）、性能（大文件内存加载、ComposeObject 拷贝、worker 并发度）、一致性（与端口签名、设计文档、AGENTS.md 约定一致；生成代码未手动修改）、测试（分片上传、token 过期、zip 异常、队列失败路径是否有测试）。
- 本模块修复后特有风险点：
  1. F5-1/F5-7e 对 function ID 加字符集校验后，需重查既有函数 ID 是否被误伤，并核对 Docker 镜像名、临时 zip 路径、execution 日志目录的命名一致性。
  2. F5-3 改解压权限与镜像 USER 后，需验证非 root 运行时镜像仍能读写工作目录，并确认 F10 CI 真实跑通了 `TestDockerExecutor_BuildAndRunNode`（若 DinD 不可用则必须显式跳过并记录，不能伪绿）。
  3. F6-1 提升 complete 锁 TTL 或引入续期后，需重查 abort/cleaner 与正常 complete 的竞态——锁持有者在回滚删对象前是否可能误删其他会话的 part；Redis 崩溃后锁残留是否导致完整上传无法再次 complete。
  4. F5-2/F5-7a 在 function/variable 操作加 project_id 前置校验后，需检查端口签名变更是否同步到全部调用方，以及跨项目调用是否被正确拒绝而非被 NotFound 掩盖提权。

## 输出要求
简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；对于部分修复项，需指出具体哪一部分未落实。
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；如某级别无问题，明确写「无」。
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查；若建议关闭，需说明关闭前提（如某个 F10 子项仍需跟踪）。

## 约束
- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./internal/app/storage/... ./internal/app/functions/... ./internal/infra/storage/... ./internal/infra/functions/... ./internal/infra/messaging/... ./internal/infra/queue/... ./cmd/worker/... ./pkg/idgen/...` 与无外部依赖的纯单元测试辅助验证。
- 不得仅因代码存在即判定修复完成，必须核对行为是否与 fix-plan 方案一致；不得虚构测试结果或文件位置。
- 若发现 fix-plan 中某修复项已在其他批次（如 F4-2 并入 F3-5、F4-6 并入 F2）处理，需在结论表中注明归属变更，不要重复打分。
