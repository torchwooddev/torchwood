# 第一性原理还债 — 实现计划

> 对应判断源：`docs/review/first-principles-design.md`（owner 附录 C；终审附录 D）  
> 日期：2026-08-21  
> 状态：**Wave 0 可开工**；Wave 1 可排 DAG；Wave 2+ 需对应设计/规格后再切 PR  
> 读者：实施 agent 与 owner。产品决策不重开。施工只认本文切片 + 评审文附录 C 禁读。

```
Wave 0  五条独立缺陷（可并行；不动模块图）
   │
   ▼
Wave 1  E-8 ∥ E-1 ∥ E-2a  →  E-3（弱依赖 E-1；建议 E-2a 冻结投影后再动 Actor）
   │
   ▼
Wave 2  E-4  Query AST     ∥  E-6 拆 DocumentDB 接口（不删 sentinel / businessSchema）
   │
   ▼
Wave 3  E-5 先独立设计再系统表化  ·  E-2b proto breaking  ·  E-7 工具 overlay（先出工具清单）
后波    C-1 worker 依赖图
```

---

## 0. 共同约束（每张 PR）

抄自评审附录 C。违反即打回。

- 读 `AGENTS.md` 与 `docs/review/first-principles-design.md` 对应 ID；对话与 commit 用简体中文。
- **不要**把 201 RPC 砍成约 20 个动词；工具目录是 overlay（E-7），不是产品 API。
- **不要**删用户文档引擎或 `fakeDocDB`；拆的是端口，不是产品。
- **不要**在 E-5 完成前拆 `pkg/ident`、`businessSchema`、`documentSchema` sentinel、`RejectExternalDatabaseID`、系统集合守卫、`projectschema.Apply`。
- **不要**把 `RefreshRotationStore` / 限流 / 上传会话折进 NonceStore。
- **不要**按过时 v3 文稿把经济表搬回 `public`。
- **不要**用旧 8 条 Appwrite 清单当任何波次完工定义。
- **不要**把 Agent 当成 `ActorKind`；身份变体是 `EndUser | Admin | Service | System`。
- **不要**把 T-1 概念图当成第一天必须切出的 Go 包。
- **不要**把「Documents 方法不带 Principal」读成 List 也不下推 ACL（`_perms` 必须走 SQL）。
- **不要**把用户 collection 默认 `read:any` 当缺陷改掉（D-9）。
- **不要**给系统集合加 `_version`。
- **不要**一夜重写所有 `RunInTx` / ctx 连接为 `uow.Run`。
- 禁止手改 `genproto/**`。改 proto 后 `task generate-proto`；改 Wire 后 `task wire-all`。
- 每张 PR：`go vet ./...`、触及路径的测试绿。不把未完成的波次混进同一提交。
- Wave 0 缺陷可进缺陷账，但 ID 仍用本文前缀（A-/D-/R-），避免与 F/G/H 碰撞。

---

## 1. Wave 0 — 独立缺陷（不动模块图）

五条可并行。每条一张 PR。禁止顺手拆包、改 proto 消息、动 ident/sentinel。

### 规划锁定（终审 OR 必须在此钉死，不留给各 PR）

| ID | 锁定 | 不选的另一支 |
|---|---|---|
| D-5 Array | **拒绝** `array=true` | 不在本波实现 PG array（另开产品单） |
| D-5 `_version` | 新建表继续带列；存量一次 catalog 驱动 reconcile；去掉写热路径 `ALTER` | 不新增 `projectschema` SQL（用户表名来自 catalog，静态迁移写不出） |
| A-4 `secret_hash` | 新会话 SHA-256 hex 写入；查找仍按 session ID；存量双读 + 下次写路径重哈希 | 不发明 secret-bearer 协议（留给 E-1/E-5） |
| R-6 | **内部**建单入口；公开 `CreateOrder` 继续拒绝 `PurposeSubscription` | 不把订阅 purpose 暴露给 Client/Server 支付 API |
| A-5 | refresh 必 `GetAdmin`；角色以库为准；禁止 `firstRole([])=="admin"` | 不改旋转/reuse→revoke（K-16） |

