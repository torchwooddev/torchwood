# 全量审查报告（Round 3）：09 - 基础设施与服务器装配

> 审查范围：`internal/infra/{bun,clients,idgen,health,server}`、`internal/pkg/{config,contexts,database,buildinfo}`、`cmd/server/`、`cmd/client/`、`db/migrations/`、`internal/testutil/`；交叉 `.github/workflows/ci.yml`、`Taskfile.yml`、`docker/local/docker-compose.yml`、`configs/config.yaml.template`、`.env.example`、`console/embed.go`。
> 基线：`docs/review/prompts/09-infra-assembly.md`、`AGENTS.md`、`docs/developer/03-configuration.md`、`docs/developer/13-operations.md`、Round 2 报告 `docs/review/round2/reports/09-infra-assembly.md` 及 G7/G11 修复。
> 性质：只读审查，未改源代码。本轮未执行 `go vet` / `go test`（审查环境无 shell）；结论来自静态通读与交叉对照。

---

## 1. 摘要

Round 2 留下的两处 P1（连接池零值、SQL 脱敏 INSERT 绕过）以及 random 集合无上限、health 并发打穿、cleanup 无超时，均已在 G7/G11 落地。JWT 校验比 Round 2 更严（弱子串由 Warn 升级为启动拒绝）。配置绑定、trusted proxies 默认空、metrics 回环、setup token 门禁、graceful shutdown 排水、CI `go test ./...` 主路径均健康。

未发现 P0。本轮新发现集中在：`Details` 探测与请求 ctx 耦合（可能缓存虚假 unavailable，极端竞态下甚至空依赖被判 ok）、`000003` down 因 FK 顺序不可执行、MinIO 服务健康检查仍假设镜像内有 `bash`。无阻断级装配缺陷。

**Verdict：CONDITIONAL PASS（有条件通过）。** Round 2 P1 已闭环；无 P0。建议先消化 P2（health 缓存语义、000003 down、CI MinIO healthcheck）再关闭本模块。

---

## 2. Round 2 遗留项复验

| 编号 | 项 | 本轮结论 | 证据 |
|------|----|----------|------|
| F7-1 | setup token 防抢占 | ✅ 保持 | `config.proto:63-65`、`bind.go:16`；用例层校验不在本模块展开 |
| F7-2 | shutdown 排水 + cleanup 后置 | ✅ 加强 | `cmd/server/main.go:41-65`：`DrainTimeout`/`ShutdownTimeout` 30s；cleanup 在 `RunE` 之后，另有 10s 超时 |
| F7-3 / R09-P1-2 / G7-2 / G11 | SQL 脱敏 INSERT | ✅ 已补 | `dbhook.go:82-104`：`setup_token` 入名单；`sensitiveInsertPattern` 覆盖批量/跨行 VALUES；`dbhook_test.go:117-174` 真实断言 |
| F7-4 | metrics 默认回环 | ✅ 保持 | `metrics.go:17-22`、模板 `:17-18` 为 `127.0.0.1:9040` |
| F7-5 | JWT 弱密钥 | ✅ 严于 R2 | `provides.go:69-86` 弱子串直接 `return error`（R2 仅 Warn）；`provides_test.go` 覆盖 padded/大小写/子串 |
| F7-6a | HTTP Recovery | ✅ 保持 | `grpc_gateway.go:104` |
| F7-6b | 启动 ping 超时 | ✅ 保持 | `database.go:37-39,92-94` |
| F7-6c / R09-P2-3 | health 缓存 + singleflight | ✅ 已补 | `checks.go:59-61,109-151`；`checks_test.go:95-132` 断言 16 并发只探测 1 次 |
| F7-6d / R09-P2-1 | idgen 显式失败 | ✅ 保持（设计） | `service.go:150-155`；运维手册 `13-operations.md` §4.4 已写明 |
| F7-6e / R09-P2-2 | random 集合容量 | ✅ 已补 | `random_redis.go:19-22,43-47` 上限 100 万；`random_capacity_test.go` |
| F7-6f / R09-P1-1 / G7-1 | 连接池零值 | ✅ 已修 | `database.go:76-79,114-134` `normalizePoolSizes`；`database_test.go` 覆盖 0/负值 |
| R09-P2-4 | 关停顺序注释 | ⚠️ 注释与框架相反 | 见 P3-1 |
| R09-P3-1 | cleanup 无超时 | ✅ 已修 | `main.go:50-65` |
| F10-1 | CI MinIO 探测 | ⚠️ 命令已换，镜像内是否有 bash 未证实 | `.github/workflows/ci.yml:39`；见 P2-3 |

