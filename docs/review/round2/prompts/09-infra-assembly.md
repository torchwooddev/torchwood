# 复审任务（Round 2）：09 - 基础设施与服务器装配

## 背景

- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色

你是资深 Go 代码审查专家（基础设施与运维领域）。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「基础设施与服务器装配」做一次**只读**复审。**不得修改任何代码**，只输出复审报告。同时你是修复验证者，需对照 fix-plan 逐条核实。

## 第一步：建立基线

- 读 `docs/review/prompts/09-infra-assembly.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 F7 全部与 F10 章节：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- internal/pkg/config/ cmd/server/ internal/infra/server/ internal/infra/clients/ internal/infra/health/ internal/infra/idgen/ internal/app/console/setup.go .github/workflows/ci.yml` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 架构分层：Lynx + Clean Architecture（`internal/api` 传输层、`internal/app` 用例层、`internal/domain` 领域与端口、`internal/infra` 适配器层）。
- 配置 schema 由 `internal/pkg/config/config.proto` 定义，`internal/pkg/config/bind.go` 绑定；环境变量前缀 `TORCHWOOD_`，点号路径映射（如 `data.database.source` → `TORCHWOOD_DATA_DATABASE_SOURCE`）。
- 服务器组件由 `cmd/server/provides.go` 启动：gRPC、grpc-gateway、独立 HTTP handler、metrics、Admin Console SPA（`console/embed.go`）。
- 运行时组合通过 Wire：`cmd/server/provides.go` → `cmd/server/wire_gen.go`（provider 变更后需 `task wire-all`）。
- 数据库：元数据静态表用 bun + golang-migrate（`db/migrations/`）；系统资源与用户动态集合用 PostgreSQL 动态文档 adapter。
- 认证中间件位于 `pkg/grpc/interceptor`；API_KEY 方法同时允许 admin console session（需带 `X-Torchwood-Project` header）。

## 复审重点 A：修复验证（逐条核实）

对 fix-plan 中本模块的每一个修复项逐条核实：

1. **F7-1 Console 首个管理员引导可被抢占（P0）**
   - 文件锚点：`internal/app/console/setup.go:98-107`、`proto/console/v1/auth.proto`（SignUp PUBLIC）
   - 核实：是否新增 `security.setup_token` 配置（env `TORCHWOOD_SECURITY_SETUP_TOKEN`）；未设置时 SignUp 是否拒绝；SignUp 是否校验 setup token；并发首次性检查是否使用 `pg_advisory_xact_lock` 或 admins 表唯一约束兜底。

2. **F7-2 graceful shutdown 顺序错误（P1）**
   - 文件锚点：`cmd/server/main.go:26-29`（OnStop(cleanup)）
   - 核实：`cleanup` 是否已移出 `OnStop`、在 `runner.Run()` 返回后调用；是否启用 `WithDrainTimeout`；关闭顺序是否为 gateway → gRPC → metrics → 连接池清理，确保在途请求不被提前断开。

3. **F7-3 慢查询/调试 SQL 记录内联参数（P1）**
   - 文件锚点：`internal/infra/clients/dbhook.go:54-74`、`configs/config.yaml.template:33`
   - 核实：`dbhook.go` 是否只记录操作名+表名+占位符；是否对 `password_hash`/`secret`/`token` 等列强制掩码；`configs/config.yaml.template` 的 `debug` 是否已默认改为 `false`。

4. **F7-4 Prometheus metrics 无鉴权且默认监听全部接口（P1）**
   - 文件锚点：`internal/infra/server/metrics.go:16-24`、`configs/config.yaml.template:17`
   - 核实：默认监听地址是否改为 `127.0.0.1:9040`（或仅 loopback）；是否加了 scoped token 中间件；管理端点 `/metrics` 是否不再对公网暴露。

5. **F7-5 JWT 弱默认被启动校验接受（P1）**
   - 文件锚点：`cmd/server/provides.go:48-50`、`.env.example:8`
   - 核实：启动时是否拒绝已知弱值（如 `change-me-in-production`）与长度 `<32` 字符的 secret；是否输出明确告警或快速失败。

6. **F7-6a HTTP 侧补 panic recovery（P2）**
   - 文件锚点：`internal/infra/server/grpc_gateway.go:100-108`
   - 核实：gateway HTTP handler 是否已挂载 recovery 中间件；panic 时是否返回 500 而非直接断开、且不泄露堆栈细节。

7. **F7-6b 启动 ping 加超时（P2）**
   - 文件锚点：`internal/infra/clients/database.go:37,89`
   - 核实：数据库启动 `ping` 是否带 `context.WithTimeout`；超时后是否明确报错、避免启动卡住。

