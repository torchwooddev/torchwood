# Round 2 复审报告：09 - 基础设施与服务器装配

> 复审范围：F7（基础设施与引导安全）全部修复项 + F10-1（CI minio 探测）。
> 审查以当前工作区代码为准；F7 修复已合入提交 `633ad3f`，位于基线 `1288705` 之前。
> 运行辅助验证：`go vet ./cmd/... ./internal/infra/... ./internal/pkg/... ./internal/testutil/...` 通过；无外部依赖单元测试 `go test ./internal/pkg/config/... ./internal/pkg/database/... ./internal/infra/health/... ./internal/infra/idgen/... ./internal/infra/clients/... ./internal/app/console/...` 全部通过。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 证据（路径:行号） | 说明 |
|--------|------|-------------------|------|
| F7-1 Console 首个管理员引导可被抢占 | ✅ 已修复 | `internal/app/console/setup.go:118-125`、`internal/app/console/setup.go:135-151`、`internal/infra/bun/bunrepo/admin_repo.go:25-32`、`internal/pkg/config/config.proto:61`、`internal/pkg/config/bind.go:16`、`proto/console/v1/auth.proto:75,110` | 新增 `security.setup_token`（env `TORCHWOOD_SECURITY_SETUP_TOKEN`），未配置时 SignUp 返回 `FailedPrecondition`；请求 token 用 `subtle.ConstantTimeCompare` 比较；首次性检查在 `pg_advisory_xact_lock` 事务内串行化。集成测试 `internal/api/consolegrpc/auth_integration_test.go:93-152` 覆盖 E2E。 |
| F7-2 graceful shutdown 顺序错误 | ✅ 已修复 | `cmd/server/main.go:44-50`、`cmd/server/main.go:40` | `cleanup` 已移出 `OnStop`，在 `runner.RunE()` 返回后调用；启用 `WithDrainTimeout(30s)` 与 `WithShutdownTimeout(30s)`。Lynx 服务按注册逆序停止：`metrics → gateway → grpc → cleanup`。 |
| F7-3 慢查询/调试 SQL 记录内联参数 | ⚠️ 部分修复 | `internal/infra/clients/dbhook.go:55-78`、`internal/infra/clients/dbhook.go:89-94`、`configs/config.yaml.template:37` | 优先使用 `QueryTemplate`（占位符），兜底内联 SQL 经正则脱敏；`debug` 默认改为 `false`。但正则 `(?i)\b(password_hash\|password\|secret\|secret_hash\|token\|access_token\|refresh_token\|auth_token\|otp\|otp_code\|api_key)...` 无法覆盖 `INSERT INTO t (password_hash) VALUES ('x')` 形式的值，且不包含 `setup_token` 列。 |
| F7-4 Prometheus metrics 无鉴权且默认监听全部接口 | ✅ 已修复 | `internal/infra/server/metrics.go:18-22`、`configs/config.yaml.template:18` | 默认地址改为 `127.0.0.1:9040`；注释说明生产走反向代理 + 网络策略。 |
| F7-5 JWT 弱默认被启动校验接受 | ✅ 已修复 | `cmd/server/provides.go:71-90`、`.env.example:9` | `validateJWTSecret` 拒绝空值、<32 字符、精确弱值（`change-me` 等），对包含弱子串的密钥仅 Warn；`.env.example` 使用 42 字符随机串。 |
| F7-6a HTTP 侧补 panic recovery | ✅ 已修复 | `internal/infra/server/grpc_gateway.go:104` | `lynxhttp.Recovery()` 挂载在最外层；panic 转为 500 + 统一 JSON 错误体，不泄露堆栈。 |
| F7-6b 启动 ping 加超时 | ✅ 已修复 | `internal/infra/clients/database.go:92-94`、`internal/infra/clients/database.go:37-39` | DB ping 与 Redis ping 均使用 `context.WithTimeout(..., 5*time.Second)`，超时返回明确错误。 |
| F7-6c health 检查结果缓存 | ✅ 已修复 | `internal/infra/health/checks.go:87-114` | `Details` 带 10s TTL 快照缓存；并发探测用 `sync.WaitGroup` 并行；单个 panic 被 recover。 |
| F7-6d idgen：每次生成打项目查询 + DB 抖动静默回退 | ⚠️ 部分修复 | `internal/infra/idgen/service.go:164-189`、`internal/infra/idgen/service.go:150-155` | 已加 30s 项目策略缓存，避免每次生成打 DB。但设计选择为「项目查询失败时显式返回错误」，而非 fix-plan 描述的「静默回退到本地生成/缓存」；DB 异常时 idgen 不可用。 |
| F7-6e random 策略 Redis 集合无界 | ⚠️ 部分修复 | `internal/infra/idgen/random_redis.go:15,31` | 已给 Redis 集合设置 30 天 TTL，空闲后自动释放。但未设置最大容量/主动清理，长期高频生成 + 短长度随机空间时仍有 OOM 风险。 |
| F7-6f 连接池零值陷阱 | ❌ 未修复 | `internal/infra/clients/database.go:76-84` | 当 `pool` 配置存在但字段为零值时，直接调用 `SetMaxIdleConns(0)`、`SetMaxOpenConns(0)`。`MaxOpenConns(0)` 在 Go 标准库中表示无限制，仍可能导致连接无限增长。 |
| F10-1 CI backend job 必失败于 minio 健康检查 | ✅ 已修复（代码层面） | `.github/workflows/ci.yml:39` | `curl -f http://localhost:9000/minio/health/live` 已替换为 `bash -c 'exec 3<>/dev/tcp/127.0.0.1/9000'`。但本次复审无法验证 CI 真实执行结果及 `TestDockerExecutor_BuildAndRunNode` 是否非跳过。 |