---

## 3. 已核实健康

### 3.1 配置绑定与敏感项

- `configKeys()` 按 proto json tag 反射全部叶子键并 `BindEnv`（`bind.go:55-58,72-78`）。`envNameForKey` 将 `data.database.source` 映射为 `TORCHWOOD_DATA_DATABASE_SOURCE`，与 `AGENTS.md` / `03-configuration.md` 一致。
- MinIO 凭据键为 `storage.s3.access_key_id` / `secret_access_key` → `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` / `_SECRET_ACCESS_KEY`；`bind_test.go:155-196` 覆盖绑定与 unmarshal。
- proto 内部字段（`state`/`sizeCache`/`unknownFields`）无 json tag，不会被绑成环境变量。
- `security.trusted_proxies` 模板默认 `[]`（`config.yaml.template:32`）；未配置时不采纳 XFF。`StringToSliceHookFunc(",")` 支持逗号分隔 CIDR。
- JWT：空 / `<32` / 含 `change-me`/`secret`/`password`/`torchwood`/`minioadmin` 启动失败。`.env.example:9` 为 42 字符、不含弱子串。
- `setup_token` 默认空 = 引导关闭。`.env.example` 仅给开发示例，不进模板明文默认。
- fail-fast：DSN 空/`非 postgres` scheme 在 `newDatabase` 报错（`database.go:63-73`）；Redis ping、DB ping 5s 超时；S3 endpoint 非法则 `NewMinioObjectStore` 失败并 `cleanup()`（`wire_gen.go:59-62`）。
- `--config-dir` 默认 `./configs`（`cmd/server/main.go:35`）。`DefaultBindConfigFunc` 在 flag 非空时 `AddSearchPath`（lynx v1.3.0 `lynx.go:305-317`），行为正确。文档写「extraPaths 默认 `./configs`」不精确（实际靠 flag 默认值），但不影响运行。

### 3.2 数据库 / Redis / S3

- 连接池：配置存在时走 `normalizePoolSizes`；`max_open<=0` → `4*GOMAXPROCS`，`max_idle<=0` → `max_open`，并 Warn。无 pool 块同样使用该默认。
- 慢查询：默认 500ms；`"0"` 禁用；`debug` 模板为 `false`。日志优先 `QueryTemplate`，兜底 `redactQuery`。
- Redis 客户端只设 Addr/Password/DB（`database.go:106-112`）；启动必 ping。测试普遍用 miniredis，CI 不需要 Redis 服务。
- `SourceFromEnv` 回退 DSN `sslmode=require`（`dsn.go:15-22`）；server 主路径使用配置 DSN 原样，本地 `.env.example` 为 `sslmode=disable`。
- S3 凭据走 `TORCHWOOD_STORAGE_S3_*`。`storage.provider: local` 仅为配置占位（`docs/developer/07-storage.md:176`），装配始终 `NewMinioObjectStore`。

### 3.3 服务器装配

