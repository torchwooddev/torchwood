# 本轮收尾对照（计划 vs main）

独立评审。对照合同：`docs/review/first-principles-plan.md`、`docs/review/first-principles-design.md`（§1 / 附录 C / E-5 验收 1/7/8）、`docs/design/system-tables.md`、Wave 规格页。抽查的是 **HEAD 源码**，不以 commit message 或「已落地」文案为准。未跑测试、未 `git add`。

**HEAD**: `41b837c65741fc655ec81da228b86180eab15d75`（`refs/heads/main` 与 `refs/remotes/origin/main` 均为该 SHA；工作区 `ref: refs/heads/main`）。  
**本轮区间**: `262f9d1`（docs 入库实现计划）→ `41b837c`。  
**结论**: **已收尾**

验收条款（计划「验收」段 + 设计 1/7/8 + 各 Wave 规格）均能在代码里对上。附录 C 硬约束未见违反。文档里仍有几处过时注释（系统集合 `version=0`、RPC 仍写 201），不挡合同收尾，见文末非阻塞备注。

可以宣布本轮收尾。

## 计分板

| ID | 状态 | 落地 commit | 缺口（file:line） |
|---|---|---|---|
| A-5 admin refresh 读库 | **done** | `a8324aa` `fix(auth): admin refresh 以库内角色为准` | 无。`RefreshToken` 先 `GetAdmin`，签发用 `admin.Role`；已删 admin 返回 `Unauthenticated` 且不带 not found；`firstRole` 全仓 Go 零命中。见 `internal/app/console/auth.go:75-118`、`auth_test.go:289-362` |
| A-4 `sessions.secret_hash` 哈希 | **done** | `61998dd` + `9fc0321`（review fix） | 无阻塞。新会话 `HashOTP` 64 hex：`internal/infra/auth/session_service.go:66-71`、`session_service_test.go:70-92`。存量双读在 bun Get：`internal/infra/bun/bunrepo/sessions_repo.go:191,216-221`。查找仍按 session id（`EnsureActiveSession` 不比对 secret）。计划「下次写路径重哈希」未见 UPDATE 回写，验收未要求观测该回写，不挡收尾 |
| D-5 拒绝 `array=true` | **done** | `2ebfefe` + `a56f656` | 无。Server `validateAttribute` + adapter `rejectArrayAttribute`：`internal/app/server/databases.go:139-148`、`internal/infra/documentdb/postgres.go:210-213,448-450,1823-1827`。Client 无 CreateAttribute RPC（DDL 仅 Server），adapter 直调亦拒。测：`databases_array_attr_test.go`、`occ_test.go` |
| D-5 `_version` 离开写热路径 | **done** | `6cbb159` + `8ccb2a3` | 无。写路径 `requireVersionColumn` 只查不 ALTER：`postgres.go:557,670,886,1026,1667-1680`。`reconcileVersionColumn` 仅 DDL（CreateCollection / CreateAttribute / CreateIndex）：`234,462,499,1683-1694`。系统集合不加列：`1610-1612`。测：`occ_test.go:402` `TestVersionColumn_WritePathDoesNotAlter`。未给动态 collection 加 `projectschema` SQL |
| R-6 订阅建单内部入口 | **done** | `90dde95` | 无。全仓 `orders.Insert` 只在 `InsertCreatedOrder`：`internal/app/payments/orders.go:324-330`，测锁路径 `payments_unit_test.go:795`。公开 `CreateOrder` 仍拒 `PurposeSubscription`：`orders.go:64-66`。订阅走 `InsertCreatedOrder`：`subscribe.go:239-257`，幂等键 `sub:{id}:{cycle}` |
| E-8 假缝 / NonceStore / domain 无 gRPC | **done** | 合入 main（execute-plan packed-ref 仍停在 `8ccb2a3`，以代码为准） | 无。`ProjectResolver` 仅存评审文档。`NonceStore`：`internal/domain/auth/nonce.go:9-18`，OTP/OAuth/one-time/account token/MFA 走 RedisNonceStore。`RefreshRotationStore` 仍独立：`internal/infra/provides.go:41,52`。`internal/domain/**` 与 `pkg/ident` 无 `google.golang.org/grpc` import（assets 测试只断言禁止 import） |
| E-1 User/Session 聚合 GetByEmail | **done** | `43c925d`（`execute-plan/432fc534-pr-3-e1-user-session`） | 无。`users.Register` + `GetByEmail`：SignUp `account.go:212-233`，CreateUser `users.go:130-138`，匿名/OTP 同聚合。测禁止 `ListDocuments(users)`：`account_signup_repo_test.go:18,235`、`users_create_repo_test.go:41,120`。Session 领域：`internal/domain/auth/session_repository.go` |
| E-2a Documents 单核 + Client 策略 | **done** | 合入 main（代码：`internal/app/documents`） | 无。Client/Server 均 `documents.New(docDB)`：`client/databases.go:23-31`、`server/databases.go:26-34`。Client owner 默认 ACE：`client/databases.go:142-145`；guest 读：`resolveReadPrincipal` → `GuestPrincipal`。List 仍 SQL `_perms`：`listPermissionFilter` 于 `postgres.go:1153,1332,1400`。系统集合守卫未拆 |
| E-3 Actor ADT + 共享 Authenticate | **done** | `bc84e95`（`execute-plan/432fc534-pr-4-e3-actor-auth`） | 无。`ActorKind` = EndUser\|Admin\|Service\|System，无 agent：`principal.go:10-22`、`principal_test.go:10-14`。`Validator.Authenticate`：`authenticate.go:12-19`；gRPC `jwt.go:80,94`、HTTP `serverhttp/auth.go:27`、Realtime `handler.go:358`。worker 注入 System 而非假 API key：`assets/authz.go:28-38`、`assets_test.go:636-641`。`HasAnyPermission` 已删；`HasAnyRole([])` fail-closed：`principal.go:138-142`。Client 投影带 `PlatformAdmin`：`databases_core_test.go:42-89` |
| E-4 Query AST + pkg/query codec | **done** | `2ddee42` + `1c850f5` | 无。`proto/shared/v1/query.proto` 有 eq/in/and/or。双栈字段：client `query=7`、server `query=6`。`pkg/query.Parse/ParseMany` 仍是字符串 codec。SQL 编 AST：`postgres.go:2447-2504`。OR 集成测：`postgres_test.go:520` `TestListDocuments_AstOr`。冲突 InvalidArgument：`documents/query.go:32-34` |
| E-6 Catalog / SchemaApplier / Documents | **done** | `eebbc27` + `0205c22` | 无。三接口 + 嵌入：`internal/domain/databases/repository.go:6-56`。`GetDatabase` 返回 `*databases.Database`：`document.go:116-122`、`postgres.go:128-143`。`ensureProjectCatalog` 零命中。`GetCollection` 只读 catalog、不 Apply：`postgres.go:246-254`。Apply 仅 `EnsureCatalog`：`1455-1465`。`fakeDocDB` 仍实现嵌入后的 `DocumentDB` |
| E-5 系统表化（验收 1/7/8；S15 无通用 version） | **done** | 设计 `af5909f` + S15 `7ac9def`；施工合入 main（bun 适配器 + `000008`/`000009`） | 无阻塞。**1**：bun `users/sessions/identities/groups/memberships/buckets/files`；`EnsureSystemCollections` no-op：`postgres.go:1433-1435`、`repository.go:15`。生产 Account/Users/Storage/Groups 不再传 `SystemDatabaseID`（仅测与守卫）。catalog 删 `_` 行：`000009_system_tables_cut.up.sql:88-100`，测 `projects_test.go:67`。**7**：`clientDocumentUpdateProtectedFields` / `filterClientProtectedFields` 零命中。**8**：`zeroSystemDocumentVersion` 已删；系统表 SQL 无 `_version`（`000008_system_tables.up.sql:2`）。S15：factors `For("UPDATE")` `users_repo.go:206`；`groups.total` SQL 增量 `groups_repo.go:146`。sentinel / `businessSchema` / `RejectExternalDatabaseID` 按设计留作皮带：`postgres.go:1504-1536` |
| E-2b 合并 Document proto | **done** | 合入 main | 无。`proto/shared/v1/document.proto` 字段 1–6。Client/Server RPC 返回 `shared.v1.Document`：`proto/client/v1/databases.proto:65-107`、`proto/server/v1/databases.proto:115-144`。handler `*sharedv1.Document`：`internal/api/servergrpc/databases.go:306`、`clientgrpc/databases.go:28`。请求消息仍分包；`ListDocumentsRequest` 未合并 |
| E-7 Agent 工具 catalog overlay | **done** | `e8be82b` + `26cfcd7` | 无。18 工具：`sdk/go/server/tools.go:42-133`、`sdk/typescript/src/server/tools.ts`。人读：`docs/developer/14-agent-tools.md`。`InvokeJSON` 仍排除 `APIKeysService`：`sdk/go/server/invoke.go:61`。未新增 gRPC service。产品 RPC 未砍到 ~20（见下「明确不做」） |
| C-1 worker 只装配作业端口 | **done** | 合入 main | 无。`cmd/worker/provides.go:35-47` 显式列作业 usecase，禁止桶包。`import_guard_test.go:16-32,48-90` 禁 `internal/app`、`internal/infra`、client/console/server/api/auth/documentdb/server、`genproto`。`wire_gen.go` 无 documentdb / Account |
| V-4 删 `Functions.Execute` | **done** | 合入 main | 无。`func (f *Functions) Execute` 零命中。生产路径是 `CreateExecution`：`executions.go:67`。proto 无 `rpc Execute` |
| D-7 删 public 幽灵 catalog | **done** | `7beedcd` `fix(db): D-7 删除 public 幽灵 document catalog` | 无。`db/migrations/000020_drop_public_document_catalog.up.sql` DROP 四张 public `document_*`。运行时 `catalogIdent` → `ident.ProjectSchemaName`：`postgres.go:1488-1494` |
| M-4 资产五动词进领域服务 | **done** | 合入 main | 无。`domain/assets`：`Grant/Consume/Transfer/Mutate/Expire/ExpireDue`（`write.go`、`expire.go`）。app 只鉴权后委托：`app/assets/write.go:49-64`。`Holding` 仅 `Expired`：`holding.go:27`。领域无 grpc import |
| S-4 对外 `uow.Run` | **done** | 合入 main | 无。`pkg/uow.Runner.Run`：`pkg/uow/uow.go:9-12`。`*clients.Database.Run` 委托 `RunInTx`：`tx.go:33-36`。支付/订阅/资产用 `uow.Runner`。`internal/domain/**` 无 `bun.Tx`。`RunInTx` 仍在，符合「不必一夜改光」 |
| D-6 整面删除 staged transaction | **done** | 合入 main（区间后段 merge） | 无。proto 无 `message Transaction` / `rpc CreateTransaction`。`000021_drop_document_transactions.up.sql` DROP 表。信封无 `transaction_id`（`envelope.go:35-55`，测 `envelope_test.go:64`）。支付渠道 `transaction_id` 保留：`proto/client/v1/payments.proto:146` |

