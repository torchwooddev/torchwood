# 租户与身份

> 切片：tenancy & identity。以代码为准，不引用 roadmap / 设计锁定 / Appendix C。
> 产品标尺：给团队做 SaaS 用的 BaaS（Firebase Auth + 项目隔离 + admin console），不是 Agent-native。

## 现状（从代码归纳）

**租户是 Project，而且只有 Project。** `public.projects` 一行对应一个租户；端用户、会话、身份、组都落在 `tw_<project.id>` 的静态表里（`users` / `sessions` / `identities` / `groups` / `memberships`），仓储经 `Scoped` 打开 schema，空 `projectID` 直接失败（`internal/infra/bun/bunrepo/project_table.go:53-59`）。平台资源（`admins`、`api_keys`、`admin_projects`）在 `public`。没有 org、workspace、environment 表或 ID。

**身份种类（代码里实际出现的）：**

| 种类 | 如何进入 | 租户怎么绑 |
|---|---|---|
| `end_user` | JWT（`PurposeEndUserJWT`）或 HMAC session cookie | claims / cookie payload 里的 `projectID` |
| `admin` | JWT（`PurposeAdminJWT`）经 Bearer 或 `TORCHWOOD_session_console` | JWT **不含** `ProjectID`；拦截器把 `X-Torchwood-Project` 写进 Principal |
| `service` | `X-Api-Key` / `Authorization: ApiKey` | `api_keys.project_id`，header 改不了 |
| `system` | 进程内 `NewSystemPrincipal` | 调用方传入的 `projectID` |
| 未认证 / guest | Principal 为 nil；文档 ACL 投影为 `GuestPrincipal{Roles:["guests"]}` | 客户端申报的 `project_id` |

`ActorKind` 是四个字符串常量（`internal/domain/shared/principal.go:10-15`），`agent` 被测试显式判非法（`principal_test.go:10-14`）。Guest **不是** ActorKind。

**Principal 是字段袋，不是封闭 ADT。** 同一结构同时装 `UserID` / `AdminID` / `APIKeyID` / `Roles` / `Permissions` / `IsPlatformAdmin`（`principal.go:41-54`）。认证靠 switch 看哪个字段非空（`IsAuthenticated`，`:69-85`）。注释和测试反复强调「禁止把 admin id 塞进 UserID」（`:48`、`principal_test.go:22`）——这是字段袋的气味，不是类型系统在守门。JWT claims 更彻底：admin 的 id 就写在 `Claims.UserID` 上（`internal/app/console/auth.go:254-257`），validator 再用 `claims.UserID` 去查 `admins`（`internal/infra/auth/validator.go:152`）。

文档 ACL 是第二套更浅的 Principal：`databases.Principal{Roles, PlatformAdmin}`（`internal/domain/databases/access.go:7-16`）。`DocPrincipal()` 丢掉 `RoleConsole`，给 admin 补 `user:<AdminID>`，System 变成 `__system__`（`principal.go:152-182`）。`PlatformAdmin` 与 `__system__` 都 `BypassesDocumentACL()`（`access.go:44-48`）——console 超管和内部履约在文档层是同一条旁路。

### 三条真实路径

**1. 端用户 SignUp → session → 写文档**