- gRPC 模板 `127.0.0.1:9060`；gateway 用 `insecure` 转发到由 `grpc.addr` 推导的端点（`grpc_gateway.go:113-127`）。
- HTTP `:9080` 对公网暴露属预期。中间件：CORS 包在 combined 外，`lynxhttp.Recovery()` 再包一层。鉴权在 gRPC 拦截器（clientInfo → auth → audit）。
- CORS：`allow_credentials=true` 时丢弃 `*`（`cors.go:18-28`）；模板给显式 origin。
- Console SPA：`/console` 与 `/console/` 前缀；未知路径 fallback `index.html`（`console.go:20-26`）；安全头 + CSP。测试覆盖 headers 与 fallback。
- metrics 无鉴权但默认仅回环。
- graceful shutdown：排水 30s → 服务 Stop → `RunE` 返回后 cleanup（10s 上限）。Wire 失败路径对已创建的 `DataClients` 调用 `cleanup()`。
- gRPC 方法缺 authz 注解启动失败（`grpc.go:119-122,138-174`）；`AssertAPIKeyScopeCoverage` fail-closed。
- `Set-Cookie` / `X-Torchwood-Project` header matcher 有回归测试。

### 3.4 ID 生成 / 健康检查 / 上下文

- idgen 单例；snowflake 带 mutex；项目策略 30s 缓存；解析失败显式报错（已文档化）。
- random：SADD + 30 天 TTL + 容量护栏。sequence 走 Redis INCR。
- Health：`Deps()` 供 lynx readiness **实时**探测（失败 503）；`Details` 10s 缓存 + singleflight，供公开 `/v1/health`。panic recover。liveness 恒 200。
- `/v1/health` 设计为 gRPC OK + body `status=unavailable`，503 由 `/healthz/readiness` 承担（`13-operations.md` §5）。用 HTTP 状态码做 LB 探针会掩盖故障——属约定，不是实现漏洞。
- context 键为自定义类型（`keys.go:3-10`），非 string。`Principal` 缺失返回 `(nil, false)`。

### 3.5 迁移 / testutil / CI / CLI

- 编号 000001–000010 连续。up 以 `IF NOT EXISTS` / `IF EXISTS` 为主；bun model 与静态表列对齐（含 `is_system`/`disabled`/functions/oauth）。
- testutil：每测独立库 `<prefix>_<pid>_<seq>`（小写），`t.Cleanup` 先 `pg_terminate_backend` 再 `DROP DATABASE`。DSN 缺失 fail-fast。
- CI backend：`go vet ./...` + `go test ./...`（非 `-short`）+ SDK/demo/build；Postgres + MinIO service；测试 DSN 指向独立库。Redis 由单测 miniredis 覆盖。
- CLI：`godotenv.Load`；缺 API key 非零退出；`--tls` 明确拒绝；`import_guard` 禁止 genproto/grpc；version 由 ldflags 注入。

### 3.6 Wire 单例

`NewDataClients` / `idgen.NewService` / snowflake / strategyCache 均为进程单例。缓存写有 mutex。未发现隐含多实例竞态。失败路径在 `wire_gen.go` 多处调用 `cleanup()`，不会把半开连接留给进程。

---

## 4. 问题清单

### 🔴 P0 严重

无。

### 🟠 P1 高

无。

### 🟡 P2 中

#### P2-1 `Details` 探测绑定请求 ctx，可污染 10s 缓存；首次探测等待方取消会得到空依赖

- **位置**：`internal/infra/health/checks.go:109-151`、`155-169`；消费方 `internal/api/servergrpc/health.go:23-32`
- **问题**：
  1. `checkOne` 用调用方 ctx 派生超时。公开 `Health.Check` 传入请求 ctx：客户端取消/超时会使全部依赖记为 `unavailable`，并写入 10s 快照。
  2. 缓存未命中且已有 inFlight 时，等待方若 `ctx.Done()`，`snapshot()` 在**首次**探测完成前对 `cached==nil` 返回空切片。`Health.Check` 对空 `dependencies` 保持 `status=ok`。
