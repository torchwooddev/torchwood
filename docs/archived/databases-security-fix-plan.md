# Databases 安全审查修复方案（Databases Security Fix Plan）

> [!WARNING] 已作废归档（ARCHIVED）
> 归档日期：2026-08-09
> 归档原因：Phase A（A1-A8）与 Phase B（B1/B2）均已实现并通过全量回归，本文档为历史实施记录，内容已被代码吸收（`pkg/grpc/interceptor/apikey_scope.go` 重写、B1 文档级优先语义、migration 000004/000007/000008 等）。
> 后续信息源：`internal/domain/databases/permissions.go`、`internal/infra/documentdb/`、`pkg/grpc/interceptor/apikey_scope.go` 及其测试。

> 状态：`v1.2 已确认`（2026-08-08：v1.1 复核落实 6 个方案级问题；v1.2 修订复核新发现问题 A–E，审查确认后可进入实现）
> 实施状态：**已实现**（Phase A 于 2026-08-08 完成；Phase B（B1/B2）于 2026-08-08 完成，实施记录见 §9）
> 适用范围：Databases / Collections / Documents 子系统的安全与健壮性修复
> 依据：两轮安全审查（问题编号 W1–W12、N1–N10），以及第一轮功能缺口方案
> `docs/databases-fix-plan.md`（v1.1 已实现）
> 本文档是实现的唯一依据；实现完成后回填实际变更与验收结果。

## 0. 背景与问题总览

对 Databases 子系统完成两轮安全审查（传输层 / 应用层 / 领域层 / 适配层四层核对），确认的安全与健壮性问题分级如下：

| 问题 | 描述 | 位置 | 修复阶段 |
|---|---|---|---|
| W1 | documentSecurity 采用"集合级 OR 文档级"语义 + 默认 `read:any`/`update:users`，文档级权限形同虚设（"私有"文档全公开） | `internal/domain/databases/permissions.go:86-99` | **B1** |
| W2 | API Key scope 前缀匹配（`HasPrefix(resource+".")`）且资源粒度=整个 service，`databases.read` 可调全部写方法 | `pkg/grpc/interceptor/apikey_scope.go:28-32` | **B2** |
| W3 | `UpdateCollection` 传空 permissions 重置为全公开默认 | `permissions.go:129-132`、`servergrpc/databases.go:170-176` | C |
| W4 | bucket 上传不校验 bucket create/write 权限 | `internal/app/storage/storage.go:148-154` | C |
| W5 | 列表权限过滤 EXISTS 缺 `p._tenant = d._tenant` 关联 | `internal/infra/documentdb/postgres_permissions.go:106-110` | **A5** |
| W6 | `databases` scope 的 key ≈ 项目超级用户（keys 角色默认全 CRUD + privileged 跳过授予校验） | `permissions.go:197-200` | C（B2 落地后重新评估） |
| W7 | TOCTOU：权限检查与数据写入无事务 | `postgres.go:479-510` | C（A4 部分缓解） |
| W8 | GetDocument 先取行后校验，错误码可探测文档存在性 | `postgres.go:435-459` | C |
| W9 | `principalOrSystem` 空角色 fail-open | `internal/app/server/teams.go:522-527` | C |
| W10 | 系统集合纵深防御不均（docDB 层仅保护 3 个集合） | `postgres.go:40-52` | C |
| W11 | memberships 文档权限面过宽（`update/delete:team:<id>`） | `internal/app/server/teams.go:466-486` | C |
| W12 | 用户可控 `label:` 角色自授 | `internal/app/client/user_roles.go:30-35` | C |
| N1 | 分页负数绕过上限：`page_size=-1` / DSL `limit(-1)` 透传 → `LIMIT -1` 全表返回 | `postgres.go:578-587` | **A1** |
| N2 | Bulk 无条数上限、非事务、逐条循环部分生效 | `postgres_permissions.go:137-179` | **A4** |
| N3 | queries 条数 / equal 数组值 / 查询串长度无上限 | `proto/server/v1/databases.proto:257`、`postgres.go:1293-1299` | **A2** |
| N4 | search 无索引约束（任意列 seqscan + 逐行 to_tsvector）；敏感列可做过滤探测 oracle（响应脱敏 ≠ 查询脱敏）；未知列透传 PG 500 | `postgres.go:1332-1334`、`docdb_errors.go:12-22` | **A7** |
| N5 | EnsureSystemCollections 元数据 INSERT 无 ON CONFLICT，多实例并发首请求 23505 | `postgres.go:756-766, 1024-1057` | **A3** |
| N6 | bootstrapCache/keysPermsCleaned 无失效机制 | `postgres.go:747-791` | C |
| N7 | 审计缺资源维度：`WithAuditResource` 零调用、ResourceID 恒空 | `pkg/grpc/interceptor/audit.go:22-61` | **A8** |
| N8 | 文档 CRUD 全部直用 `p.db.DB`（autocommit），与元数据/DDL 的 `p.conn(ctx)` 不一致；RunInTx 假事务隐患 | `postgres.go:416/444/496/539/653` | **A4** |
| N9 | idgen `resources.documents/sessions` 死配置 | `internal/infra/idgen/service.go` | C |
| N10 | 错误映射仅 2 类，PG 原始错误（22P02/42703/22001/23502）直出客户端 500 | `internal/app/shared/docdb_errors.go:12-23` | **A6** |

## 1. 决策点