- `AccountService.SignUp` 为 `ACCESS_PUBLIC`（`proto/client/v1/account.proto:62-64`）。请求体带 `project_id`。
- `Account.SignUp` 查 `projects`，`users.Register` 造聚合，插入该项目 schema（`internal/app/client/account.go:184-240`）。
- `finishSignInWithProvider` 调 `SessionService.CreateSessionAndTokens`：在项目 schema 写 `sessions` 行，用 `PurposeEndUserJWT` 签 access/refresh（claims 含 `akd=end_user`、`pid`、`sid`、`uid`），再用 HMAC 签 opaque cookie `base64(projectID:sessionID):sig`（`internal/infra/auth/session_service.go:49-139`，`session_cookie.go:22-27`）。
- gRPC handler **丢掉 cookie**（`internal/api/clientgrpc/account.go:30-35`，第三返回值 `_`）。浏览器主路径拿到的是响应体里的 JWT，不是 cookie。HMAC cookie 只在 OAuth HTTP 回调里 `SetCookie("TORCHWOOD_session_<project>", ...)`（`internal/api/serverhttp/oauth_handler.go:64-72`）。
- 之后 `CreateDocument`：服务默认 `ACCESS_AUTHENTICATED`（`proto/client/v1/databases.proto:60-61`）。`collectMethodsByAccess` 把这类方法编成 `permissionMethods[method]=["users"]`（`internal/infra/server/grpc.go:232-236`）。拦截器要求 Principal 带角色 `users`，并 **拒绝 API key**（`pkg/grpc/interceptor/jwt.go:144-155`）。
- Validator 验 JWT → 查 session 行仍有效 → 实时 `LoadUserRoles`（`users` + `user:<id>` + labels + `group:<id>`）（`validator.go:180-204`，`internal/app/client/user_roles.go:21-46`）。
- `client.Databases.resolveProject` 只用 `p.ProjectID`，不接受请求里另写一个项目（`internal/app/client/databases.go:60-70,131-145`）。文档 ACE 默认 `user:<OwnerID>`。

**2. Console admin 登录 → 管项目**

- `ConsoleAuthService` 整服务 `ACCESS_PUBLIC`（`proto/console/v1/auth.proto:52-53`）。`Auth.SignIn` 查 `public.admins`，签发 **无 `ProjectID`** 的 admin JWT（`internal/app/console/auth.go:51-72,247-262`）。
- 传输层把 access JWT 放进 `TORCHWOOD_session_console`（Path=/），refresh 放进 `TORCHWOOD_console_refresh`（Path=`/v1/console/auth`）（`internal/api/consolegrpc/cookies.go:23-39`）。Cookie 运输中的值被 `ParseAuthnRequest` 标成 `CredentialTypeToken`，不是 Session（`internal/domain/shared/authn.go:24,62-64`）。
- Console 前端把当前项目写在 `localStorage.TORCHWOOD_console_project`，每个请求加 `X-Torchwood-Project`（`console/src/api/client.ts:50-66`）。
- 拦截器对 `ActorKindAdmin`：**原地改** `principal.ProjectID = header`，再 `ValidateAdminProjectAccess`（`jwt.go:128-141`）。`owner`/`admin` 的 `IsPlatformAdmin=true`，直接放行任意项目（`validator.go:297-305`）。`member`/`viewer` 查 `admin_projects` 是否存在行（`:307-318`）。
- 随后调 Server Users/Databases：这些 RPC 是 `ACCESS_API_KEY`，拦截器允许「API key **或** admin 会话」（`jwt.go:110-114`）。写方法再套 `adminRoleMethodRules`（viewer 禁写）。

**3. API Key 调 Server Users / Databases**

- Key 在 `public.api_keys`，`project_id` FK 到 `projects`（`db/migrations/000001_init_tables.up.sql:13-21`）。创建只允许平台 admin（`internal/app/server/apikeys.go:40-45`）。
- `validateAPIKey` 按 sha256 全局查找，Principal 的 `ProjectID` 来自 key 行（`validator.go:119-140`）。拦截器 **不会** 用 `X-Torchwood-Project` 覆盖 service 主体（那段只对 admin）。
- `UsersService.projectID` / `DatabasesService.projectID` 都只读 `principal.ProjectID`（`internal/api/servergrpc/users.go:29-35`，`databases.go:27-33`）。请求体没有项目字段可覆写。
- API key 不能调 APIKeys 服务（`jwt.go:116-120`），也不能调 `permissionMethods`（含所有 Client `ACCESS_AUTHENTICATED`）。OpenAPI 文案却写「Server API key（需同时携带 X-Torchwood-Project）」——从 `account.proto` 复制到几乎每个 swagger 块，与运行时不符。

