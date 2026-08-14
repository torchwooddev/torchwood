# Round 3 全量审核：06 - Server/Console 用例层

> 范围：`internal/app/server/`、`internal/app/console/`、`internal/app/shared/`（全部 `*.go` 含测试），交叉只读 `internal/domain/{projects,users,teams,databases}`、`pkg/grpc/interceptor`、`internal/api/{servergrpc,consolegrpc}`、`proto/{server,console}`。  
> 基线：`docs/review/prompts/06-server-console-use-cases.md`、`AGENTS.md`、`docs/review/round2/reports/06-server-console-use-cases.md`。  
> 约束：只读审查，未改源码；集成测试依赖 Postgres/Redis，按约定未运行。  
> 辅助：静态通读 + 与拦截器 / proto / 系统集合 spec 交叉对照。

---

## 1. 摘要

Round 3 相对 Round 2：**F2-2 权限收口与 Functions 产品决策 B 已落地**，核心级联删除、last-owner、邮箱查重、setup 一次性引导仍保持正确。本轮**未发现跨项目越权或级联截断类 P0**。

仍有两处会直接打到产品契约的 P1：

1. **Databases schema DDL 用例层用 `RequirePlatformAdmin`，持 `databases.write` 的 API Key 被一律拒绝**，与拦截器 scope 门禁、路线图「Agent 可运行时建库/集合」以及 Functions 已改为 `RequireServerWriteActor` 不一致。
2. **`CreateMembership` 无幂等、memberships 无 `(team_id, user_id)` 唯一索引**，重复邀请/重复 accepted 会使 `total` 漂移。

其余为事务边界、角色加载分页、邮箱规范化、纵深防御缺口等 P2/P3。

**Verdict：有条件通过（conditional pass）**。无 P0；建议先拍板并修齐 Databases DDL 守卫与邀请幂等后再关闭本模块。

---

## 2. Round 2 遗留项核实

| 项 | 结论 | 证据 | 说明 |
|----|------|------|------|
| F2-2 Functions 写方法对 viewer/member 开放 | ✅ 已修复 | 拦截器 `pkg/grpc/interceptor/admin_roles.go:35-41` 登记全部 Functions 写 RPC；用例 `internal/app/functions/management.go:57` 等改为 `RequireServerWriteActor`（G12 产品决策 B） | viewer/member 由拦截器拒绝；API Key 由 scope 放行，用例层不再误用 `RequirePlatformAdmin` |
| F2-2 CreateUser / DeleteUserSession / UpdateProject | ✅ 已修复 | `users.go:76`、`:262` 用 `RequireServerWriteActor`；`admin_roles.go:18-22,48` 收 viewer；`projects.go:173-175` 非平台 admin 仅能改绑定项目 | 与前端 member 可写业务资源、viewer 只读对齐 |
| F4-1 级联删除 50 条截断 | ✅ 仍正确 | `users.go:330-335` `RunInTx`；`users.go:426-445` `cascadeListAll` 循环至 `NextPageToken == ""`；`teams.go:467-468` 删除团队 memberships 复用同一循环 | `documentdb` 虽将 `limit(1000)` clamp 到 `maxQueryLimit=100`，分页循环仍能拉全量 |
| F4-3 last-owner | ✅ 仍正确 | `teams.go:473-499` `guardLastOwner`；`UpdateMembership:294-297`、`DeleteMembership:371-372` 均调用 | pending owner 放行；仅 accepted+owner 计入 |
| F4-4 UpdateUser 改邮箱查重 | ✅ 仍正确 | `users.go:160-176` 查重并排除自身；`shared/docdb_errors.go:28,43-45` 将 23505/`ErrDuplicateKey` 映射 `AlreadyExists` | |
| F4-5 GetProject nil,nil | ✅ 仍由传输层兜底 | 用例 `projects.go:147` 透传 repo；handler 侧 Round 2 已对 nil 返回 NotFound | 本模块 GetProject 对越权返回 NotFound（`:144-145`） |
| F4-6 DeleteTeam 事务 | ❌ 未修 | `teams.go:165-182` 仍先循环删 memberships 再删 team，无 `RunInTx` | 见下方 P2 |
| F4-6 CreateUserToken 生命周期 | ⚠️ 未收紧 | `users.go:281-283` 注释仍为默认会话 TTL（7 天）；仅有 slog 审计（`:307-315`） | 见下方 P2 |
| F5-4 GetVariables 掩码写回 | ✅ 后端已补 | `internal/app/functions/variables.go:16-18,43-49` SetVariables 将 `"******"` 视为保留旧值 | 前端适配属模块 11，本模块不再记为打开项 |
| F7-1 setup 可被抢占 | ✅ 仍正确 | `console/setup.go:122-127` 未配置/错误 token 拒绝；`:137-160` `WithBootstrapLock` + 空表检查 + bootstrap principal | 二次 SignUp / rollback 测试仍在 `setup_test.go` |
| R06-P3 CreateAPIKey scope ⊆ 调用者 | ✅ 已补 | `apikeys.go:47-70` `ensureScopesWithinCaller`；平台 admin 放行 | 当前入口仍先 `RequirePlatformAdmin`，该校验是未来放宽时的纵深防御 |
| G2-4 Admins ActorKind | ✅ 仍正确 | `console/admins.go:77,117,166` `RequireAdminActor` | Create/Update/Delete 对 API Key / 端用户 / 匿名拒绝 |

