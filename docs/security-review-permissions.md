# Torchwood 权限系统专项评审与完整修复方案

> 审查日期：2026-08-07（两轮深度评审 + 独立审查）
> 范围：认证链路（API Key / JWT / Session Cookie）、授权链路（proto authz 注解 → 拦截器 → 文档 ACL → 用例层 ownership）、Server/Client/Console 三套 API 的权限边界、动态文档层 `_perms` 模型、OAuth/OTP 认证流、HTTP 网关与存储端点
> 状态：**C1/H1/H2/H3/M1/M3/M4/M6/M7/M8/M9/M10 及 keys 权限收窄已由子代理实施完成并通过全量验证（2026-08-07）；M2/M5 与 L 系列低危项待办**
> 关联文档：[security-review.md](./security-review.md)（既有审查，含 P0-1 等方案；本文档与其有**结论冲突**，见 §1 说明）

---

## 1. 问题总览

| 编号 | 级别 | 问题 | 利用前提 | 影响面 |
|------|------|------|----------|--------|
| **C1** | 🔴 Critical | Server Databases API 击穿系统集合保护 → 任意用户账户接管 | 持有 `databases.*` scope 的 API key | 全项目用户/团队/身份数据沦陷 |
| H1 | 🔴 高 | `ValidateGrantablePermissions` 被合成角色 `any` 绕过 | 持有任意文档 update 权限的共享者 | 权限转授放大至"全员可改删" |
| H2 | 🔴 高 | end-user JWT roles 时效性窗口 | 被移出团队/降级/删号的用户 | 最长 accessTTL 内保留旧角色 |
| H3 | 🟠 中 | `validateEndUserSession` 不校验 session 归属 | session 文档 `user_id` 被污染 | 认证纵深防御缺口 |
| M1 | 🟠 中 | `ACCESS_PERMISSION` 空 permissions fail-open | 未来新增方法漏配 | 任意凭证放行 |
| M2 | 🟠 中 | `keys` 角色对系统敏感集合文档权限过大 | 任意 scope 的 API key | sessions/identities 可读写删 |
| M3 | 🟠 中 | Session Cookie 用户缺失 team/label/verified 角色 | OAuth cookie 流用户 | 团队功能不可用（功能缺陷） |
| M4 | 🟠 中 | HTTP 存储端点 scope 检查粗粒度且语义错位 | `storage.read` scope 的 key | 只读 scope 可越权上传（独立审查修正） |
| M5 | 🟠 中 | 默认集合权限 `read:any` 匿名公开可读 | 新建集合未显式收紧 | 默认数据泄露 |
| M6 | 🟠 中 | 匿名请求可探测项目存在性并触发系统集合 Ensure | 无 | 存在性探测 + DDL 浪费（配套需禁删 default 库） |
| M7 | 🟠 中 | `ListProjects`/`GetProject`/`CreateProject` 无项目级权限与配额 | 任意已认证调用者 | 跨项目信息泄露 + 资源耗尽 |
| M8 | 🟠 中 | OAuth email 验证依赖 provider adapter 自律 | 新增/修改 provider 实现 | 未验证邮箱占号 |
| M9 | 🟠 中 | OAuth 失败错误信息泄入重定向 URL | 无 | 内部细节泄露 |
| M10 | 🟠 中 | `DeleteUser` 残留孤儿 identities/memberships | API key 删除用户后 | 阻塞同邮箱重新注册 |
| L1-L9 | 🟡 低 | 见 §6 | 视各自前提 | 局部 |

> 与既有 `security-review.md` 的差异：
> - **冲突**：该文档 P0-1 声明「Server API（API key）走独立调用链，不受影响（已验证）」——**该结论不成立**，详见 §2 C1。C1 是当前全系统最高优先级问题。
> - 该文档已覆盖的问题（P0-1 Client 系统集合、P0-2 存储 XSS、P0-3 Teams、P1-2~P1-5、P2-1~P2-10、审计盲区）本文档不再重复，修复顺序上与之合并排序。

---

## 2. 🔴 C1 Server Databases API 系统集合越权（账户接管）

### 2.1 位置与根因

三层缺陷叠加：

1. **app 层无系统集合黑名单**：`internal/app/server/databases.go:184-205`（`ensureCollection`）只校验标识符 + 集合存在 + Disabled，不拦截系统集合。对比 Client 版有 `clientSystemCollections` 黑名单（`internal/app/client/databases.go:17-30`）。
2. **docDB 层 UpdateDocument 缺写保护**：`internal/infra/documentdb/postgres.go:412-462` 的 `UpdateDocument` 没有 `isWriteProtectedSystemCollection` 检查——而 `CreateDocument`（:344）与 `DeleteDocument`（:473）都有。纵深防御不闭合。
3. **API key 恒有 `keys` 角色且文档级全权**：`internal/infra/auth/validator.go:109` 注入 `Roles: ["keys"]`；系统集合文档级 `_perms` 授予 keys update/delete（`system_collection_specs.go` users:39 / sessions:69-74 / identities:98-102；`session_service.go:172-184`）。

### 2.2 攻击链（已全链路验证）

```
前提：scope=databases.write（或 "*"）的 API key
  ↓
① servergrpc/databases.go:334 UpdateDocument — data 原样透传，无字段过滤
② app/server/databases.go:272 UpdateDocument — ensureCollection 放行系统集合
③ postgres.go:412 UpdateDocument — checkDocumentPermission("update")：
   users 文档 _perms 含 update:keys → 通过
④ password_hash 被覆盖 → 攻击者用新密码登录受害者账号
```

