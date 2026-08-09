# Databases 系统集合只读化设计（RFC）

> [!WARNING] 已作废归档（ARCHIVED）
> 归档日期：2026-08-09
> 归档原因：RFC 已实现——migration `000009_document_collections_is_system`（含存量回填）、domain 层系统集合名单常量（`internal/domain/databases/system_collections.go`）、读写分流与 PlatformAdmin 分级脱敏、schema 级写拦截、Console 只读态均已落地并有测试覆盖（`system_collections_readonly_test.go` 等）。
> 后续信息源：`internal/domain/databases/system_collections.go`、`internal/app/server/databases.go`、`internal/app/client/databases.go` 及其测试。

> 状态：已修订（v2，独立审查通过后定稿）
> 目标版本：当前 main（3e701d2）
> 审查记录：docs 评审 agent 已实际核对全部引用；修订项见 §8 变更日志

## 1. 背景与目标

### 1.1 需求

系统 Collections（users/sessions/identities/teams/memberships/buckets/files，位于 `default` database）在 Console 界面**不可修改，只能查看**。

### 1.2 目标

1. 修复 P0 漏洞：Server Databases API 的 schema 级操作（DeleteCollection/UpdateCollection/CreateAttribute/DeleteAttribute/CreateIndex/DeleteIndex）目前**完全没有系统集合保护**——具有 databases scope 的 API key 或 Console 会话可直接 `DROP TABLE` 删除 users/sessions 表，或修改系统集合权限（如把 users 改成 `read:any` 造成全量用户数据泄露）。
2. 实现"系统集合只读"的完整语义：文档读放开（分级）、写全量锁死。
3. 控制影响范围：**不改专用服务**（Users/Teams/Storage/Auth/Account），不改 adapter 层 DDL 签名，**不扩展 adapter 写保护名单**（避免切断 Teams/Storage 用户路径，见 §3.3 关键结论），前端改动聚焦 databases 模块。

## 2. 现状（研究结论摘要）

### 2.1 已有防护（4 层）

| 层 | 位置 | 覆盖 |
|---|---|---|
| 用例层黑名单 | `app/server/databases.go:21` `serverSystemCollections`；`app/client/databases.go:17` `clientSystemCollections` | 仅文档操作（`ensureCollection`） |
| adapter 写保护 | `infra/documentdb/postgres.go:39` `systemCollectionsWriteProtected` | 仅 users/sessions/identities 3 个集合的文档 create/delete |
| `__system__` 旁路 | `domain/databases/access.go` `SystemPrincipal` | 专用服务内部路径 |
| 迁移收窄 | `000007`/`000008` + 运行时 `cleanupKeysWritePerms` | keys 角色写权限 |

### 2.2 缺口

1. **P0**：`UpdateCollection`/`DeleteCollection`/`CreateAttribute`/`DeleteAttribute`/`CreateIndex`/`DeleteIndex`（app/server/databases.go:125-205）不走 `ensureCollection`，无任何系统集合拦截 → 可 DROP 系统集合表、可改系统集合权限/结构。`CreateCollection` 也未拦截（default 库建同名集合返回脏错误或产生 is_system=false 的重复元数据）。
2. adapter 写保护名单不完整（3/7），且不覆盖 schema 级操作（**但名单不扩展**，见 §3.3）。
3. proto `serverv1.Collection`（databases.proto:160）无 `is_system` 字段；`document_collections` 表无 system 列；Console 前端无任何系统集合概念（`console/src` 搜索 `system` 零结果），系统集合与用户集合无差别展示、可删可改。
4. 黑名单按 collectionID 全局匹配、不区分 database → 用户在自定义 database 建 `id=users` 的集合会被误拒。

### 2.3 关键约束（研究 + 审查确认）

- **`mapUserDoc`（servergrpc/users.go:104）脱敏，但 `mapDocument`（servergrpc/databases.go:484）不过滤** → 放开 users/sessions 文档读必须脱敏或分级，否则 API key 可经 databases API 拿到 `password_hash`/`secret_hash`。
- **`app/client/account.go` 大量以用户 principal（`user:{id}`）直接调 docDB 读/改自己的 users/sessions 文档**（account.go:293/413/511 等）→ adapter 层 `UpdateDocument` 的 owner 例外（postgres.go:429）**必须保留**。
- **adapter 层不能扩展写保护名单**：已验证 `app/server/teams.go:204`（CreateMembership）、`:254/304`（UpdateMembership）、`app/storage/storage.go:128`（DeleteBucket）、`:169`（CreateFile）均以**调用方 principal**（非 SystemPrincipal）直接写 teams/memberships/buckets/files 集合。若把名单补全为 7 个，将切断建团/退团/改成员/上传/删除文件等全部用户路径（§3.5"不改专用服务"与名单补全矛盾）。
- teams/buckets/files 集合级 `read:any|keys|admin` 是既有公开语义（Storage 专用 API 同语义）；memberships 无 `read:any`（spec 为 `user:{id}/team:{id}/keys/admin`）→ 放行这 4 个集合的读**不扩大 keys 主体权限面**。
- **匿名 Guest 语义决策（明示）**：Client API 读放行后，匿名访客可经 `read:any` 读取 teams/buckets/files 文档（团队名、文件元数据、bucket 权限数组）。这是集合权限的显式声明语义；若产品要求收紧，后续可在用例层加 Guest 限制，本次不处理。
- 现有集成测试（app/server、app/client 的 databases 集成测试、acceptance §9）均无"Databases API 拒绝系统集合"的断言 → 行为变更不破坏现有测试。Console 无前端测试文件。