| 编号 | 决策 | 结论 |
|---|---|---|
| D1 | W1 修复后"匿名读 teams/buckets/files"（集合级 `read:any`）是否保留 | **保留**：系统集合（`coll.IsSystem`）豁免文档级覆盖，维持 OR 语义；仅用户集合改为文档级优先。理由：匿名读系统集合是现有产品功能；`system_collections_readonly_test.go:20-63` 的匿名读测试文档为 nil perms（无 `_perms` 行，`docHasPerms=false → collOK` 兜底，即使无豁免也通过），**豁免真正保障的是显式 permissions（不含 `read:any`）的系统集合文档**（teams/files 文档**默认**权限模板含 `read:any`，但调用方可显式指定不含 `read:any` 的 permissions，此时需集合级兜底） |
| D2 | B2 scope 语义收紧（`databases.read` 从"全方法"变"仅读方法"） | **实施**，属预期破坏性变更；seed 默认 key（`cmd/seed/main.go:88`）签发的 read/write 细分 scope 在新语义下语义精确化，无需改签发数据 |
| D3 | B1 后 `UpdateDocument` 是否仍强制 read 预检（`:479-484` 先查 read 再查 update） | **去掉 read 预检，仅检查 update 权限**。理由：对齐 Appwrite/Supabase 语义（update 策略独立于 select 策略）；B1 文档级优先下"仅持 update 权限"的文档对持权者不可用（自锁）是错误行为。该改动对系统模块无影响（SystemPrincipal 短路） |
| D4 | A7 响应脱敏清单（`app/server/databases.go:21-23` 含 phone/phone_verified/factors/user_agent/ip/country/provider_uid）与查询侧黑名单不对称 | **记录并接受**：查询侧仅封凭据/令牌类列（password_hash/secret_hash/prefs/labels/provider_data）；phone 等 PII 管理列保留可查（keys/admin 语义下合法管理查询），响应侧继续脱敏。发布说明标注此不对称 |

## 2. Phase A 修复项（低破坏，先实施）

### A1 分页/limit/offset 校验

**现状与证据**：`pkg/query/query.go:123-141` 解析期不校验正负；`postgres.go:578-587` ListDocuments 中 `limit == 0` 才回退默认，负数（`page_size=-1` 或 DSL `limit(-1)`）透传 → PG `LIMIT -1` = 无上限返回全集合。`ListCollections`（`:184-190`）已是 `<=0 → 50`，两处不对称。offset 无正数上限（`offset(10^9)` 深翻页拖慢查询）。

**方案**：
1. `pkg/query/query.go`：`limit`/`offset` 解析后 `n < 0` → `fmt.Errorf("limit/offset must be non-negative")`（解析期 fail-fast）。
2. `postgres.go` ListDocuments：`if limit <= 0 { limit = 50 }`（防御兜底，覆盖 page_size 负数）。
3. 新增 `const maxQueryOffset = 10000`；`offset > maxQueryOffset` → `status.Error(codes.InvalidArgument, ...)`（ListDocuments 与 CountDocuments 共用计算路径）。
4. 保持 `limit > maxQueryLimit(100)` clamp 不变。

**涉及文件**：`pkg/query/query.go`、`internal/infra/documentdb/postgres.go`
**测试**：`pkg/query/query_test.go` 新增：`limit(-1)`/`offset(-1)` 报错；`postgres_test.go` 新增：`page_size=-1`、DSL `limit(-1)` 均回退默认页大小；`offset` 超上限报 InvalidArgument。
**风险**：低。现有测试与调用方全为正值（已核实）。

### A2 输入上限（queries 条数 / 查询串长度 / equal 数组值）

**现状与证据**：`ListDocumentsRequest.queries` 为 `repeated string`（`proto/server/v1/databases.proto:257`、client :132）无条数限制；`buildAppwriteQuery` 多值 `IN (?,?,...)` 无值数限制，超 PG 65535 参数上限 → 500；单条查询串长度无限制。

**方案**（`internal/infra/documentdb/postgres.go` 新增常量并在 `ListDocuments`/`CountDocuments` 入口校验）：
1. `const maxQueryCount = 100`、`const maxQueryStringLen = 4096`、`const maxFilterValues = 1000`。
2. `len(q.Queries) > maxQueryCount` → InvalidArgument；任一条 `len(raw) > maxQueryStringLen` → InvalidArgument。
3. `buildAppwriteQuery`：`equal`/`notEqual` 多值 `len(values) > maxFilterValues` → 返回 error。

**涉及文件**：`internal/infra/documentdb/postgres.go`
**测试**：`postgres_test.go` 新增：101 条 queries、超长查询串、1001 个 equal 值均报 InvalidArgument。
**风险**：低。正常调用远低于上限。

### A3 EnsureSystemCollections / createCollectionMetadata 幂等化

**现状与证据**：DDL 全部 `IF NOT EXISTS` 幂等（`postgres.go:857/864/903/924-929`），但元数据 INSERT 无 ON CONFLICT：`document_databases`（`:762-765`）、`createCollectionMetadata` 三处（`:1024/1042/1057`）。双实例并发首请求 → 23505 → 请求失败。
PK/UNIQUE 约束（`db/migrations/000003_document_catalog_composite_keys.up.sql`）：
- `document_databases` PK `(project_id, id)`
- `document_collections` PK `(project_id, database_id, id)`；UNIQUE `(project_id, database_id, name)`
- `document_attributes` PK `(project_id, database_id, collection_id, id)`；UNIQUE `(project_id, database_id, collection_id, key)`
- `document_indexes` PK `(project_id, database_id, collection_id, id)`

bun `InsertQuery.On("CONFLICT (...) DO NOTHING")` 与 `Exec` 结果的 `RowsAffected()` 可用（bun v1.2.18 已核实）。

**方案**：
1. **仅集合行 INSERT 用 DO NOTHING + 行数判断**（子表 attrs/indexes 保持报错语义，避免同 key 冲突 rows=0 语义未定义问题）：
   - `createCollectionMetadata` 集合行（`:1024`）改 `On("CONFLICT (project_id, database_id, id) DO NOTHING")`；`RowsAffected()==0` 时：若 `databases.IsSystemCollection(projectID, databaseID, collectionID)`（`system_collections.go:45-47`）→ 幂等成功，返回 nil 并跳过子表插入（**已知窗口**：并发窗口极小，仅当先到者插入集合行后崩溃才遗留 attrs/indexes 子表缺失，属人工可修复的极端场景，记录于日志）；否则 → 返回 `ErrDuplicateKey`（→ A6 映射 AlreadyExists）。