---

### PR-W0-1  A-5 admin refresh 读库

**目标**：`RefreshToken` 在签发新 token 之前 `GetAdmin`（存在性 + 当前角色）。

**改**：`internal/app/console/auth.go` `RefreshToken`（今日 L75–111）。两处 `issueAdminTokens*` 都走库里的 `admin.Role`，不再 `firstRole(claims.Roles)`。

**验收**

- admin 已删、refresh 仍有效 → `Unauthenticated`（不泄露「用户不存在」）。
- admin 角色已改（如 owner→viewer）→ 新 access 的角色是库值，不是 JWT 快照。
- `firstRole([])=="admin"` 不再出现在 refresh 路径。
- reuse → revoke、RotateMissing → session expired 保持（K-16）。

**测试**：补「已删仍持 refresh」「角色已改 refresh 签发新角色」。现有 rotate/reuse 测不得当成本洞验收。

---

### PR-W0-2  A-4 / K-22 `sessions.secret_hash` 写入哈希

**目标**：字段名与内容一致。不发明新会话协议。

**改**

- `internal/infra/auth/session_service.go` 创建会话：secret 用高熵 UUID，`secret_hash` 存 `HashOTP` 同档 SHA-256 hex（`internal/infra/auth/otp.go`；高熵 secret 适用）。
- `EnsureActiveSession`：查找仍按 document ID。存量双读：64 字符 hex = 已哈希；否则当遗留明文，**仅本进程比对后在下次写路径重哈希**，不立刻全局踢会话。
- 不把 secret 放进 cookie / JWT（cookie 仍 HMAC `projectID:sessionID`）。

**验收**

- 新会话行 `secret_hash` 是 64 字符 hex，不是 UUID 原文。
- 遗留明文会话仍能 `EnsureActiveSession`；登录/刷新不回归。
- **不要**用 V-1（SignUp）当本条验收——V-1 不经过 secret_hash。

---

### PR-W0-3  D-5 `Attribute.Array` 拒绝

**目标**：产品不再撒谎。catalog 不得再写入 `IsArray=true` 而物理列是标量。

**改**

- `CreateAttribute` 与 `CreateCollection` 的 attrs：`array=true` → `InvalidArgument`。
- 已有脏 catalog 行（若有）保持标量物理列，读路径按标量。Wave 0 **不做**数组回填。

**验收**

- Client/Server `CreateAttribute(array=true)` 与带 array attr 的 `CreateCollection` 均拒绝。
- 既有非 array 属性 / 文档读写不变。
- `attributeColumnSQL` / `pgTypeFor` 本波可不改（拒绝后走不到）。

---

### PR-W0-4  D-5 `_version` 离开写热路径

**目标**：去掉 `ensureVersionColumn` 在写路径上的 `ALTER TABLE ... ADD COLUMN`（AccessExclusiveLock）。

**改**

- 新建用户集合表：继续 `createCollectionTable` 带 `_version BIGINT`（已如此）。
- 存量缺列：一次 catalog 驱动 reconcile（ensure / 写 DDL 路径，**禁止**每次文档写都 `information_schema` + ALTER）。
- 类型冲突继续 fail-closed（`ErrVersionColumnConflict`）。
- **禁止**给系统集合加 `_version`；出站 version=0 保持。

**验收**

- 文档写路径不再调用 `ALTER TABLE ... ADD COLUMN` / 不再每次查 `information_schema` 补列。
- 缺列集合经一次 reconcile 后 OCC 仍过。
- 系统集合 version 仍为 0。
- 不新增 `internal/infra/projectschema/migrations/*.sql` 试图给动态 collection 加列。

---

### PR-W0-5  R-6 订阅建单走内部入口

