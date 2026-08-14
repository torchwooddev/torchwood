# Round 3 审查报告：01 安全与认证

> 审查对象：当前工作区（非对照旧 diff）。范围：`pkg/grpc/interceptor/`、`internal/infra/auth/`、`pkg/jwtparser/`、`pkg/secretbox/`、`pkg/password/`；交叉 `internal/domain/auth/`、`proto/shared/v1/authz.proto`、`internal/pkg/contexts/`。
> 为核实 G12 / B1 / `adminRoleMethodRules` 闭环，只读交叉了 `internal/app/client/account.go`、`internal/app/server/apikeys.go`、`internal/app/server/users.go`、`internal/app/shared/authz.go`、`internal/app/functions/authz_test.go`。
> `go vet` 对应包未作为阻断项单独复跑集成测试；结论均锚定当前源码行号。

## 摘要

认证主路径（JWT 算法强制、域分离密钥、refresh 原子轮换、OTP Lua 一次性、TOTP 锁定/防重放、trusted-proxy、cookie 属性、审计 3s 超时、API Key `functions.write` scope）整体健康，Round 2 已修项未回退。主导风险是 **`adminRoleMethodRules` 不完整 denylist**：viewer 会话可调用未登记的写 RPC；其中 `DeleteAPIKey` / `UpdateUser` 无 use-case 守卫，构成真实越权。其次是公开 `ConfirmEmailChange` 沿用「先 GETDEL 再比 secret」——知道 `project_id`+`user_id` 即可烧毁待确认 token（不能接管账号，但可稳定拒绝邮箱变更/找回）。

## 已核实为健康 / 已修复不再复报

- **F1-2 GETDEL 原子消费（正确 secret 路径）**：`internal/infra/auth/account_token_redis.go:120-140` 用 `GetDel`；二次使用 Unauthenticated。email_change purpose 隔离见同文件 `:78-85` 与 `account_token_redis_test.go:116-155`。
- **F1-3 MFA 锁定 + 防重放**：`internal/infra/auth/totp.go:111-178`（5 次/15min、三窗口 SETNX 60s）；`totp_test.go` 覆盖 replay / lockout。
- **F1-4 TOTP 密钥域分离**：`totp.go:47-51` `DeriveKey(master, "totp")`；存量主密钥双读 + 成功后重加密（`:132-146`、`:89-96`）。
- **F2-1 API Key 不得调 permission 方法 / AdminsService**：`pkg/grpc/interceptor/jwt.go:144-155`；`jwt_auth_test.go:142-171`。
- **F2-4 多凭证拒绝**：跨 scheme（`jwt.go:161-205`）+ 同 key 多值（`:207-218`）；HTTP 侧已对齐（`internal/api/serverhttp/auth.go:33-64`，交叉确认，不复报 R2 P1）。
- **F7-5 弱 JWT 密钥**：`cmd/server/provides.go:56-86` 子串即拒绝；不再是「仅 Warn」。
- **F7-6 审计超时**：`pkg/grpc/interceptor/audit.go:71-79` `WithoutCancel` + 3s timeout；`audit_test.go:51-101`。
- **G11 max_per_user 默认 50**：`internal/infra/auth/session_service.go:22-24,204-217`。
- **G12 `functions.write` 真能拦住**：
  - 规则表：`apikey_scope.go:99-116` 七个写方法均为 `{"functions","write"}`。
  - 拦截器在 handler 前：`jwt.go:110-125`，未映射方法 fail-closed（`apikey_scope.go:135-139`）。
  - 启动期 `AssertAPIKeyScopeCoverage`（`internal/infra/server/grpc.go:72-74`）。
  - `functions.read` 不能调写方法（`APIKeyScopeAllowed` 精确匹配 `.write`）。
  - 端用户/匿名被 `RequireServerWriteActor` 拒绝（`internal/app/shared/authz.go:43-54`）。
  - viewer/member admin 被 `admin_roles.go:35-41` + `admin_roles_test.go:77-108` 拦住。