2. `EnsureSystemCollections` 中 `document_databases` INSERT（`:763`）加 `On("CONFLICT (project_id, id) DO NOTHING")`。
3. 顺序说明：`ErrDuplicateKey` → `AlreadyExists` 是**既有映射**（`docdb_errors.go:19-21`），A3 落地后用户重名集合自动返回 409，不依赖 A6。

**涉及文件**：`internal/infra/documentdb/postgres.go`
**测试**：`postgres_test.go` 新增：重复调用 `EnsureSystemCollections` 幂等；用户 `CreateCollection` 建同名集合经 `MapDocumentDBError` 后为 AlreadyExists。
**风险**：低。正常路径 RowsAffected=1 行为不变。

### A4 文档 CRUD 事务连接统一 + Bulk 事务化 + 上限

**现状与证据**：`p.conn(ctx)`（`internal/infra/clients/tx.go:17-22`，tx 感知）只用于元数据/DDL；文档 CRUD 直用 `p.db.DB`（`postgres.go:416/444/496/539/624/646/653/731`、`postgres_permissions.go:32`、setPermissions/clearPermissions）→ 上层 `RunInTx` 内调用文档操作逃逸事务。`BulkUpdateDocuments`/`BulkDeleteDocuments`（`postgres_permissions.go:137-179`）逐条循环、非事务、无上限。bun `IDB` 内嵌 `IConn`，`QueryContext/ExecContext/QueryRowContext` 全部可用（已核实）。

**方案**：
1. **统一连接来源**：下列调用改为 `p.conn(ctx)`（无 tx 时行为不变）：
   - `postgres.go`：CreateDocument INSERT（`:416`）、GetDocument（`:444`）、UpdateDocument（`:496`）、DeleteDocument（`:539`）、ListDocuments cursor SELECT（`:624`）/COUNT（`:646`）/主查询（`:653`）、CountDocuments（`:731`）、setPermissions/clearPermissions（约 `:1122/:1146`）、DeleteAttribute DDL（`:190`）、DeleteIndex DDL（`:208`）。
   - `postgres_permissions.go`：`getDocumentPermissions`（`:32`）。
2. **Bulk 事务化**：`BulkUpdateDocuments`/`BulkDeleteDocuments` 用 `p.db.RunInTx` 包裹循环（内部 UpdateDocument/DeleteDocument 已统一 conn，含尾随 GetDocument `:511`，整体在同一 tx；出错整体回滚）。
   - **嵌套防护**：`internal/infra/clients/tx.go` 新增 `func InTx(ctx context.Context) bool`；Bulk 入口若 `clients.InTx(ctx)` 则不再嵌套（直接执行，复用外层事务）。
3. **Bulk 条数上限**：`const maxBulkOperations = 1000`；app 层（`internal/app/server/databases.go` Bulk 用例）提前校验 → InvalidArgument。

**涉及文件**：`internal/infra/clients/tx.go`、`internal/infra/documentdb/postgres.go`、`postgres_permissions.go`、`internal/app/server/databases.go`
**测试**：`postgres_test.go` 新增：
- Bulk 1001 条 → InvalidArgument（app 层）；
- **Bulk 中途失败整体回滚**：第 2 条为**不存在的文档 ID**（UpdateDocument 尾随 GetDocument 返回 nil → error，`postgres.go:515-517`）→ 断言第 1 条的更新也未生效（整体回滚）；
- `clients.InTx` 单测。
**风险**：中。连接统一是行为不变的重构（现有 RunInTx 调用方仅 `projects.go:71`，内部全走 conn，自洽）；Bulk 从"部分成功"变"原子成功"——**行为收紧**（含"不存在文档导致整体回滚"），现无调用方依赖部分成功。
**边界注明**：`CreateDatabase/DeleteDatabase/DeleteCollection` 的 schema DDL（`postgres.go:81/141/270/273`）不在本次统一范围，仍走 `p.db.DB`——若未来有调用方在 tx 内执行这些操作会逃逸事务，由调用方自行约束（当前无此调用方）。

### A5 列表权限过滤补 `_tenant` 关联

**现状与证据**：`postgres_permissions.go:106-111` 的 EXISTS 子查询只按 `_collection + _document + _type + _permission` 匹配，缺 `p._tenant = d._tenant`（单文档路径 `:33` 有 tenant 过滤）。`_perms` 索引首列为 `_tenant`（`:877/881`），追加谓词不伤索引。

**方案**：EXISTS 子查询 WHERE 追加 `p._tenant = d._tenant`（参数顺序不变：`collectionID, pgTextArray(expanded)`）。

**涉及文件**：`internal/infra/documentdb/postgres_permissions.go`
**测试**：`permissions_test.go` 新增：不同 tenant 同 collection 同文档 ID 的 `_perms` 行不影响本租户列表可见性。
**风险**：低。

### A6 错误映射扩展

**现状与证据**：`internal/app/shared/docdb_errors.go:12-23` 仅映射 ErrPermissionDenied/ErrDuplicateKey。`pgdriver.Error` 有 `Field(byte) string` 方法（`'C'`=SQLSTATE，pgdriver@v1.2.18 已核实）；go.mod 已有 pgdriver 依赖，`internal/app/shared` 新增 import 无循环依赖；`isUniqueViolation`（`postgres.go:1238-1244`）已用 `errors.As` 穿透 `fmt.Errorf %w` 链，生产路径证明有效。

