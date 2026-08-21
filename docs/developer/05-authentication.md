# Torchwood 认证与授权

> 本文描述 Torchwood 的认证方法与授权模型：四种认证方式（终端用户 JWT、session cookie、API Key、Console admin session）、Principal 注入、gRPC 方法级 authz 注解、JWT claims 与密码/加密工具。
> 相关代码：`pkg/grpc/interceptor/`、`internal/infra/auth/validator.go`、`internal/infra/auth/session_cookie.go`、`pkg/jwtparser/`、`pkg/password/`、`pkg/secretbox/`、`proto/shared/v1/authz.proto`、`internal/infra/server/grpc.go`、`internal/api/consolegrpc/cookies.go`、`internal/infra/bun/bunrepo/apikey_repo.go`。

---

## 1. 认证方法矩阵

所有认证在这里归结为三种**凭证类型**（`internal/domain/shared/principal.go`）与三种**主体类型**（ActorKind）：

| 维度 | 取值 |
|------|------|
| `CredentialType` | `token`（JWT：Bearer）、`session`（session cookie）、`api_key`（API Key） |
| `ActorKind` | `end_user`（终端用户）、`admin`（Console 管理员）、`service`（API Key / 自动化） |

`pkg/grpc/interceptor/jwt.go` 的 `extractCredential` 按以下优先级从 gRPC metadata 提取凭证：

| 优先级 | 来源 | 映射 |
|:---:|------|------|
| 1 | `authorization` 头 | `Bearer <jwt>` → `token`；`Session <cookie>` → `session`；`Apikey` / `Api-Key <key>` → `api_key`（`ParseAuthorizationHeader`，无法识别 scheme 时一律拒绝） |
| 2 | `cookie` 头 | `TORCHWOOD_session_console` → 项目 `console`；`TORCHWOOD_session_<projectID>` → 对应项目（`parseSessionCookie`） |
| 3 | `x-api-key` 头 | 视为 `api_key` |

| 认证方法 | 凭证类型 | 面向对象 | 说明 |
|----------|----------|----------|------|
| 终端用户 JWT | `token` | Client API 终端用户 | 用 `end-user-jwt` 派生密钥签发，claims 带 `end_user` 角色，可含 `pid`/`sid` |
| End-user session cookie | `session` | Client API 浏览器 | `TORCHWOOD_session_<projectID>`，HMAC 签名的不透明 cookie，或 JWT 形式（见 §3） |
| Console admin session | `session` / `token` | Admin Console | `TORCHWOOD_session_console` HttpOnly cookie；刷新限 `/v1/console/auth`（见 §3.1） |
| API Key | `api_key` | Server API（Agent/自动化） | 细粒度 scoped（见 §4） |

```go
// internal/pkg/grpc/interceptor/jwt.go —— 凭证解析优先级
md 中:
  authorization: "Bearer eyJ..."   -> CredentialTypeToken
  authorization: "Session <val>"   -> CredentialTypeSession
  authorization: "ApiKey <val>"    -> CredentialTypeAPIKey
  cookie: "TORCHWOOD_session_console=..."   -> CredentialTypeSession, project="console"
  cookie: "TORCHWOOD_session_proj-x=..."    -> CredentialTypeSession, project="proj-x"
  x-api-key: "...key..."           -> CredentialTypeAPIKey
```

---

## 2. Principal 注入机制

认证通过后，`AuthInterceptor.UnaryAuthMiddleware`（`pkg/grpc/interceptor/jwt.go`）把校验结果封装为 `shared.Principal` 并写入上下文（`contexts.WithPrincipal`），handler 通过 `contexts.Principal(ctx)` 读取。

Principal 结构（`internal/domain/shared/principal.go`）：

| 字段 | 说明 |
|------|------|
| `ActorID` / `ActorKind` | 主体 ID 与类型（end_user / admin / service） |
| `CredentialType` | 本次认证使用的凭证类型 |
| `IsPlatformAdmin` | 管理员是否为 owner/admin 平台级 |
| `ProjectID` / `UserID` / `SessionID` / `APIKeyID` | 归属信息 |
| `Roles` | 角色（终端用户角色如 `users`、`user:{id}`；API Key 固定 `["keys"]`；admin 为其角色） |
| `Permissions` | **API Key 专用于存放 scopes**（`Principal.Permissions` 即 `api_keys.scopes`） |