- **B1 邮箱 staging（已合入，接管路径健康）**：
  - `UpdateAccount` 只写 `pending_email`，不改 `email`（`internal/app/client/account.go:454-477`）。
  - `ConfirmEmailChange` 为 `ACCESS_PUBLIC`（`proto/client/v1/account.proto:135-137`），安全模型与 recovery 一致：256-bit secret + 24h TTL + user_id 绑定在 Redis record。
  - 错误码对无效/过期/错 secret 统一 Unauthenticated，**不能**靠该 RPC 枚举用户是否存在。
  - `mapUserDoc` 不暴露 `pending_email`（`account.go:821-834`）。
  - 确认成功先撤会话再改邮箱（`:597-608`）；token 一次性有测试（`account_g3_test.go:344-378`）。
- **JWT alg 强制**：`pkg/jwtparser/jwt.go:56-58,73-75` `WithValidMethods([]string{"HS256"})`；签发固定 HS256（`:104`）。
- **secret 不入日志**：`jwt.go:63-74` 只记 method/reason/credential_type/ip/ua；`jwt_auth_log_test.go:91-94` 断言 token 本体不出现。
- **refresh rotation**：Lua 原子比较替换（`refresh_rotation_redis.go:16-26`）；mismatch 删会话（`account.go:421-424`）/ 撤 admin token（`internal/app/console/auth.go:102-107`）。
- **OTP 一次性**：`otp_store_redis.go:34-53` Lua：错码只 `HINCRBY`，对码才 `DEL`；HMAC-SHA256 存哈希（`:58-73`）。
- **login throttle**：email 10 / IP 30 / 15min（`login_throttle_redis.go:14-18`）；空 IP 不建键（`:85-90`）。
- **trusted proxy**：未配置不信任任何代理（`trusted_proxy.go:15-16,59-68`）；有 peer 时忽略伪造 XFF（`client.go:38-47`，`client_test.go:46-102`）。
- **session cookie**：Console `HttpOnly` + `SameSite=Lax` + refresh `Path=/v1/console/auth`（`internal/api/consolegrpc/cookies.go:25-63`）；OAuth 端用户 cookie 同样 HttpOnly/Lax（`internal/api/serverhttp/oauth_handler.go:65-72`）。
- **密码 / secretbox**：Argon2id m=64M,t=3,p=4（`pkg/password/password.go:13-31`）；AES-256-GCM（`pkg/secretbox/secretbox.go:21-43`）。
- **一次性 JWT**：未登记/二次使用 fail-closed（`validator.go:161-170`）。
- **系统集合写拒绝**：Client/Server Databases API 禁止写 `users`/`sessions`（交叉 `internal/app/client/databases.go:93-98`），不能经文档 API 改 `pending_email` / `expire_at`。

## 问题

### P0

无。

### P1