衍生攻击（同一入口）：
- `databases.read` scope → `GetDocument/ListDocuments(collection_id="users")` 读出全部 `password_hash`（Argon2id，可离线爆破）、`sessions`（`secret_hash`/IP/UA）、`identities`；
- `CreateDocument(collection_id="memberships", {user_id:自己, team_id:<受害团队>, status:accepted, roles:[owner]})` → 刷新 token 后获得 `team:<id>/owner` → 团队接管；
- 直接改 `buckets`/`files` 元数据。

### 2.3 修复方案（三层，建议全部落地）

**第 1 层：Server Databases API 拒绝系统集合（核心修复）**

`internal/app/server/databases.go` 新增黑名单并在 `ensureCollection` 前置拦截（与 `internal/app/client/databases.go:17-30,110-113` 对齐）：

```go
// serverSystemCollections 是 Server Databases API 禁止直接读写的系统集合。
// 系统集合只能经专用服务（Users/Teams/Storage/Auth）访问。
var serverSystemCollections = map[string]struct{}{
    "users": {}, "sessions": {}, "identities": {},
    "teams": {}, "memberships": {},
    "buckets": {}, "files": {},
}

func isServerSystemCollection(collectionID string) bool {
    _, ok := serverSystemCollections[collectionID]
    return ok
}
```

`ensureCollection`（:184-205）在 `GetCollection` 之前增加：

```go
if isServerSystemCollection(collectionID) {
    return shared.MapDocumentDBError(databases.ErrPermissionDenied)
}
```

> 影响面核验：
> - `server.Users` / `server.Teams` / `storage.Storage` / `auth.SessionService` 均**直接调用 docDB**（不经 `server.Databases`），不受影响；
> - Console UI 管理用户/团队/存储走 Server API 的专用服务（Users/Teams/Storage），也不受影响；
> - 受影响的仅为"用 Databases API 直接 CRUD 系统集合"这一非法用法（当前无合法调用方）。

**第 2 层：docDB 层 UpdateDocument 补系统集合写保护（纵深防御）**

`internal/infra/documentdb/postgres.go` 的 `UpdateDocument`（:412）在 `checkDocumentPermission` 之前增加（与 Create/Delete 对齐）：

```go
// 非 System 且非文档 owner（user:<id> 匹配）时，禁止写入写保护系统集合。
// owner 例外：end-user 自助路径（UpdateAccount/UpdatePrefs）以 user:<id> 角色更新自己的 users 文档。
if !principal.IsSystem() &&
    !principal.HasRole(fmt.Sprintf("user:%s", doc.ID)) &&
    isWriteProtectedSystemCollection(collectionID) {
    return doc, ErrPermissionDenied
}
```

> ⚠️ **独立审查修正（2026-08-07）**：初版方案"非 System 一律拒绝"会破坏以下**在线合法路径**（不止 UpdateUserStatus）：
> - `internal/app/client/account.go:415-418`（UpdateAccount，含 password_hash 更新）与 `:513-516`（UpdatePrefs）——以 `Principal{Roles: p.Roles}`（**非 System**，但含 `user:<id>`）更新自己的 users 文档。若不加 owner 例外，**end-user 改密码/改邮箱/改 prefs 全部 403**。
> - `UsersService.UpdateUser`（`server/users.go:63-98`）/ `DeleteUser`（:100-105）——非 System 的 keys 角色直调 docDB。
> - `UsersService.UpdateUserStatus`（`server/users.go:107-125`）——全仓 grep 确认**无 handler 引用（死代码）**，可顺带删除或与 UpdateUser 一起改造。
>
> 因此第 2 层的正确落地组合为：
> - (a) `server.Users` 的 `UpdateUser`/`DeleteUser`（及死代码 `UpdateUserStatus`）改为用 `databases.SystemPrincipal` 调用 docDB——keys 角色已由拦截器 scope 把关，改用 System 不扩大权限面，用例层即权限层；推荐；
> - (b) 上述 owner 例外放行 end-user 自助路径（UpdateAccount/UpdatePrefs），并补充这两个方法的回归测试。

**第 3 层：keys 角色对系统敏感集合的权限收窄（纵深防御）**

> ⚠️ **独立审查修正（2026-08-07）**：`AllowsDocumentAccess` 是 `collOK || docOK`（`permissions.go:92`），**集合级**权限单独存在即可放行——初版方案只收窄**文档级** `_perms` 对 C1 无效（keys 仍经集合级 `update:keys` 通过）。第 3 层必须同时处理**集合级**权限（`system_collection_specs.go`）：
> - users（:33-44）：移除 `update:keys`/`delete:keys`，保留 `read:keys`——**前提**是第 2 层方案 (a) 已落地（UsersService 改 SystemPrincipal），否则 UsersService 会被集合级收窄破坏；
> - sessions（:62-75）：移除 `update:keys`/`delete:keys`，保留 `read:keys`；
> - identities（:92-103）：移除 `update:keys`/`delete:keys`，保留 `read:keys`；
> - 文档级 `_perms`（`session_service.go:172-184`、`userDocumentPermissions` 等）同步收窄（第 3 层纵深，非主防线）。
>
> 另注意 `UpdateDocument` 同样存在于 `BulkUpdateDocuments` 路径（`postgres_permissions.go:137-160` 逐文档调用 `UpdateDocument`）——第 2 层拦截自动覆盖。

