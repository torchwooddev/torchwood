# Torchwood 部署与运维指南

> 基于当前实现编写（以 `cmd/server/main.go`、`Taskfile.yml`、`docker/local/docker-compose.yml`、`internal/pkg/config/config.proto`、`internal/infra/health/checks.go` 为准）。
> 关联：`docs/developer/11-testing.md`（测试与门禁）、`AGENTS.md`。
> 修订记录：2026-08-23 重写（核对 Lynx 三进程、compose 三件套、`task build` 含 `console:build`、`TORCHWOOD_SECURITY_*`、`TORCHWOOD_ENV` 排水、`/healthz`/`/metrics`/`migrate`/`backup`）；2026-09-05 增补 §6.6 vector（pgvector）启用 runbook（转出门禁 A3：镜像预装/superuser 引导两路径 + 验证 SQL + 实测记录），§2 镜像行同步 pgvector 基座。

---

## 1. 运行形态：Lynx 三进程

| 进程 | 入口 | 职责 |
|------|------|------|
| **server** | `cmd/server/main.go` | gRPC（`server.grpc.addr` 127.0.0.1:9060）+ grpc-gateway HTTP `/v1/*` + 独立 `serverhttp`（Storage 上传下载、OAuth/Functions/Payments）+ Metrics + Admin Console SPA（`console.Dist`）+ 健康/版本 |
| **worker** | `cmd/worker/main.go` | 函数异步执行消费者：`BRPOP torchwood:queue:functions-executions`，4 goroutine 并发；启动对账——超 1h 的 `queued/building/running` 标 `failed`；瞬时失败重入队最多 3 次（`maxProcessAttempts`） |
| **CLI** | `cmd/client/main.go` | `bin/torchwood[.exe]`，经 `sdk/go/server` 的 `InvokeJSON` 走 gRPC 调 Server API（不直连 `genproto`） |

本地开发：

```bash
task dev:server   # go run ./cmd/server
task dev:worker       # go run ./cmd/worker
```

> 仅使用数据库/存储时 `server` 单进程即可；启用 Functions 才需 `worker`。

### 1.1 端口（`configs/config.yaml.template` 默认）

| 端口 | 用途 | 配置键 |
|------|------|--------|
| `:9099` | HTTP（gateway + `/console/`） | `server.http.addr` |
| `127.0.0.1:9060` | gRPC（回环，gateway 同机转发） | `server.grpc.addr` |
| `127.0.0.1:9040` | Prometheus `/metrics`（留空回退同值） | `server.metrics.addr` |

`cmd/server/main.go:16` 注入 `lynx.WithDrainTimeout`（见 §4.3）与 `lynx.WithShutdownTimeout(30s)`，`OnStop` 后才执行 `cleanup`（避免排水期关连接池）。

---

## 2. 外部依赖（`docker/local/docker-compose.yml`）

`task docker:up` / `task docker:down` / `task docker:purge`（`-v` 删卷）一键启停。

| 依赖 | 镜像 | 端口 | 用途 | 健康检查 |
|------|------|------|------|----------|
| PostgreSQL | `pgvector/pgvector:0.8.6-pg18`（pgvector 预装基座，锁 0.8.6 + PG18） | `5432` | 元数据静态表（`bun` + `golang-migrate`）与动态文档层（含 vector 列/HNSW） | `pg_isready` |
| Redis | `redis:7-alpine` | `6379` | 队列/上传会话/ID 生成 | `redis-cli ping` |
| MinIO | `minio:RELEASE.2024-11-07T00-52-20Z` | `9000`/`9001` | S3 兼容对象存储 | `exec 3<>/dev/tcp/127.0.0.1/9000` |

