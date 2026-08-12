# Torchwood 配置体系

> 本文描述 Torchwood 的**配置单一入口**：`config.proto` 定义 schema、`bind.go` 负责环境变量绑定、`config.yaml.template` 与 `.env` 的组合方式，以及若干关键配置项（setup token、可信代理、Console 会话 cookie、测试 DSN）。
> 相关代码：`internal/pkg/config/config.proto`、`internal/pkg/config/bind.go`、`cmd/server/main.go`、`cmd/worker/main.go`、`configs/config.yaml.template`、`.env.example`。
> 最新更新：2026-08-12

---

## 1. 配置 schema（internal/pkg/config/config.proto）

配置 schema 用 **protobuf message** 定义（`internal/pkg/config/config.proto`，顶层 `AppConfig`），避免 YAML 样例与结构体两处维护。proto 生成 Go 代码后，既作为 YAML/环境变量的解码目标，也用于校验与反射推导键路径。

### 1.1 顶层分组

| 分组 | 对应 message | 说明 |
|------|-------------|------|
| `server` | `Server` | 监听与超时：`grpc`、`http`、`metrics` 三个子块 |
| `security` | `Security` | JWT（secret / access_ttl / refresh_ttl）、API Key header、setup token、会话上限、可信代理网段 |
| `data` | `Data` | `database`（PG DSN / 连接池 / 慢查询阈值）、`redis`（addr / password / db） |
| `storage` | `Storage` | 对象存储 provider（`s3` / `local`） |
| `functions` | `Functions` | 函数执行器（`docker` host / network / registry） |
| `telemetry` | `Telemetry` | OTLP 开关、endpoint、service_name |
| `messaging` | `Messaging` | SMTP、SMS（Twilio）、dev 模式的 OTP 日志输出 |
| `idgen` | `IdGen` | ID 生成策略（uuid / ulid / snowflake / sequence / random） |

### 1.2 子块与关键字段

**server**

| 路径 | 类型 | 说明 |
|------|------|------|
| `server.grpc.addr` | string | gRPC 监听地址（默认 `127.0.0.1:9060`，模板注释建议仅本机回环供 gateway 转发） |
| `server.grpc.timeout` | string | gRPC 超时（默认 `30s`） |
| `server.http.addr` | string | grpc-gateway + Console SPA 监听地址（默认 `:9080`） |
| `server.http.timeout` | string | HTTP 超时（默认 `60s`） |
| `server.http.public_url` | string | 公共 base URL，用于构造 OAuth 回调地址；同时决定 Console 会话 cookie 是否带 `Secure` 标记 |
| `server.http.cors.*` | message | CORS：`allow_origins`、`allow_methods`、`allow_headers`、`expose_headers`、`allow_credentials`、`max_age`（`allow_credentials=true` 时 `*` 会被拒绝） |
| `server.metrics.addr` | string | Metrics（Prometheus）监听地址（默认 `127.0.0.1:9040`；留空回退同值，`internal/infra/server/metrics.go` 的 `NewMetricsServer`） |

**security**

| 路径 | 类型 | 说明 |
|------|------|------|
| `security.jwt.secret` | string | JWT 主密钥；**必填**（启动校验，见 §4；≥32 字符且不得含弱子串） |
| `security.jwt.access_ttl` | string | access token 有效期（默认 `15m`） |
| `security.jwt.refresh_ttl` | string | refresh token 有效期（默认 `7d`） |
| `security.api_key.header` | string | API Key 请求头名（默认 `x-api-key`） |
| `security.setup_token` | string | Console 首次引导令牌；**未配置时首个管理员注册被拒绝**（`internal/app/console/setup.go` 的 `Setup.SignUp`） |
| `security.sessions.max_per_user` | int32 | 单用户最大并发会话数（end-user sessions）；未配置/0 = 默认 50；-1 = 不限 |
| `security.trusted_proxies` | repeated string | 可信代理 CIDR 列表（见 §5.1） |

**data**

| 路径 | 类型 | 说明 |
|------|------|------|
| `data.database.source` | string | PostgreSQL DSN；**必填**（worker 启动校验） |
| `data.database.debug` | bool | 打印全量 SQL 调试日志（模板默认 `false`） |
| `data.database.slow_query_threshold` | string | 慢查询日志阈值，如 `500ms`；空串 = 默认 500ms，`"0"` = 禁用 |
| `data.database.pool.*` | message | 连接池：`max_idle_conns`、`max_open_conns`、`conn_max_lifetime`、`conn_max_idle_time` |
| `data.redis.addr` / `password` / `db` | - | Redis 连接（本地模板默认 `127.0.0.1:6379`、db 0） |

**storage**