### 2.4 验收标准

- [ ] `databases.write` scope 的 key 调用 `UpdateDocument(collection_id="users", ...)` 返回 403/400（**含 password_hash 覆盖尝试**）
- [ ] `databases.read` scope 的 key 对 users/sessions/identities/teams/memberships/buckets/files 的 Get/List 被拒
- [ ] `databases.*` 对非系统集合的 CRUD 不受影响
- [ ] `UsersService` 的 List/Get/Update/Delete 回归通过（方案 (a) 落地后）；`UpdateUserStatus` 死代码删除或同步改造
- [ ] **end-user 自助路径回归**：UpdateAccount（改密码/改邮箱）、UpdatePrefs、DeleteSession 正常（owner 例外生效）
- [ ] SignUp / OTP / 匿名注册 / session 创建/删除回归通过
- [ ] 非 SystemPrincipal 且非 owner 直调 docDB 对 users/sessions/identities 的 update 被拒（adapter 层测试）

---

## 3. 🔴 H1 "any" 合成角色击穿授予约束

### 3.1 位置与根因

`internal/domain/databases/permissions.go`：
- `ExpandPermissionRoles`（:34-57）**无条件注入 `"any"`**；
- `ValidateGrantablePermissions`（:148-162）用 `roleHeld(expanded, p.Role)` 校验授予角色——由于 `any` 恒在 expanded 中，**任意持有文档 update 权限的共享者**（如团队普通成员、被授予 update 的其他用户）都可以把权限转授为 `update:any`/`delete:any`，突破"不能授予自己未持有的角色"约束。

### 3.2 影响面

- 持有文档 update 权限的共享者 → 文档转授为全员可改/可删；
- 注意：`any` 的写操作最终仍需通过 gRPC 认证（变更端点非 PUBLIC），故实际暴露面是"任意已认证用户"而非匿名；但 `read:any` 转授会使匿名可读（配合既有 P2-1 的公开读路径）。

### 3.3 修复方案

`permissions.go` 中区分**合成角色**（无条件注入、不可作为授予凭据）：

```go
// syntheticRoles 由 ExpandPermissionRoles 无条件注入、不构成授予凭据的角色。
// "any" 的写类授予一律拒绝；read 类授予保留（文档公开读取是集合级显式行为）。
var syntheticRoles = map[string]struct{}{
    "any": {},
}
```

`ValidateGrantablePermissions`（:148-162）修订：

```go
func ValidateGrantablePermissions(grantor Principal, perms []Permission, privileged bool) error {
    if privileged || grantor.IsSystem() || grantor.PlatformAdmin {
        return nil
    }
    expanded := ExpandPermissionRoles(grantor.Roles)
    for _, p := range perms {
        if p.Type == "create" {
            continue
        }
        if _, synthetic := syntheticRoles[p.Role]; synthetic {
            if p.Type != "read" {
                return fmt.Errorf("cannot grant %q for role %q", p.Type, p.Role)
            }
            continue
        }
        if !roleHeld(expanded, p.Role) {
            return fmt.Errorf("cannot grant role %q without holding it", p.Role)
        }
    }
    return nil
}
```

> 边界说明（**独立审查补充，2026-08-07**）：
> - `read:any` 授予保留（文档公开读取是常见合法操作）；`users` 角色不落入 synthetic 集合（所有 end-user 都持有 `users`，授予 `read:users` 语义等价于公开给已认证用户，保留现状）。
> - **修复比初版验收标准更宽**：owner（持有 `user:<id>`）同样会被禁止授予 `update:any`/`delete:any`——这是 Appwrite 兼容语义（文档级 `update:any` 是标准用法）的产品级行为变更，需在 SDK/文档中明示，并补充 owner 场景验收。
> - **边界声明**：storage（`storage.go:322-331` `parseRawPermissions`）与 teams（`servergrpc/teams.go:39` 显式 `permissions` 参数）的显式权限**不经过** `ValidateGrantablePermissions`（直接 `docDB.CreateDocument(..., SystemPrincipal)`）——H1 修复不覆盖这些特权路径（有 teams/storage scope 的 key 仍可铸造 `update:any`）；如需完整闭环，后续可将这些入口纳入同一校验（当前优先级低：仅特权凭证可达）。

### 3.4 验收标准

- [ ] 共享者（非 owner）向文档授予 `update:any` / `delete:any` 被拒；`read:any` 放行
- [ ] owner（持有 `user:<id>`）正常授予/回收权限回归通过
- [ ] `ValidateGrantablePermissions` 纯函数单测覆盖 synthetic 分支

---

## 4. 🔴 H2 end-user 角色时效性窗口

### 4.1 位置与根因

`internal/infra/auth/session_service.go:76-130`：access token 签发时把 `LoadUserRoles` 的结果固化进 JWT `rls` claim（:99）；`internal/infra/auth/validator.go:159` 直接信任 claims.Roles。用户被移出团队、降级、加/删 label、验证状态变化后，最长一个 accessTTL（默认 15min，可配置更大）内旧角色仍生效。

### 4.2 影响面

- 被移出团队的用户在窗口内仍持有 `team:<id>`/`team:<id>/owner` → 可继续删团队/管理成员；
- 账号禁用已有实时防护（`ensureUserCanAuthenticate` 每次验证查 users 文档），不受影响；
- 窗口大小 = 配置的 `security.jwt.access_ttl`。