**目标**：禁止第三条 `orders.Insert` 路径长期存在。公开支付 API 形状不变。

**改**

- 从 `payments.CreateOrder` 抽出内部建单（幂等键、index、审计、状态机、TTL）。
- `Subscribe.createBillingOrder` / `processDue` 只走内部入口（今日 `subscribe.go:228-258` 直接 `orders.Insert`）。
- 公开 `CreateOrder` **继续**拒绝 `PurposeSubscription`（避免 Client 乱建订阅单；渠道 HTTP 仍不得拖进订阅外层事务——内部入口允许「只落单、不下渠道」的变体）。

**验收**

- 全仓 `orders.Insert` 只出现在内部建单函数。
- `CreateOrder(PurposeSubscription)` 仍 `InvalidArgument`。
- platform 订阅扣款仍能落 `PurposeSubscription` 订单；幂等键 `sub:{subID}:{cycle}` 行为保持。
- V-3：`Order.Transition` / 履约同事务不被本 PR 改浅。

---

## 2. Wave 1 — 不改物理存储、不合 proto 消息

T-1 概念模块 **不是** 本波要切出的 Go 包。第一刀是内部缝。

```
E-8  随时可做（与 E-1 / E-2a 并行）
E-1  User/Session 聚合（存储可暂同一张表）
E-2a Documents 单核 + Client 策略包装
        │
        ▼
E-3  Authenticate + Actor ADT（弱依赖 E-1；建议 E-2a 冻结 Principal 投影后再动）
```

若 E-1 与 E-8 并行：E-8 先改 `domain/users/password.go` / `domain/groups/membership.go` 的错误类型，E-1 只消费 sentinel。

---

### PR-W1-E8  收口假缝与传输泄漏

**目标**（E-8）：删死端口；一次性挑战可合并；gRPC status 退出 domain/pkg。

**做**

- 删除 `projects.ProjectResolver`（全仓仅接口定义）。
- Redis OTP / OAuth state / one-time JWT / account token / MFA challenge → `NonceStore`（register/consume/ttl）。**禁止**并入 `RefreshRotationStore`、`RateLimiter`、`LoginThrottle`、`UploadSessionStore`。
- `domain/users/password.go`、`domain/groups/membership.go`、`pkg/ident` 的 `status.Error` 改为 sentinel / 普通 error；映射留在 api。
- `PaymentProvider.VerifyCallback(http.Header, …)` 可本波不动（HTTP 泄漏，非死端口）；若改，用归一化 `CallbackRequest`，四渠道测试必须绿。

**不做**：经济仓储锁接口、`uow.Run` 全量替换、worker 拆进程。

**验收**

- `ProjectResolver` 零引用且类型删除。
- `RefreshRotationStore` 仍独立；reuse 检测测绿。
- `internal/domain` 与 `pkg/ident` 不再 import `google.golang.org/grpc`。
- V-3 负向：不碰 `Order.Transition` / 履约同事务。

---

### PR-W1-E1  User / Session 聚合

**目标**（E-1）：Account / CreateUser 调用 `GetByEmail` / `User.Register`，**不是** `ListDocuments` + `query.BuildEqual("email")` 薄包装。

**做**

- `domain/users`：`User` 聚合（邮箱唯一、状态、密码、匿名属性）；`GetByEmail`、`Register`。
- `domain/auth` 或 sessions：Session 签发/轮换/吊销接口（行为保持 K-22：上限驱逐、rotation key=`project+session`）。
- 适配器 **暂时**仍读 `tw_<project>` 系统文档表。禁止本波加 FK、静态 DDL、删 `EnsureSystemCollections`（那是偷跑 E-5）。

**验收（V-1）**

- SignUp / `CreateUser` 测试断言走 `GetByEmail` / `User.Register`（或等价），用 `fakeDocDB` 交一层换皮 → 打回。
- 匿名 / OTP / OAuth 创建用户走同一聚合。
- 权限切片第三份拷贝收敛到一处。