1. **`adminRoleMethodRules` 是不完整 denylist，viewer 可调用未登记的写 RPC；`DeleteAPIKey` / `UpdateUser` 无 use-case 纵深防御**
   - 位置：
     - `pkg/grpc/interceptor/admin_roles.go:14-49`（表内只有 `CreateAPIKey`，没有 `DeleteAPIKey` / `UpdateUser` / 多数 Storage·Teams 写方法）
     - `pkg/grpc/interceptor/jwt.go:129-134`（仅当 `adminRoleMethodRules[method]` 非空才拦角色）
     - `internal/app/server/apikeys.go:124-133`（`Delete` 无任何 `Require*`）
     - `internal/app/server/users.go:138-198`（`UpdateUser` 无 actor 守卫；保护字段仅 `password_hash`，`:67-69`）
   - 描述：注释写明「viewer 只读（仅 List/Get/Count）」，实现却是「未登记 = 放行」。viewer/member 的 console 会话是 `ActorKind=admin`，可通过 `ACCESS_API_KEY` 门（`jwt.go:110-114`）。对比：`CreateAPIKey` 同时有表项 + `RequirePlatformAdmin`；`CreateProject` 虽未入表但 use-case 拒绝非平台 admin（`internal/app/server/projects.go:49-52`）。`DeleteAPIKey` / `UpdateUser` 两层都空。
   - 影响：
     - **viewer 删除任意本项目 API Key**（破坏 CI/Agent 自动化；Create 被禁、Delete 却开放）。
     - **viewer 改用户 `email`/`status`/`factors`**：`UpdateUser` 改邮箱会置 `email_verified=false`（`users.go:174-175`），随后对公开 `CreateRecovery` 即可把重置信发到攻击者邮箱，构成**只读管理员接管终端用户账号**。
     - 同模式还覆盖未入表的 `UpdateBucket`/`DeleteBucket`、Teams 写方法、Databases 文档写等（视产品是否坚持 viewer 只读）。
   - 建议：
     - 立刻把 `DeleteAPIKey`、`UpdateUser` 及所有非 List/Get/Count 的 Server 写方法补进 `adminRoleMethodRules`（API Key / 用户敏感写保持 `owner,admin`；业务写保持 `member,owner,admin`）。
     - `APIKeys.Delete` 加 `RequirePlatformAdmin`；`Users.UpdateUser` 至少 `RequireServerWriteActor`，并扩大 `userUpdateProtectedFields`（`email`/`status`/`factors`/`pending_email` 走专用 RPC）。
     - 启动期对「ACCESS_API_KEY 写方法 ⊆ 角色表 ∪ 显式只读豁免」做与 `AssertAPIKeyScopeCoverage` 同类的 fail-closed 断言。
     - 补拦截器测试：viewer 调 `DeleteAPIKey`/`UpdateUser` 必须 PermissionDenied。

### P2

1. **公开 account-token 校验先 GETDEL 再比 secret：知道 `user_id` 即可烧毁 token**
   - 位置：`internal/infra/auth/account_token_redis.go:120-139`（`verifyTokenWithEmail`）；`ConfirmEmailChange` 调用链 `internal/app/client/account.go:574`；测试自行承认错 secret 也会删记录（`account_token_redis_test.go:152-155`）。
   - 描述：OTP 用 Lua「比对成功才 DEL、失败只计数」（`otp_store_redis.go:34-53`）。account token（email_change / verification / recovery / magic_url）则先 `GETDEL`，哈希不等也已消费。`ConfirmEmailChange` 现为 `ACCESS_PUBLIC` 且无频控（`account.go:555-574` 无 `rateLimiter`）。密钥按 `purpose:projectID:userID`（`:142-144`），**不需要 secret**。
   - 影响：不能伪造确认（secret 256-bit）。但 `project_id` 对客户端公开，`user_id` 出现在确认链接 `userId=`（`verification.go:132-141`）以及 Teams/Users 等 API。攻击者对已知用户发任意 secret，即可让进行中的改邮/验证/找回失效；用户点邮件链接得到 Unauthenticated，须重新发起。B1 把 Confirm 做成 public 后，该 DoS **不再需要登录态**。
   - 建议：改成与 OTP 相同的 Lua：哈希匹配才 DEL，否则 `HINCRBY` 并在 N 次后锁定；错 secret 不得删除记录。`ConfirmEmailChange` / `UpdateRecovery` 等公开消费口加 IP 频控。

2. **固定窗口限流 `INCR` 与 `EXPIRE` 非原子，进程在首次计数后崩溃会导致永久锁定**
   - 位置：
     - `internal/infra/auth/login_throttle_redis.go:69-82`
     - `internal/infra/auth/ratelimit_redis.go:28-36`
     - `internal/infra/auth/otp_store_redis.go:88-97`（IP 窗口）
   - 描述：`count==1` 时才 `Expire`。若 `Incr` 已成功、`Expire` 未执行（进程被杀 / Redis 瞬时错误），键无 TTL，计数只增不减。登录/OTP/通用限流会把该 email 或 IP **永久 ResourceExhausted**。
   - 影响：可用性；需人工删 Redis 键。不是认证绕过。
   - 建议：Lua 或 `SET key 1 EX ttl NX` + 随后 `INCR`；失败路径不要留下无 TTL 计数器。

