# Torchwood 部署与运维指南

> 本文档基于仓库现有实现编写（Dockerfile 缺失、`task build-docker` 未接线等现状
> 均已如实标注，以代码为准）。目标读者：部署与运维负责人。
> 关联：`docs/implementation-health-observability.md`（Health & 可观测性实现细节）、
> `docs/roadmap.md`、`AGENTS.md`。
> 最新更新：2026-08-12

---

## 1. 运行形态：server 与 worker

Torchwood 由两个服务进程组成，均为 `cmd/` 下独立 `main` 包，运行时组合由
Wire 注入（`cmd/server/provides.go` → `wire_gen.go`）；另有第三个可执行
`cmd/client`（Torchwood CLI）：

| 进程 | 入口 | 职责 |
|------|------|------|
| **server** | `cmd/server/main.go` | gRPC 服务、grpc-gateway（HTTP `/v1/*`）、独立 HTTP handler（Storage 上传下载）、Metrics、Admin Console SPA（`console/embed.go` 嵌入）、健康/版本端点 |
| **worker** | `cmd/worker/main.go` | 函数异步执行队列消费者：BRPOP 消费 `torchwood:queue:functions-executions`，单进程 4 个并发 goroutine；启动时对账——将停留 `queued/building/running` 超过 1h 的执行标记为 `failed`（兜底 Redis 重启丢任务、worker 崩溃孤儿）；瞬时失败重抛回队最多 3 次（`maxProcessAttempts`，重试计数持久化在队列 payload） |
| **CLI** | `cmd/client/main.go` | `bin/torchwood[.exe]`，运维/自动化工具，通过 API Key 走 gRPC 调用 Server API（见 `docs/developer/02-quickstart.md` §7） |

本地开发：

```bash
task dev-server    # go run ./cmd/server
task worker        # go run ./cmd/worker
```

> worker 仅在启用 Functions 异步执行时需要；仅使用数据库/存储能力时 server 单进程
> 即可运行。

### 1.1 端口一览（默认配置）

| 端口 | 用途 | 配置项 |
|------|------|--------|
| `:9080` | HTTP（grpc-gateway + Console `/console/`） | `server.http.addr` |
| `127.0.0.1:9060` | gRPC（仅本机回环，gateway 同机转发） | `server.grpc.addr` |
| `127.0.0.1:9040` | Prometheus metrics（`/metrics`；留空回退同值） | `server.metrics.addr` |

---

## 2. 外部依赖

本地基础设施由 `docker/local/docker-compose.yml` 提供（`task up` 启动，
`task down` 停止，`task clean` 连数据卷一并删除）：

| 依赖 | 镜像 | 端口 | 用途 |
|------|------|------|------|
| PostgreSQL | `postgres:18-alpine` | 5432 | 元数据静态表（bun + golang-migrate）与动态文档层（schema-per-database + `_tenant` + `_perms`） |
| Redis | `redis:7-alpine` | 6379 | 队列（函数执行）、上传会话、ID 生成 |
| MinIO | `minio/minio:RELEASE.2024-11-07T00-52-20Z` | 9000（API）/ 9001（Console） | S3 兼容对象存储 |

三个容器均配置 healthcheck（`pg_isready` / `redis-cli ping` / MinIO live 端点）。
生产环境可将 MinIO 替换为任意 S3 兼容服务（`storage.provider: "s3"`）。

---

## 3. 构建与发布

### 3.1 本机构建（`task build`）

`task build` 先执行 `console-build`（pnpm），再编译三个产物到 `./bin/`：

```yaml
build:
  deps: [console-build]          # 先构建 Console（pnpm run build），供 Go embed 打包
  cmds:
    - go build -ldflags "...-X main.version=... -X main.commit=... -X main.date=..." -o ./bin/ ./cmd/server
    - go build -ldflags "...同上..." -o ./bin/ ./cmd/worker
    - go build -ldflags "...同上..." -o ./bin/torchwood[.exe] ./cmd/client
```