---

## 3. 已核实健康

### 3.1 项目隔离

- 传输层一律从 `contexts.Principal(ctx).ProjectID` 取项目（API Key 绑定项目；admin 由拦截器写入 `X-Torchwood-Project` 并经 `ValidateAdminProjectAccess`）。
- `Projects.CreateProject`（`projects.go:51-52`）仅平台 admin；`ListProjects`（`:114-117`）非平台 admin 返回空列表，避免枚举；`GetProject` / `UpdateProject`（`:144-145,173-175`）越权返回 NotFound。
- Users / Teams / Databases / APIKeys / OAuth 的读写都带 `projectID` 进入 docDB 或按 `project_id` 过滤；`APIKeys.Get/Delete` 校验 `key.ProjectID == projectID`（`apikeys.go:118-120,129-131`）。
- 用例层**不二次核对** `principal.ProjectID == 入参 projectID`（信任传输层）。当前 handler 均取自 principal，未见跨项目入口。

### 3.2 级联删除

| 删除对象 | 行为 | 结论 |
|----------|------|------|
| User | 事务内 `cascadeListAll` 清理 sessions / identities / memberships，再删 users；accepted membership 递减 team `total`（`users.go:326-379`） | 满足 roadmap §2.2「级联 sessions/tokens」。不删用户在自定义集合中的文档——验收标准未要求 |
| Team | `cascadeListAll` 清 memberships 再删 team（`teams.go:165-182`） | 满足 §2.3「删除团队级联 memberships」；缺事务（P2） |
| Database | 禁止删 `default`（`databases.go:88-90`）；其余委托 `docDB.DeleteDatabase` | 元数据/schema 清理归属 documentdb（Round 2 已标 F3-5） |
| Collection | 系统集合拒绝（`:147-149`）；其余委托 adapter | 系统集合保护有集成测试 |
| Project | **无 Server API**；仅 setup 回滚调 `projectRepo.DeleteProject`（`setup.go:174-177`） | 回滚不拆 schema，见 P2 |

### 3.3 API Key

- 创建：平台 admin + scope 格式/数量上限 + `ensureScopesWithinCaller`；secret 只在 Create 返回，List/Get 映射不含 `SecretHash`。
- 删除即失效（哈希比对，无独立轮换 RPC）。
- 拦截器禁止 API Key 管理 APIKeys 服务；用例 `Create` 再以 `RequirePlatformAdmin` 拒绝 service actor。
- List/Get/Delete **无**用例层 actor 守卫（见 P2）。

### 3.4 Users