**方案**：`MapDocumentDBError` 增加对最小接口 `interface{ Field(byte) string }` 的 `errors.As` 类型匹配（**仅类型匹配，不做 SQLSTATE 字符串回退**——现有 `docdb_errors_test.go:19-21` 断言普通错误原样透传，字符串回退会破坏该测试）。`pgdriver.Error` 天然满足该接口（`'C'` 字段=SQLSTATE，pgdriver@v1.2.18 已核实；其字段 map 未导出、无导出构造函数，**单测不得直接构造 pgdriver.Error**）：
- `22P02`/`22001`/`23502`/`42703`/`42601`/`23503`/`42883` → `codes.InvalidArgument`
- `53100`/`53200`/`54000`/`53400` → `codes.ResourceExhausted`

**涉及文件**：`internal/app/shared/docdb_errors.go`
**测试**：
- `docdb_errors_test.go`：新增用**本地 stub 类型**（实现 `Field(byte) string`）断言映射；现有 :19-21 原样透传断言保持；
- 集成断言走 **app 层**（adapter 直调返回 raw PG error，映射在 app 层生效）：`internal/app/server/databases_integration_test.go` 新增写入类型不匹配 → gRPC InvalidArgument；adapter 层 `postgres_test.go` 仅断言错误链可 `errors.As` 到 `*pgdriver.Error`。
**风险**：低。错误码从 500 变 400 属改善。

### A7 查询字段白名单 + 敏感列黑名单 + search 索引约束

**现状与证据**：
- 未知列：字段过 `safeNameRe` 但表无此列 → PG 42703 透传 500（`postgres.go:1287`）。
- 敏感列 oracle：`server ListUsers` 把用户 queries 直传 docDB（`internal/app/server/users.go:41-50`），keys 角色因集合级 `read:keys` 跳过文档级过滤（`postgres_permissions.go:100-102`），可对 `users.password_hash`/`prefs` 做 `equal`/`search` 过滤探测；响应已脱敏（`internal/api/servergrpc/users.go:104-125`）但过滤侧未脱敏。
- search：`to_tsvector('simple', col::text)`（`postgres.go:1332-1334`）任意列可用（含 jsonb），未索引列 seqscan + 逐行 to_tsvector（CPU DoS）；系统集合仅 `files.name_fulltext`（`system_collection_specs.go:129`）；**全仓 Go/TS 零处 `search(` 调用**（已核实），收紧无回归。
- 系统模块查询字段（email/user_id/team_id 等）全部为已声明 attr（已逐一核实），白名单不误伤。

**方案**（`internal/infra/documentdb/postgres.go`）：
1. **白名单校验仅作用于非 System 路径**（System 信任内部调用，避免 session 校验/登录等热路径增加元数据查询）：
   - `ListDocuments`/`CountDocuments` 中把集合获取（现有非 System 路径经 `listPermissionFilter` 的 `getCollectionForAccess`）提升为显式一次 `GetCollection`，结果**复用**传入 `listPermissionFilter`（消除重复查询）；coll==nil 且非 System → `status.NotFound "collection not found"`（**行为变更**：现有非 System 路径为 `ensureCollectionAccessible(nil)` → `ErrPermissionDenied` → 403，改动为 404，发布说明标注；System 路径保持 42P01 → 500 现状不动）。
   - 新增 `validateQueryFields(parsed *query.Query, coll *databases.Collection, collectionID string, isSystem bool) error`：
     - Filters/Orders/Selects 字段（`mapQueryField` 映射后）不在 allowedFields（系统列 + attrs key）→ InvalidArgument "invalid query field"；
     - 命中敏感列 → InvalidArgument "field is not queryable"；
     - `search` 字段不在 fulltext 索引 Attributes 中（`Type` 比较用 `strings.ToLower`，容忍 "FULLTEXT"）→ InvalidArgument；
     - **顺带**：ORDER 路径非法字段从静默跳过（`:1355-1356`）改为显式报错，与 cursor 路径（`:617-619`）一致。
2. 敏感列黑名单（**仅限 default 库系统集合**，按 `databases.IsSystemCollection(projectID, databaseID, collectionID)` 限定，避免自定义库同名集合误伤——`system_collections_readonly_test.go:160-180` 证实自定义库可建同名集合）：
   ```go
   var sensitiveQueryFields = map[string]map[string]struct{}{
       "users":      {"password_hash": {}, "prefs": {}, "labels": {}},
       "sessions":   {"secret_hash": {}},
       "identities": {"provider_data": {}},
   }
   ```
   （凭据/令牌类列，任何角色禁止作为过滤条件；phone 等 PII 列按 D4 决策保留可查。）

**涉及文件**：`internal/infra/documentdb/postgres.go`
**测试**：`postgres_test.go` 新增：非 System 查询未声明列 → InvalidArgument；`equal("password_hash",...)`（users 集合）→ InvalidArgument；`search("name",...)` 对无 fulltext 索引集合 → InvalidArgument、对 files.name → 可用；order 非法字段 → InvalidArgument；SystemPrincipal 查询白名单外字段 → 不受限（信任路径）。
**风险**：中。行为收紧（未知列 500→400、search 需索引列）；System 热路径零额外查询。

### A8 审计资源维度

**现状与证据**：`contexts.WithAuditResource`（`internal/pkg/contexts/principal.go:31`）零调用 → 审计 `ResourceID` 恒空（`audit.go:54` 读取；`domain/audit/audit.go:14` 字段存在；testutil 有 `AuditLogCount/LatestAuditLog`）。

**方案**：在 databases gRPC handler（servergrpc **21 个** + clientgrpc **6 个** = 27 个方法）中为方法开头注入 `ctx = contexts.WithAuditResource(ctx, resource)`：
- 文档方法：`databases/{database_id}/collections/{collection_id}/documents/{document_id}`；
- schema 方法：`databases/{database_id}/collections/{collection_id}`；
- 库方法：`databases/{database_id}`；List/Count 用集合级资源。