### P3

1. **拦截器缺少 `functions.write` 专用用例**  
   `apikey_scope_test.go` 覆盖 databases/storage 的 read/write 互斥，但没有 `FunctionsService/CreateFunction` + `functions.read` 必须拒绝的断言。逻辑本身正确，回归靠启动期 coverage。建议补一条与 `admin_roles_test.go` 对称的 API Key 测试。

2. **JWT `alg=none` / 非 HS256 无单测**  
   `pkg/jwtparser/jwt.go:58` 已 `WithValidMethods`，但 `jwt_test.go` 只测过期。建议补 alg=none / HS384 必须 `Parse==false`。

3. **account token 哈希比较非恒定时间**  
   `account_token_redis.go:136` `record.SecretHash != HashOTP(secret)`。secret 为 256-bit，实际利用价值极低；仍建议 `subtle.ConstantTimeCompare`。

4. **会话缺 `expire_at` 时不过期**  
   `validator.go:209-218`、`session_service.go:157-166`：字段缺失则跳过过期检查。创建路径总会写入（`session_service.go:74`）；属防御深度。建议缺失与不可解析一样 fail-closed。

5. **`principalFromJWT` 不绑定验签密钥域与 `ActorKind`**  
   `validator.go:102-108,134-198`：先试 admin 派生钥再试 end-user 钥，然后只看 `ActorKind`。两端钥都来自同一 master，持有 master 可直接签 admin token。仅当 end-user 派生钥单独泄露时，才能用 `ActorKind=admin` 提权。建议记录验签 purpose，admin 声明必须配 admin 钥。

6. **OTP HMAC 未走 `DeriveKey`**  
   `otp_store_redis.go:28-29,65` 使用 `"torchwood-otp:"+jwtSecret` 作 HMAC key，与 TOTP/cookie/JWT 的 HMAC-SHA256(master, purpose) 不一致。功能安全，建议统一派生以免密钥用途混用。

7. **`X-Torchwood-Project` 同 key 多值只取首个**  
   `jwt.go:135,254-259` `firstMetadataValue` 不拒绝多值。凭证头已拒绝多值，项目头未对齐。建议同样拒绝。

8. **会话文档 `secret_hash` 存的是明文 UUID 且从未用于校验**  
   `session_service.go:67-72`。认证靠 HMAC cookie / JWT `session_id`。误导性字段，建议删除或改为真哈希。

9. **无 peer 时 ClientInfo 信任 XFF**  
   `client.go:41-46` 注释为进程内/测试。生产 gRPC 均有 peer。保持注释即可，避免网关漏注入 peer。

## 模块结论

- **架构符合度**：端口在 domain、适配器在 infra、拦截器做第一道门、use-case 做纵深防御——主路径符合 Clean Architecture 与 AGENTS.md。G12 后 `functions.write` 不再是死登记，拦截器 + 启动断言 + `RequireServerWriteActor` 闭环完整。B1 staging 契约（pending_email、公开 Confirm、一次性 token、不暴露 pending）已落地。
- **安全水平**：凭据解析、JWT、轮换、OTP/TOTP、代理 IP、cookie、审计、日志脱敏达到可上线水平。最大缺口是 **console 角色 denylist 漏登记**，把「viewer 只读」变成「未点名的写方法默认放行」。
- **最需优先处理的 3 项**：
  1. 补全 `adminRoleMethodRules` 并给 `DeleteAPIKey` / `UpdateUser` 加 use-case 守卫（P1，viewer 可删密钥、改邮箱接管用户）。
  2. account-token 校验改为「比对成功才删除」，公开 Confirm/Recovery 加频控（P2）。
  3. 登录/OTP/通用限流的 INCR+EXPIRE 改为原子 TTL（P2，防永久锁死）。