- 产物：Windows 下 `bin/server.exe`、`bin/worker.exe`、`bin/torchwood.exe`；Linux/macOS 下
  `bin/server`、`bin/worker`、`bin/torchwood`。
- 版本注入：`VERSION` = `git describe --tags --always`，`COMMIT` = `git rev-parse
  --short HEAD`，`DATE` = `yyyyMMddHHmmss`；通过 `-X main.version/commit/date`
  （全小写变量名）注入，由 `/v1/server/health/version` 端点暴露。
- **注意**：修改 Console 代码后必须先 `task console-build` 再 `task build`，
  否则 Go embed 会打包旧版本。

### 3.2 Docker 镜像（`task build-docker`）

Taskfile 已定义：

```yaml
DOCKER_IMAGE: 'torchwood:{{.VERSION}}-{{.GIT_VERSION}}-{{.TIMESTAMP}}'
# docker build -t torchwood:1.0.0-<git describe>--<yyyyMMddHHmmss> .
```

镜像名规则：`torchwood:<VERSION 固定 1.0.0>-<GIT_VERSION>-<TIMESTAMP>`。

> **现状说明（以代码为准）**：仓库当前**尚无 Dockerfile**（2026-08 核对根目录与
> `docker/` 下均不存在）。`task build-docker` 已定义镜像名与构建命令，但需补充
> 多阶段 Dockerfile（builder 阶段构建二进制 + console embed，runner 阶段
> 提供 Postgres/Redis/MinIO 连接）后方可使用，否则 `docker build` 会失败。

---

## 4. 生产配置要点

配置 schema 由 `internal/pkg/config/config.proto` 定义，模板为
`configs/config.yaml.template`。`cmd/server/main.go` 通过 `godotenv` 加载 `.env`，
默认从 `./configs` 绑定配置；环境变量覆盖前缀为 `TORCHWOOD_`，键名由点号路径
映射（如 `data.database.source` → `TORCHWOOD_DATA_DATABASE_SOURCE`）。

### 4.1 必配项

| 配置 | 环境变量 | 说明 |
|------|----------|------|
| JWT secret | `TORCHWOOD_SECURITY_JWT_SECRET` | 签发 access/refresh token 的密钥；**必须替换为强随机值**（≥32 字符，含弱子串如 `change-me`/`secret`/`torchwood`/`minioadmin` 会拒绝启动）；`access_ttl` 默认 `15m`、`refresh_ttl` 默认 `7d` |
| Setup token | `TORCHWOOD_SECURITY_SETUP_TOKEN` | Console 首次引导令牌；**未设置时注册第一个管理员被拒绝**（`internal/app/console/setup.go` 的 `SignUp`）；生成方式：`openssl rand -hex 32` |
| DB 连接串 | `TORCHWOOD_DATA_DATABASE_SOURCE` | Postgres DSN；`task migrate` 优先读取同一变量 |
| S3 Access Key | `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` | MinIO/S3 访问密钥 |
| S3 Secret Key | `TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY` | MinIO/S3 密钥 |
| API Key 头 | — | `security.api_key.header` 默认 `x-api-key`（一般无需修改） |

> MinIO 本地默认凭据为 `minioadmin`/`minioadmin`（docker-compose 环境变量），
> 生产务必覆盖。S3 其他项：`endpoint`（S3 兼容服务地址）、`bucket`、
> `region`（默认 `us-east-1`）。

### 4.2 反向代理与真实 IP

```yaml
security:
  trusted_proxies: []    # 默认不信任 X-Forwarded-For/X-Real-Ip，一律使用 gRPC peer 地址
```

- 反向代理（Nginx/ALB）后恢复客户端真实 IP：设置
  `TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32`（逗号分隔 CIDR）。
- 仅当直连 peer 命中可信网段时才采纳 `X-Forwarded-For` 首跳；grpc-gateway 与
  gRPC 同进程部署时需包含 `127.0.0.1/32`。

### 4.3 其他生产相关配置