**涉及文件**：`internal/api/servergrpc/databases.go`、`internal/api/clientgrpc/databases.go`
**测试**：新增集成断言：CreateDocument 后 `LatestAuditLog().ResourceID` 非空且格式正确。
**风险**：低。纯增量字段。

## 3. Phase B 修复项（语义变更，审查确认后实施）

### B1 documentSecurity 文档级优先语义（W1）

**现状与证据**：`AllowsDocumentAccess`（`internal/domain/databases/permissions.go:86-99`）为"集合级 OR 文档级"；默认集合权限含 `read:any` + `update/delete:users`（`:14-29`），客户端"私有"文档（`ownerDocumentPermissions`，`client/databases.go:249-256`）实际全公开可读、全体登录用户可改删；列表路径 `SkipDocumentPermissionFilter`（`:115-120`）集合级 read 命中即跳过逐文档过滤。

**目标**：用户集合文档级优先（Appwrite 语义：documentSecurity=true 且文档有 `_perms` 时文档权限覆盖集合权限）；系统集合豁免（D1）。

**方案**：
1. `permissions.go` `AllowsDocumentAccess`：
   ```go
   if coll == nil { return false }
   expanded := ExpandPermissionRoles(roles)
   collOK := CollectionAllows(coll.Permissions, permType, expanded)
   if !coll.DocumentSecurity { return collOK }
   if !docHasPerms { return collOK }
   if coll.IsSystem { return collOK || CollectionAllows(docPerms, permType, expanded) } // D1 豁免
   return CollectionAllows(docPerms, permType, expanded) // 用户集合：文档权限覆盖
   ```
2. `permissions.go` `SkipDocumentPermissionFilter`：仅当（系统集合且集合级有 read）或（`!coll.DocumentSecurity` 且集合级有 read）时跳过；用户集合 documentSecurity=true 一律逐文档过滤。
3. `postgres_permissions.go` `listPermissionFilter`：
   - 集合级有 read（用户集合 + documentSecurity=true）：
     ```sql
     (EXISTS (SELECT 1 FROM %s p WHERE p._tenant = d._tenant AND p._collection = ? AND p._document = d._id AND p._type = 'read' AND p._permission = ANY(?::text[]))
      OR NOT EXISTS (SELECT 1 FROM %s p2 WHERE p2._tenant = d._tenant AND p2._collection = ? AND p2._document = d._id))
     ```
     （文档有 `_perms` 须匹配；无 `_perms` 由集合级 read 兜底——与 `AllowsDocumentAccess` 一致）
   - 集合级无 read：仅 EXISTS（现有行为）；
   - 系统集合 + 集合级有 read：跳过过滤（现状，D1 豁免）。
   - **A5 的 tenant 关联在两个子查询同步应用**。
4. **`UpdateDocument` 权限检查调整（决策 D3）**：`postgres.go:479-484` 去掉 read 预检，仅保留 update 检查（对齐 Appwrite：update 不要求 read）。
5. **自锁修复**：`UpdateDocument` 尾随 `GetDocument(ctx, ..., principal)`（`:511`）改为 `SystemPrincipal` 读回（与 `CreateDocument:425` 模式一致）——否则用户在 B1 下把文档权限改成不含自己 read 的集合后，数据已提交但调用返回 PermissionDenied（半完成状态）。
6. `DefaultCollectionPermissions` 保持不变；"私有文档"语义由文档级优先保证。
7. **一致性验证项**：`_perms` 表可能存在 `_type='write'` 行（`ParsePermissionStrings` 会展开为三行，但直调 adapter 的路径可能不展开）；单文档路径 `matchTypes`（`permissions.go:228-235`）使 create/update/delete 检查命中 write 行，列表过滤只匹配 `_type='read'`——两者语义一致（write 不隐含 read），实现时补充测试确认该一致性，不改变行为。

**涉及文件**：`internal/domain/databases/permissions.go`、`internal/infra/documentdb/postgres_permissions.go`、`internal/infra/documentdb/postgres.go`
**测试（重写/新增）**：
- `permissions_test.go:32-55`（OR 语义断言）→ 重写：集合 `read:any` + 文档 `user:bob` → alice 拒、bob 放行；系统集合（IsSystem=true）场景 → OR 语义保持；
- `permissions_test.go:57-66`（docSecurity=false）→ 保持通过；
- `internal/app/server/databases_integration_test.go:246-289` `TestDatabases_CreateDocument_PermissionTemplates` → **重写**：场景改为文档权限含 `update:user:alice` 时才能更新权限（B1 下"仅 read 权限即改权限"应被拒——文档级优先的正确行为）；
- `internal/infra/documentdb/permissions_test.go`：`TestPermissions_CollectionLevelFallback`（nil perms 文档）保持通过（NOT EXISTS 兜底）；keys/PlatformAdmin 用例回归；
- `internal/app/client/databases_integration_test.go`：用户集合"私有文档"匿名读拒、他用户改删拒；owner 可读写删；
- `system_collections_readonly_test.go:20-63`：匿名读 teams/buckets 保持通过（豁免验证）；
- `internal/app/client/account.go` 路径（UpdateAccount/UpdatePrefs 以 `user:<id>` 更新自己 users 文档）：users 是系统集合（豁免 + owner 例外），回归确认。
**风险**：中。语义收紧属安全改善；D1 豁免保护匿名读；D3 去 read 预检与 D1/D5 组合需全量回归权限测试。

### B2 API Key scope 精确匹配 + 方法级读写细分（W2）

**现状与证据**：`APIKeyScopeAllowed`（`pkg/grpc/interceptor/apikey_scope.go:12-34`）前缀匹配 + service 级资源（`:36-67`）。**核实修正**：`ACCESS_API_KEY` 服务为 7 个（Databases/Users/Teams/Storage/Projects/OAuthProviders/APIKeys），**HealthService 是 `ACCESS_PUBLIC`**（`proto/server/v1/health.proto:11`）——现有 `health` 资源映射为死代码，可移除或保留（保留不产生放行）。`IsAPIKeysServiceMethod` 禁调逻辑保留。