- 创建：`RequireServerWriteActor`、邮箱规范化、密码策略、email 查重、`SystemPrincipal` 写库。
- 更新：屏蔽 `_` 前缀与 `password_hash`；status 白名单；改邮箱查重并强制 `email_verified=false`。
- 重置密码：`RequirePlatformAdmin` + 强度校验 + `DeleteSessionsByUser`。
- 模拟登录：`RequirePlatformAdmin`、inactive/blocked 拒绝、`provider=server_token`、调用者审计日志。JWT 即普通用户会话，无 impersonator claim、未缩短 TTL（P2）。
- `DeleteUserSession` 校验 session 归属同一 userID，否则 NotFound。

### 3.5 Teams

- 邀请默认 `pending`，不可回退 pending；accept 时补 `user_id`/`joined_at` 并 `+1 total`。
- last-owner 覆盖 Update/Delete（含 client 自退）。
- prefs：空对象、整体替换、存量 reconcile 自愈、无团队角色写拒绝，均有集成测试。
- 邀请**不幂等**（P1）；`AcceptedTeamRoleLabels` 默认页 50（P2）。

### 3.6 Databases / 系统集合

- DDL 入口均 `RequirePlatformAdmin` + 标识符校验；禁止创建/删除 `default` 库。
- 系统集合 schema 7 项操作与文档写路径（含 Bulk）拒绝；`users/sessions/identities` 读仅 `PlatformAdmin` 且脱敏。
- 自定义库同名 `users` 不受系统名单限制。覆盖见 `system_collections_readonly_test.go`。

### 3.7 Console setup / admins

- 未配置 setup token 默认关闭；常量时间比较；advisory lock 串行化首个 owner。
- 后续步骤失败 best-effort 回删（admin 走 repo，避开 last-owner 守卫）。
- Admins：角色枚举、邮箱规范化、密码强度、自我改角色/自删拒绝、last-owner `FailedPrecondition`。
- proto `permissions: ["owner"]` 限制 Create/Update/Delete；用例只保证 `ActorKind=admin`（角色细粒度交给拦截器，符合 `RequireAdminActor` 设计）。
- Auth：登录节流、refresh 重用撤销、SignOut 对过期 cookie 仍尽力撤销。

### 3.8 `RequireServerWriteActor` vs `RequirePlatformAdmin`

| 守卫 | 当前用法 | 判定 |
|------|----------|------|
| `RequirePlatformAdmin` | CreateAPIKey、UpdateUserPassword、CreateUserToken、DeleteUser、Databases 全部 DDL | 前四项正确（平台敏感）。**DDL 对 API Key 过严**，见 P1 |
| `RequireServerWriteActor` | CreateUser、DeleteUserSession；Functions 全部写；Storage CreateBucket | 正确：允许 admin 会话或 API Key，拒绝端用户/匿名；viewer/member 由拦截器管 |
| `RequireAdminActor` | Admins Create/Update/Delete | 正确；Setup 注入 bootstrap principal 后创建首个 owner |
| 无守卫 | UpdateUser、Teams 写、OAuth Upsert/Delete、APIKeys List/Get/Delete | Teams 被 Client 复用，不能简单套 ServerWriteActor。其余见 P2 |

`shared/authz.go:12-15` 注释仍把「Functions 写方法」列在 `RequirePlatformAdmin` 适用面，已过时（P3）。

---

## 4. 新发现 / 仍打开的问题

### 🔴 P0 严重

无。未发现可直接利用的跨项目越权、提权或级联截断导致数据损坏。

### 🟠 P1 高

#### P1-1 Databases schema DDL 用错守卫，API Key 无法建库/集合