### 4.3 修复方案（推荐 A，备选 B）

**方案 A（推荐）：验证时重算角色，不信任 JWT roles claim**

`internal/infra/auth/validator.go`：
1. `Validator` 注入 `domainauth.UserRoleResolver`（与 `session_service.go` 共用同一实现 `internal/app/client/user_roles.go`）；
2. `principalFromJWT` 的 end_user 分支（:151-160）与 `principalFromSession`（:190-198）改为：

```go
roles := []string{"users", fmt.Sprintf("user:%s", claims.UserID)}
if v.roleResolver != nil {
    resolved, err := v.roleResolver.LoadUserRoles(ctx, projectID, userID)
    if err != nil {
        // 角色解析失败按拒绝处理（fail-closed），避免旧角色残留
        return nil, status.Error(codes.Unauthenticated, "role resolution failed")
    }
    roles = resolved
}
```

> 代价（**独立审查修正，2026-08-07**）：每请求实际新增 **2 次**查询——`LoadUserRoles` 会再次读取 users 文档（`user_roles.go:23`，而 `ensureUserCanAuthenticate` 已读一次）+ 1 次 memberships 列表。优化：将 `ensureUserCanAuthenticate` 已取到的 user 文档传入角色解析复用（合并为 1 次 memberships 查询）；后续可加短 TTL（如 30s）角色缓存。
> 注意：`session_service.go:87` 签发时的 `LoadUserRoles` 调用可保留（作为初始值），验证端重算后角色始终最新。
> 依赖注入可行性（**独立审查核实**）：`UserRoleResolver` 端口在 `internal/domain/auth/session.go:29-32`，实现 `UserRoles` 在 `app/client`。infra/auth 仅依赖 domain 接口、具体实现由 Wire 在组合根注入——与 `NewSessionService` 既有模式完全相同（`internal/app/provides.go:14-15` 的 `wire.Bind(new(domainauth.UserRoleResolver), new(*client.UserRoles))` + `cmd/server/wire_gen.go:54-56`），`task wire-all` 重新生成即可，**不产生依赖环**。
> 测试影响：`NewValidator` 新增参数需同步 17 处调用点（`internal/infra/auth/validator_test.go` 11 处、`internal/testutil/grpc.go:33`、`internal/app/console/auth_test.go:155`、`internal/api/serverhttp/file_handler_integration_test.go` 4 处）。新参数置于 `docDB` 之后，测试可传 `nil`（代码已有 `if v.roleResolver != nil` 兜底）；**注意**：fail-closed 验收（"角色解析失败按拒绝处理"）仅在 resolver 非 nil 时生效，生产路径必须由 Wire 注入非 nil 实现。

**方案 B（备选，零查询）：角色版本号**

1. JWT 增加 `rv`（roles version）claim：签发时取 `memberships` 集合中该用户 accepted 记录的最大 `updated_at`（或 user 文档 `updated_at`）的 unix 值；
2. 验证时（`ensureUserCanAuthenticate` 已查 user 文档）比对 user 文档 `updated_at`；memberships 变化需额外一次轻量查询或依赖 session 文档的 `roles_version` 字段；
3. 不匹配 → 拒绝并要求刷新 token。

> 方案 B 实现复杂度高于 A（memberships 版本需要额外查询或联动更新），收益（省 1 次查询/请求）有限，**不推荐**。

### 4.4 验收标准

- [ ] 用户被移出团队后，**无需等待 access 过期**，后续请求立即失去 `team:<id>` 角色（删除团队/管理成员被拒）
- [ ] 用户 label/verified 变化即时生效
- [ ] 角色解析失败（DB 异常）时请求被拒而非放行
- [ ] 回归：正常登录 → 会话内文档权限一致；性能：每请求查询次数符合预期

## 5. 🟠 中危修复（H3, M1-M10）

### 5.1 H3 `validateEndUserSession` 不校验 session 归属

**位置**：`internal/infra/auth/validator.go:201-220`

**修复**：与 `EnsureActiveSession`（`session_service.go:140-142`）对齐，增加归属校验：

```go
if uid, _ := sessionDoc.Data["user_id"].(string); uid != claims.UserID {
    return status.Error(codes.Unauthenticated, "invalid session")
}
```

**验收**：session 文档 `user_id` 与 JWT `uid` 不一致时拒绝；正常路径回归。
> 独立审查补充：cookie 会话路径（`principalFromSession`，validator.go:164-199）从 session 文档读取 `user_id` 构建 principal，自洽但无外部比对——若 session 文档被 keys 角色篡改（C1/M2 场景），cookie 身份随之改变；该风险依赖 C1 第 1/2 层与 M2 收窄闭合，H3 本身仅覆盖 JWT 路径，建议在 §7 迁移说明中一并标注。

---

### 5.2 M1 `ACCESS_PERMISSION` 空 permissions fail-open

**位置**：`internal/infra/server/grpc.go:185-186`

**修复**：`collectMethodsByAccess` 对 `ACCESS_PERMISSION` 且 `len(perms)==0` 直接报错（与 :172 缺失注解一致 fail-closed）：

```go
case sharedv1.AccessLevel_ACCESS_PERMISSION:
    if len(perms) == 0 {
        return nil, nil, nil, fmt.Errorf("access_permission method %s/%s requires explicit permissions", service.FullName(), method.Name())
    }
    permissionMethods[fullMethod] = perms
```