- 均挂 `postgres_data`/`redis_data`/`minio_data` 卷；
- 生产可将 `storage.provider: "s3"` 指向任意 S3 兼容服务；
- 环境变量均支持 `${POSTGRES_USER:-torchwood}` 等覆盖（见 `compose` 中 `environment` 与 `ports` 模板）；
- PostgreSQL 为 pgvector 预装基座（vector 非 trusted extension，启用路径与验证见 §6.6）；
- **字符串排序语义（locale=C，现状注记）**：compose 与 CI 显式 `POSTGRES_INITDB_ARGS="--locale=C"`（61ac141；Debian/glibc 基座默认 `en_US.utf8` 与原 musl 基座行为不一致，锁 C 恢复跨镜像/跨平台确定性）——**string 列的 `ORDER BY`/范围比较 = UTF-8 码点字节序**，非语言学序（中文不按拼音、`'Z'<'a'` 大小写混排）；等值与 equal/filter 语义不受影响。决策背景与选项见 `docs/developer/15-exit-poc.md` A9（推荐维持 C，待拍板）。initdb 参数仅首次建库生效，已有卷不受影响；改 locale 须整库 dump→重建→restore 并 REINDEX 全部 text 索引。

---

## 3. 构建与发布

### 3.1 `task build`（`Taskfile.yml:127`）

```yaml
build:
  deps:
    - task: console:build
  cmds:
    - go build -ldflags "-X main.version={{.VERSION}} -X main.commit={{.COMMIT}} -X main.date={{.DATE}}" -o ./bin/ ./cmd/server
    - go build -ldflags "..." -o ./bin/ ./cmd/worker
    - go build -ldflags "..." -o ./bin/torchwood{{if eq .OS "Windows_NT"}}.exe{{end}} ./cmd/client
```

- `console:build`（`Taskfile.yml:81`）为 `pnpm run build`（`tsc -b && vite build`），产物 `console/dist/` 再被 `console/embed.go:8` 的 `//go:embed dist` 打进二进制，由 `internal/infra/server/console.go:7` 的 `NewConsoleHandler` 在 `/console/` 下 serve（含 SPA fallback 与 `X-Frame-Options: DENY`/CSP 等安全头）；
- 版本：`VERSION=$(git describe --tags --always)`、`COMMIT=$(git rev-parse --short HEAD)`、`DATE=$(date +%Y%m%d%H%M%S)`，注入 `main.version`/`main.commit`/`main.date`（全小写），由 `GET /v1/server/health/version` 暴露；
- Windows 产物为 `bin/server.exe` / `bin/worker.exe` / `bin/torchwood.exe`；
- **修改 Console 后必先 `task console:build` 再 `task build`**，否则 embed 旧 `dist/`。

### 3.2 Docker 镜像（`task docker:build`）

`Taskfile.yml:195` 已定义：`DOCKER_IMAGE='torchwood:1.0.0-{{GIT_VERSION}}-{{TIMESTAMP}}'`，命令为 `docker build -t {{.DOCKER_IMAGE}} .`。仓库根已提供多阶段 `Dockerfile`（builder 构 console + 三二进制，runner 含最小运行时），可直接：

```bash
task docker:build
docker run --env-file .env -p 9099:9099 -p 9060:9060 torchwood:1.0.0-xxx-yyy
```

---

## 4. 生产配置要点

配置 schema `internal/pkg/config/config.proto`，模板 `configs/config.yaml.template`。`cmd/server/main.go:8` 先 `godotenv.Load()` 再 `config.NewBindConfigFunc()` 从 `./configs` 绑定；环境变量前缀 `TORCHWOOD_`，点号路径转下划线大写（如 `data.database.source` → `TORCHWOOD_DATA_DATABASE_SOURCE`）。

### 4.1 必配项

| 配置 | 环境变量 | 说明 |
|------|----------|------|
| JWT secret | `TORCHWOOD_SECURITY_JWT_SECRET` | ≥32 字符，含弱子串（`change-me`/`secret`/`torchwood`/`minioadmin`）拒绝启动；`access_ttl` 默认 `15m`、`refresh_ttl` 默认 `7d` |
| Setup token | `TORCHWOOD_SECURITY_SETUP_TOKEN` | 未设置时首次 `SignUp` 被拒（`internal/app/console/setup.go`），生成：`openssl rand -hex 32` |
| DB 连接串 | `TORCHWOOD_DATA_DATABASE_SOURCE` | Postgres DSN；`task db:migrate` 优先读同一变量 |
| S3 凭据 | `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` / `TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY` | 生产务必覆盖本地 `minioadmin` |
| API Key 头 | `security.api_key.header` | 默认 `x-api-key` |