**目标**：精确匹配 + read/write 细分；通配符不破坏"未映射服务 fail-closed"安全属性。

**方案**：
1. `apikey_scope.go` 重写匹配（**通配符在 `resource==""` 之后判断**，保持"新增 ACCESS_API_KEY 服务默认拒绝"属性）：
   ```go
   func APIKeyScopeAllowed(fullMethod string, scopes []string) bool {
       resource, op := apiKeyScopeResource(fullMethod)
       if resource == "" { return false } // 未映射服务 fail-closed（含 * / all）
       for _, s := range scopes {
           if s == "*" || s == "all" { return true }
           if s == resource { return true }
           if op == "read" && s == resource+".read" { return true }
           if op == "write" && s == resource+".write" { return true }
       }
       return false
   }
   ```
2. 方法→(resource, op) 显式映射表（替换 `apiKeyScopeResource` 的 service 级实现，覆盖 7 个 ACCESS_API_KEY 服务全部方法）：
   - DatabasesService：`ListDatabases/GetDatabase/ListCollections/GetCollection/ListDocuments/GetDocument/CountDocuments` → read；其余 14 个（Create/Delete/Update 类 + Bulk）→ write。
   - UsersService/TeamsService/StorageService/ProjectsService/OAuthProvidersService/APIKeysService：读方法（List/Get/Count 类）→ read，其余 → write；APIKeysService 仍禁 API key 调用（IsAPIKeysServiceMethod）。
3. `internal/api/serverhttp/file_handler.go:241-251` 改用新映射表统一来源（GetFile→read、CreateFile→write）。
4. scope 格式校验（`internal/app/server/apikeys.go`，**仅 Create**——核实无 UpdateAPIKey 方法）：每个 scope ∈ {`*`, `all`, 裸资源名, `<resource>.read`, `<resource>.write`}，否则 InvalidArgument；上限 32 项、每项 ≤64 字符。
5. seed（`cmd/seed/main.go:88`）scope 清单不变。

**涉及文件**：`pkg/grpc/interceptor/apikey_scope.go`、`internal/api/serverhttp/file_handler.go`、`internal/app/server/apikeys.go`、`console/src/routes/api-keys/pages.tsx`（文案提示，可选）
**测试**：
- `apikey_scope_test.go` 重写受影响用例：`:54-56`（`storage.read` → CreateFile 从放行改拒绝）、`:39-42`（未映射服务 + `*` → 拒绝，保持）；新增：`databases.read` → ListDocuments 放行 / DeleteDatabase 拒绝；`databases.write` 反向；裸 `databases` 全放行；`storage.read` → GetFile 放行 / CreateFile 拒绝；
- `jwt_auth_test.go:33-37` **无需调整**（走 client TeamsService 的 permissionMethods 路径，不触 scope 匹配）；
- `file_handler_integration_test.go:358-412`（"users" scope 上传 403）回归（新语义仍 403）；
- `p0_acceptance_test.go:93`（裸 "users" scope + ListUsers）回归（裸资源全放行）。
**风险**：中（破坏性语义变更：`databases.read` key 不再能写、`storage.read` key 不再能上传）。存量 key 均 read+write 成对签发（seed/测试），放行面完整；发布说明标注。

## 4. Phase C 延后项（记录原因）

| 项 | 原因 |
|---|---|
| W3 UpdateCollection 空 permissions → 全公开默认 | 与 Appwrite 行为一致；"空=默认"语义已被 Console 依赖；改为"空=私有"需前端空态区分，另立专项 |
| W4 bucket ACL 强制（上传校验 bucket create/write） | 跨 storage 子系统，需同步调整 bucket 文档权限模板与现有测试，另立专项 |
| W6 keys 特权收窄 | B2 落地后 key 的操作面已收窄到方法级；文档级 `keys` 角色语义保留（系统模块依赖 keys 读），重新评估后决定 |
| W7 TOCTOU | A4 事务化后同事务内检查-写入仍分离；完全修复需行级锁或乐观锁（`_rev` 列 + 条件 UPDATE），另立专项 |
| W8 文档存在性探测 | A6 后错误码更规范；文档 ID 枚举仅影响存在性（需文档级权限方可读），可接受 |
| W9 `principalOrSystem` fail-open | 当前拦截器保证 Principal 非空；内部调用路径均显式传 principal，风险低 |
| W10 系统集合纵深防御不均 | 简单扩展写保护会破坏 storage（bucket 创建走调用方 principal）；需先统一系统集合写入权限模型 |
| W11 memberships 权限面过宽 | 当前 app 层守门完整；权限层收紧需同步改 teams 流程与测试 |
| W12 label 角色自授 | 与 Appwrite 同模型；如需收紧另立专项 |
| N6 缓存失效机制 | bootstrapCache 只在项目新建后变化；加失效需版本化 spec，收益低 |
| N9 idgen 死配置 | 配置 proto + bind + provider 链改动面大；文档层 UUID 无缺陷 |
| 性能项（COUNT(*) 可选计数、select 投影 SQL 化、未索引过滤告警） | 与 Appwrite 行为一致的性能优化，另立专项 |

## 5. 回归与测试策略

### 5.1 受影响调用方
- 系统模块（users/teams/account/identity/OTP/session/storage）以 SystemPrincipal 直调 docDB：A4 连接统一后无 tx 时行为不变；B1 对 SystemPrincipal 无影响（IsSystem 短路）；A7 白名单仅非 System 路径生效（System 零额外查询）；B1 的 D3（去 read 预检）不影响 SystemPrincipal。
- B2 影响 server API key 调用方：seed/测试 key 均为 read+write 成对签发，新语义下放行面不变。
- 无函数签名变更（A4/A7 均为内部实现与内部 helper）。