同时给 `HasAnyPermission`（`internal/domain/shared/principal.go:69-79`）的空列表恒真行为加注释说明依赖关系（调用点已由 `len(perms)>0` 把关，行为保留）。

**验收**：新增无 permissions 的 ACCESS_PERMISSION 方法 → 启动失败；现有 proto 启动通过。

---

### 5.3 M2 `keys` 角色对系统敏感集合文档权限过大

**位置**：`internal/infra/auth/session_service.go:172-184`（sessions）、`internal/infra/documentdb/system_collection_specs.go:92-103`（identities）

**修复**（与 C1 §2.3 第 3 层合并执行）：
- sessions 集合 `_perms` 移除 `update:keys`/`delete:keys`，保留 `read:keys`；
- identities 集合移除 `update:keys`/`delete:keys`，保留 `read:keys`；
- users 集合的 keys 权限按 C1 §2.3 方案 (a) 落地后一并收窄为 `read:keys`。

> 影响面核验：全仓库搜索确认无 keys 角色写 sessions/identities 的合法调用（删除路径均走 SystemPrincipal）。
> ⚠️ 存量项目：系统集合 `_perms` 表是**文档级数据**，集合级 spec 只影响新建项目；存量文档级权限需迁移脚本（见 §7 迁移清单）或将"第 1/2 层修复"作为主防线（推荐：M2 作为纵深，不阻塞存量迁移）。

**验收**：API key 无法 update/delete 任意 session/identity 文档；UsersService/SignOut/DeleteSessionsByUser 回归。

---

### 5.4 M3 Session Cookie 用户缺失 team/label/verified 角色

**位置**：`internal/infra/auth/validator.go:190-198`（`principalFromSession`）

**修复**：与 H2 方案 A 相同——注入 `UserRoleResolver`，cookie 会话也调用 `LoadUserRoles` 补齐角色：

```go
roles := []string{"users", fmt.Sprintf("user:%s", userID)}
if v.roleResolver != nil {
    resolved, err := v.roleResolver.LoadUserRoles(ctx, projectID, userID)
    if err != nil { return nil, status.Error(codes.Unauthenticated, "role resolution failed") }
    roles = resolved
}
```

（与 H2 §4.3 复用同一逻辑，建议抽私有方法 `resolveEndUserRoles`。）

**验收**：OAuth 回调后经 cookie 访问团队文档不再 PermissionDenied；Bearer 流行为一致。

---

### 5.5 M4 HTTP 存储端点 scope 检查粗粒度且语义错位

**位置**：`internal/api/serverhttp/file_handler.go:241-244`

**修复**：按操作区分 scope：

```go
// upload 用 CreateFile scope；download 用 GetFile scope
if principal.CredentialType == shared.CredentialTypeAPIKey {
    check := interceptor.StorageServiceCreateFile
    if r.Method == http.MethodGet {
        check = interceptor.StorageServiceGetFile
    }
    if !interceptor.APIKeyScopeAllowed(check, principal.Permissions) {
        return nil, status.Error(codes.PermissionDenied, "api key missing required scope")
    }
}
```

`pkg/grpc/interceptor/apikey_scope.go:6` 增加常量：

```go
const StorageServiceGetFile = "/torchwood.server.v1.StorageService/GetFile"
```

**验收**：`storage.read` scope 的 key 可下载、不可上传；`storage.write` 反之；`storage`/`*` 均可。
> 独立审查修正：初版"影响面：`storage.read` 无法下载"表述有误——前缀匹配使 `storage.read` 对上传/下载**均放行**（`apikey_scope.go:25-29` 前缀规则），实际缺陷是**只读 scope 可越权上传**（权限过剩）。修复方向不变（下载改用 GetFile 后 `storage.read` 仍可下载、不可上传），影响面描述修正为上述语义。

---

### 5.6 M5 默认集合权限 `read:any` 匿名公开可读

**位置**：`internal/domain/databases/permissions.go:14-29`

**性质**：产品决策（Appwrite 兼容），与既有文档 P2-1 一致。建议三步落地（不破坏兼容）：

1. **Console 新建集合 UI 明示**：未指定权限时展示"默认对匿名公开可读"警告；
2. **server `CreateCollection` 增加可选开关**：`security.database.default_public_read`（默认 `false` 时，未显式提供权限的集合默认 `read:users` 而非 `read:any`）；
3. 文档明确建议生产项目显式收紧。

**验收**：配置开关生效；显式权限不受影响。

---

### 5.7 M6 匿名请求可探测项目存在性并触发系统集合 Ensure

**位置**：`internal/app/client/databases.go:70-91`（`resolveReadPrincipal` guest 分支）→ `loadProject`（:41-56）→ `EnsureSystemCollections`

**根因**：guest 读路径调用 `loadProject`，触发 `EnsureSystemCollections`（7 次 GetCollection DDL 检查）；且项目不存在/存在的错误码差异可探测。

**修复**：guest 分支改用轻量项目校验（不触发 EnsureSystemCollections；guest 无法访问系统集合，无需引导 schema）：

```go
// guest 分支：仅校验项目存在，不做系统集合引导
project, err := d.projectRepo.GetProject(ctx, projectID)
if err != nil { return "", databases.Principal{}, err }
if project == nil { return "", databases.Principal{}, status.Error(codes.NotFound, "project not found") }
```