## 附录 C 约束

**未见违反。**

| 禁读 | 本轮事实 |
|---|---|
| 不要把 201 RPC 砍成约 20 个动词 | 产品 RPC 仍是完整 CRUD 面。D-6 删 staged tx 后约 **187**（Client 62 + Server 115 + Console 10），不是 overlay 替换。E-7 是 18 个工具名映射已有 unary |
| 不要删用户文档引擎或 `fakeDocDB` | `internal/infra/documentdb` 仍在；多处 `type fakeDocDB struct` 且 `var _ databases.DocumentDB` |
| 不要把 Agent 当成 `ActorKind` | 仅 `end_user/admin/service/system`；`ActorKind("agent").IsValid()==false` |
| 不要把用户 collection 默认 `read:any` 当缺陷改掉 | `DefaultCollectionPermissions` 仍含 `{Type:"read", Role:"any"}`：`permissions.go:14-18` |
| 不要给系统集合加 `_version` / 通用 version | 系统表 DDL 无该列；`createCollectionTable` 仅用户集合追加 `_version` |
| 不要一夜重写所有 `RunInTx` | `RunInTx` 仍大量存在；仅加了 `uow.Run` 对外缝 |
| 禁止手改 `genproto/**` | 未见手改迹象；E-2b/E-4 有对应 proto 源。本次未逐字节证明生成器输出 |
| 系统表化完成前不要拆 ident / businessSchema / sentinel | E-5 已 cut；皮带仍在：`documentSchema` sentinel、`businessSchema` 两段式、`RejectExternalDatabaseID` |
| `RefreshRotationStore` 不是 nonce | 仍独立接口与 Redis 实现 |
| 不要用旧 8 条 Appwrite 清单当 E-5 完工 | 抽查只认 1/7/8，未把 D-9/DSL/staged 算进 E-5 |