| 路径 | 类型 | 说明 |
|------|------|------|
| `storage.provider` | string | `s3` / `minio` / `local` |
| `storage.s3.endpoint` / `region` / `bucket` | string | S3/MinIO 端点、区域、桶名 |
| `storage.s3.access_key_id` / `secret_access_key` | string | 对象存储凭据（敏感，走环境变量） |
| `storage.local.path` | string | 本地 provider 的根目录（默认 `./data/files`） |

**functions**

| 路径 | 类型 | 说明 |
|------|------|------|
| `functions.executor` | string | 执行器（默认 `docker`） |
| `functions.docker.host` | string | Docker daemon 地址（默认 `unix:///var/run/docker.sock`） |
| `functions.docker.network` | string | 函数容器网络（不存在时执行器启动自动创建） |
| `functions.docker.registry` | string | 构建镜像 registry 前缀（必须小写） |

**telemetry / messaging / idgen**

| 路径 | 类型 | 说明 |
|------|------|------|
| `telemetry.enabled` / `otlp_endpoint` / `service_name` | - | OTLP 导出 |
| `messaging.smtp.*` | message | SMTP host/port/username/password/from/use_tls |
| `messaging.dev_log_otp` / `dev_log_sms` | bool | SMTP/SMS 未配置时把 OTP/验证码写入日志（**仅开发**） |
| `messaging.sms.provider` | string | `twilio` |
| `messaging.sms.twilio.account_sid` / `auth_token` / `from` | string | Twilio 凭据（敏感） |
| `idgen.default_strategy` | string | `uuid` / `ulid` / `snowflake` / `sequence` / `random` |
| `idgen.random.*` / `snowflake.*` / `sequence.*` | message | 各策略参数 |
| `idgen.resources.users` / `sessions` / `documents` | string | 不同资源使用的生成策略 |

---

## 2. 运行时绑定（internal/pkg/config/bind.go）

绑定逻辑集中在 `configureViper` 与 `unmarshalConfig`：

1. **设置搜索路径**：`configureViper` 先调用 `lynx.DefaultBindConfigFunc`，再把 `extraPaths`（默认 `./configs`）加入搜索路径（`c.AddSearchPath`）。
2. **环境变量前缀**：`c.SetEnvPrefix(EnvPrefix)`（`EnvPrefix = "TORCHWOOD"`），随后 `c.AutomaticEnv()`。
3. **逐叶子显式绑定**：`configKeys()` 用反射遍历 `AppConfig` 的所有**叶子 json 键路径**（由 proto 生成的 json tag 推导），对每个键调用 `c.BindEnv(key, envNameForKey(key))`——即显式绑定字面量环境变量名，避免依赖 viper 的 KeyReplacer（lynx 不再暴露该能力），语义不变。
4. **解码**：`unmarshalConfig` 用 mapstructure 以 `TagName = "json"` 逐叶子取值组装嵌套 map 后解码到 `AppConfig`；`WeaklyTypedInput: true` 允许宽松类型；`DecodeHook: StringToSliceHookFunc(",")` 让**逗号分隔的环境变量能解码为 repeated 字段**（如 `trusted_proxies`）。

> 选记（`envBoundKeys` 变量仅供说明模板里出现过的关键敏感键，实际绑定以 `configKeys()` 反射结果为全集，即**所有叶子键都可被环境变量覆盖**）。

### 2.1 正则环境变量名

`envNameForKey` 把点号路径键名映射为环境变量名：`.` 与 `-` 转 `_`，整体大写，并加前缀——与 `AGENTS.md` 表述一致。

```
"data.database.source"  ->  TORCHWOOD_DATA_DATABASE_SOURCE
```

---

## 3. 环境变量覆盖规则

### 3.1 规则

- 前缀固定为 `TORCHWOOD_`；
- 键名从点号路径映射：`.` → `_`、`-` → `_`，字母转大写；
- 键路径以 `AppConfig` 的 **proto json tag 叶子键**为准（不是 YAML 注释）；
- repeated 字段用逗号分隔（如 `TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32,10.0.0.0/8`）；
- 环境变量 > 配置文件：环境变量优先于 `config.yaml` / 默认值。

### 3.2 映射对照示例表