## 3. 设计决策

### 3.1 数据模型：`is_system` 单一事实来源

- migration `000009_document_collections_is_system`：`document_collections` 加 `is_system BOOLEAN NOT NULL DEFAULT FALSE`；**同时回填存量**：`UPDATE document_collections SET is_system=true WHERE database_id='default' AND id IN ('users','sessions','identities','teams','memberships','buckets','files')`（migration 内直接回填，避免运行时逐项目补）。
- bun model `DocumentCollection` 加 `IsSystem bool`（`bun:"is_system,notnull,default:false"`）。
- domain `databases.Collection` 加 `IsSystem bool`。
- proto `serverv1.Collection` 加 `bool is_system = 11;`（`task generate-proto`，勿手编 genproto）。
- `EnsureSystemCollections` 创建系统集合时写 `IsSystem: true`（createCollectionMetadata 透传）。
- **名单常量移入 domain 层**：`domain/databases` 新增系统集合名单常量（ID 表），供 app/server、app/client、documentdb 三方引用，消除 `serverSystemCollections`/`clientSystemCollections`/`systemCollectionSpecs` 三处硬编码漂移；`system_collection_specs.go`（infra）仅保留 attrs/indexes 定义并引用 domain 名单。判定逻辑：仅当 `database_id == "default"` 且集合 ID 命中名单时按系统集合处理（修复 2.2-4 的误拒）。

### 3.2 读策略（分级放行，保留权限过滤）

| 集合 | Server API（API key / Console 会话） | Client API |
|---|---|---|
| teams / memberships / buckets / files | 文档 List/Get/Count 放行（docDB 权限过滤兜底） | 文档 List/Get/Count 放行 |
| users / sessions / identities | 文档读**仅 PlatformAdmin**；返回前剔除敏感字段 | 保持全拒（有 Account 专用 API） |
| 全部 7 个（schema 级） | GetCollection/ListCollections/GetDatabase 等读保持现状（已允许） | N/A（Client 无 schema API） |

实现位置：
- `app/server/databases.go`：`ensureCollection` 按调用拆分：
  - 写路径（Create/Update/Delete/Bulk 文档）：现有逻辑保持——7 个系统集合全拒。
  - 读路径（List/Get/Count 文档）：新增 `ensureReadableCollection`——teams/memberships/buckets/files 放行；users/sessions/identities 要求 `principal.PlatformAdmin`（Console 会话经 validator.go:137 置位，`dbPrincipal` 透传，已验证），否则 PermissionDenied；并在 Get/List 返回前对这三个集合做敏感字段脱敏。
  - **脱敏字段清单（读取侧，覆盖专用 API 不公开的字段）**：users → `password_hash`、`phone`、`phone_verified`、`labels`、`prefs`；sessions → `secret_hash`、`factors`、`user_agent`、`ip`、`country`；identities → `provider_data`、`provider_uid`。剔除后字段不参与返回（mapDocument 前过滤）。
- `app/client/databases.go`：`ensureCollectionForProject`（读路径）改为仅拒 users/sessions/identities；`ensureCollection`（写路径）保持 7 个全拒。

### 3.3 写策略（全量锁死）

- 文档写（CreateDocument/UpdateDocument/DeleteDocument/BulkUpdate/BulkDelete）：现有 `ensureCollection` 已覆盖，保持 7 个全拒。
- **新增 schema 级拦截**：`CreateCollection`（default 库 + 名单 → PermissionDenied）、`UpdateCollection`/`DeleteCollection`/`CreateAttribute`/`DeleteAttribute`/`CreateIndex`/`DeleteIndex` 统一接入系统集合拒绝（复用 `ensureCollection` 的拦截逻辑，在 `resolveProject` 之后、调 docDB 之前）。
- `UpdateCollection` 的 permissions 更新补 `ValidateGrantablePermissions` 校验（与文档路径对齐，app 层）。
- **adapter 层最小调整**：`systemCollectionsWriteProtected` 名单内容（3 个）与 owner 例外（postgres.go:429）不动，仅将 `isWriteProtectedSystemCollection` 判定**限定为 default 库**（与 §3.1 的"仅 default 库判定"一致，消除自定义库同名集合的文档写误拒）。7 集合写锁主体在 app 层 databases 用例实现（专用服务不经 databases 用例，故不受影响）。**不做**名单扩充。