### 2.1 拦截器判定流程

```
UnaryAuthMiddleware(ctx, req)
  ├─ ACCESS_PUBLIC 方法: 尽力解析凭证（可选）→ 直接放行
  ├─ 无凭证 → 401 Unauthenticated
  ├─ ValidateCredential 失败 → 401
  ├─ API_KEY 方法（apiKeyMethods）:
  │    ├─ 要求 CredentialType==api_key 或 ActorKind==admin
  │    │      （所以 API_KEY 方法允许 admin console session 调用）
  │    ├─ 若为 API key 凭证:
  │    │    ├─ 禁止调用 APIKeys 服务（防泄露 key 自铸新 key 提权）→ 403
  │    │    └─ APIKeyScopeAllowed 校验 scope → 403
  ├─ admin 主体: 读取 X-Torchwood-Project 头 → principal.ProjectID，
  │    ValidateAdminProjectAccess（非平台级管理员须有该项目访问权）
  ├─ permissionMethods: HasAnyPermission 校验
  └─ contexts.WithPrincipal(ctx, principal) → handler
```

**Admin console session 指定项目**：仅针对 `ActorKind == Admin`，当请求带 `X-Torchwood-Project` header 时把该值写入 `principal.ProjectID`，随后 `ValidateAdminProjectAccess` 校验此管理员是否有权访问该项目（owner/admin 平台级豁免，`internal/infra/auth/validator.go`）。这也是 console 多项目访问的载体。

### 2.2 Validator

`internal/infra/auth/validator.go` 实现 `interceptor.Validator` 接口，`ValidateCredential` 分发到：

| 凭证 | 校验逻辑 |
|------|---------|
| api_key | `sha256(raw)` → `GetAPIKeyBySecretHash` 查询（secret 只存哈希）；校验 `Enabled` 与 `ExpireAt`；Principal：`ActorKind=service`、`Roles=["keys"]`、`Permissions=Scopes`、`ProjectID=key.ProjectID` |
| token | 两次尝试：先用 `admin-jwt` 派生密钥解析，失败再用 `end-user-jwt`（同密钥域内由 ActorKind claim 分发；`parseJWT`） |
| session | 先尝试当 JWT 解析（console access/refresh token 是 JWT）；失败则用 `SessionCookieCodec.Verify` 解出 `projectID:sessionID` 后在 `sessions` 集合查证（`principalFromSession`） |

终端用户请求还会做**实时角色解析与账户状态检查**（fail-closed）：
- `ensureUserCanAuthenticate`：用户文档存在且 `status` 允许登录（`users.CanAuthenticate`）；
- `resolveEndUserRoles`：通过 `UserRoleResolver` 实时加载角色，避免 JWT claims 里的旧角色残留；解析失败一律按 Unauthenticated 拒绝。

---

## 3. Session 处理

### 3.1 Console admin session（HttpOnly cookie，不用 localStorage）

见 `internal/api/consolegrpc/cookies.go` 与 `internal/app/console/auth.go`：

| 项 | 值 |
|----|-----|
| access cookie | `TORCHWOOD_session_console`，Path `/`，**HttpOnly + SameSite=Lax**，`Max-Age = access_ttl` |
| refresh cookie | `TORCHWOOD_console_refresh`，**Path 限 `/v1/console/auth`**（只发向刷新端点，界面无法在其它路径使用它），`Max-Age = refresh_ttl` |
| `Secure` | **仅当 `server.http.public_url` 以 `https://` 开头**（`SecureCookies()`），本地 HTTP 开发不置位 |
| XSS / CSRF | HttpOnly 免疫 JS 窃取；SameSite=Lax 下跨站 POST 不携带 cookie，本服务变更类端点均为 POST，故无需额外 CSRF token（前提：cookie 仅限同源 `/v1` 使用） |
| 刷新 | `POST /v1/console/auth/refresh`；`refresh_token` 为空时从 refresh cookie 读取（cookie-only 浏览器流，`refreshTokenFromCookie`） |
| 登出 | `Max-Age=0` 清除两个 cookie，并撤销该 admin 此前签发的全部 token（`RevokeBefore`） |