| 配置键（点号路径） | 环境变量 | 来源/说明 |
|-------------------|----------|-----------|
| `security.jwt.secret` | `TORCHWOOD_SECURITY_JWT_SECRET` | JWT 主密钥（必填，≥32 字符且不含弱子串） |
| `security.setup_token` | `TORCHWOOD_SECURITY_SETUP_TOKEN` | Console 首次引导令牌（未配置拒绝注册首个管理员） |
| `security.sessions.max_per_user` | `TORCHWOOD_SECURITY_SESSIONS_MAX_PER_USER` | 单用户最大并发会话数 |
| `security.trusted_proxies` | `TORCHWOOD_SECURITY_TRUSTED_PROXIES` | 逗号分隔 CIDR 列表 |
| `data.database.source` | `TORCHWOOD_DATA_DATABASE_SOURCE` | PG DSN（必填） |
| `data.database.slow_query_threshold` | `TORCHWOOD_DATA_DATABASE_SLOW_QUERY_THRESHOLD` | 慢查询阈值 |
| `data.redis.addr` | `TORCHWOOD_DATA_REDIS_ADDR` | Redis 地址 |
| `data.redis.password` | `TORCHWOOD_DATA_REDIS_PASSWORD` | Redis 密码 |
| `data.redis.db` | `TORCHWOOD_DATA_REDIS_DB` | Redis 库号 |
| `storage.s3.endpoint` | `TORCHWOOD_STORAGE_S3_ENDPOINT` | S3/MinIO 端点 |
| `storage.s3.bucket` | `TORCHWOOD_STORAGE_S3_BUCKET` | 桶名 |
| `storage.s3.access_key_id` | `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` | MinIO 凭据（见注意） |
| `storage.s3.secret_access_key` | `TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY` | MinIO 凭据（见注意） |
| `messaging.smtp.host` | `TORCHWOOD_MESSAGING_SMTP_HOST` | SMTP 主机 |
| `messaging.smtp.password` | `TORCHWOOD_MESSAGING_SMTP_PASSWORD` | SMTP 密码 |
| `messaging.sms.twilio.account_sid` | `TORCHWOOD_MESSAGING_SMS_TWILIO_ACCOUNT_SID` | Twilio SID |
| `messaging.sms.twilio.auth_token` | `TORCHWOOD_MESSAGING_SMS_TWILIO_AUTH_TOKEN` | Twilio Token |
| `telemetry.otlp_endpoint` | `TORCHWOOD_TELEMETRY_OTLP_ENDPOINT` | OTLP 端点 |
| `idgen.snowflake.node_id` | `TORCHWOOD_IDGEN_SNOWFLAKE_NODE_ID` | 雪花节点号 |

> **注意**：`config.yaml.template` 中 storage 的注释写的是 `TORCHWOOD_STORAGE_S3_ACCESS_KEY`，但 `bind.go` 的 `envNameForKey`（按字段 `access_key_id`）实际映射为 `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID`；`.env.example` 与 `AGENTS.md` 也使用 `_ACCESS_KEY_ID` / `_SECRET_ACCESS_KEY`。**以 `.env.example` / 环境变量映射为准**（`config.yaml.template` 注释为过时表述）。

---

## 4. 文件组成与加载顺序

### 4.1 三份文件的角色

| 文件 | 角色 |
|------|------|
| `configs/config.yaml.template` | **配置模板**：完整展示全部键与默认值，每个敏感键都有环境变量注释；可作为生产配置起点 |
| `configs/config.yaml` | 本地实际配置（**.gitignore**，不入库），随时可从模板复制 |
| `.env` / `.env.example` | **环境变量**载体：`.env.example` 是示例与注释文档，`.env` 是本机实际值（`.gitignore`）；敏感信息一律走环境变量，不进 `config.yaml` |

原则：**模板保留默认值，敏感信息（JWT secret、setup token、数据库密码、对象存储凭据、SMTP/Twilio 凭据）必须通过环境变量注入**，避免把密钥写进版本库。

### 4.2 cmd/server/main.go 加载顺序

`cmd/server/main.go`（`cmd/worker/main.go` 结构相同）依次执行：

1. `godotenv.Load()` 从项目根目录加载 `.env`（失败不阻塞）；
2. `lynx.NewRunner` 装配服务，注册 flag：`--config-dir`（默认 `./configs`）与 `--log-level`（默认 `info`）；
3. `lynx.WithBindConfigFunc(config.NewBindConfigFunc())` 把键绑定到 viper/lynx 配置源（含环境变量覆盖）；
4. Wire 构造阶段 `NewAppConfig` 调用 `config.UnmarshalConfig(app.Config(), &c)` 解码出 `*config.AppConfig`；
5. **启动校验**：`cmd/server/provides.go` 的 `NewAppConfig` 校验 `security.jwt.secret` 非空、≥32 字符且不含已知弱子串，否则直接报错退出；`cmd/worker/provides.go` 的 `NewAppConfig` 校验 `data.database.source` 非空，否则直接报错退出。

```
cmd/server/main.go
  │  godotenv.Load()          # 1. 加载 .env（失败容忍）
  ▼
  lynx.NewRunner
  │  --config-dir ./configs   # 2. flag 默认值
  │  WithBindConfigFunc       # 3. cmd/server 用 NewBindConfigFunc()
  ▼
  wireBootstrap → NewAppConfig
  │  config.UnmarshalConfig   # 4. 解码 + 环境变量覆盖
  ▼
  校验 security.jwt.secret 非空/长度/弱子串   # 5. 失败即退出
```