---

### PR-W1-E2a  Documents 单核 + 策略

**目标**（E-2a）：抄 K-8 **组合**（一个核心 + Client 策略包装），不抄 Groups 仍返回 `*databases.Document` 的领域形状。

**做**

- `app/shared` 或 `app/documents`：单一 Documents 用例（CRUD / List / OCC / grant）。
- Client：owner 默认 ACE、guest 读、敏感字段过滤。
- Server：脱敏、platform admin。
- Client/Server handler 只做投影。返回类型本波可暂留 `*databases.Document`。
- **禁止**把 `users` / `sessions` 收进 Documents 核。
- **禁止**把 List 改成查出后再 `Check`。现有 `_perms` SQL 下推（`listPermissionFilter`）必须保留。T-3「方法不带 Actor」是 E-6 的形状，不是本波许可。
- **禁止** E-2b（合并 client/server `message Document`）。

**验收（V-2）**

- Client 与 Server 文档写走同一核；策略差异有单测。
- 大结果集 + 无权文档：List 不得先全取出再丢（SQL 下推回归）。
- 系统集合 Databases API 只读分级保持；sentinel 守卫不拆。

---

### PR-W1-E3  Authenticate + Actor ADT

**目标**（E-3）：一个身份模块；三处认证共用；System 是第四变体。

**依赖**：弱依赖 E-1（不要在本 PR 发明 User 仓储）。建议 E-2a 已冻结 `Roles` / `PlatformAdmin` 投影。

**做**

- Actor = `EndUser | Admin | Service | System`。禁止 `ActorKind=agent`。`Service` = 今日 API key。
- 禁止 `UserID` 复用 admin id。
- gRPC / HTTP / Realtime 共享 `Authenticate(...)`。Realtime 禁 API key、HTTP upload 禁 end-user = Grant 配置，不是第三份解析器。
- System：消灭三处互不一致判据（shared 无、assets 看空 `APIKeyID`、databases 看 `PlatformAdmin`/`__system__`）。
- 删除 `HasPermission` 把角色与 scope OR 在一起。Wave 1 **不**重写 120 条 `apiKeyScopeRules`（R-4 属更后；启动对齐 K-9 保留）。
- Client Databases/Transactions 投影带上 `PlatformAdmin`（A-2 回归，不当独立 P0）。
- Cookie 仍只是运输；console cookie 解析结果是 AccessToken，不是 Session。

**验收**

- 三处认证只调一次 Authenticate。
- worker 不再注入「缺 APIKeyID 的假 API key」。
- `HasAnyPermission([])` fail-open 退出领域类型（守门留在启动期收集器，若仍需要）。
- 无 `ActorKind=agent`。

---

## 3. Wave 2 — 接口演进（需写清规格后再切 PR）

本波 **不是** Wave 0 明天就能写的代码。开工前各出一页规格（不必 E-5 级设计文）。

### E-4  Query AST

- proto `Query` AST（eq/in/range/text/and/or + keyset page）。
- `pkg/query` 降为 Appwrite 字符串 codec。
- 分页收敛到 AIP `page_token`（K-20），不要第三套。
- **不要**把 ListProjects 的空 Filter 当实现样板（handler 接线 ≠ 过滤生效）。
- 可双栈：旧 `queries[]string` 仍解析。

### E-6  拆 DocumentDB 接口

- Catalog / SchemaApplier / Documents（List 消费 DocACL Filter）。
- SchemaApplier 只走写路径 / 启动对账；`GetCollection` 不再 `projectschema.Apply`。
- **不删** sentinel / `businessSchema` / 系统集合守卫。
- Database 独立类型（M-5）随本波。
- 用户文档引擎保留；`fakeDocDB` 跟新接口走（K-21）。

---

## 4. Wave 3 — 迁移、breaking、Agent overlay