刷新令牌采用 **rotation + 重用检测**：`RefreshToken` 通过 `RefreshRotationStore.Rotate` 校验 `jti`，旧 refresh token 被再次使用 → `RotateMismatch` → 撤销该 admin 全部 token 并返回 Unauthenticated（`internal/app/console/auth.go`）。Console admin 的 access/refresh 均用 `admin-jwt` 派生密钥签发（HS256）。

### 3.2 End-user session cookie

- 浏览器端 Client API 可用 `TORCHWOOD_session_<projectID>` cookie 认证。
- 另一种形式是**不透明 session cookie**：`SessionCookieCodec.Sign` 生成 `base64url(projectID:sessionID):hmac-sha256`（密钥由 `jwt.secret` 经 `PurposeSessionCookie` 派生，`internal/infra/auth/session_cookie.go`）；验证时先验签名，再查 `sessions` 文档确认存在、未过期、`user_id` 匹配。
- refresh token 换新、重用即删会话：`POST /v1/account/refresh` 在 `RotateMismatch` 时直接删除该 sessions 文档，使该会话全部 token 失效（`internal/app/client/account.go`）。

---

## 4. API Key（scoped）

### 4.1 存储与校验

- 创建时：`secret = uuid()+uuid()`，只往 `api_keys` 表写 `sha256(secret)` 的 hex（`internal/app/server/apikeys.go`）；secret 仅在创建响应中出现一次，数据库不存明文。
- 校验：`Validator.validateAPIKey` 对原始 key 做 sha256 后 `GetAPIKeyBySecretHash` 精确命中；`api_key` 不在库中 / 被禁用（`Enabled=false`）/ 过期（`ExpireAt`）均拒绝。
- 请求头默认 `x-api-key`（`security.api_key.header`），也支持 `Authorization: ApiKey <key>`。

### 4.2 scope 命名规则（B2）

`pkg/grpc/interceptor/apikey_scope.go` 显式登记全部 8 个 `ACCESS_API_KEY` 服务的方法 → `{resource, op}`，是 scope 格式的**单一事实来源**：

| 资源（裸 scope） | 服务 | 读 scope | 写 scope |
|-----------------|------|----------|----------|
| `databases` | DatabasesService | `databases.read` | `databases.write` |
| `users` | UsersService | `users.read` | `users.write` |
| `groups` | GroupsService | `groups.read` | `groups.write` |
| `storage` | StorageService | `storage.read` | `storage.write` |
| `projects` | ProjectsService | `projects.read` | `projects.write` |
| `oauthproviders` | OAuthProvidersService | `oauthproviders.read` | `oauthproviders.write` |
| `apikeys` | APIKeysService | `apikeys.read` | `apikeys.write` |
| `functions` | FunctionsService | `functions.read` | `functions.write` |

**scope 合法格式**：`*` / `all`（全量）、裸资源名（`databases` 全量放行该资源）、`<resource>.read`（仅读方法）、`<resource>.write`（仅写方法）。创建时校验 `interceptor.ValidAPIKeyScope`，上限：**≤32 项、每项 ≤64 字符**。

**匹配规则**：
- 裸资源名与 `*` / `all` 全量放行该资源；
- `<resource>.read` 只放读方法（List/Get/Count 类），`<resource>.write` 只放写方法；
- **fail-closed**：未登记的方法即使带 `*`/`all` 也拒绝；新增 `ACCESS_API_KEY` 服务必须在 `apiKeyScopeRules` 登记，否则 `APIKeyScopeAllowed` 恒返回 false。

**防护**：API key 凭证**禁止调用 APIKeys 服务**（`IsAPIKeysServiceMethod` → 403 `"api keys cannot manage api keys"`），防止泄露的 key 自铸新 key 造成永久提权；admin console session 不受此限制。