### 5.2 回归重点测试
- `internal/domain/databases/permissions_test.go`（B1 重写 + 新增）
- `internal/infra/documentdb/permissions_test.go`（A5/B1）
- `internal/infra/documentdb/postgres_test.go`（A1/A2/A3/A4/A6/A7）
- `internal/app/server/databases_integration_test.go`（A6/A3/B1 测试重写）
- `internal/app/client/databases_integration_test.go`（B1）
- `internal/app/client/system_collections_readonly_test.go`（B1 豁免回归）
- `internal/app/storage/storage_integration_test.go`、`internal/api/serverhttp/file_handler_integration_test.go`（A7/B2）
- `pkg/grpc/interceptor/apikey_scope_test.go`（B2 重写）
- `internal/app/shared/docdb_errors_test.go`（A6）
- `tests/acceptance/p0_acceptance_test.go`（端到端）

### 5.3 验证命令
```bash
go build ./...
task test    # go test -v ./... -cover；集成测试需要本地 PG（TORCHWOOD_TEST_DATABASE_SOURCE，默认 127.0.0.1:5433）
```

## 6. 实施顺序（子代理实现建议）

Phase A（顺序执行）：
1. A5（单行 SQL）→ 2. A1（分页）→ 3. A2（输入上限）→ 4. A3（幂等化，依赖 A6 同批落地）→ 5. A4（conn 统一 + InTx helper + bulk 事务）→ 6. A6（错误映射）→ 7. A7（字段白名单，依赖 A6 错误出口）→ 8. A8（审计资源）
Phase B：
9. B1（文档级优先，依赖 A5；含 D3/D5）→ 10. B2（scope 精确匹配，含测试重写）
收尾：`go build ./...` → `task test` 全量回归。

## 7. 验收标准（DoD）

1. Phase A 全部 8 项 + Phase B 全部 2 项实现，每项配套测试。
2. `go build ./...` 通过；`task test` 全绿（既有测试未删除；语义变化项明确重写并记录，含 `TestDatabases_CreateDocument_PermissionTemplates`、`apikey_scope_test.go`、`permissions_test.go:32-55`）。
3. B1/B2 的语义变更在 §1 决策点范围内，未扩大。
4. Phase C 项一律不动，文档保持单一事实来源。
5. 实现完成后回填本文档：每项标注变更摘要与验证结果，状态改为 `已实现`。

## 8. 独立审查记录

### v1 审查（2026-08-08，独立审查子代理）
结论：证据定位准确率极高（行号全部核实）；**6 个方案级问题需修订后实施**：
1. B1 尾随 GetDocument 自锁 + 半提交（`postgres.go:511`）→ **v1.1 修订**：改用 SystemPrincipal 读回（B1 步骤 5）。
2. B1 破坏 `TestDatabases_CreateDocument_PermissionTemplates`（`databases_integration_test.go:246-289`，方案重写清单遗漏；根因是"update 强制 read 预检 + 文档级优先"）→ **v1.1 修订**：D3 决策去 read 预检 + 测试重写（B1 步骤 4/测试清单）。
3. B2 通配符绕过未映射服务 fail-closed（`*` 须在 `resource==""` 之后）→ **v1.1 修订**：B2 步骤 1 代码顺序。
4. A6 测试位置矛盾（adapter 返回 raw error、映射在 app 层）+ 现有 `docdb_errors_test.go:19-21` 字符串透传断言 → **v1.1 修订**：仅类型匹配 + 测试改 app 层。
5. A7 无条件 GetCollection 的 nil 集合未定义 → **v1.1 修订**：非 System 路径 coll==nil → NotFound；System 路径跳过白名单。
6. A3 幂等分支的子表竞态（集合行冲突直接返回会永久丢失 attrs/indexes）→ **v1.1 修订**：仅集合行 DO NOTHING + 行数判断；子表保持报错；冲突路径按 IsSystem 区分。
**细节修正**：A8 方法数 23→27；§5.2 删除不存在的 `documents_integration_test.go`；A1 补 offset 正数上限；A4 补"Bulk 含不存在文档→整体回滚"说明与 InTx 嵌套防护、测试构造改"第 2 条不存在"；A6 明确仅 errors.As 类型匹配；A7 黑名单限定 default 库系统集合、fulltext Type 大小写、System 路径跳过白名单、记录 D4 不对称决策；B2 修正服务数为 7（Health 为 PUBLIC）、jwt_auth_test 无需调整、apikeys.go 仅 Create、通配符顺序；B1 的 D1 理由修正（匿名读测试文档为 nil perms，豁免保障显式 perms 系统集合文档）。

### v1.1 审查确认
（待审查子代理确认后填写）

### v1.2 审查确认（2026-08-08）
审查复核 v1.1 落实：6 个方案级问题 + 全部细节修正**均已正确落实**；新发现问题 A–E 已在 v1.2 修订：
- A（实现阻断）：A6 单测不可直接构造 `pgdriver.Error`（字段未导出）→ 修订为最小接口 `interface{ Field(byte) string }` + 本地 stub 单测；
- B：A3 的 A6 依赖表述修正（ErrDuplicateKey→AlreadyExists 为既有映射，不依赖 A6）；
- C：A7 nil 集合行为变更表述修正（非 System 路径 403→404，System 路径不动）；
- D：D1 豁免理由措辞修正（默认模板含 `read:any`，豁免保障显式 permissions 场景）；
- E：A4 边界注明 schema DDL 路径；B1 补 `_type='write'` 一致性验证项。
确认结论：`v1.2 确认通过，可进入实现`。

## 9. 实施记录（Phase B，2026-08-08）

### B1 documentSecurity 文档级优先语义（W1）