**统计**：✅ 7 项，⚠️ 3 项，❌ 1 项，🔴 0 项。

---

## 2. 新发现问题

### 🟠 P1

#### P1-1 `data.database.pool` 零值仍可导致连接无限制增长
- **位置**：`internal/infra/clients/database.go:76-84`
- **问题**：`newDatabase` 在 `cfg.GetPool() != nil` 分支直接透传 `max_idle_conns`/`max_open_conns` 等字段。若配置文件中显式写 `max_open_conns: 0` 或 proto 默认值导致为 0，`sql.DB.SetMaxOpenConns(0)` 在 Go 标准库语义下为「无限制」，高并发时可能压垮 PG。
- **影响**：生产配置误配或自动化生成配置时触发连接风暴。
- **建议**：对 pool 字段加校验，0 值使用安全默认值（如 `max_open_conns` 默认 `4*GOMAXPROCS`，`max_idle_conns` 默认 `max_open`），并记录 Warn。

#### P1-2 SQL 慢查询/调试日志的脱敏正则存在绕过
- **位置**：`internal/infra/clients/dbhook.go:82,89-94`
- **问题**：`sensitiveColumnPattern` 只匹配 `column = value` / `column IN (...)` 等比较/赋值上下文，无法覆盖 `INSERT INTO admins (password_hash) VALUES ('$plaintext')`；同时未包含 `setup_token` 列。
- **影响**：若 `QueryTemplate` 为空（某些 bun 查询路径或旧驱动），INSERT 语句中的敏感值会原样入日志；`setup_token` 作为新的根信任凭据也可能被记录。
- **建议**：扩展正则覆盖 `INSERT ... VALUES (...)` 中目标列对应的值；将 `setup_token` 加入敏感列名单；并为 `redactQuery` 补 INSERT 场景的单元测试。

### 🟡 P2