> 影响面核验：guest 只读非系统集合，schema 在 CreateCollection 时已就绪；认证用户路径不变。
> 独立审查补充：正常生命周期内"项目存在 ⇒ schema 存在"不变式成立（`CreateProject` 在 DB 事务内 `EnsureSystemCollections`，`projects.go:52-60` + `clients/tx.go` 事务语义已核实），但存在可到达的破坏路径，需配套处理：
> 1. **`DeleteDatabase("default")`**（`app/server/databases.go:62-67` + `postgres.go:128-139` `DROP SCHEMA CASCADE`）：任何 databases scope 的 key 可执行，项目行保留而 schema 被删——建议纳入 C1 黑名单体系（禁止删除 "default" 数据库）；
> 2. `bootstrapCache.Store`（`postgres.go:661`）在事务内提前置位，事务回滚后缓存与真实状态不一致——建议移到事务提交后。

**验收**：匿名 ListDocuments 不再触发 DDL；项目不存在返回 NotFound（保留既有语义）。

---

### 5.8 M7 项目级 API 无权限过滤与配额

**位置**：`internal/app/server/projects.go:34-97`；`internal/api/servergrpc/projects.go`

**修复**（**独立审查修订，2026-08-07**；与产品"多项目=平台级资源"定位对齐）：

1. **ListProjects / CreateProject / GetProject 限定 actor**：
   - `CreateProject` 仅允许 `ActorKindAdmin` 且 `IsPlatformAdmin`；
   - `ListProjects`：平台 admin 返回全部；**非平台 admin 返回 `console_admin_projects` 中已授权项目**（复用 `validator.go:240-258` 的授权机制，`adminProjectRepo.HasProjectAccess` 按 UserID 过滤）——否则 console 的 viewer/member 角色管理员（测试已证明存在：`tests/acceptance/p0_acceptance_test.go:42`、`file_handler_integration_test.go:429`）会失去 ProjectSelector/Dashboard/项目管理页全部功能；
   - `GetProject`：非平台 admin 仅允许 `principal.ProjectID`（API key 绑定其所属项目；admin 走 `X-Torchwood-Project` + `ValidateAdminProjectAccess`）；
2. **CreateProject 项目 ID 白名单**：`^[a-z0-9-]{1,64}$`（与既有 P2-9 合并）；
3. **（可选）配额**：`projects` 表 `name` unique 已限制同名，建议记录 `created_by` 并按 admin 数量限制。

> 影响面核验：
> - console 前端确认调用全部三个方法并携带 `X-Torchwood-Project`（`console/src/api/projects.ts` + `console/src/api/client.ts:59-65`）；owner/admin 平台管理员不受影响；
> - **SDK 破坏性变更**：`sdk/typescript/src/server/projects.ts:7-24` 的 `list()/get()/create()` 以 API key 调用——M7 落地后对 API key 全部失效。SDK 三个方法需标注"平台 admin 专用"并提供 admin token 鉴权示例，或改走 console admin 会话路径；发布前与 SDK 版本一起规划。

**验收**：非平台 admin 的 key 无法 Create/List 全量项目；GetProject 越权返回 NotFound；项目 ID 非法字符被拒。

---

### 5.9 M8 OAuth email 验证依赖 provider 自律

**位置**：`internal/domain/auth/oauth.go:29-37`（`OAuthUserInfo` 无 EmailVerified）；`internal/app/client/identity.go:85-145`（`resolveOAuthUser` 无条件信任 email）

**修复**（**独立审查修订，2026-08-07**）：
1. `OAuthUserInfo` 增加 `EmailVerified bool`；
2. `githubOAuthAuthenticator`（`oauth_provider.go:179-213`）填充 `EmailVerified = verified`（当前已强制 verified，直接赋值即可）；
3. **Google adapter（`oauth_provider.go:77-112`）从已保留的 `raw["email_verified"]` 取值填充**（Google `/oauth2/v2/userinfo` 自带该字段，零额外请求）——**adapter 填充与 `resolveOAuthUser` 的强制逻辑必须同一提交原子落地**，否则 `EmailVerified` 默认 false 会导致所有带 email 的 Google 登录被拒；
4. 微信系 `Email` 恒空（`oauth_wechat.go` 不取 email，`identity.go:86-88` 分流）不受影响；
5. `resolveOAuthUser`（identity.go:128）在 `info.Email != "" && !info.EmailVerified` 时拒绝（`codes.FailedPrecondition`，提示走邮箱验证/链接流程）。

> 行为边界：Google Workspace 域下存在 `email_verified=false` 的边缘账户（管理员未验证/域未验证），强制校验会拒绝这类登录——属预期收紧，需在文档/配置中说明。

**验收**：模拟未验证 email 的 provider profile 被拒；GitHub/Google 正常路径回归；adapter 级单测覆盖"Google 返回 email_verified=false"与"adapter 未填充时强制逻辑拒绝"两场景。

---

### 5.10 M9 OAuth 失败错误信息泄入重定向 URL

**位置**：`internal/app/client/oauth2.go:212`

**修复**：错误信息脱敏，仅输出固定类别：

```go
return &OAuth2CallbackResult{
    SuccessURL:  successURL,
    FailureURL:  failureURL,
    RedirectURL: appendQuery(failureURL, "error", "oauth_failed"),
}, err
```

（完整错误保留在服务端日志——`HandleOAuth2Callback` 调用方已有 slog 记录。）

**验收**：回调失败时 URL 不再携带内部错误细节；成功路径回归。

---

### 5.11 M10 `DeleteUser` 残留孤儿数据