其它：`storage.endpoint`/`bucket`/`region`（默认 `us-east-1`）、`data.redis.addr/password`、`storage.provider`（`s3`/`minio`/`local`）、`idgen.default_strategy`（`uuid`/`ulid`/`snowflake`/`sequence`/`random`）、`telemetry.enabled`（当前未接线，保持 `false`）、`messaging.smtp`（`dev_log_otp`/`dev_log_sms` 为无 SMTP 时的开发兜底）。

### 4.2 反向代理与真实 IP

```yaml
security:
  trusted_proxies: []  # 默认不信任 X-Forwarded-For / X-Real-Ip
```

- 需恢复真实 IP：`TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32,10.0.0.0/8`（逗号分隔 CIDR，`internal/grpc/interceptor/trusted_proxy.go`）；
- 仅直连 peer 命中可信网段时才采信 `X-Forwarded-For` 首跳；gateway 与 gRPC 同进程部署时须包含 `127.0.0.1/32`。

### 4.3 关停排水：`TORCHWOOD_ENV`

`cmd/server/main.go:13` 在 `NewRunner` 前即确定 `drainTimeout`（`config.CurrentDrainTimeout()`）：

| `TORCHWOOD_ENV` | `drainTimeout` | 说明 |
|-----------------|----------------|------|
| `development` | `0` | 本地 `Stop` 立即关 |
| `production`（默认/未设） | `30s` | 先排水 30s 再停，给 LB 摘流 |
| 任意值 + `TORCHWOOD_SERVER_DRAIN_TIMEOUT` | 显式覆盖 | 如 `15s`、`0` |

- Lynx 在绑定 YAML 前就需要该值，故不进 `config.proto`；
- K8s `terminationGracePeriodSeconds` 应大于 `drainTimeout + shutdownTimeout(30s)`，否则排水未完即被 `SIGKILL`。

### 4.4 ID 生成策略的 fail-closed 语义

`random`/`sequence` 等需读项目设置的策略会先取 `settings.idgen.*`（30s 缓存），**读取失败（DB 抖动）宁可报错也不静默回退**到平台默认——否则会破坏全局唯一性/顺序语义。现象为 `resolve idgen strategy` 错误伴随 `/v1/health` 的 postgres `unavailable`，DB 恢复后自愈。

---

## 5. 健康检查与可观测

`internal/infra/health/checks.go` 并行探测 **postgres / redis / minio**（`ObjectStore.Ping` → `BucketExists`），各自 `2s` 超时，panic 兜底 `unavailable`。

| 端点 | 说明 |
|------|------|
| `GET /v1/health`（别名 `/v1/server/health`，`ACCESS_PUBLIC`） | `{status:"ok"\|"unavailable", dependencies:[{name,status,error?}]}`，整体状态由任一失败决定，gRPC 码保持 200 |
| `GET /v1/server/health/version` | `{version,commit,date}` |
| `/healthz/liveness` | Lynx 常驻 200 |
| `/healthz/readiness` | Lynx 驱动：全健康 200 / 任一失败 503 |
| `grpc.health.v1.Health` | gRPC 侧 10s 轮询快照 |

**Metrics**：`internal/infra/server/metrics.go` 独立 HTTP，`GET /metrics`（`promhttp.Handler()`），`server.metrics.addr`（默认 `127.0.0.1:9040`），当前仅 runtime 采集器，无自定义业务指标。

**日志**：统一 `slog`（`lynx` + `lynxzap`），`--log-level` 控制；gateway 请求日志为 `Debug`（`lynxhttp.WithRequestLog(true)`，`grpc_gateway.go`），`RequestURL` 含完整 query（含 OAuth code），生产开 debug 前需评估；认证拒绝由 `internal/grpc/interceptor/jwt.go:logAuthFailure` 输出 Warn（无 token 明文）。