8. **F7-6c health 检查结果缓存（P2）**
   - 文件锚点：`internal/infra/health/checks.go:78-90`
   - 核实：health 探测结果是否带 TTL 缓存；并发探针风暴是否被抑制；缓存过期/错误状态切换是否及时。

9. **F7-6d idgen：每次生成打项目查询 + DB 抖动静默回退（P2）**
   - 文件锚点：`internal/infra/idgen/service.go:111-136`
   - 核实：idgen 是否在 DB 异常时仍可用（降级到本地生成或缓存）；降级后的 ID 是否仍满足唯一性/可排序性承诺。

10. **F7-6e random 策略 Redis 集合无界（P2）**
    - 文件锚点：`internal/infra/idgen/random_redis.go:16-31`
    - 核实：random ID 策略是否对 Redis 集合设置最大容量/过期清理；是否存在 OOM 风险。

11. **F7-6f 连接池零值陷阱（P2）**
    - 文件锚点：`internal/infra/clients/database.go:73-81`
    - 核实：连接池 `MaxOpenConns`/`MaxIdleConns`/`ConnMaxLifetime` 是否都有安全默认值或显式校验，避免零值导致连接无限增长或立即关闭。

12. **F10-1 CI backend job 必失败于 minio 健康检查（P1）**
    - 文件锚点：`.github/workflows/ci.yml:38`
    - 核实：`curl -f http://localhost:9000/minio/health/live` 是否已替换为无需 curl 的 TCP 探测或 busybox wget；push 后 backend job 是否全绿；`TestDockerExecutor_BuildAndRunNode` 是否真实执行（非被跳过）。

对每条修复项确认：
1. 修复是否已落地（代码中能否找到对应改动）；
2. 修复是否正确完整——有无绕过路径、边界遗漏（例如只改了入口 A 没改入口 B、校验可加在错误层、并发场景仍可乘）；
3. 修复是否引入新问题（接口/行为变化是否同步到全部调用方与前端/SDK）；
4. 承诺的测试是否真实存在且断言的是真实行为（不是恰好通过的假断言）。

## 复审重点 B：回归与新问题排查

- **修复触动的文件及其上下游**：F7 改动 `config.proto` → 需确认 `task generate-config` 与 `task wire-all` 已执行且无 diff 残留；`setup_token` 贯穿 `cmd/server`、console auth、配置绑定；metrics/health/shutdown 改动影响运维可观测性；按 Round 1「审查重点」重新扫一遍配置绑定、连接池、服务器装配、健康检查、迁移文件、CLI 入口。
- **Round 1 报告中的 P2/P3 未修项**：确认仍存在则原级保留，被修复波及的标注变化（如连接池零值、health 缓存、HTTP recovery 若已修则降级或关闭）。
- **按 round-1「通用检查项」重扫本模块**：安全（注入/越权/信息泄露/凭据处理）、正确性（错误处理/并发/事务边界）、一致性（与 AGENTS.md 约定、proto 注解、domain 端口签名）、测试质量。
- **本模块修复后特有风险点**：
  1. **配置生成与 Wire 一致性**：F7 改 `config.proto` 后若 `wire_gen.go` 或生成配置未同步，启动会直接失败或读到旧字段；需验证 `task generate-all` 无未提交 diff，且 `SetupToken` 字段已出现在生成代码与 `bind.go` 的 env 映射中。
  2. **setup_token 安全处理**：该 token 是首次 admin 创建的根信任；确认它不被打印到日志、不出现在错误响应、不写入 `.env.example` 默认值、不因为配置 proto 默认值而为空字符串通过校验。
  3. **graceful shutdown 重排后的 fx 生命周期竞态**：`cleanup` 移出 `OnStop` 后，需确认 fx 提供的依赖（如 DB、Redis、MinIO client）在 `runner.Run()` 返回时仍未关闭，否则 `cleanup` 访问依赖会 panic；同时确认不存在两个关闭路径导致 double-close。
  4. **CI minio 探测改动后的真实测试执行**：F10 仅换探测方式不够，需确认 `TestDockerExecutor_BuildAndRunNode` 在 CI 中真正运行过（日志中可见 RUN 而非 SKIP），否则 F5-3 的 Docker 解压权限修复仍无法验证。

## 输出要求

简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束

- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./cmd/... ./internal/infra/... ./internal/pkg/... ./internal/testutil/...` 与无外部依赖的纯单元测试辅助验证。