### 3.4 Console 前端（只读态）

- `console/src/api/databases.ts`：`Collection` 类型加 `is_system: boolean`。
- `console/src/components/list/DataTable.tsx`：新增可选 `isRowSelectable?: (item) => boolean` prop（向后兼容，默认全可选），供系统集合行禁用勾选。
- `console/src/routes/databases/pages.tsx`：
  - `DatabaseDetailPage`：系统集合显示"系统"徽标；行删除、批量删除禁用；行勾选经 `isRowSelectable` 禁用。
  - `CollectionDetailPage`：系统集合时权限编辑、属性增删、索引增删进入只读态（禁用输入与提交按钮）。
  - `DocumentsListPage`：系统集合时隐藏"新建文档"与删除按钮（users/sessions/identities 在 admin 会话下同样只读可看——Console 会话为 PlatformAdmin，读已放行且脱敏）。
  - `DocumentDetailPage`：系统集合时隐藏删除按钮。
- `console/src/routes/databases/CollectionLayout.tsx`：系统集合时隐藏 DeleteButton，标题旁显示"系统"徽标。
- SDK：`sdk/typescript/src/types.ts`（`Collection` 类型所在文件）加 `is_system?: boolean`（透传）。

### 3.5 明确不做（控制影响范围）

- 不处理 CreateDatabase/CreateCollection 的事务性（建表与元数据非事务，另立 task）。
- 不处理 BulkUpdate/BulkDelete 循环单条的性能问题。
- 不处理 ListDocuments 前端分页。
- 不改专用服务（Users/Teams/Storage/Auth/Account）的任何代码。
- 不扩展 adapter 层写保护名单、不改 DDL 方法签名。
- 不做匿名 Guest 读收紧（§2.3 语义决策，后续可另立 task）。

## 4. 变更文件清单

| 文件 | 变更 |
|---|---|
| `db/migrations/000009_document_collections_is_system.up.sql` / `.down.sql` | 新增列 + 存量回填 |
| `internal/infra/bun/model/document.go` | `DocumentCollection.IsSystem` |
| `internal/domain/databases/document.go` | `Collection.IsSystem` |
| `internal/domain/databases/` （新增，如 `system_collections.go`） | 系统集合名单常量（ID 表） |
| `proto/server/v1/databases.proto` | `Collection.is_system = 11` |
| `genproto/` | `task generate-proto` 生成 |
| `internal/infra/documentdb/postgres.go` | mapCollectionRow/createCollectionMetadata 透传 IsSystem；`EnsureSystemCollections` 写 IsSystem（名单引用 domain 常量） |
| `internal/infra/documentdb/system_collection_specs.go` | 引用 domain 名单常量（去重） |
| `internal/app/server/databases.go` | 读/写分流；schema 级写拦截（含 CreateCollection）；PlatformAdmin 分级；脱敏；黑名单限定 default 库；UpdateCollection 权限校验 |
| `internal/app/client/databases.go` | 读路径仅拒 3 个高敏集合；名单引用 domain 常量 |
| `console/src/api/databases.ts` | `Collection.is_system` |
| `console/src/components/list/DataTable.tsx` | `isRowSelectable` prop |
| `console/src/routes/databases/pages.tsx` | 徽标/只读态/禁删 |
| `console/src/routes/databases/CollectionLayout.tsx` | 隐藏删除 + 徽标 |
| `sdk/typescript/src/types.ts` | `is_system?: boolean` |
| 测试（新增） | 见 §6 |

## 5. 实施步骤（子代理执行顺序）

1. migration 000009（含回填）+ bun model + domain（Collection.IsSystem + 名单常量）+ proto（`task generate-proto`）。
2. documentdb 适配器：IsSystem 透传（名单引用 domain 常量）。
3. app/server/databases.go 读写分流 + schema 级拦截 + 脱敏 + 校验。
4. app/client/databases.go 读路径调整。
5. `task build` 编译通过（wire 无新 provider，无需 wire-all；如编译报错再执行 `task wire-all`）。
6. 集成测试（新增用例）→ `task test` 全绿。
7. Console：类型 + DataTable + 3 个文件 → `task console-build` → `task build`（Go embed 打包新前端）。
8. SDK 类型透传。