## 明确不做的项

均按约定保留，没有被误改：

- **D-9** 用户 collection 默认 `read:any`：`internal/domain/databases/permissions.go:17`。
- **R-7** Storage/Functions 仍 Server-only（Client proto 无 Storage/Functions service）。
- **R-4** `apiKeyScopeRules` 仍在 `pkg/grpc/interceptor/apikey_scope.go`，方法名表未换成 Grant 表。
- **未一夜重写全部 `RunInTx`**：`clients.Database.RunInTx` 与 documentdb/outbox 热路径仍在。
- **未砍 201→~20**：E-7 overlay 18 工具；完整 API 仍在 proto。
- **未删用户文档引擎 / fakeDocDB**。
- **未给系统表加通用 `_version`**。
- **未把 Agent 加成 `ActorKind`**。

附：D-6 删 staged tx RPC 后，文档仍多处写「201 RPC」（`docs/developer/14-agent-tools.md:3`、`sdk/README.md`）。这是计数文案漂移，不是砍产品面。当前手工点数：Client 62 + Server 115 + Console 10 = **187**。

## 阻塞收尾的问题

无。可以宣布本轮收尾。

---

### 非阻塞备注（不计入缺口、不挡收尾）

1. **过时注释仍写「系统集合恒为 0」**（E-5 #8 运行时已退役该双契约，注释未 scrub）：
   - `internal/domain/databases/document.go:41`
   - `proto/shared/v1/document.proto:17`（并流入 `genproto/shared/v1/document.pb.go`）
   - `internal/infra/documentdb/postgres.go:2184` 仍写「由 app 层按 IsSystemCollection 归零」，app 已无该赋值。
2. **A-4** 遗留明文 `secret_hash` 只在 Get 时 canonicalize，未见 CAS 回写哈希（S15 写「若仍要写」才要求）。新会话已是 64 hex。
3. **Account 文案**仍说 `create user document`：`internal/app/client/account.go:237`、`email_otp.go:161`（实现已是 `usersRepo.Insert`）。
4. **packed-refs** 里若干 `execute-plan/*` 分支停在 Wave 0 SHA，不能当落地证明；本表以 `main` @ `41b837c` 源码为准。