**Client / Console / Server 是三个 proto 门、一个身份模型、一个拦截器。** `NewGRPCServer` 把三套 file descriptor 喂进同一个 `collectMethodsByAccess` + 同一个 `AuthInterceptor`（`internal/infra/server/grpc.go:64-86,98-123`）。门的差异是注解（`ACCESS_PUBLIC` / `AUTHENTICATED` / `PERMISSION` / `API_KEY`），不是三套 Principal。HTTP upload 与 Realtime 再各写一份 Grant（Realtime 禁 API key 与 end-user session cookie：`internal/api/realtime/handler.go:333-403`）。

---

## 设计问题

### P-1 Principal 是字段袋，身份接口没有封闭

- **证据：** `shared.Principal` 用可选字段区分种类（`principal.go:41-54`）；`ActorKind` 是字符串，不是 sum type。`IsAuthenticated` / `OwnerID` / `DocPrincipal` 各自 switch 一遍。JWT `Claims.UserID` 同时承载 end-user 与 admin（`pkg/jwtparser/jwt.go:23-36`，`console/auth.go:256`）。`Roles` 注释写「文档 ACL / console RBAC，不是 API scope」，`Permissions` 才是 scope（`principal.go:51-52`）——同一对象上两列字符串数组，调用方必须记住用哪一列。
- **为何对「SaaS 的 BaaS」是问题：** 身份是整条调用链的杠杆模块。字段袋让「admin 误入 UserID」「service 没有 OwnerID」「guest 不是一种 Actor」全靠注释与回归测试。每加一种主体（团队成员、环境机器人、模拟登录）都是再加字段，而不是编译期穷尽。删除测试：删掉 `Principal` 几乎所有 app/interceptor 都编译失败，但删不掉任何不变量——不变量本来就不在这个类型里。
- **更优接口草图：** 封闭 ADT（`EndUser{Project,User,Session}` | `Admin{Admin,Role}` | `Service{Project,Key,Scopes}` | `System{Project}` | `Guest{Project?}`），认证函数返回 ADT；租户绑定、ACL 投影、OwnerID 都是该 ADT 上的方法，而不是共享字段。JWT 不要用 `uid` 装 admin。

### P-2 三扇门共用一个身份，ACCESS_AUTHENTICATED 塌成角色 `"users"`

- **证据：** 一个拦截器服务三套 API。`ACCESS_AUTHENTICATED` 没有独立语义，被编译成「必须有角色 `users`」（`grpc.go:232-236`）。于是 Client 写文档看起来像「已登录即可」，实际是「必须是被 `LoadUserRoles` 打上 `users` 的端用户」。Admin 角色是 `owner`/`admin`/`member`/`viewer` + `console`（`validator.go:166`），API key 角色是 `"keys"`（`validator.go:138`）——他们过不了 Client 认证门，但 **可以** 走 Server 门写同一份文档，且平台 admin `BypassesDocumentACL`。
- **为何是问题：** 给 SaaS 团队的心智是「Client = 我的终端用户，Server = 我的后端，Console = 我的同事」。代码里这是同一 Principal 的三扇门 + 一串角色字符串，而不是三个产品边界。缝在注解表，不在模块。
- **更优缝：** 三个门各有显式 Grant（`EndUserOnly` / `AdminSession` / `ProjectKey`），认证成功后的主体类型就是门的前置条件；不要用 `"users"` 这个 ACL 角色兼做「已登录端用户」的门禁。

### P-3 四套授权词汇共用 owner/admin/member

- **证据：**
  - 平台 admin：`admins.role ∈ {owner,admin,member,viewer}`，**全局一行**（`internal/domain/projects/project.go:28-35`，`db/migrations/000001_init_tables.up.sql:26-33`）。`IsPlatformAdmin = role==owner || role==admin`（`validator.go:163`）。
  - 项目内 Groups：`owner/admin/member`（`internal/domain/groups/membership.go:13-16`），邀请邮箱、pending/accepted（`client/groups.go:73-96`）。这是 **应用内** 社交图，不是平台团队。
  - API key：`*` / `all` / `databases.write` …（`pkg/grpc/interceptor/apikey_scope.go:152-166`）。
  - 文档 ACL：`users`、`user:<id>`、`group:<id>`、`guests`、`keys`、`label:`、`user:<id>/verified`（`user_roles.go:21-46`，`access.go:18-19`）。
  - Console RPC 的 `method_auth.permissions` 直接把 admin role 当权限串（`proto/console/v1/admins.proto:60-84`）。Client Account 用 `permissions: ["users"]`（`account.proto:88`）。