#### P2-1 idgen 项目策略解析选择「显式失败」而非「静默回退」
- **位置**：`internal/infra/idgen/service.go:150-155`
- **问题**：`resolveStrategy` 在项目 settings 查询失败时直接返回错误，注释说明「宁可显式失败」。这与 fix-plan F7-6d 描述的「DB 抖动静默回退到本地生成或缓存」不完全一致。
- **影响**：项目 DB 短暂不可用时，所有依赖 idgen 的写请求（创建用户、文档等）失败，可用性降低；但避免了策略错用导致 ID 语义破坏。
- **建议**：若要保持显式失败，应在运行手册中明确说明；或实现带熔断的缓存回退（缓存命中且未过期时直接复用缓存策略，仅在缓存也缺失时失败）。

#### P2-2 random ID 策略 Redis 集合缺少容量上限
- **位置**：`internal/infra/idgen/random_redis.go:15,31`
- **问题**：集合通过 30 天 TTL 自动过期，但没有 `MAXLEN`、LRU 淘汰或按容量截断。对高吞吐 + 短长度随机 ID（如 8 位 numeric）场景，集合会无限增长直到 Redis 内存上限。
- **影响**：极端情况下 Redis OOM，导致 random ID 策略完全不可用。
- **建议**：为集合增加容量上限（如 `SCARD > N` 时拒绝生成并告警），或改用按时间窗口分片 key（每日/每周新 key）。

#### P2-3 health 详情缓存可能在故障切换时引入 10s 延迟
- **位置**：`internal/infra/health/checks.go:87-114`
- **问题**：10s TTL 内直接返回缓存结果；若依赖在 TTL 期间由 ok 变为 unavailable，外部轮询最多延迟 10s 感知。并发缓存失效时多个请求会同时打穿探测。
- **影响**：readiness 由 Lynx 实时聚合（`checkers.Deps()` 不使用缓存），故 K8s readiness 不受影响；但 `Details` RPC/HTTP 消费方可能看到 10s 旧状态，且存在并发探测风暴。
- **建议**：对缓存失效加单飞（singleflight）或写时锁，避免并发打穿；或将 TTL 设为可配置。

#### P2-4 graceful shutdown 服务停止顺序与任务书描述不完全一致
- **位置**：`cmd/server/provides.go:103-107` + Lynx `Run()` 逆序停止
- **问题**：服务注册顺序为 `grpcServer, gatewayServer, metricsServer`，Lynx 逆序停止为 `metrics → gateway → grpc`，而任务书期望 `gateway → grpc → metrics`。实际语义合理（gateway 依赖 grpc，应先停 gateway），但与复审检查项文字有出入。
- **影响**：无功能性影响；metrics 先停会导致关停期间缺少 metrics，属可接受。
- **建议**：在代码注释中显式说明停止顺序，避免后续审计误解。

### 🟢 P3

#### P3-1 `cleanup()` 在 `main.go` 中无超时保护
- **位置**：`cmd/server/main.go:47-50`
- **问题**：`cleanup()` 关闭 DB/Redis/MinIO 等连接无超时；若某个 Close 挂起，进程无法退出。
- **影响**：极端关停场景下进程可能长期挂起。
- **建议**：给 `cleanup()` 包装 `context.WithTimeout` 或至少记录开始/结束日志。

---

## 3. 模块总体结论

- **修复完成度估计**：约 75%。F7 的 P0/P1 核心项（setup token 抢占防护、shutdown 排水、metrics loopback、JWT 弱密钥、HTTP recovery、ping 超时、health 缓存）均已落地并通过测试；剩余主要风险是连接池零值陷阱未彻底消除、SQL 日志脱敏存在 INSERT 绕过。
- **剩余风险 Top 3**：
  1. **连接池零值导致无限制连接**（P1-1）：配置层唯一未闭环的高危项，建议立即补校验/默认值。
  2. **SQL 日志敏感字段脱敏不完整**（P1-2）：INSERT 场景与 `setup_token` 列可能泄露，建议扩展正则并补测试。
  3. **idgen 项目 DB 抖动时拒绝服务**（P2-1）：需明确运维预期，必要时加缓存回退。
- **是否建议关闭本模块审查**：不建议立即关闭。待 P1-1、P1-2 修复并补充单元测试后再关闭；其余 P2/P3 可排入后续迭代。