- `TORCHWOOD_ENV`：`production`（默认/未设置）关停先排水 30s 再停服务，给 LB 摘流；`development` 跳过排水。可用 `TORCHWOOD_SERVER_DRAIN_TIMEOUT` 覆盖。K8s `terminationGracePeriodSeconds` 需大于排水 + 关停上界（默认约 40s+），否则排水未结束就会被 SIGKILL。
- `server.http.public_url`：对外公共地址（OAuth 回调等生成链接用）。
- `server.http.cors.allow_origins`：显式允许来源；`*` 在 `allow_credentials=true`
  时会被拒绝。
- `data.database.pool`：连接池（max_idle/max_open/lifetime/idle_time）。
- `data.redis.addr/password`：Redis 地址与密码。
- `storage.provider`：`s3` / `minio` / `local`（local 开发用，路径 `./data/files`）。
- `telemetry.enabled`：OpenTelemetry 配置，**当前未接线**（保持 `false`）。
- `messaging.smtp`：SMTP 配置；`dev_log_otp`/`dev_log_sms` 为无 SMTP 时的开发兜底
  （OTP/SMS 打印到 stdout）。
- `idgen.default_strategy`：`uuid`（默认）/ `ulid` / `snowflake` / `sequence` / `random`。

### 4.4 ID 生成策略的显式失败语义（重要）

当项目启用 `random`/`sequence` 等需要读取项目级设置的策略时，生成路径会先
读取项目设置（`settings.idgen.*`，带 30s 短缓存），**读取失败（如数据库
抖动/不可用）时 ID 生成显式返回错误，不会静默回退**到平台默认策略——这是
既定设计（R09-P2-1）：策略不一致会破坏 ID 的全局唯一性/顺序语义，宁可
让生成请求失败也不悄悄产出错误策略的 ID。

- 现象：create user/document 等写入请求报错，错误日志含
  `resolve idgen strategy` 字样，`/v1/server/health` 的 postgres 依赖
  可能同时显示 `unavailable`。
- 处理：确认 `TORCHWOOD_DATA_DATABASE_SOURCE` 指向的 Postgres 连通性；
  数据库恢复后自动恢复，无需重启进程。
- 配置了 `random` 策略时另注意：ID 保留集合在 Redis 中有容量上限
  （100 万/集合，`internal/infra/idgen/random_redis.go`），达到上限后生成
  会被拒绝并输出 Warn 日志；正常规模下不会触及，异常增长时需排查为何
  集合未随 30 天 TTL 释放。

---

## 5. 健康检查与观测

实现位于 `internal/infra/health/checks.go`，探测 **postgres / redis / minio**
三个依赖（`ObjectStore.Ping` → MinIO `BucketExists`），并行执行、各自 2s 超时、
panic 兜底为 `unavailable`。

| 端点 | 说明 |
|------|------|
| `GET /v1/health`（别名 `GET /v1/server/health`） | 依赖明细：`{"status":"ok"\|"unavailable","dependencies":[{name,status,error?}]}`；任一依赖失败整体 `unavailable`（gRPC 返回码保持 200，503 语义由 readiness 承担） |
| `GET /v1/server/health/version` | `{"version","commit","date"}`（构建注入，见 §3.1） |
| `/healthz/liveness` | lynx 提供，恒 200 |
| `/healthz/readiness` | lynx 提供，依赖 checkers：全部健康 200 / 任一失败 503 |
| `grpc.health.v1.Health` | gRPC 侧同步注册（10s 轮询快照） |

### 5.1 Metrics

`internal/infra/server/metrics.go`：独立 HTTP 服务，监听 `server.metrics.addr`
（模板 `127.0.0.1:9040`，留空回退同值），`GET /metrics` 为 Prometheus 格式
（`promhttp.Handler()`）。当前仅 runtime 采集器，**无自定义业务指标**。

### 5.2 日志

- 统一结构化日志：`slog`（zap 后端，`lynxzap`），`--log-level` 控制级别。
- **HTTP gateway 请求日志为 Debug 级别**：需 `--log-level debug` 才可见；
  注意 `RequestURL` 含完整 query string（含 OAuth code 等敏感参数），生产启用
  debug 前需知悉；preflight OPTIONS 与静态资源也会产生 Debug 日志。