- **为何是问题：** 做 SaaS 的团队会问「我怎么邀请同事进这个项目、只读生产数据」。代码里「邀请」只存在于 Groups（终端用户之间）；「同事」是全局 `admins` 表；「只读」是 viewer + 空的 `admin_projects`。同一组词在四层出现，无法做删除测试——删 Groups 不会影响 Console，删 `admins.role` 不会影响文档 ACE，但人读起来像同一件事。
- **更优接口：** 平台 IAM（谁能打开 Console、对哪个 Project 什么角色）与应用 ACL（谁能读哪篇文档）分成两个模块；禁止共享 `owner/admin/member` 字符串。API scope 留在 key 上，不要投影成文档角色 `"keys"` 除非产品真要「key 作为文档主体」。

### P-4 会话不是一个模块，是三套栈

- **证据：**
  1. **端用户：** 项目 schema 里的 `sessions` 行 + access/refresh JWT + Redis refresh rotation（key=`Torchwood:refresh:<project>:<session>`）+ 可选 HMAC cookie。每次带 JWT 都 `GetByID` 会话（`validator.go:180-183,244-261`）。登出删行。Refresh 复用则删会话（`account.go:385-388`）。
  2. **Admin：** **没有** session 表。JWT + Redis `RevokeBefore(adminID, t)`（签发时间戳比较，`validator.go:321-332`）+ rotation key=`Torchwood:refresh:admin:<adminID>`（`console/auth.go:103`）。Cookie 里装的是 access JWT 本身。
  3. **API key：** 哈希密钥，无 session、无 rotation、无吊销时间轴，删行即失效。
- 密钥派生倒是分开的：`end-user-jwt` / `admin-jwt` / `session-cookie`（`pkg/jwtparser/keys.go:10-14`）。Realtime 只收 JWT（SDK access 或 console cookie），拒 HMAC session cookie（`realtime/handler.go:401-403`）。gRPC SignUp/SignIn 又不下发 cookie（见现状 §1）。
- **为何是问题：** 「会话」本应是身份模块的深度核心（创建、轮换、吊销、设备、绑定项目）。现在深度散在三个 adapter 里，locality 差：改 refresh 要碰 Account、console Auth、两套 Redis、两个 JWT purpose。SaaS 团队要的「踢用户下线 / 吊销同事 / 轮换 key」是三个按钮三套语义。
- **更优模块：** 一个 `Credential` 端口：`Issue` / `Rotate` / `Revoke` / `Lookup`，三种主体都是实现；cookie 只是运输，不该改变凭证类型（console cookie 今天被标成 Token，end-user cookie 是 Session）。

### P-5 平台身份是全局超管，租户边界只对端用户硬

- **证据：** `admins` 无 `project_id`。`CreateProject` / `DeleteProject` 要求 `ActorKindAdmin && IsPlatformAdmin`（`internal/app/server/projects.go:44-53,113-120`）。平台 admin 看 `ListProjects` 是全表；非平台 admin **恒返回空列表**，因为 `AdminProjectRepository` 没有 List（`projects.go:207-213`，`internal/domain/projects/admin_project.go:4-7`）。`GetProject` 对非平台 admin 只放行 `principal.ProjectID == id`（`projects.go:238-244`）。
- **为何是问题：** Firebase / 同类 BaaS 的「团队」是：Google 账号（或 org）→ 被邀请进 Project → 项目内 IAM。这里的 Console 账号是 **整台服务器的管理员表**。第二个项目不是「同一团队的另一个租户」，而是超管再 `CREATE SCHEMA`。对「卖给团队做 SaaS」来说，这是把多租户控制面做成了单机管理后台。
- **更优缝：** `Org`（计费/所有权）→ `Project`（数据面隔离，现有 schema 可保留）→ `OrgMember`（邀请、角色、按项目授权）。`admins` 全局表降为「实例 bootstrap owner」，不再是日常身份。