## 6. 测试计划

- app/server 集成测试（新增 `internal/app/server/system_collections_readonly_test.go`）：
  - 用户集合（is_system=false）行为不变：schema 级与文档级操作照常；自定义 database 中 `id=users` 集合可正常创建与读写（黑名单限定 default 库）。
  - 系统集合文档读：teams 等 4 个放行（keys 主体）；users 文档读：keys 拒绝 / PlatformAdmin 允许且**脱敏断言**（password_hash/factors/provider_data 等不在返回 data 中）。
  - 系统集合文档写（Create/Update/Delete/Bulk）拒绝。
  - schema 级：CreateCollection（default+名单）、UpdateCollection/DeleteCollection/CreateAttribute/DeleteAttribute/CreateIndex/DeleteIndex 全部 PermissionDenied。
  - UpdateCollection 授予未持有角色（如 `read:any` 之外的写类）时 InvalidArgument。
- app/client 集成测试：teams/buckets 读放行（匿名 read:any / 用户 read:user:{id}）；users 读/写全拒；自定义集合行为不变。
- **专用服务回归**（adapter 未改动的保险）：Teams CreateMembership/UpdateMembership、Storage CreateFile/DeleteBucket 以用户 principal 路径仍正常（现有集成测试覆盖，`task test` 全量回归即可）。
- 回填断言：migration 后存量项目系统集合 `is_system=true`（测试前置 EnsureSystemCollections 后 GetCollection 断言）。
- `docs/manual-acceptance-checklist.md`：§9 DynamicDocuments 增补系统集合只读/拒绝断言条目。
- 回归：`task test`、`task build`、`task console-build`。

## 7. 风险与回滚

- **专用服务依赖 adapter 例外**：adapter 层完全不动（名单 3 个、owner 例外保留）→ account.go/teams.go/storage.go 零影响。
- **脱敏遗漏风险**：users/sessions/identities 的读仅对 databases API 生效，且仅 PlatformAdmin 可读；专用 UsersService 已脱敏（mapUserDoc），无遗漏路径。
- **回滚**：is_system 列 DEFAULT FALSE，回滚迁移不影响存量数据；app 层逻辑回退即可恢复旧行为（系统集合回到全拒，比现状更安全）。
- **行为变化记录**：Client API 对 teams/buckets/files 文档读从"拒绝"变为"按集合权限放行"（匿名可读 read:any 内容），属集合权限显式语义（§2.3 明示）。

## 8. 变更日志（v1 → v2，独立审查修订项）

| # | 审查发现 | 修订 |
|---|---|---|
| 1 | adapter 名单补全会切断 Teams/Storage 用户路径（teams.go:204/254、storage.go:128/169 均以调用方 principal 写系统集合） | §3.3 取消名单补全，adapter 维持现状；写锁全部在 app 层 databases 用例 |
| 2 | 存量项目 is_system 不会回填（EnsureSystemCollections 对已存在集合 continue） | migration 000009 内直接回填 |
| 3 | 脱敏清单不完整（sessions.factors / identities.provider_data 等） | §3.2 扩展为三集合完整敏感字段清单 |
| 4 | CreateCollection 未纳入拦截（default 库同名集合） | §3.3 新增拦截 |
| 5 | SDK 类型位置错误；名单常量放 infra 违反 Clean Architecture | SDK 改 `types.ts`；名单常量移入 `domain/databases` |
| 6 | Console 行勾选无法按行禁用（DataTable selectable 为整体开关） | DataTable 加 `isRowSelectable` prop；文件清单补 DataTable.tsx |
| 7 | memberships 无 read:any，表述不精确；匿名 Guest 读扩大未明示 | §2.3 修正 + 明示语义决策 |
| 8 | 测试计划缺专用服务回归与 manual checklist | §6 增补 |
| 9 | 实现期：adapter 名单全局匹配导致自定义库 `id=users` 文档写被误拒（子代理按硬性约束保留，与 §3.1 本意冲突） | §3.3 修订：名单内容不动，判定加 default 库限定；对应测试按"自定义库可写"断言 |
| 10 | 验收期（独立复核）：脱敏 `redactSensitiveCollectionData` 仅按 collectionID 匹配，自定义库同名集合被错误剔除字段 | 增加 `IsSystemCollection(projectID, databaseID, collectionID)` 判定，脱敏仅对 default 库生效；server 测试补"自定义库同名集合不脱敏"断言 |
| 11 | 验收期（独立复核）：client 测试过期注释（声称写被 adapter 全局匹配拒绝，与 default 限定实现矛盾）+ 自定义库 users 文档写无直接断言 | 修正注释，补 CreateDocument 自定义库 users 成功断言 |
