# 修复任务 F7：基础设施与引导安全

## 角色

你是资深 Go 后端工程师（基础设施/运维领域），负责修复 Torchwood 基础设施与引导安全缺陷。
方案详见 `docs/review/fix-plan.md` §7（F7 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`、`docs/review/fix-plan.md` §7、
  `docs/developer/03-configuration.md`、`docs/developer/13-operations.md`
- 审查报告（背景）：`docs/review/` 下的 09 报告

## 修复清单

1. **Console 首个管理员引导可被抢占**（P0）：
   - 位置：`internal/app/console/setup.go:98-107`（SignUp 仅凭 admins 表为空即授 owner，
     并发竞态 + 无凭据门槛）、`proto/console/v1/auth.proto`（SignUp 服务级 PUBLIC）。
   - 修复：
     a. 新增配置 `security.setup_token`（`internal/pkg/config/config.proto` 增加字段 +
        `bind.go` 映射 `TORCHWOOD_SECURITY_SETUP_TOKEN`，执行 `task generate-config`）；
     b. `Setup.SignUp` 增加 setupToken 校验：配置未设置时 SignUp 拒绝
        （FailedPrecondition/PermissionDenied）；请求携带的 token 与配置比对失败同样拒绝；
     c. 并发兜底：首次性检查用 `pg_advisory_xact_lock` 串行化（Setup 依赖的 repo
        增加一个带锁的检查方法，或先做一个最小实现：admins 表创建时唯一约束 +
        捕获 AlreadyExists 冲突）；
     d. `Setup` 构造参数增加 setupToken（wire 变更，执行 `task wire-all`）；
     e. Console 前端登录页支持输入 setup token（`console/src/routes/Login.tsx` 增加
        可选输入框，未设置 token 时隐藏；改动小可做，不做则注明）。
2. **graceful shutdown 顺序错误**（P1）：`cmd/server/main.go:26-29` 将 cleanup（关
   DB/Redis）注册到 OnStop，而 lynx 在服务 GracefulStop **之前**执行 OnStop。
   修复：cleanup 移出 OnStop，在 `runner.Run()` 返回后调用；启用排水窗口
   （`WithDrainTimeout` 如 30s，按 lynx 框架 API 调整）。
3. **慢查询/调试 SQL 记录内联参数**（P1）：`internal/infra/clients/dbhook.go:54-74`
   bun 的 QueryEvent.Query 是参数内联后的 SQL，password_hash/token/OTP/API key 明文入日志；
   `configs/config.yaml.template:33` debug 默认 true。
   修复：日志只输出操作名+表名+占位符形态（e.Operation + e.Query 脱敏或不用内联 SQL）；
   password_hash/secret/token 等敏感列强制掩码；模板 `data.database.debug` 默认改 false。
4. **Prometheus metrics 无鉴权且默认监听全部接口**（P1）：`internal/infra/server/metrics.go:16-24`
   裸 promhttp；`configs/config.yaml.template:17` 默认 `:9040`。
   修复：默认改 `127.0.0.1:9040`；或加 scoped token 中间件（二选一，推荐前者）。
5. **JWT 弱默认被启动校验接受**（P1）：`cmd/server/provides.go:48-50` 仅校验非空；
   `.env.example:8` 公开示例值 `change-me-in-production`。
   修复：启动校验拒绝已知弱值（`change-me-in-production`、空、`minioadmin` 等黑名单）
   与 <32 字符；弱值时启动日志 Warn 明确提示。
6. **P2 补强**：
   - `internal/infra/server/grpc_gateway.go:100-108` HTTP 侧挂 `lynxhttp.Recovery()` 中间件
     （确认框架 API，gRPC 侧已内置）
   - `internal/infra/clients/database.go:37,89` ping 加 `context.WithTimeout(5s)`
   - `internal/infra/health/checks.go:78-90` 健康检查结果缓存（如 10s 快照，gRPC 侧已轮询）
   - `internal/infra/idgen/service.go:111-136` 项目策略加短 TTL 缓存；DB 错误不静默回退
   - `internal/infra/idgen/random_redis.go:16-31` random 集合加 TTL 或改 INCR 编码
   - `configs/config.yaml.template:51-52` S3 环境变量注释修正为
     `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` / `_SECRET_ACCESS_KEY`
   - `db/migrations/000003_document_catalog_composite_keys.up.sql` DROP CONSTRAINT 加
     `IF EXISTS`
   - `internal/pkg/database/dsn.go:14-21` 回退 DSN 默认 sslmode 改 require（或文档强提示）
   - `internal/infra/server/grpc_gateway.go:39-40,111-117` gateway 转发地址从 grpc.addr 推导

## 约束

- 本批次涉及 config proto 与 wire：完成后必须执行 `task generate-config` 与
  `task wire-all`，并 `go build ./...` 验证
- **不要**改 `pkg/grpc/interceptor/audit.go`（归属 F2 批次）
- 不修改 genproto（config.pb.go 由 generate-config 重新生成，属预期变更）
- 保持现有代码风格；不引入新依赖；除必要外不新增注释

## 验证

- `task generate-config`、`task wire-all`、`go build ./...`
- `go vet ./cmd/... ./internal/infra/... ./internal/pkg/... ./internal/app/console/...`
- `go test ./internal/pkg/config/... ./internal/pkg/database/...`（纯单元测试）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；列出配置变更说明
（新增 env 变量、默认值变化）与前端联动项（setup token 输入框是否完成）。