### P-6 `admin_projects` 是死缝：有表、有校验、没有产品接口

- **证据：** 表是 `(admin_id, project_id)` 布尔授权（`db/migrations/000002_audit_and_admin_projects.up.sql:16-21`）。端口只有 `HasProjectAccess` / `GrantProjectAccess`（`admin_project.go:4-7`）。`Grant` 的生产调用点只有 bootstrap `Setup.SignUp`（`internal/app/console/setup.go:185-187`）。`CreateAdmin` 只插 `admins` 行，不授权任何项目（`internal/app/console/admins.go:74-113`）。Console AdminsService 也没有 Grant/Revoke/ListProjectsForAdmin RPC（`proto/console/v1/admins.proto:61-102`）。`member`/`viewer` 若无行，header 指到任何项目都是 PermissionDenied；而 `owner`/`admin` 根本不查这张表。
- **为何是问题：** 这是「看起来有项目级同事授权、实际不可用」的浅模块。删除测试：删掉 `admin_projects`，平台超管路径零变化；受限角色则永远进不了项目——说明它不是完整 IAM，只是拦截器上的补丁。SaaS 团队无法邀请同事、无法列出「我的项目」、无法把角色绑到项目（viewer 是全局的，不是「此项目只读」）。
- **更优接口：** `Grant(admin, project, role)` 成为唯一事实来源；`IsPlatformAdmin` 不再跨项目旁路；List 走授权表而不是「超管全量 / 其他人空数组」。

### P-7 租户绑定不对称：凭证内 vs 可变 header；公开读靠客户端申报

- **证据：**
  - 端用户：JWT `pid` + session 行在该 schema。`RefreshToken` 若 body `project_id` 与 claims 不一致则拒（`account.go:355-358`）。写文档不用请求里的 project。
  - API key：绑定在行上；header 不能换项目。这是三条里最硬的。
  - Admin：JWT 无项目；`jwt.go:135-137` 与 `serverhttp/auth.go:44-47` **mutate** `principal.ProjectID`。Realtime 同样 `principal.ProjectID = hello.ProjectID`（`realtime/handler.go:384-387`）。空 ProjectID 时 `ValidateAdminProjectAccess` **直接成功**（`validator.go:301-303`）——所以必须「先写 header 再校验」，顺序是注释里的不变量，不是类型。
  - 公开读：`ListDocuments` 等为 `ACCESS_PUBLIC`（`databases.proto:69-77`）。`resolveProjectID` **优先请求字段 / header**，再回落到 Principal（`internal/api/clientgrpc/databases.go:175-188`）。未认证则 `GuestPrincipal`（`client/databases.go:72-93`）。租户 ID 来自调用方。
- **跨项目？** 端用户 JWT 与 API key 不能换项目（session lookup / key 行）。平台 admin 的 JWT + 任意 `X-Torchwood-Project` = 任意租户。这不是实现漏洞，是模型：控制面身份没有租户。
- **为何是问题：** 对 SaaS builder，泄漏一个 console cookie（无项目绑定）加上猜得到的 project id，就是超管进别人的数据面（若角色是 owner/admin）。端用户侧隔离是深的；控制面侧隔离是浅的。公开读把租户当成 query parameter，ACL 只能靠集合权限配 `guests`——这倒像 Firebase，但和「admin header 可变租户」叠在同一套 Principal 上，本地推理成本高。
- **更优缝：** Admin 会话签发时绑定「当前可访问项目集合」，或每次切项目签发短 token（项目在凭证内，与 end-user 对称）。公开读单独 `Guest{ProjectID}` 认证函数，不要复用「可选地 Authenticate 任意凭证」。

### P-8 E-5 之后 User 仍是贫血记录，Account 是上帝对象