**变更文件**：
- `internal/domain/databases/permissions.go`：`AllowsDocumentAccess` 改为文档级优先（docSecurity=false→collOK；docHasPerms=false→collOK；系统集合→OR（D1 豁免）；用户集合→仅 docOK）；`SkipDocumentPermissionFilter` 改为仅（系统集合且集合级有 read）或（!DocumentSecurity 且集合级有 read）时跳过。
- `internal/infra/documentdb/postgres_permissions.go`：`listPermissionFilter` 对"用户集合 + documentSecurity=true + 集合级有 read"输出 `(EXISTS 匹配) OR NOT EXISTS(该文档有 _perms 行)`，两个子查询均带 `p._tenant = d._tenant`（A5 同步应用）；系统集合+read 与 docSecurity=false 仍走 Skip 跳过；集合级无 read 仅 EXISTS（现状）。
- `internal/infra/documentdb/postgres.go`：`UpdateDocument` 删除 `"read"` 预检（D3），仅保留 update 检查；尾随 `GetDocument` 改用 `databases.SystemPrincipal` 读回（D5 自锁修复，与 CreateDocument 一致）。

**测试（重写/新增）**：
- `internal/domain/databases/permissions_test.go`：`TestAllowsDocumentAccess_UserCollectionDocumentOverrides`（集合 read:any + 文档 user:bob → alice/carol 拒、bob 放行、nil perms 兜底）、`TestAllowsDocumentAccess_SystemCollectionOR`（系统集合 OR 保持）、`TestSkipDocumentPermissionFilter`（4 分支）；`TestAllowsDocumentAccess_DocumentSecurityOffIgnoresDocPerms` 保持通过。
- `internal/infra/documentdb/permissions_test.go`：新增 `TestPermissions_ListORFallback`（列表 NOT EXISTS 兜底 + 私有文档覆盖）、`TestPermissions_WriteRowTypeConsistency`（B1 步骤 7：`_type='write'` 行命中 update/delete、不命中 read 与列表，语义一致）；`TestPermissions_CollectionLevelFallback`/keys/PlatformAdmin 用例回归通过。
- `internal/app/server/databases_integration_test.go`：`TestDatabases_CreateDocument_PermissionTemplates` 重写（场景 1：文档权限含 update:user:alice 时可更新权限且模板展开；场景 2：仅 read 权限更新权限 → PermissionDenied）。
- `internal/app/client/databases_integration_test.go`：新增 `TestClientDatabases_PrivateDocumentEnforced`（匿名读拒/列表不可见、他用户改删拒、owner 可读写删；集合级配 read:any 验证覆盖）。
- `internal/app/client/system_collections_readonly_test.go`：匿名读 teams/buckets 保持通过（D1 豁免验证）。
- 回归：`internal/app/server/system_collections_readonly_test.go`（用户集合/系统集合读写策略）、`internal/app/client` account 路径、`tests/acceptance/p0_acceptance_test.go` §9.4（users 系统集合文档级过滤）全部通过。

**与方案偏差**：无实现偏差。方案 §3 B1 测试清单中 `permissions_test.go:32-55` 的 OR 断言按计划重写为两个测试（用户集合覆盖 + 系统集合 OR）。

### B2 API Key scope 精确匹配（W2）

**变更文件**：
- `pkg/grpc/interceptor/apikey_scope.go`：重写为显式方法→(resource, op) 映射表 `apiKeyScopeRules`（覆盖 7 个 ACCESS_API_KEY 服务全部方法，Health 不映射）；`APIKeyScopeAllowed` 通配符在未映射（resource 不存在）之后判断（fail-closed）；匹配规则 `s==resource` / `s==resource+".read"`（读方法）/ `s==resource+".write"`（写方法）/ `*`/`all`；新增导出 `ValidAPIKeyScope` 供 app 层格式校验。`IsAPIKeysServiceMethod` 保留。
- `internal/app/server/apikeys.go`：`Create` 校验 scope 格式 ∈ {*, all, 裸资源名, `<resource>.read`, `<resource>.write`}（上限 32 项、每项 ≤64 字符），非法 → InvalidArgument。
- `internal/api/serverhttp/file_handler.go`：无需代码改动——现有逻辑已通过 `StorageServiceCreateFile`/`StorageServiceGetFile` 常量走 `APIKeyScopeAllowed`，即经新映射表统一来源（GetFile→read、CreateFile→write）。

**测试（重写/新增）**：
- `pkg/grpc/interceptor/apikey_scope_test.go`：重写 `TestAPIKeyScopeAllowed`（`storage.read`→CreateFile 从放行改拒绝；`storage.write`→GetFile 拒绝；未映射服务 + `*`/`all` 拒绝保持；新增 databases.read→ListDocuments/GetDocument/CountDocuments 放行、DeleteDatabase/CreateDocument/BulkUpdateDocuments 拒绝；databases.write 反向；裸 scope 全放行）；新增 `TestValidAPIKeyScope`。
- `internal/app/server/apikeys_test.go`：新增 `TestAPIKeys_Create_ScopeValidation`（合法/非法 scope、超 32 项、单项 >64 字符）。
- 回归：`internal/api/serverhttp/file_handler_integration_test.go`（"users" scope 上传 403）、`tests/acceptance/p0_acceptance_test.go`（裸 "users" scope + ListUsers 放行）、`jwt_auth_test.go`（不触 scope 匹配）全部通过。

**与方案偏差**：无实现偏差。console 文案提示（`console/src/routes/api-keys/pages.tsx`）按方案标注为可选，未修改。

### 验证结果

- `go build ./...` 通过。
- 全量回归（`go test ./internal/domain/databases/... ./internal/infra/documentdb/... ./internal/app/... ./internal/api/... ./pkg/grpc/interceptor/... ./pkg/query/... ./internal/app/shared/... ./internal/infra/clients/... ./tests/acceptance/... -count=1`）全绿，无其他包因 B1/B2 语义变化失败。
- B1/B2 语义变更均处于 §1 决策点（D1/D2/D3/D5）范围内，未扩大；Phase C 项未动。