**慢查询**：`internal/infra/clients/dbhook.go` 的 `SlowQueryHook`（`bun.QueryHook`）

| `data.database.slow_query_threshold` | 行为 |
|--------------------------------------|------|
| `"500ms"`（默认） | 超阈值 Warn `slow query`（`operation/query/duration/error`） |
| `"0"` | 禁用 |
| 非法格式 | Warn 并禁用 |
| `data.database.debug: true` | 全量 SQL Debug（覆盖阈值） |

环境覆盖：`TORCHWOOD_DATA_DATABASE_SLOW_QUERY_THRESHOLD`；`e.Query` 为含内联参数的格式化 SQL，可能含 PII。

---

## 6. 运维操作

### 6.1 迁移

```bash
task db:migrate
```

DSN 优先级：`TORCHWOOD_DATA_DATABASE_SOURCE` → `postgres://torchwood:torchwood@127.0.0.1:5432/torchwood?sslmode=disable`（可用 `POSTGRES_USER/PASSWORD/HOST/PORT/DB` 覆盖，`Taskfile.yml:48`）。发布前先迁移再启动新进程。

### 6.2 首次引导（bootstrap）

全新库上启动 `server` 后打开 `/console/`，登录页自动切为初始化表单（依赖 `TORCHWOOD_SECURITY_SETUP_TOKEN`，`auth.go:59` 的 `GetSetupStatus`）。填写首个管理员（固定 `owner`，仅 `admins` 为空时可用）+ `project_id` + `database_id`，将创建项目（含系统 `default` 库）与业务库；API Key 登录后在 Console 自行创建（secret 仅展示一次）。重置：删 `admins`/`projects`/`api_keys` 后重启引导。

### 6.3 备份

| 数据 | 位置 | 建议 |
|------|------|------|
| 元数据 + 动态文档 | Postgres（`postgres_data`） | `pg_dump` 或卷快照，需覆盖全部 `tw_*` schema |
| 对象 | MinIO（`minio_data`） | `mc mirror` 到异地 S3 或卷快照 |
| Redis（`redis_data`） | 仅队列/缓存/ID | 丢失可由 `worker` 启动对账兜底（超 1h 标 `failed`） |

### 6.4 升级

1. 备份 PG + MinIO → 2. `task db:migrate` → 3. 滚动 `server`（校验 `/healthz/readiness` 200 与 `/v1/server/health/version`）→ 4. 重启 `worker` → 5. 灰度验证 Client/Server API → 6. 摘旧实例。

### 6.5 排障

- 健康 `unavailable`：看 `dependencies[].name/error` 定位 PG/Redis/MinIO；
- 代理后登录异常：检查 `trusted_proxies` 是否含代理网段；
- Console 旧页面：`task console:build && task build`；
- 慢查询无日志：确认阈值非 `"0"` 且级别 ≥ Warn；
- 首次引导被拒：确认 `TORCHWOOD_SECURITY_SETUP_TOKEN` 已设且进程已重启。

### 6.6 启用 vector（pgvector）

vector 属性类型（`VECTOR(dims)` 列、HNSW 索引、`vectorSearch` 算子）依赖 pgvector 扩展；迁移 `db/migrations/000030_pgvector.up.sql` 在元数据库执行 `CREATE EXTENSION IF NOT EXISTS vector;`。**vector 非 trusted extension**：非 superuser 即使身为库 owner 也会被拒（permission denied），因此启用方式取决于迁移执行身份，按下述两条路径二选一。当前架构为单 PG database 多 schema（控制面 `public` + 项目/文档面均走 `CREATE SCHEMA`，`internal/infra/projectschema/migrator.go:85`），故只需对 `TORCHWOOD_DATA_DATABASE_SOURCE` 指向的这一个库启用一次。