- **位置**：`internal/app/server/databases.go:50,83,98,141,154,178,204,226,245`（Create/Delete Database/Collection/Attribute/Index、UpdateCollection）；对照 `internal/app/shared/authz.go:16-24`、`pkg/grpc/interceptor` 中 DDL 映射到 `databases.write`、`docs/roadmap.md:20`「Agent 可运行时建库/集合/文档」。
- **问题**：`RequirePlatformAdmin` 要求 `ActorKind==admin && IsPlatformAdmin`，**API Key（`ActorKind=service`）必然 PermissionDenied**。拦截器却会让持 `databases.write` 的 Key 通过；文档写路径（CreateDocument 等）无此守卫，Key 可写文档但不能建库。Functions 已在 G12 改为 `RequireServerWriteActor`，DDL 未同步。
- **影响**：CLI / SDK / Agent 用合法 API Key 调 CreateDatabase/CreateCollection 在用例层失败；与 Agent-Native 验收和 scope 契约不一致。不是越权，是**功能被 fail-closed 误伤**。
- **修复建议**：若产品确认 Agent 可做 schema（与路线图一致），将 DDL 改为 `RequireServerWriteActor`（对齐 Functions），角色仍由 `adminRoleMethodRules` 限制为 owner/admin。若产品确认 schema 仅限平台 admin，则应在拦截器对 API Key 拒绝 DDL，并改路线图/OpenAPI 说明。补「API Key + databases.write 创建库/集合」测试。

#### P1-2 团队邀请不幂等，可重复 membership、total 漂移

- **位置**：`internal/app/server/teams.go:184-255` `CreateMembership` 在校验角色/状态后直接 `CreateDocument`，无「同 team+user / 同 team+email 已存在」检查；`internal/infra/documentdb/system_collection_specs.go:189-192` memberships 仅有 `team_id` / `user_id` / `email` 普通 key，**无 unique(team_id, user_id)**。
- **问题**：审查重点明确要求「邀请幂等性」。Server 可对同一用户反复发 pending，或直接再写一条 `status=accepted`（每次 `adjustTeamTotal +1`）。
- **影响**：`total` 大于真实成员数；ListMemberships / JWT 角色出现重复 `team:{id}`；last-owner 按 membership 行计数，脏数据会干扰运营判断。不构成稳定的 last-owner 绕过，但是状态机/数据完整性缺陷。
- **修复建议**：创建前按 `team_id` + `user_id`（或规范化 email）查询，已存在则 `AlreadyExists`（或返回原文档）。系统集合补 unique 索引兜底并发。accepted 与 pending 的冲突策略需写清（例如 pending 重复视为幂等返回）。

### 🟡 P2 中

#### P2-1 DeleteTeam 级联仍无事务（Round 2 遗留）

- **位置**：`internal/app/server/teams.go:165-182`。
- **问题**：先逐条删 memberships，再删 team，未包 `RunInTx`。中途失败留下无 team 的 membership 或删了一半的成员。
- **影响**：与 `DeleteUser`（`users.go:330-335`）不一致；孤儿 membership 会污染角色加载。
- **修复建议**：给 `Teams` 注入 `*clients.Database`，将 list+delete memberships + delete team 放入同一事务。

#### P2-2 `AcceptedTeamRoleLabels` / `ListAcceptedTeamRoles` 未分页，默认最多 50 条

- **位置**：`internal/app/server/teams.go:382-429`；adapter 默认页 `internal/infra/documentdb/postgres.go:891-898`（Limit/PageSize 为 0 时回退 50）。
- **问题**：Client 用 `AcceptedTeamRoleLabels` 判断「是否 owner 才能删团队/踢人/邀请」（`internal/app/client/teams.go:62-68,77-83,152-158`）。用户 accepted membership > 50 时，排在默认页外的团队会误判「不是成员/不是 owner」。
- **影响**：多团队用户在第 51+ 个团队上出现假阴性 PermissionDenied（fail-closed，非提权）。
- **修复建议**：复用 `cascadeListAll`，或显式 `limit` + 循环至 `NextPageToken` 为空。

#### P2-3 Console 登录未规范化邮箱

- **位置**：`internal/app/console/auth.go:61` `GetAdminByEmail(ctx, cmd.Email)`；创建侧 `admins.go:80` / `normalizeAdminEmail` 存小写；repo `admin_repo.go:60` 精确匹配。
- **问题**：节流键用了 `ToLower`（`auth.go:53`），查找却用原始大小写。`Admin@X.com` 对已存 `admin@x.com` 会走「无效凭据」。
- **影响**：大小写不一致导致无法登录；与 Users/Account 路径不一致。
- **修复建议**：查找前 `strings.ToLower(strings.TrimSpace(cmd.Email))`。