- **影响**：`/v1/health` 可能连续 10s 报全红（假故障），或在启动竞态下报 ok 且无依赖（假健康）。`/healthz/readiness` 走 `Deps()`，K8s 探针不受影响。
- **建议**：探测用 `context.Background()` + 自身超时，与请求取消解耦；等待方取消时若快照为空则同步再等或返回 `unavailable`；`Health.Check` 对空依赖不要判 ok。

#### P2-2 `000003` down 因 FK 引用顺序无法执行

- **位置**：`db/migrations/000003_document_catalog_composite_keys.down.sql:6-7,16-17,24-25`
- **问题**：indexes/attributes 在 collections 仍为复合主键 `(project_id, database_id, id)` 时就 `REFERENCES document_collections (id)`；collections 在 databases 仍为 `(project_id, id)` 时就 `REFERENCES document_databases (id)`。000003 up 之后 `id` 上没有单独 UNIQUE。
- **影响**：`migrate down` 从 3→2 会失败。项目升级路径是 forward-only，生产回滚不能靠该 down。
- **建议**：down 先把父表改回 `PRIMARY KEY (id)`，再加子表 FK；或在运维文档标明 000003 起 down 不可用、回滚靠备份。

#### P2-3 CI / compose 的 MinIO healthcheck 仍依赖镜像内 `bash`

- **位置**：`.github/workflows/ci.yml:39`；`docker/local/docker-compose.yml:45`
- **问题**：F10-1 把 `curl …/minio/health/live` 换成 `bash -c 'exec 3<>/dev/tcp/127.0.0.1/9000'`。官方 `minio/minio` 多为 scratch/静态二进制，容器内往往既无 curl 也无 bash。GHA `options: --health-cmd` 在 **service 容器内**执行。
- **影响**：若镜像无 bash，service 一直 unhealthy，backend job 在跑测试前失败——与 F10-1 同类。本轮无法核实线上 CI 是否已绿。
- **建议**：去掉 service healthcheck，在 ubuntu job 里 `curl -f http://localhost:9000/minio/health/live` 重试等待；compose 同步。

#### P2-4 模板默认 `dev_log_otp` / `dev_log_sms: true`

- **位置**：`configs/config.yaml.template:83-84`
- **问题**：模板是生产起点，这两项为 true。SMTP/SMS 未配时 OTP 进应用日志。
- **影响**：按模板部署且忘记改开关，验证码进日志。
- **建议**：模板改 `false`；仅 `.env.example` / 本地 overlay 开 true。

#### P2-5 多副本使用 snowflake 且未改 `node_id` 会撞号

- **位置**：`internal/infra/idgen/service.go:46,60-61`；`configs/config.yaml.template:100`
- **问题**：`node_id` 默认 0。水平扩展且 `default_strategy`/`resources.*` 为 snowflake 时，各实例同一 node，时钟重叠窗口内 ID 冲突。
- **影响**：默认策略是 uuid，未切 snowflake 无事。切了必须每实例唯一 `TORCHWOOD_IDGEN_SNOWFLAKE_NODE_ID`。
- **建议**：多实例启动校验 node_id，或文档列为必配；可考虑 hostname hash。

### 🟢 P3 低

#### P3-1 关停顺序注释与 oklog/run 实际相反

- **位置**：`cmd/server/provides.go:96-103`
- **问题**：注释写正常关停为「注册顺序 grpc → gateway → metrics」。lynx v1.3.0 `Run` 用 oklog/run，interrupt **LIFO**，实际为 metrics → gateway → grpc。失败路径 `stopServices` 才是显式逆序。
- **影响**：无功能问题；实际先停 gateway 再停 grpc 更合理。注释会误导后续审计。
- **建议**：按 LIFO 改正注释。

#### P3-2 CORS `expose_headers` 未实现；`Vary` 不完整