**验证 SQL**（两路径通用，连接目标库执行，任意身份可查）：

```sql
SELECT extname, extversion FROM pg_extension WHERE extname='vector';
-- 期望 1 行：vector | 0.8.6；返回 0 行 = 本库未启用
```

**路径一（推荐）：pgvector 预装镜像 + superuser 迁移身份**

适用于 docker/local、CI，以及迁移 DSN 即镜像 bootstrap 超管（`POSTGRES_USER` 创建的角色）的自管部署。`pgvector/pgvector:0.8.6-pg18`（docker/local 与 `.github/workflows/ci.yml` 已统一）把扩展文件预装进镜像，000030 由迁移身份直接执行成功，无额外步骤。

实测记录（2026-09-05，本地 compose）：

```text
$ docker exec torchwood-postgres psql -U torchwood -d torchwood \
    -c "SELECT current_user, usesuper FROM pg_user WHERE usename=current_user;" \
    -c "SELECT extname, extversion FROM pg_extension WHERE extname='vector';"
 current_user | usesuper
--------------+----------
 torchwood    | t
(1 row)

 extname | extversion
---------+------------
 vector  | 0.8.6
(1 row)
```

**路径二：自备 PG 实例，迁移身份为非 superuser（authenticator 形态）**

前提确认——迁移用户应为库 owner 且非 superuser（期望返回 `f`）：

```sql
SELECT rolsuper FROM pg_roles WHERE rolname = '<迁移用户>';   -- 期望 f
```

- **第 1 步（每库一次）**：由 DBA 或一次性引导容器以 superuser 在目标库执行 `CREATE EXTENSION IF NOT EXISTS vector;`；
- **第 2 步**：非 superuser 迁移身份照常 `task db:migrate`。000030 命中 `IF NOT EXISTS` 幂等分支，输出 `NOTICE: extension "vector" already exists, skipping` 后继续，迁移不报错。

实测记录（2026-09-05，临时库 `vector_probe`，owner 为非 superuser 登录角色 `tw_migrator_probe`；验证完成后 `DROP DATABASE`/`DROP ROLE` 清理，主库无残留）。命令与输出摘要（按语句归纳）：

```text
-- 场景 a：未预装，非 superuser 迁移身份直接执行 000030 语句（失败形态）
$ psql -U tw_migrator_probe -d vector_probe -c "CREATE EXTENSION IF NOT EXISTS vector;"
ERROR:  permission denied to create extension "vector"
HINT:  Must be superuser to create this extension.

-- 场景 b：superuser 预装后，同一非 superuser 身份重跑同一语句
$ psql -U torchwood -d vector_probe -c "CREATE EXTENSION IF NOT EXISTS vector;"
CREATE EXTENSION
$ psql -U tw_migrator_probe -d vector_probe -c "CREATE EXTENSION IF NOT EXISTS vector;"
CREATE EXTENSION
NOTICE:  extension "vector" already exists, skipping
$ psql -U tw_migrator_probe -d vector_probe \
    -c "SELECT extname, extversion FROM pg_extension WHERE extname='vector';"
 extname | extversion
---------+------------
 vector  | 0.8.6
(1 row)
```

**失败自诊断**（`task db:migrate` 在 000030 失败时按报错形态分流）：

| 报错形态 | 原因 | 处置 |
|----------|------|------|
| `permission denied to create extension "vector"`（HINT: Must be superuser to create this extension.） | 迁移身份非 superuser 且目标库未启用扩展（场景 a 实测形态） | 走路径二第 1 步，superuser 预装后重跑迁移；golang-migrate 失败事务已回滚、版本记录未推进，重放安全 |
| `extension "vector" is not available`（HINT: The extension must first be installed on the system where PostgreSQL is running.） | PG 实例基座不含 pgvector 扩展文件（本会话以不存在扩展名实测同形态报错） | 换 pgvector 预装基座（如 `pgvector/pgvector:0.8.6-pg18`）或按 pgvector 官方文档为实例安装扩展文件，之后仍走路径二第 1 步 |