- **证据：** `users.User` 有 `Register` / `CanAuthenticate` / `IsAnonymous`，但变更是 `map[string]any` 列白名单（`internal/domain/users/update.go:8-53`），仓储 `Update(..., cols map[string]any)`（`repository.go:28-29`）。MFA 因子是 `User.Factors json.RawMessage`（`user.go:38`），不是子实体。`DocumentData()` 仍把用户摊成 `databases.Document.Data`（含 `password_hash`）（`user.go:163-198`），Server Users 的 List/Get/Create 经 `userAsDocument` 回到文档形状（`internal/app/server/users.go:396-401`）。
- `Account` 持有 20 个依赖（`account.go:29-51`），方法散落 signup/oauth/mfa/otp/magic/wechat/recovery/verification/jwt/anonymous/logs……删除 `Account` 等于拆掉整个端用户身份，但它不是聚合根，只是过程袋子。Session、Identity、Factor 的不变量（邮箱 staging、改密撤会话、OAuth 链接）都在这个袋子里，不在 `User` 上。
- **为何是问题：** 物理表已经离开文档层（无 `_id`/`_perms`/`_version`，`000008_system_tables.up.sql:1-4`），领域模型还停在「记录 + 用例脚本」。SaaS 要的用户生命周期（封禁、匿名升级、主身份、会话吊销）没有一个深聚合可测；每个新登录方式就在 Account 上加方法。杠杆极高、局部性极差。
- **更优模块：** `User` 聚合：`Register` / `Block` / `VerifyEmail` / `ChangePassword` / `AttachIdentity` / `EnableFactor`，会话作为子实体或独立 `Session` 聚合由 User 命令驱动。`Account` 降为对这个聚合的应用服务，按登录方式拆 adapter，而不是一个类型七十个方法。Server Users 返回 User DTO，删掉 `DocumentData` 这条文档时代的缝。

### P-9 用例层比拦截器宽：同一写路径两条门

- **证据：** `clientActorOK` 允许 `EndUser | Admin | Service`（`client/databases.go:48-58`）。拦截器对 `ACCESS_AUTHENTICATED` 则要求角色 `users` 且禁 API key。直调 use-case（测试、内部、漏网）时，admin/service 可以按 Client 路径写文档；走 gRPC 则不能。`RequireServerWriteActor` 又允许 admin/service 走 Server 路径（`internal/app/shared/authz.go:40-55`）。
- Groups 更拧：Client Groups 要求 `p.UserID != ""`（`client/groups.go:25-30`），admin 没有 UserID，走 Client 组接口会 401；Server Groups 却是同一 `*server.Groups`，admin/key 可写。Client 只是薄包装。
- **为何是问题：** 身份门禁的深度在拦截器表，不在领域。删除拦截器，Client 文档写入会对三种 Actor 开放。这不是「纵深防御」，是不变量放错层——Firebase 规则在数据层，这里规则在 RPC 注解。
- **更优缝：** use-case 入口收 ADT（`func CreateDocument(ctx, EndUser, ...)`），编译期禁止 admin 走 Client 写；Server 写走另一函数并显式 `ACLBypass`。不要用「ctx 里有 Principal 再 switch」。

### P-10 `CreateUserToken` 是模拟登录，却仍签发普通 end_user

- **证据：** 注释写「模拟登录……调试/客服」（`internal/app/server/users.go:305-308`）。`RequirePlatformAdmin` 后调用与 SignIn 相同的 `CreateSessionAndTokens`，provider=`server_token`（`:325`）。签发的 JWT 与用户自己登录无法区分（仍是 `ActorKind=end_user` + 真 session 行）。SDK 注释还把它说成 Agent 凭证（`sdk/go/server/users.go` 附近）。
- **为何是问题：** 客服接管、Agent、终端用户是三种产品主体。现在后两种坍缩成第一种。审计只有 slog（`users.go:330-337`），Principal 上看不出「这是被签发的模拟会话」。对做 SaaS 的团队，这是合规与支持工具的缺口，不是功能彩蛋。
- **更优接口：** `Impersonation{Actor: Admin, Target: User, TTL, Reason}` 作为独立凭证种类；claims 带 `impersonator`；session 行标记不可升级、短 TTL。不要复用 `end_user` 签发路径。