- **慢查询日志**（`internal/infra/clients/dbhook.go`）：

| 配置 | 行为 |
|------|------|
| `data.database.slow_query_threshold: "500ms"` | 默认阈值；超阈值 SQL 输出 Warn `slow query`（operation/query/duration/error 字段） |
| `"0"` | 禁用慢查询日志 |
| 其他值 | `time.ParseDuration` 解析，失败 Warn 并禁用 |
| `data.database.debug: true` | 全量 SQL Debug 日志（`sql` 事件），覆盖阈值语义 |

> `e.Query` 为含内联参数的格式化 SQL，可能包含 PII，输出到日志前请评估。

---

## 6. 运维注意事项

### 6.1 数据库迁移

```bash
task migrate    # golang-migrate up，路径 ./db/migrations
```

DSN 优先级：`TORCHWOOD_DATA_DATABASE_SOURCE` → 默认
`postgres://torchwood:torchwood@127.0.0.1:5432/torchwood?sslmode=disable`
（可用 `POSTGRES_USER/PASSWORD/HOST/PORT/DB` 覆盖）。发布前先迁移再启动新版本
进程。

### 6.2 首次部署引导（bootstrap）

不再需要离线 seed 脚本：全新数据库上启动 server 后，打开 `/console/`，
登录页会自动切换为「初始化设置」表单。**前提：必须配置 `TORCHWOOD_SECURITY_SETUP_TOKEN`**
（未配置时 `POST /v1/console/auth/sign-up` 返回 FailedPrecondition，`internal/app/console/setup.go`
的 `Setup.SignUp`）。注册第一个管理员时需同时填写 `project_id` 与
`database_id`，将自动创建：

- **owner** 管理员账户（首个管理员固定为超管，仅当 `admins` 表为空
  时可用；二次调用返回 `FailedPrecondition`）；
- 指定项目（随项目自动创建系统 `default` 库）；若 `database_id` 不是
  `default`，再额外创建该业务库。

API Key **不在注册时生成**；登录后在 Console 的 API Key 页面创建（明文 secret
仅创建时展示一次）。Console 与 SDK demo 的 Server API Key 均由此页面产出。
若需在空库上重置，删除 `admins`、`projects`、`api_keys` 相应行后重启引导即可。

### 6.3 备份要点

| 数据 | 位置 | 建议 |
|------|------|------|
| 元数据表 + 动态文档库 | Postgres（docker volume `postgres_data`） | `pg_dump` 定期逻辑备份，或快照卷；动态文档为 schema-per-database，备份需覆盖全部 database |
| 对象存储 | MinIO（volume `minio_data`） | 使用 `mc mirror` 同步到异地 S3，或备份卷 |
| Redis | volume `redis_data` | 仅队列/缓存/ID 生成，非持久权威数据；队列丢失可由 worker 启动对账兜底（超 1h 停留任务标 failed） |

### 6.4 升级顺序

1. 备份 Postgres 与 MinIO（§6.3）。
2. 执行新版本迁移：`task migrate`（或发布流水线中 migrate job）。
3. 滚动启动新版本 **server**（先验证 `/healthz/readiness` 200 与
   `/v1/server/health/version` 版本号正确）。
4. 再启动/重启 **worker**（消费函数执行队列）。
5. 灰度验证 Client/Server API 后逐步摘除旧实例。

### 6.5 常见排障

- 健康检查 `status=unavailable`：按 `dependencies[].name` 定位（postgres/redis/
  minio），错误信息在 `error` 字段。
- 反向代理后登录/会话异常：检查 `trusted_proxies` 是否包含代理网段。
- Console 打开是旧页面：`task console-build` 后重新 `task build`。
- 慢查询看不到：确认 `slow_query_threshold` 非 `"0"`、日志级别 ≥ Warn。
- 首次引导被拒：确认 `TORCHWOOD_SECURITY_SETUP_TOKEN` 已配置（server 启动后
  修改环境变量需重启进程生效）。