#### P2-4 CreateMembership 按邮箱解析用户时未规范化

- **位置**：`internal/app/server/teams.go:219-225,511-523`。
- **问题**：`query.BuildEqual("email", email)` 使用调用方原始字符串；users.email 创建时已小写（`users.go:82,448-449`）。
- **影响**：`Member@X.com` 邀请无法绑定已存在用户，留下 `user_id=""` 的 pending；accept 时再次解析仍可能失败。
- **修复建议**：解析与落库前对 email 做与 Users 相同的 normalize。

#### P2-5 CreateUserToken 仍为完整用户会话（7 天），无模拟标记

- **位置**：`internal/app/server/users.go:281-316`。
- **问题**：Round 2 已标「生命周期未额外收紧」。签发的是默认同款 TokenBundle；JWT 无 impersonator/purpose 字段；审计仅 slog。
- **影响**：SDK 已将其当作 Agent 登录凭证（`docs/developer/12-sdk.md`）。泄露后与真实登录无法区分，且默认一周有效。
- **修复建议**：若定位调试/客服，缩短 TTL 并在 claims 加 `server_token`/`impersonator`；若定位 Agent 凭证，在 proto/注释去掉「仅调试」以免误导。

#### P2-6 若干写方法缺少用例层 actor 守卫

- **位置**：
  - `users.go:138-199` `UpdateUser` 以 `SystemPrincipal` 写 users，无 `RequireServerWriteActor`（与 CreateUser 不一致）。
  - `oauth_providers.go:34-91` Upsert/Delete 无守卫。
  - `apikeys.go:109-133` List/Get/Delete 无守卫。
- **问题**：绕过拦截器直接调用例时，端用户/匿名可走 SystemPrincipal 改任意用户，或改 OAuth / 删 Key。拦截器对 OAuth 写已限 owner/admin、对 APIKeys 服务已禁 API Key；**DeleteAPIKey / UpdateUser / Teams 写方法未进 `adminRoleMethodRules`**，viewer 会话可调（属拦截器 denylist 残留，跨模块 01）。
- **影响**：纵深防御不完整；viewer 删 Key / 改用户是真实路径（需拦截器配合）。
- **修复建议**：UpdateUser / OAuth / APIKeys.Delete 补 `RequireServerWriteActor` 或 `RequirePlatformAdmin`（按敏感度）；拦截器 allowlist/denylist 补 UpdateUser、Teams 写、DeleteAPIKey。Teams 因 Client 复用，不要直接套 ServerWriteActor。

#### P2-7 复合写非原子

- **位置**：`users.go:225-233` 先改密码再撤会话；`teams.go:87-101` CreateTeamWithOwner；`teams.go:249-252,356-359,374-378` membership 与 `total` 分两步。
- **问题**：后一步失败时密码已改但旧会话仍在，或 team 已建无 owner，或 total 与成员数不一致。
- **影响**：与 DeleteUser 已包事务的标准不一致。
- **修复建议**：能进同一 `RunInTx` 的（docDB 已感知外层事务）一并包入；密码重置失败应回滚哈希或先撤会话。

#### P2-8 Setup 回滚只删 projects 行

- **位置**：`internal/app/console/setup.go:168-184`。
- **问题**：`CreateProjectInternal` 会 `EnsureSystemCollections`；回滚 `DeleteProject` 只删元数据行，default schema / 系统集合表残留。注释已写 best-effort。
- **影响**：引导失败后重试：admin 已回删可再 SignUp，但 `default` 项目 id 或 schema 可能冲突导致再次失败。
- **修复建议**：回滚调用与创建对称的 schema 清理，或对「default 项目已存在但无 admin」做自愈。

#### P2-9 CreateProject 从名称派生 id，撞车时错误分类弱