**位置**：`internal/app/server/users.go:100-105`

**修复**：`DeleteUser` 在删除 users 文档前，用 `SystemPrincipal` 级联清理：
- 该用户的 sessions（复用 `auth.SessionService.DeleteSessionsByUser` 或按 `user_id` 直删）；
- 该用户的 identities（`ListDocuments(identities, user_id=X)` → 逐条删除）；
- 该用户的 memberships（`ListDocuments(memberships, user_id=X)` → 逐条删除，并同步 `adjustTeamTotal`——可复用 `server.Teams` 的删除逻辑，注意按团队聚合后递减 total）。

> 影响面核验：identity 残留会阻塞同 provider 的重新注册（`findIdentity` → "identity references missing user"）；memberships 残留会让 `loadTeamRoles` 返回孤儿团队角色。

**验收**：删除用户后 sessions/identities/memberships 无残留；团队 total 计数正确；同邮箱重新注册正常。

## 6. 🟡 低危项（L1-L9）

| # | 问题 | 位置 | 修复要点 |
|---|------|------|----------|
| L1 | Session `secret_hash` 死代码 | `internal/infra/auth/session_cookie.go:30-52` | 设计残留：cookie 只验 HMAC(projectID:sessionID)，文档 `secret_hash` 从未校验。二选一：删除该字段，或实现双层验证（cookie 值携带 secret，验证时比对文档哈希，提升 cookie 泄露后的对抗能力） |
| L2 | `DeleteFile` 不校验 bucket 归属 | `internal/app/storage/storage.go:218-227` | 与 `GetFile`（:208-210）对齐：先 `GetDocument` 校验 `file.BucketID == bucketID`，防止错误 bucketID 删除导致 S3 孤儿对象/元数据残留 |
| L3 | Bulk 更新/删除非原子 | `internal/infra/documentdb/postgres_permissions.go:137-179` | 逐文档循环无回滚。改进：单条 SQL 或事务包裹；至少失败时返回已处理数量并文档化部分成功语义 |
| L4 | 文档权限信息泄露 | `internal/infra/documentdb/postgres_permissions.go:114-124` | `attachDocumentPermissions` 把完整 `_perms`（含全部授权者 `user:<id>`）附在响应。改进：仅对持有文档 update 权限的调用者附加，或响应中移除权限明细 |
| L5 | `IsAPIKeysServiceMethod` 子串匹配 | `pkg/grpc/interceptor/apikey_scope.go:69-75` | 改为精确服务名比较（与 `apiKeyScopeResource` 一致），避免未来服务名误伤 |
| L6 | admin 无软禁用 | `internal/infra/bun/model/project.go:36-45` | `console_admins` 表增加 `enabled` 字段；`GetConsoleAdmin`/token 验证时检查，支持禁用而不删行 |
| L7 | admin 调 server API 缺 header 时 ProjectID="console" | `pkg/grpc/interceptor/jwt.go:191-192` | cookie 特判 `projectID="console"` 误导报错。改进：admin 分支在无 `X-Torchwood-Project` 时返回明确的 `InvalidArgument`（"admin must specify X-Torchwood-Project for server API"）而非进入 resolveInternalID 报 project not found |
| L8 | session `expire_at` 缺失时 fail-open | `internal/infra/auth/validator.go:143,209` | 字段缺失时按已过期处理（fail-closed），与现有注释意图一致 |
| L9 | 显式权限解析静默丢弃非法格式 | `internal/app/storage/storage.go:322-331`、`internal/app/server/teams.go:513-520` | `parseRawPermissions`/`splitPermission` 对非法条目改为返回错误（`InvalidArgument`），避免配置被静默忽略 |

---

## 7. 迁移与配置变更清单

| 项 | 说明 | 影响对象 |
|----|------|----------|
| 系统集合文档级 `_perms` 收窄（C1 第 3 层 / M2） | 存量项目 `_perms` 表：`DELETE FROM _perms WHERE _collection='sessions' AND _type IN ('update','delete') AND _permission='keys'`（identities 同理）；**集合级**权限（`system_collection_specs.go`）随 `EnsureSystemCollections` 幂等跳过，存量项目需 UPDATE `document_collections.permissions` 元数据行 | 各项目 schema 下的 `_perms` 表 + `document_collections` 元数据表（按 tenant 遍历） |
| `DeleteDatabase("default")` 禁止（M6 配套） | `app/server/databases.go:62-67` 对 `databaseID=="default"` 拒绝（纳入 C1 黑名单体系），否则破坏"项目存在 ⇒ schema 存在"不变式 | Server Databases API |
| `bootstrapCache` 置位时机（M6 配套） | `postgres.go:661` 事务内提前置位，回滚后缓存失真——移至事务提交后 | documentdb 实现 |
| users 集合 keys 权限（如按 C1 方案 (a) 落地） | 同上模式收窄为 `read:keys` | 同上 |
| `security.jwt.access_ttl` 上限 | 建议增加配置校验：`access_ttl` 超过 2h 启动警告或拒绝（配合 H2 方案 A 前的最低成本缓解） | 配置层 |
| M7 项目权限收紧 | SDK/文档同步：`ListProjects`/`CreateProject` 仅平台 admin | `sdk/`、`docs/` |
| M5 开关 | 新增 `security.database.default_public_read`（默认 false） | 配置 schema + Console UI |

---

## 8. 修复顺序建议