---

## 能力缺失

### G-1 平台层没有 Org / Workspace

控制面身份是实例级 `admins`，数据面身份是项目级 `users`。没有「一个客户公司拥有多个项目」的聚合，也没有跨项目的成员目录。计费/审计的 `project_id` 可空（`audit_logs.project_id`，`000002_audit_and_admin_projects.up.sql:3`），因为主体可以是无项目的 admin。要卖给团队，缺的是控制面租户，不是再多一个 schema。

### G-2 没有 Environment（dev / staging / prod）

全库搜不到作为资源的 environment。隔离手段只有再创建一个 Project（再一套 users/keys/schema）。SaaS builder 的刚需是：同一套 schema 定义、不同数据与密钥、成员权限在 prod 更严。今天必须靠约定命名 `foo` / `foo-stg`，且平台超管天然看见全部。

### G-3 没有平台成员邀请；应用内 Groups 不能顶替

Groups 邀请是项目内用户之间的 pending membership（`groups.go:73-96`），角色进入 JWT 的文档 ACL。Console `CreateAdmin` 是超管加一个全局账号，不发信、不绑项目、不选项目角色。`admin_projects` 没有产品 API（见 P-6）。结果：邀请同事进 Console ≠ 邀请玩家进公会，但只有后者被实现。

### G-4 端用户 cookie 会话不是登录主路径

`SessionService` 每次登录都产出 HMAC cookie，gRPC Account 全部丢弃。浏览器端用户若要用 cookie，只能走 OAuth 回调。Console 相反，cookie 是主路径。Realtime 再拒 end-user cookie。对「我的 SaaS 是 Web」的团队，这不是统一的 Auth 模块，是三条运输适配器各写各的。

---

## 已足够深、不必再拆的部分

- **单凭证解析。** `ParseAuthnRequest` 多凭证 / 多 cookie 失败关闭（`authn.go:23-81`）。gRPC / HTTP / Realtime 共用，Grant 留在调用方。这是一条干净的缝，删除测试会立刻露出重复解析器。
- **JWT purpose 派生。** end-user / admin / session-cookie 子密钥（`jwtparser/keys.go`）。admin token 不能当 end-user 验过（`validator.parseJWT` 先试 admin 再试 end-user，再靠 `ActorKind` 分发）。不必再拆一个「密钥服务」。
- **项目 schema 作为数据面租户墙。** `Scoped` 拒绝空 projectID，表名强制 `tw_<id>.users`（`project_table.go:53-64`）。端用户行不能 SQL 扫到别的租户。这是整套 tenancy 真正深的模块；控制面 IAM 浅，不代表这层该拆。
- **API key 的项目绑定。** 哈希全局查找但 Principal 的项目来自行，header 改不了。自管 key 被禁。scope 表与 proto 启动期对齐（`AssertAPIKeyScopeCoverage`）。作为「服务账号」这一类身份，已经够深。
- **端用户会话行作为吊销源。** JWT 每次打 session；改密/改邮/refresh 复用删行。这是 end_user 这条线上正确的深度（问题是 admin/key 没有对等物，见 P-4，不是要把 session 行再拆碎）。
- **Refresh 复用检测。** Redis 原子 Compare-and-swap（端用户删会话，admin 撤时间点）。实现可以留在 adapter；不要在 Account 与 console Auth 再各写一套旋转语义——该合并，不该再分支。

---

*审查范围：`internal/domain/shared`、`users`、`auth`、`projects`、`groups`、`databases/access.go`；`internal/infra/auth`；`internal/app/client`、`console`、`server/{users,apikeys,projects}.go`、`shared/authz.go`；`pkg/grpc/interceptor`；`pkg/jwtparser`；`proto/client/v1/account.proto`、`console/v1/{auth,admins}.proto`、`server/v1/{users,apikeys,projects}.proto`、`shared/v1/authz.proto`；交叉 `internal/api/{clientgrpc,consolegrpc,servergrpc,serverhttp,realtime}`、`console/src/api/client.ts`。未改代码。*