### E-5  系统表化（闸门：先独立设计）

**未出** `docs/design/` 下独立设计 + 执行计划之前，**禁止**任何系统表化施工。闸门：`docs/design/system-tables.md`。

设计必须包含：FK（`sessions.user_id` → `users`）、退役 sentinel 与 Databases 系统集合守卫、Client 认证字段黑名单、系统集合 version=0 两套契约。**不要**把文档 `_version` / If-Match 搬进系统表（S5 / S15）；行级并发按写形状收口（分列 UPDATE、`groups.total` SQL 增量、状态 CAS、JSON RMW 的 `FOR UPDATE`），不加通用 `version` 列、不改 User/Session proto。验收 **只**含评审 §11 表「E-5 验收只含…」的 1/7/8；**不含** `read:any`（D-9）、staged tx（D-6）、查询 DSL（E-4）。

载体继续 `projectschema`（K-14）。完成前禁止拆 K-13/K-15。

若届时 Wave 0 的 `secret_hash` 未做，迁移必须补同一写入侧闭合。

### E-2b  合并 client/server `message Document`

breaking。单独版本策略。依赖 E-2a 已落地。

### E-7  Tool catalog overlay

开工前必须有工具清单（「约 20」不是规格）。显式决定是否暴露任何 key 管理类工具（今日 `InvokeJSON` 排除 `APIKeysService` 是有意安全设计，T-4/R-5）。201 RPC 全部保留。

建议叠在 E-2a + E-4 之后，避免在错误查询表面上重做。

---

## 5. 后波

### C-1 worker 模块边界

作业切分已对。另开清单：worker 只依赖 `app/{functions,payments,assets,subscriptions,billing,storage}` 端口，而不是整份 `app.ProviderSet` 心智模型。不进 Wave 0/1。

### 明确不在本计划施工

| ID | 原因 |
|---|---|
| D-9 | 产品默认，不是代码债 |
| D-6 / D-7 | 已发布 staged API / 幽灵 catalog 删除可另开运维单，非 Wave 0/1 |
| R-7 | v1 已决定 Storage/Functions Server-only |
| R-4 资源 Grant 表 | 只有方向，没有替换 120 条方法名的规格 |
| V-4 删 `Functions.Execute` | 测试死方法；可随 Functions 清理顺手，不写进任何波次完工 |
| M-4 五动词进 Assets 领域服务 | 经济已深；可另开，不挡身份/文档还债 |
| S-4 `uow.Run` 全量 | 对外缝方向接受；实现可暂用 ctx |

---

## 6. 代表路径（改完是否更深）

| ID | 何时变深 | 不要用来验收 |
|---|---|---|
| V-1 SignUp/CreateUser | E-1 | W0-2 secret_hash |
| V-2 CreateDocument | E-2a | W0-3/W0-4（旁路） |
| V-3 CreateOrder | 保持；W0-5 只闭合订阅入口 | Wave 1 不应改浅 |
| V-4 CreateExecution | 未排期 | Wave 0/1 完工 |

---

## 7. 建议实施顺序（给派发用）

1. **并行** PR-W0-1 … PR-W0-5（互不改同一热路径：console auth / session_service / databases attr / documentdb version / payments+subscriptions）。W0-3 与 W0-4 都动 `documentdb`/`app/server/databases.go`，若冲突则 **W0-3 先于 W0-4**。
2. Wave 0 全绿后：**并行** PR-W1-E8、PR-W1-E1、PR-W1-E2a。
3. E-1 与 E-2a 合入后：PR-W1-E3。
4. Wave 1 后停下来写 E-4 / E-6 一页规格，再切 Wave 2。
5. E-5 独立设计（批准后才有迁移 PR）。E-2b / E-7 各要自己的版本/产品规格。

owner 审查不以实施方自报为准；Wave 0 五条按上列验收测，Wave 1 按 V-1/V-2「深度非零」打回换皮。