### 4.3 API Key 以 keys 角色参与 _perms（不默认 bypass）

- 校验成功后 Principal 的 `Roles = ["keys"]`（`internal/infra/auth/validator.go` 的 `validateAPIKey`）；
- 动态文档层把 `keys` 视为与 `users`、`user:{id}` 并列的角色参与 `_perms` 判定，**不默认绕过文档权限**：
  - `ExpandPermissionRoles` 只在调用方持 `keys` 角色时注入 `keys`（`postgres_permissions.go` / `internal/domain/databases/`）；
  - API Key `keys` 角色只参与**用户 collection** `_perms`；系统资源不走 `_perms`，只经 Account、Server Users、Storage、Groups 专用 RPC。
  - Server API 读写用户文档时，文档 `_perms` 上需显式授予 `read:keys` / `write:keys` 等才可访问。
- 特权主体（`SystemPrincipal`、PlatformAdmin）才走完全绕过（`IsSystem()`）。

API Key 不在首次部署引导中生成。登录 Console 后到 **API Keys** 页面创建；
scope 由创建者指定（`all` 表示全量放行，等价于旧的逐资源 read/write 组合）。

---

## 5. gRPC 方法级 authz 注解（method_auth）

### 5.1 注解定义

`proto/shared/v1/authz.proto` 定义 AccessLevel 与扩展：

```proto
enum AccessLevel {
  ACCESS_LEVEL_UNSPECIFIED = 0;
  ACCESS_PUBLIC         = 1;  // 无需认证（仅尽力解析可选凭证）
  ACCESS_AUTHENTICATED  = 2;  // 已认证 + required permissions [配置为 users]
  ACCESS_PERMISSION     = 3;  // 已认证 + 显式 permissions
  ACCESS_API_KEY        = 4;  // 需要 API Key 凭证 或 admin session
}

extend google.protobuf.MethodOptions  { MethodAuth  method_auth  = 52001; }
extend google.protobuf.ServiceOptions { ServiceAuth service_auth = 52002; }
```

- `method_auth = { access: ACCESS_API_KEY }` 标注方法；
- 服务可用 `(service_auth) = { default_access: ... }` 声明默认级别，方法未标注时回退到服务默认（`resolveMethodAccess`）。
- 例：`ConsoleAuthService` 服务默认 `ACCESS_PUBLIC`；`APIKeysService` 服务默认 `ACCESS_API_KEY`；`AdminsService` 默认 `ACCESS_PERMISSION` 并对个别方法显式声明。

### 5.2 为什么必须带注解（collectMethodsByAccess）

`internal/infra/server/grpc.go` 启动时调用 `collectMethodsByAccess(...)` 扫描所有 proto 描述符，按 access 归集方法清单：

| access | 归集 |
|--------|------|
| `ACCESS_PUBLIC` | `publicMethods`（拦截器放行 + 可选凭证） |
| `ACCESS_API_KEY` | `apiKeyMethods`（须 API Key 或 admin） |
| `ACCESS_AUTHENTICATED` | `permissionMethods[method] = perms`；未写 permissions 时默认 `["users"]` |
| `ACCESS_PERMISSION` | `permissionMethods[method] = perms`；**未写 permissions 直接报错** |

校验规则：
- 方法未标注、且服务无默认级别 → `missing auth policy for method ...`，**启动失败**；
- **服务默认未标注**：注册的 gRPC 方法若未覆盖到 authz 注解（`verifyRegisteredMethodsMissingAuthz`），同样导致启动失败（`registered grpc methods missing authz annotation`）。

因此**每个新增 gRPC 方法都必须带 `method_auth`（或依赖服务默认）**，否则 server 起不来——这是把「漏标注解」从运行期漏洞变成「启动期硬失败」的守门机制（`HasAnyPermission` 对空列表 fail-open，依赖该守门保证注解非空）。

---

## 6. JWT claims 与加密工具

### 6.1 pkg/jwtparser —— claims 映射

`pkg/jwtparser/jwt.go` 定义 Claims 与 JSON claim 名（短名映射）：