1. **C1**（账户接管，一票否决级）——第 1 层（server 黑名单）+ 第 2 层（docDB update 写保护 + UsersService 改 SystemPrincipal），第 3 层随迁移执行；
2. **H1**（授予约束）——纯函数改动，与 C1 无耦合，可并行；
3. **H2 + M3**（角色时效 + cookie 角色补齐）——共享 `UserRoleResolver` 注入改动，建议合并实施；
4. **H3**（一行改动）随 H2 一并提交；
5. **M1、M7、M4**（拦截器/项目/存储 scope）——各自独立，按需插空；
6. **M6、M10、M9、M8**（guest 探测、孤儿数据、OAuth 细节）；
7. **M2/M5/§7 迁移与配置**（随 C1 第 3 层与配置发布节奏）；
8. **L1-L9** 与既有 security-review.md 的 P2 项合并排期。

> 与既有文档合并排序建议：本方案 C1 → 既有 P0-1/P0-2/P0-3 → 本方案 H1/H2 → 既有 P1 各项 → 本方案 M 系列 → 低危项。

---

## 9. 全局回归验证清单（实施完成后）

- [ ] `go build ./...` + `task wire-all` + `task generate-all`（如改动 proto/依赖注入）
- [ ] 拦截器单测：`pkg/grpc/interceptor`（scope 匹配、APIKeys 自管禁止、permission 检查）
- [ ] 认证单测：`internal/infra/auth`（validator 角色解析、session 归属、cookie 会话）
- [ ] 文档层单测：`internal/infra/documentdb`（系统集合写保护、perms 过滤、synthetic 授予）
- [ ] 用例层回归：SignUp/SignIn/OTP/OAuth/团队全流程/存储上传下载
- [ ] 集成测试：`internal/testutil` 覆盖存量项目迁移后行为
- [ ] Console 端到端：admin 登录、项目管理、用户管理、团队管理

---

## 10. 独立审查记录（2026-08-07）

> 对本文档 §1-§8 的第二轮独立审查（两个独立探索代理交叉验证全部攻击链与修复副作用），结论已回写进对应章节。本节记录审查结论摘要。

### 10.1 审查结论摘要

| 项目 | 结论 | 修正内容 |
|------|------|----------|
| C1 攻击链（scope 匹配 / 权限链 / 集合级 update:keys） | ✅ 成立 | 补充：scope 匹配仅到 service 粒度（read/write 不区分）；集合级权限独立使 collOK=true |
| C1 第 3 层（只收窄文档级 `_perms`） | ❌ 无效，需修正 | `AllowsDocumentAccess` 为 `collOK \|\| docOK`——必须同时收窄**集合级**权限；users 集合收窄以方案 (a) 为前提 |
| C1 第 2 层（非 System 禁止更新系统集合） | ⚠️ 会误伤，需修正 | 漏报 `Account.UpdateAccount`/`UpdatePrefs`（end-user 自助改密码/改 prefs 会被 403）；`UpdateUserStatus` 实为死代码；第 2 层增加 owner（`user:<id>` 匹配）例外 |
| H1 修复副作用 | ✅ 成立 | 不破坏 storage/teams（本就不经校验）；修复超出验收（owner 也被禁 `update:any`，产品语义变更需明示）；storage/teams 显式权限路径不覆盖（低优先级边界） |
| H2 方案 A | ✅ 成立 | 成本修正：每请求新增 2 次查询（可复用 user 文档优化）；依赖注入论证改为"infra 只依赖 domain 接口，Wire 注入"；测试 17 处调用点需补参（可传 nil，生产必须非 nil） |
| H3 | ✅ 成立 | 补充 cookie 会话路径依赖 M2/C1 闭合的说明 |
| M1 | ✅ 成立 | 现有 11 处 ACCESS_PERMISSION 全部非空，修复无回归 |
| M4 | ⚠️ 影响面表述修正 | 实际缺陷是"只读 scope 可越权上传"，非"无法下载"；修复方向不变 |
| M6 | ✅ 有条件成立 | 补 `DeleteDatabase("default")` 破坏不变式 + `bootstrapCache` 事务回滚风险两个配套修复 |
| M7 | ⚠️ 影响面漏报 | console viewer/member 角色 admin 会被锁死（ListProjects 改为按授权过滤）；SDK API key 建项目路径确认将被破坏（标注破坏性变更） |
| M8 | ✅ 有条件成立 | Google adapter 从 `raw["email_verified"]` 填充（零额外请求）；adapter 填充与强制逻辑必须原子落地；补 adapter 级单测 |

### 10.2 审查未发现问题的项目

- H3 修复代码与 `EnsureActiveSession` 既有写法一致，无副作用；
- M1 启动期报错不破坏现有任何 proto；
- H2 方案 A 无并发写一致性问题（签发/验证均为纯读，Postgres 强一致）；
- L1-L9 低危项的行号与描述均已核对无误。

### 10.3 后续实施提醒

- C1 是**三层联动修复**：第 1 层（server 黑名单）独立生效；第 2 层（owner 例外写保护）必须与方案 (a)（UsersService 改 SystemPrincipal）同提交；第 3 层（集合级+文档级 keys 权限收窄）依赖前两层，且需存量迁移（§7）；
- M8 的 adapter 填充与强制校验必须原子落地；
- M7 属行为破坏性变更，需与 SDK 版本同步规划。

---

*本报告为研究结论，不包含任何代码改动。各修复项的验收标准均为可测试断言，实施时建议以单测先行。*