- **位置**：`internal/app/server/projects.go:68-76,86-97`。
- **问题**：`"My App"` → `my-app`。第二次同名（或派生 id 相同）走 repo 插入失败，包装为 `create project: ...`，不一定是 `AlreadyExists`。UpdateProject 撞名则明确 `InvalidArgument`（`:196`）。
- **影响**：客户端看到 500/未知错误，难以区分校验失败与冲突。
- **修复建议**：插入前查 id/name；冲突映射 `AlreadyExists`。

### 🟢 P3 低

#### P3-1 `RequirePlatformAdmin` 注释过时

- **位置**：`internal/app/shared/authz.go:12-15` 仍写「Functions 写方法……必须有平台 admin 凭证」。
- **影响**：后续改守卫时容易再改回过严。
- **建议**：改为「API Key 管理、用户密码/令牌/删除、以及（若保留）schema DDL」。

#### P3-2 ListUserSessions 固定 PageSize=100

- **位置**：`internal/app/server/users.go:249-252`。
- **影响**：会话 >100 时列表截断（删除路径已用 cascadeListAll，不受影响）。
- **建议**：循环拉取或暴露分页。

#### P3-3 ListProjects 全量加载再切片

- **位置**：`internal/app/server/projects.go:118-133`。
- **影响**：项目数很大时多余 IO。当前规模可接受。
- **建议**：repo 层分页，或继续用 `pkg/crud` 但下推 offset/limit。

#### P3-4 UpdateUser labels/prefs 类型不在用例层校验

- **位置**：`users.go:177-186` 只过滤字段名；labels/prefs 原样写入。
- **影响**：传输层已把 labels 收到 `[]any`，直接调用例可写入非数组。系统集合 JSONB 能吃下，查询侧可能异常。
- **建议**：labels 断言 `[]string`/`[]any` 的字符串元素；prefs 断言 `map[string]any`。

#### P3-5 `GetAPIKey` 跨项目返回 `nil, nil`

- **位置**：`apikeys.go:113-121`。
- **影响**：handler 已映射 NotFound（`servergrpc/apikeys.go:87-89`），与旧 GetProject 坑不同。用例本身风格不统一。
- **建议**：用例直接 `NotFound`，与 Delete 对齐。

---

## 5. 测试观察

覆盖较好：Projects 平台 admin / 越权 NotFound、API Key scope 与 PlatformAdmin、Users 创建/改密/级联 61 条、Teams last-owner / prefs / 邀请接受、Databases 验收链与系统集合只读、Admins 自我保护与 ActorKind、Setup token/二次注册/rollback、Functions `RequireServerWriteActor`。

缺口：无「API Key 调 CreateDatabase/CreateCollection」用例；无邀请幂等/重复 accepted；无 DeleteTeam 事务故障注入；无 `AcceptedTeamRoleLabels` >50 条；无 Console 登录大小写邮箱。

---

## 6. 模块结论

- **隔离**：项目边界由 principal.ProjectID + 资源上的 project 过滤构成，Get/UpdateProject 越权 NotFound，API Key 绑定项目。未见跨项目读写。
- **级联**：User（含 >50 分页）与 Team memberships 清理语义正确；User 在事务内，Team 仍非事务。无 DeleteProject 业务 API。
- **守卫**：Functions / CreateUser / DeleteUserSession 的 `RequireServerWriteActor` 用法正确。Databases DDL 的 `RequirePlatformAdmin` 与 API Key 产品路径冲突，是本轮最需要拍板的一项。
- **最需优先修复的 3 项**：
  1. **P1-1** 统一 Databases DDL 守卫与 API Key/`databases.write` 契约（或明确禁止并改拦截器）。
  2. **P1-2** 邀请幂等 + memberships 唯一约束，避免 `total` 与成员行膨胀。
  3. **P2-1 / P2-2** DeleteTeam 包事务，角色加载改为拉全量——都是已验证过的级联/权限路径上的残留。

**模块 Verdict：conditional pass**（无 P0，2 个 P1 待产品确认后修复，方可关闭审查）。