| 字段 | claim | 说明 |
|------|-------|------|
| `TokenID` | `tid` | token ID（rotation 用的 jti） |
| `UserID` | `uid` | 用户/管理员 ID |
| `Username` | `usn` | 邮箱（user）或 email（admin） |
| `ActorKind` | `akd` | `end_user` / `admin` / `service` |
| `ProjectID` | `pid` | 项目 ID |
| `SessionID` | `sid` | 会话 ID |
| `TokenType` | `ttp` | `access` / `refresh` |
| `Roles` | `rls` | 角色列表 |
| `Scopes` | `scp` | scope 列表 |
| `ExpiresAt` | `exp` | Unix 秒 |
| `IssuedAt` | `iat` | Unix 秒 |

- 用 **HS256** 签名；解析强制要求 `exp` 与 `iat`，且仅接受 HS256（`jwt.WithExpirationRequired()`、`jwt.WithValidMethods([]string{"HS256"})`）。
- 验证时会校验 `TokenType`（access/refresh 必须匹配）与 `ActorKind`（admin/end_user）。

### 6.2 密钥派生（域分离）

`pkg/jwtparser/keys.go` 用 `HMAC-SHA256(master, purpose)` 从同一个 `security.jwt.secret` 派生**域分离子密钥**，跨域 token 互不通用：

```
PurposeEndUserJWT    = "end-user-jwt"     # 终端用户 access/refresh/一次性 JWT
PurposeAdminJWT      = "admin-jwt"        # Console admin access/refresh
PurposeSessionCookie = "session-cookie"   # 不透明 session cookie 的 HMAC 密钥
```

改动 master secret 或 purpose 标签会立即使对应域的全部凭证失效。

### 6.3 pkg/password —— 密码哈希（Argon2id）

`pkg/password/password.go` 使用 **Argon2id**：`time=3`、`memory=64*1024`、`parallelism=4`、`keyLen=32`、`saltLen=16`，存储格式：

```
$argon2id$v=19$m=65536,t=3,p=4$<salt-b64>$<hash-b64>
```

`Verify` 按格式解析参数并重新计算，用 `subtle.ConstantTimeCompare` 常时比较。用于 users `password_hash`、Console admins `PasswordHash`。

### 6.4 pkg/secretbox —— 敏感字段加密（AES-256-GCM）

`pkg/secretbox/secretbox.go`：密钥由 `sha256("torchwood-secretbox:" + secret)` 派生，AES-256-GCM，密文带前缀 `enc:v1:`（`Encrypt` 空值返回空串；`Decrypt` 对无前缀的旧明文直接透传，保证向后兼容）。

实际用途（以代码为准）：

| 场景 | 位置 |
|------|------|
| OAuth provider `client_secret` 落库加密 | `internal/infra/bun/bunrepo/oauth_provider_repo.go`（用 JWT secret 作加密密钥） |
| MFA TOTP factor secret 加密存储 | `internal/infra/auth/totp.go`（`secretbox` 加密/解密 `factor.Secret`） |

---

## 7. 参考

- `pkg/grpc/interceptor/*.go`：凭证提取、Principal 注入、API Key scope 校验、可信代理、审计。
- `internal/infra/auth/validator.go`：三种凭证的完整校验。
- `internal/infra/auth/session_cookie.go`：不透明 session cookie 的 HMAC 格式。
- `internal/api/consolegrpc/cookies.go` / `internal/app/console/auth.go`：Console 会话 cookie 与 admin 刷新/撤销。
- `internal/app/server/apikeys.go`：API Key scope 校验与 secret 哈希。
- `internal/infra/server/grpc.go`：`collectMethodsByAccess` 启动期守门。
- `proto/shared/v1/authz.proto`：authz 注解定义。
- `pkg/jwtparser/`、`pkg/password/`、`pkg/secretbox/`：JWT、密码哈希、敏感字段加密。
- `docs/developer/06-databases.md` §3：`_perms` 权限模型与角色清单（含 `keys`）。
- `docs/developer/03-configuration.md` §5：cookie 与 trusted_proxies 配置。