- **位置**：`internal/pkg/config/config.proto:34`；`internal/infra/server/cors.go:31-50`
- **问题**：proto/文档有 `expose_headers`，中间件从不写 `Access-Control-Expose-Headers`。`Vary: Origin` 仅在 `credentials && origin != ""` 时设置。
- **影响**：前端读自定义响应头可能被浏览器挡住；中间缓存可能串 origin。
- **建议**：实现 expose；反射 Origin 时始终 `Vary: Origin`。

#### P3-3 脱敏名单不含 `client_secret`；转义引号/嵌套括号仍可能漏

- **位置**：`internal/infra/clients/dbhook.go:82,90`
- **问题**：`\bsecret\b` 匹配不到 `client_secret`（下划线是 word char）。`'it''s'`、`VALUES (jsonb_build_object(...))` 可能匹配失败。主路径有 `QueryTemplate`，属兜底残留。
- **建议**：名单加 `client_secret`；补转义引号用例。

#### P3-4 `nextSequence` 在 `rdb==nil` 时静默 UUID

- **位置**：`internal/infra/idgen/service.go:191-194`
- **问题**：与 random（`ErrRandomRedisRequired`）和「策略失败显式报错」不一致。生产 Wire 总有 Redis，仅测试/误构会走到。
- **建议**：与 random 一样返回错误。

#### P3-5 迁移/模型上的已知 MVP 与缺口

- `000010_functions.up.sql:34`：`function_variables.value` 明文，SQL 已注明 MVP。
- `functions.project_id` 无 FK，删项目会留孤儿（对比 `api_keys` 有 `ON DELETE CASCADE`）。
- `000007`/`000008` down 是 best-effort 数据回放，不能精确还原每行权限。
- `000008` 只扫 `TORCHWOOD_[0-9]+_default` schema，非 default 库的 `_perms` 不动。

#### P3-6 Redis 客户端未显式超时 / TLS

- **位置**：`internal/infra/clients/database.go:106-112`
- **问题**：依赖 go-redis 默认超时；无 TLS 选项。本地 compose 无密码 Redis 可接受。
- **建议**：配置项补 dial/read/write timeout 与 TLS；生产远程 Redis 再启用。

#### P3-7 其它小项

- `parseDuration`（`grpc.go:241-249`）与 pool 的 lifetime/idle 解析失败时静默回退/忽略，无 Warn。
- `strategyCache` 只覆盖、不淘汰过期 key；项目极多时微涨。
- testutil `CreateTestProject`/`CreateTestAdmin`/`CreateTestAPIKey` 用 `panic` 而非 `t.Fatal`；`CREATE DATABASE` 拼接未引用标识符（名字由自身生成，风险限于被污染的测试 DSN）。
- JWT 黑名单子串 `secret` 可能误杀含该片段的随机口令（启动失败，安全方向可接受）。
- `storage.provider=local`、`telemetry.enabled`、无 Dockerfile / `task build-docker` 未接线：文档已标明，保持记录。
- CLI `--tls` 占位拒绝：已知。
- `portFromAddr`（`grpc_gateway.go:129-135`）疑似死代码。

---

## 5. 模块结论

**生产就绪度**：装配层已达可上线水平——密钥/凭据不进默认日志、trusted proxies 默认不信任、metrics/gRPC 默认回环、启动 fail-fast、排水关停、authz 缺注解拒启、CI 会跑全量 `go test`。Round 2 的 P1 已关闭。

**最需优先处理的 3 项**：

1. **P2-1 health `Details` 与请求 ctx 耦合**——唯一可能让公开健康面给出假 ok/假红的逻辑问题。
2. **P2-3 CI MinIO healthcheck**——若镜像无 bash，整条 backend CI 仍会红；应用侧测试本身是实跑的。
3. **P2-2 `000003` down 不可执行**——不影响前向发布，但破坏「down 可逆」假设；回滚只能靠备份。

**是否建议关闭本模块审查**：不建议立即关闭。P2-1 补测 + P2-3 用 job 侧探测验证一次 CI 绿灯后，即可关闭；P2-2/P2-4/P2-5 与全部 P3 可进下一迭代。