`cmd/worker/main.go` 也通过 `godotenv.Load()` 加载 `.env`。

---

## 5. 特殊配置项

### 5.1 security.setup_token（首次引导令牌）

- 语义：Console「初始化设置」注册**第一个**管理员的门禁令牌。`Setup.SignUp` 先校验：token 未配置（空）→ 直接 `FailedPrecondition`；请求令牌与配置不一致 → `PermissionDenied`（`internal/app/console/setup.go` 的 `SignUp` / `setupTokenEqual`，常量时间比较）。
- **默认空 = 引导入口关闭**，防无凭据抢占首个 owner。
- 部署时生成强随机值：`openssl rand -hex 32`，经 `TORCHWOOD_SECURITY_SETUP_TOKEN` 注入。

### 5.2 security.trusted_proxies（反向代理恢复真实 IP）

- 语义：声明可信代理的 CIDR 网段（也接受裸 IP，按 `/32`、`/128` 处理）。**仅当 gRPC 直连 peer 命中这些网段时，才采纳其转发的 `X-Forwarded-For` 首跳或 `X-Real-Ip`**；否则一律使用 peer 自身地址（`pkg/grpc/interceptor/trusted_proxy.go`）。
- **默认空列表 = 不信任任何代理**，此时 XFF/X-Real-Ip 一律忽略，防止客户端伪造来源绕过 IP 限流与审计。
- grpc-gateway 与 gRPC 同进程部署时，需包含 `127.0.0.1/32` 才能恢复客户端真实 IP（配置模板注释已说明）。

```yaml
security:
  trusted_proxies: ["127.0.0.1/32"]   # 模板默认 []
# 环境变量：TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32,10.0.0.0/8
```

### 5.3 Console 会话 cookie（TORCHWOOD_session_console / TORCHWOOD_console_refresh）

见 `internal/api/consolegrpc/cookies.go` 与 `internal/app/console/auth.go`：

- **access cookie**：`TORCHWOOD_session_console`，Path `/`；
- **refresh cookie**：`TORCHWOOD_console_refresh`，**Path 限 `/v1/console/auth`**（只发向 auth 刷新端点，缩小攻击面）；
- 两者均 `HttpOnly`（浏览器 JS 不可读，免疫 XSS 窃取）+ `SameSite=Lax`（跨站 POST 不携带，覆盖全部变更类端点，无需额外 CSRF token）+ `Secure`（仅当 `server.http.public_url` 以 `https://` 开头时置位，`internal/app/console/auth.go` 的 `SecureCookies()`）；
- 前端**不使用 localStorage 存 token**；`RefreshTokenRequest.refresh_token` 为空时走 cookie-only 浏览器流（`refreshTokenFromCookie` 从 cookie 读取，`internal/api/consolegrpc/auth.go`）；
- `SignOut` 时以 `Max-Age=0`（秒数 -1）清除同名 cookie（Path 需与签发一致）。

### 5.4 测试环境变量

`TORCHWOOD_TEST_DATABASE_SOURCE` 与 `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE` **不属于 `AppConfig` schema、不经 bind.go 绑定**，由 `internal/testutil/db.go` 用 `os.Getenv` 直接读取：

| 变量 | 含义 | 默认（.env.example） |
|------|------|---------------------|
| `TORCHWOOD_TEST_DATABASE_SOURCE` | 基础测试库 DSN（库名前缀 `TORCHWOOD_test`，每个测试创建独立 `<前缀>_<pid>_<序号>` 库） | `postgres://torchwood:torchwood@127.0.0.1:5432/TORCHWOOD_test?sslmode=disable` |
| `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE` | 维护库 DSN（用于 `CREATE DATABASE` / `DROP DATABASE` / `pg_terminate_backend`） | `postgres://torchwood:torchwood@127.0.0.1:5432/postgres?sslmode=disable` |

- 缺失时 `internal/testutil` fail-fast（提示通过 `task test` 运行，它会自动从 `.env` 加载）；
- 集成测试（`internal/infra/documentdb/*_test.go` 等在 `testing.Short()` 时跳过）。详见 `docs/developer/06-databases.md` §7。

---

## 6. 参考

- `internal/pkg/config/config.proto`：schema 单一事实来源。
- `internal/pkg/config/bind.go`：环境变量绑定与解码。
- `cmd/server/provides.go` / `cmd/worker/provides.go`：`NewAppConfig` 启动校验。
- `configs/config.yaml.template`、`.env.example`：配置模板与环境变量示例。
- `internal/app/console/setup.go`：setup token 校验与首次引导。
- `pkg/grpc/interceptor/trusted_proxy.go`：可信代理解析与真实 IP 恢复。
- `internal/api/consolegrpc/cookies.go`、`internal/app/console/auth.go`：Console 会话 cookie。
- `internal/testutil/db.go`：测试 DSN 读取。
