# 审查任务：01 - 安全与认证（拦截器 / 会话 / OTP / OAuth / MFA）

## 角色

你是资深 Go 安全代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「认证与授权安全」模块做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/roadmap.md` §0（AI/Agent-Native 鉴权策略）
- 架构：`internal/api`（传输）→ `internal/app`（用例）→ `internal/domain`（端口）→ `internal/infra`（适配器）；`pkg/` 为跨层工具
- 鉴权模型：支持 API Key（带 scope）、用户 JWT（access/refresh）、Console admin 会话（HttpOnly cookie `TORCHWOOD_session_console`）、匿名会话、OTP、OAuth2、TOTP MFA
- 关键约定：API Key 参与 `_perms` 的 `keys` 角色，不默认 bypass 文档权限；`X-Torchwood-Project` 头用于 admin 指定目标项目；JWT claims 与 `pkg/jwtparser` 的映射必须兼容；反向代理恢复真实 IP 需显式配置 `security.trusted_proxies`

## 审查范围

- `pkg/grpc/interceptor/`：`apikey_scope.go`、`jwt.go`、`client.go`、`audit.go`、`trusted_proxy.go`（含 `*_test.go`）
- `internal/infra/auth/`：`validator.go`、`session_service.go`、`session_cookie.go`、`totp.go`、`oauth_provider.go`、`oauth_wechat.go`、`otp_store_redis.go`、`otp.go`、`login_throttle_redis.go`、`ratelimit_redis.go`、`refresh_rotation_redis.go`、`account_token_redis.go`、`mfa_challenge_redis.go`、`oauth_state_redis.go`、`admin_token_revoke_redis.go`（含 `*_test.go`）
- `pkg/jwtparser/`、`pkg/secretbox/`、`pkg/password/`
- 交叉引用（只读）：`internal/domain/auth/`（端口定义）、`proto/shared/v1/authz.proto`（鉴权注解）、`internal/pkg/contexts/principal.go`

## 审查重点

1. **API Key scope 校验**：`apikey_scope.go` 的 scope 解析与端点授权逻辑——是否存在缺失 scope 检查的路径、scope 通配/前缀匹配是否可绕过、错误时机（是否在业务执行前拒绝）。
2. **JWT 安全**：签名算法是否强制（防 alg=none/HS256 混淆）、过期/签发时间校验、`pkg/jwtparser` 与密钥管理（`keys.go`）、claims 映射（`sub`/`team:{id}`/`member:{id}`/scope）、刷新轮换（`refresh_rotation_redis.go`）是否防重放。
3. **会话安全**：`session_service.go` 的创建/验证/撤销流程、登出后 token 是否真失效、`session_cookie.go` 的 HttpOnly/SameSite/Secure/Path 属性、刷新 cookie 的路径限制。
4. **OTP / 登录节流**：`otp_store_redis.go` 的过期时间、尝试次数限制、验证后是否立即失效；`login_throttle_redis.go` 与 `ratelimit_redis.go` 的限流键设计与绕过（如按 IP 但在无 trusted_proxies 时误信 X-Forwarded-For）。
5. **TOTP**：`totp.go` 的密钥生成熵、时间窗口/漂移容差、校验后防重放、与 Redis challenge 的绑定。
6. **OAuth2**：`oauth_provider.go`/`oauth_wechat.go` 的 state 随机性与过期、redirect_uri 校验（防 open redirect）、token 交换、用户信息伪造风险；`oauth_state_redis.go` 的存储与消费。
7. **Trusted proxy**：`trusted_proxy.go` 的解析顺序与 CIDR 判断，未配置时是否正确回退到 peer 地址。
8. **凭据处理**：`pkg/password` 的哈希算法与成本参数；`pkg/secretbox` 的加密模式与密钥来源；代码中是否有 secret 硬编码/日志泄露。
9. **审计**：`audit.go` 是否记录关键鉴权事件、是否可被伪造（principal 来源）。

## 通用检查项

1. 安全：注入、越权、信息泄露（错误信息是否泄露内部细节）、secret 处理、输入校验
2. 错误处理：错误吞掉、错误状态码映射不当、panic
3. 并发：Redis 操作竞态（check-then-use）、事务边界、上下文传播
4. 性能：Redis 往返次数、不必要的加锁
5. 一致性：与 `internal/domain/auth` 端口签名、与 AGENTS.md 约定一致；生成代码未手动修改
6. 测试：关键逻辑（scope 校验、refresh 轮换、TOTP、throttle）是否有测试且断言真实行为

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：可被利用的安全漏洞、越权、凭据泄露
- 🟠 **P1 高**：功能缺陷、边界条件错误、潜在可利用风险
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（架构符合度、安全水平、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./pkg/grpc/interceptor/... ./internal/infra/auth/... ./pkg/jwtparser/... ./pkg/secretbox/... ./pkg/password/...`（仓库根目录执行）辅助检查
- 集成测试需要本地 Postgres/Redis，**不要运行**；纯单元测试（无 DB）如已存在可运行 `go test` 验证行为推断
