# 06 数据库：三类库、标识与动态文档

> 面向后端开发者：围绕 Postgres 三层 schema、标识规则、DDL 约束、静态/动态表分工与查询/权限实现。
> 源码锚点：`pkg/ident/ident.go`、`internal/infra/documentdb/`、`pkg/query/query.go`、`pkg/crud/`、`internal/domain/databases/`。

## 0 子系统定义与边界

**DocumentDB 子系统 = torchwood 的文档数据存储整体方案**：以 `Databases → Collections → Documents` 三级资源模型为核心，端到端覆盖**元数据目录（catalog）、物理表管理（DDL 与项目数据面迁移）、文档 CRUD 与查询编译、权限集成（集合/文档两级 ACL + 认证期角色注入）、事件集成（事务性 outbox → realtime 按可见性扇出）**。七张系统静态表（`users/sessions/identities/groups/memberships/buckets/files`）是它的边界邻居而非组成部分——共用项目数据面 schema 与 `Principal`/角色语义，但不走动态文档路径（`IsSystemCollection` 名单仅用于 sentinel 写保护与测试重建）。

### 模块地图（范围内）

| 层 | 模块 | 职责 |
|---|---|---|
| 领域 | `internal/domain/databases/` | Document/Collection/Attribute/Index/Permission/Principal 模型，权限判定（`AllowsDocumentAccess`/`CollectionAllows` 等），`DocumentDB` 三端口（`repository.go`：Catalog / SchemaApplier / Documents） |
| 查询 | `pkg/query/`（客户端语法糖解析器 + 程序化构造器 + `ToWireJSON`）+ `pkg/query/proto/`（proto→AST 编解码） | 单 typed AST（C7）：`shared.v1.Query` 是服务端唯一消费形态；DSL 串仅作 SDK/CLI 客户端糖 |
| 适配器 | `internal/infra/documentdb/`（同包 7 文件） | catalog 寻址、collection DDL、文档 CRUD（OCC/Upsert/Bulk/advisor lock）、查询编译与执行、权限 SQL 下推、SQLSTATE 翻译 |
| 应用 | `internal/app/documents/`（Client/Server 共用核）+ `internal/app/server|client` 的 Databases 用例 | 用例守卫（sentinel 拒绝/标识校验/系统集合拦截/disabled）、空 ACE 种子、grant 展开/校验、错误映射 |
| 数据面 | `internal/infra/projectschema/` + `pkg/ident/` | 项目 schema 生命周期（Apply/迁移/孤儿对账/缓存失效桥接）、两段式寻址与标识规则（≤28 字符） |
| 迁移 | `db/migrations/`（public 控制面）+ `internal/infra/projectschema/migrations/`（项目数据面模板） | catalog/outbox 控制面演进；新项目一次性建面 + 存量 `EnsureAll` 自愈 |
| 事件 | `internal/infra/events/`（outbox + worker）→ `internal/infra/realtime/`（subscriber/hub/stream） | 写路径同事务落 `document_events_outbox` → Redis Pub/Sub → hub 按快照 ACL 过滤扇出（`VisibleTo`），出站帧剥 ACL |
| 分页 | `pkg/crud/pagination.go`（HMAC 签名 offset token，静态表/控制面列表用）+ documentdb `ka:/kb:` keyset token | 文档面 keyset-only（C2 阶段①）：`ListDocuments` 只发/只认 keyset token；offset token 族仅存于 `pkg/crud` 静态表路径 |
| 幂等 | `internal/domain/databases/idempotency.go`（端口）+ `internal/infra/bun/bunrepo/idempotency_repo.go` + `internal/app/documents/idempotency.go`（核层包裹） | `request_id` 写幂等（public.`idempotency_keys`）：只缓存成功响应、24h 重放、KEY_CONFLICT/IN_PROGRESS 域码 |
| 传输 | `internal/api/servergrpc|clientgrpc/databases.go` + `proto/server|client/v1/databases.proto` | 请求校验、authz 注解（`method_auth` + scope 表）、AST 参数绑定（`BindListQuery`）、OpenAPI 契约 |

### 范围外

Storage 对象本体（`files` 行只是元数据，对象在 S3/MinIO）、Functions 执行、账本/OAuth 目录（同在 `tw_<project>` 但独立演进）、billing 用量统计（消费 `files.SumSize`，不经过文档端口——`SumDocumentField` 已删除，`:aggregate` 的 sum 即其继任）。

### 关键不变量（变更评审锚点）

1. **租户隔离**：所有文档行访问强制 `d._tenant = ?`（`_tenant = projects.internal_id`，进程内缓存 + 删除失效桥接）。
2. **DDL 只走两段式**：`businessSchema` 显式拒绝 sentinel `_` 与一段式；`DROP SCHEMA` 永不指向 `tw_<project>`。
3. **同事务原子性**：文档数据行、`_perms` 替换、outbox 事件三者同事务提交，任一失败整体回滚。
4. **OCC**：用户集合强制 `_version`，`Update/Delete` 必填且匹配；列缺失/类型冲突 fail-closed（不落 PG 42703）。
5. **注入防御**：标识符 `safeNameRe` + `quoteIdent` 双重转义；查询值全程参数绑定；LIKE 走 `escapeLikePattern` + `ESCAPE`。
6. **权限语义对齐**：SQL 过滤谓词（`listPermissionFilter`）与内存判定（`AllowsDocumentAccess`）必须逐语义一致，改动需同步两侧与测试（`permissions_test.go`）。
7. **事件语义**：at-least-once、不保证顺序；客户端按 `event_id` 去重、按 `version` 判序；出站帧永不含 ACL 快照。
8. **默认私有**：`DefaultCollectionPermissions` 不含 `read:any`；空 ACE 文档按种子规则私有化（owner/创建者角色/`__private__`）。
9. **标识长度**：`project.id/database.id ≤ 28`（schema 名 ≤60 字节，`pkg/ident`）；`collectionID ≤40`、属性 key ≤63、索引 ID ≤40（app 层入口），叠加索引名拼接校验 `idx_<coll>_<id>` ≤63（infra 二道防线：表/列名 ≤63 + 组合校验——静态段上限封不死组合长度）。PG 63 字节截断类缺陷已机制性封死；redesign 阶段②逻辑/物理名解耦后上限将收紧（collectionID ≤36）并随物理名分配退役。
10. **查询单栈（C7）**：wire 只收 `query`（typed AST，`shared.v1.Query`）；`queries` DSL 字符串字段已 reserved，服务端文档查询栈零字符串解析（`ResolveQuery`/`astFrom` 无 ParseMany 回退）。算子全集 `eq ne lt lte gt gte in between notBetween isNull isNotNull contains notContains startsWith notStartsWith endsWith notEndsWith search notSearch + and/or`（嵌套深度 ≤8；无通用 NOT，取反由 not* 变体承担——索引友好）；`select` 投影。DSL 串是 SDK/CLI 的客户端糖（`pkg/query.Parse/ParseMany`、`sdk/go/query.FromDSL`），解析为 AST 后发送。跨 filter 绑定参数累计 ≤2000（封死 PG 65535 语句参数上限）。
11. **写幂等（redesign §4.1/§10.1）**：携带 `request_id` 的写请求键作用域 `(project_id, actor_id, request_id)`；只缓存成功响应（失败释放、重试重新执行）；同 key 异体 → `IDEMPOTENCY.KEY_CONFLICT`；并发同 key 短轮询 ≤2s 后仍 in-flight → `IDEMPOTENCY.IN_PROGRESS`；重放返回原响应 + `x-torchwood-replayed: true` 响应头；done TTL 24h、in_flight 兜底 TTL 5min、惰性清理。
12. **keyset-only（redesign C2 完成态）**：`ListDocuments` 只发/只认 `ka:/kb:` token；`offset()` 算子与非 keyset token 一律 `InvalidArgument`；ORDER BY = 全部排序键 + `_id` tiebreaker（方向随首键），keyset 谓词按方向行比较或逐键 OR 展开（多键游标完整支持，token 仍只编码 docID + 服务端查行取全部键值）。`ListCollections` 的 offset 分页维持到阶段②。
13. **聚合一律在可见行集上执行（redesign §11-J D1）**：`:aggregate` 复用 `listPermissionFilter` 过滤链且过滤先于 GROUP BY——不可见行不进聚合、group 键不泄露；聚合目标必须是声明数值属性（integer/float）。

## 1 三类库

| 层 | Schema 形态 | 技术 | 关键表 |
|---|---|---|---|
| `public` 控制面 + 事件脊柱 | 固定 `public` | `bun` + `golang-migrate`（`db/migrations/`） | `projects`/`admins`/`admin_projects`/`api_keys`/`audit_logs`/`provider_resource_index`/`document_events_outbox`+`_dead` |
| 项目数据面 `tw_<project>` | 一段式 `tw_<p>` | `bun` + `internal/infra/projectschema/` | 静态表 `users`/`sessions`/`identities`/`groups`/`memberships`/`buckets`/`files` + 目录 `document_databases`/`document_collections`/`document_attributes`/`document_indexes` + 账本/Functions/OAuth |
| 业务文档面 `tw_<project>_<database>` | 两段式 `tw_<p>_<db>` | 原生 SQL（`documentdb`） | 每个 `database.id` 一个 schema，只放用户 collection 真实表 + 每业务 schema 一张 `_perms` |

`default` 是首个业务库（普通库，可删可重建）；系统静态表不再是文档集合（`internal/infra/projectschema/migrator.go` 在 `CreateProject` 同事务 `CREATE SCHEMA` + `Apply`，进程启动 `EnsureAll` 自愈）。

## 2 标识与 Schema 规则

`pkg/ident/ident.go:27`：`^[a-z][a-z0-9]{0,27}$`、`MaxSchemaResourceIDLen=28`。入口 `ValidateSchemaResourceID`；对外再走 `RejectExternalDatabaseID` 显式拒绝 sentinel。

- `ProjectSchemaName(p)` → `tw_<p>`，匹配 `^tw_[a-z][a-z0-9]{0,27}$`（`projectSchemaNameRe`）——仅一段式。
- `SchemaName(p,db)` → `tw_<p>_<db>`，匹配 `^tw_[a-z][a-z0-9]{0,27}_[a-z][a-z0-9]{0,27}$`（`schemaNameRe`）——两段式。
- `IsTwoSegmentSchema(name)` 断言 DDL 目标必须两段式，与一段式不相交。
- `ident.ProjectDataPlaneID = "_"` 仅内部寻址：`documentSchema` 在 `databaseID=="_"` 时映射到 `ProjectSchemaName`；对外非法。
- `_tenant` 取 `projects.internal_id`（`postgres.go:resolveInternalID` + `sync.Map` 缓存），所有行查询强制 `d._tenant=?`。

字段/表名均 `quoteIdent` 转义（`"` → `""`），并经 `safeNameRe=^[a-zA-Z_][a-zA-Z0-9_]*$` 白名单。

## 3 两段式 DDL（`businessSchema`）

只接受两段式，永不解析一段式：

```go
schema, err := ident.SchemaName(projectID, databaseID) // 非法直接 InvalidArgument
if !ident.IsTwoSegmentSchema(schema) { return status.Error(codes.InvalidArgument, "...") }
conn.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
```

- `CreateDatabase` = `CREATE SCHEMA IF NOT EXISTS`；`DeleteDatabase` = `DROP SCHEMA ... CASCADE`（绝不 `DROP` 一段式 `tw_<project>`）。
- catalog 无 `database_id='_'` 行；`ListDatabases` 过滤 sentinel。
- 进程内 `projectschema.Apply` 带 `sync.Map` 就绪缓存（事务内不写缓存）。

## 4 静态表 vs 动态表

**静态表**（`tw_<project>`，`internal/infra/bun/model/*.go`）：`users`/`sessions`/`identities`/`groups`/`memberships`/`buckets`/`files`，`bun` 模型，无 `_id`/`_perms`/`_version`，经 Account / Groups / Storage 专用 RPC 读写。`SystemCollectionIDs` 仍在 `internal/domain/databases/system_collections.go` 仅用于 DocumentDB 跳过 `_version`/写保护与测试重建。`users.DocumentData()` 投影**不含 `password_hash`**（密码校验走 `usersRepo` 的 `User.PasswordHash`）。

**动态表**（`tw_<project>_<db>.<collection>`，每集合一张真实表）：

```sql
CREATE TABLE tw_shop_app.posts (
  _id TEXT NOT NULL, _tenant BIGINT NOT NULL DEFAULT 1,
  _created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  _updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  _created_by TEXT, _updated_by TEXT,
  _version BIGINT NOT NULL DEFAULT 1, -- 用户集合有，系统静态表无
  -- 每个 attribute 一列（pgTypeFor 映射）
  PRIMARY KEY (_tenant, _id)
);
CREATE TABLE tw_shop_app._perms (
  _id BIGSERIAL PRIMARY KEY, _tenant BIGINT NOT NULL,
  _collection TEXT NOT NULL, _document TEXT NOT NULL,
  _type TEXT NOT NULL, _permission TEXT NOT NULL,
  UNIQUE (_tenant,_collection,_document,_type,_permission)
);
```

目录位于项目面：`document_databases` → `document_collections`（含 `permissions`/`document_security`/`disabled`/`is_system`）→ `document_attributes` → `document_indexes`。`DeleteCollection` 同步清理 `_perms` 行，防同名重建泄漏。

## 5 Attribute / Index 动态管理

| 操作 | SQL |
|---|---|
| `CreateAttribute` | `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` + 写 `document_attributes`（含 `default_value`），`required→NOT NULL`、`default→DEFAULT` |
| `DeleteAttribute` | `ALTER TABLE ... DROP COLUMN IF EXISTS` + 同事务清理依赖该列的索引（`postgres_permissions.go:DeleteAttribute`） |
| `CreateIndex` | `CREATE INDEX idx_<coll>_<idx> ON <tbl>(cols)` / `UNIQUE` / `USING gin(to_tsvector('simple', col))` + 写 `document_indexes`；索引名拼接 ≤63 校验（`validateIndexNameLen`） |
| `DeleteIndex` | `DROP INDEX IF EXISTS` + 删目录（`RunInTx` 原子，`DeleteIndex`） |
| `UpdateCollection` | 权限替换与字段更新同一事务，统一刷 `updated_at`（空 patch no-op） |

类型映射（`pgTypeFor`）：`string/email/url→VARCHAR(n)/TEXT`、`integer→BIGINT`、`float→DOUBLE PRECISION`、`boolean→BOOLEAN`、`datetime→TIMESTAMPTZ`、`json→JSONB`。

## 6 查询（单 typed AST，C7）

**wire 形态唯一**：List/Count/Aggregate 的过滤/排序/投影一律走 `query`（`shared.v1.Query`：`filter` 树 + `orders` + `select` + `pageSize/pageToken`）。`queries` DSL 字符串字段已 reserved（POC 无兼容期）；GET 面保留 `page_size/page_token` 简单分页参数，过滤条件一律 POST body（`:list` 的 body 即 Query JSON）。绑定链：`BindListQuery`（proto codec，`pkg/query/proto.FromProto`）→ `ResolveQuery`（合并 GET 面分页字段 + 校验）→ infra `astFrom`（归一后再校验）。

**算子全集**（`Filter` oneof，`pkg/query` 常量同源）：`eq ne lt lte gt gte in between notBetween isNull isNotNull contains notContains startsWith notStartsWith endsWith notEndsWith search notSearch + and/or`（嵌套深度 ≤8，`MaxDepth`）；无通用 NOT——取反全部由 not* 变体承担（索引友好，德摩根展开可表达）。值数量约束：比较族 ≥1（eq/ne 多值 → IN/NOT IN）、between/notBetween 恰 2、isNull/isNotNull 0。

| 类 | 算子（proto oneof 分支） | SQL |
|---|---|---|
| 过滤 | `eq`/`ne`/`in` | `=` / `IN` / `NOT IN`（eq/ne 多值自动进集合语义） |
|  | `lt`/`lte`/`gt`/`gte`/`between` | `<`/`<=`/`>`/`>=`/`BETWEEN ? AND ?`；`notBetween` → `NOT BETWEEN` |
|  | `contains`/`startsWith`/`endsWith`（及 not* 变体） | `ILIKE '%v%' ESCAPE '\'`（`escapeLikePattern` 转义 `%_\'；not* → `NOT ILIKE`） |
|  | `search`/`notSearch` | `to_tsvector('simple',col::text) @@ plainto_tsquery('simple',?)`（not → `NOT (...)`） |
|  | `isNull`/`isNotNull` | `IS NULL` / `IS NOT NULL` |
| 排序 | `orders[]`（attribute+desc） | `ORDER BY d.k1 dir1, …, d._id <首键方向>`（与 cursor 续页路径同构的 `_id` tiebreaker） |
| 分页 | `pageSize`/`pageToken` | LIMIT；**keyset-only（C2 完成态，见 §9）**：`pageToken` 只认 `ka:/kb:` token；count/aggregate 对排序/分页算子（orders/pageSize/pageToken/cursor）显式拒绝（R9+R9b，整集语义） |
| 投影 | `select[]` | 返回后裁剪 `Data` |

别名 `$id→_id`、`$createdAt→_created_at`、`$updatedAt→_updated_at`、`$version→_version`（`mapQueryField`）。

**DSL 是客户端糖**：`pkg/query.Parse/ParseMany`（含 `BuildFilter`/`BuildEqual` 拼串助手与 `ToWireJSON`——AST→protojson 形态，CLI 用）与 `sdk/go/query.FromDSL` 在客户端把 Appwrite 风格串解析为 AST 后发送；服务端零消费。程序化构造用 `pkg/query` 构造器（`query.Eq/Gt/Between/IsNull/And/Or…`）或 SDK 的 `sdk/go/query`（链式 `Builder`）。

**输入上限**（`internal/infra/documentdb/postgres.go`）：AST 叶数 ≤100（`pkg/query.MaxQueries`，`Validate` 封顶）、eq/in 多值 ≤1000、**跨 filter 绑定参数累计 ≤2000**（`maxTotalFilterParams`，封死 PG 65535 语句参数上限）、`maxQueryLimit=100`（页大小上限 clamp）。DSL 串条数/长度上限随双栈退役移除（服务端不再收字符串）。**写入载荷上限**（`internal/app/documents`）：总量 ≤1 MiB、单属性值 ≤256 KiB，超限 `DOCUMENT.TOO_LARGE`（InvalidArgument，违规属性定位走 BadRequest violations）。

**编译与校验**（`postgres_query_compile.go`）：`astFrom` 收归一后的 AST（无字符串回退）；`validateQueryFields` 白名单=系统列+已声明 attribute，`search` 需命中 `fulltext` 索引，`_version` 缺列返回 `version_column_unavailable`（`InvalidArgument`），系统集合敏感列（`users.password_hash/prefs/labels` 等）黑名单仅按 `IsSystemCollection` 生效。

## 7 权限模型（`_perms`）

条目 `type:role`，`type∈{read,create,update,delete}`（`write` 展开为三写）。角色：`any`（合成，仅 read 可授予）/`users`/`user:{id}`/`group:{id}`/`keys`/`admin`/`guests`/`__system__`。`ExpandPermissionRoles` 无条件注入 `any`；`ExpandPermissionTemplates` 展开 `user:`/`group:` 模板。

`_perms` 按 `_type='read'` 匹配；`documentSecurity=false` 只看集合级，`true` 时文档有 `_perms` 则覆盖，无行回集合级（`AllowsDocumentAccess`）。

| 操作 | 检查点 |
|---|---|
| `CreateDocument` | 集合 `create` + `isWriteProtectedSystemCollection` 拦截（`users/sessions/identities` + `databaseID=="_"`） |
| `GetDocument` | 文档 `read`（`checkDocumentPermission`） |
| `UpdateDocument` | 仅文档 `update`（不强制 `read`） |
| `DeleteDocument` | 文档 `delete` |
| `ListDocuments`/`CountDocuments`/`Aggregate` | `ListAccessDenied` 预拒 → `listPermissionFilter` 生成 `EXISTS(SELECT 1 FROM _perms p WHERE p._tenant=d._tenant AND ... p._type='read' AND p._permission=ANY(?::text[]))`；由集合 `read` 兜底分支时追加 `OR NOT EXISTS(...)`；聚合复用同一过滤链且过滤先于 GROUP BY（D1：不可见行不进聚合、键不泄露） |

`ValidateGrantablePermissions`：普通用户不可授予未持有角色与 `any` 写权限（`keys`/`System`/`PlatformAdmin` 跳过）。

## 8 OCC（`_version`）

用户集合 `BIGINT NOT NULL DEFAULT 1`，`Create/Upsert/Bulk` 盲写但 `_version+1`（`Bulk` `SkipVersion=true` LWW），`Update/Delete` 必填且等于当前值（行锁下比较，`versionColumnReady` 校验 `bigint`），成功 `+1`。错误 `version_required`（缺省）/`version_mismatch`/`version_column_conflict`（`FailedPrecondition`），`version_invalid`（**显式 ≤0**，`InvalidArgument`——与缺省态不同码，Phase 1 裁决②），`version_column_unavailable`（`InvalidArgument`）。`_version` 可作过滤/排序/投影；系统表无此列。`Upsert` 的 `conflictColumns` 必须无序命中集合一个 unique 索引（非 Bypass 主体前置校验 `validateConflictColumns`，否则 InvalidArgument；Bypass 主体靠 PG 42P10 兜底）。

## 8.1 事务内核 execute-tx（redesign §4.8 Phase 1）

`DatabasesService/ExecuteTransactions`（Server 面）：单事务内顺序执行异构 op 批（`internal/infra/documentdb/postgres_transactions.go`，Bulk 的泛化）。op 模型 `{type(create/update/upsert/delete), collection_id, document_id, data, permissions, increment, expected_version, conflict_columns}`，上限 1000（`MaxBulkOperations`）。锁纪律：按 `(collection, documentID)` 排序预取 `pg_advisory_xact_lock` 防批间死锁，op 按请求序执行（事件序 = op 序）；各 op 复用单文档事务体（权限/OCC/conflictColumns 校验同源）。`ATOMIC`（默认）任一失败整批回滚（错误带 op index 域码定位）；`PARTIAL` 逐 op SAVEPOINT 容错、已成功不回滚、返回 per-op 结果（含失败域码）。create/upsert 空 ACE 种子与单文档 API 同语义。

## 8.2 域错误码（redesign §4.1）

域码稳定 snake_case（`DOCUMENT.NOT_FOUND`、`DOCUMENT.VERSION_CONFLICT`、`DOCUMENT.ATTRIBUTE_UNSERIALIZABLE`、`IDEMPOTENCY.KEY_CONFLICT` 等，`internal/domain/databases/errors.go`）静态映射 gRPC code；消息格式 `CODE: message`，ErrorInfo detail 携带 `reason`/`retryable`（OCC 冲突、资源耗尽与幂等执行中可重试）与 `error_id`（**DomainStatus 生成处统一注入**，与 infra SQLSTATE 路径对齐——每个错误实例可唯一引用；限流拒绝为 `RATE_LIMIT.EXCEEDED` / `torchwood.platform` + RetryInfo 精确退避）。字段级违规定位走 **google.rpc BadRequest 标准 detail（field_violations）**：execute-tx 的 op 定位迁移为字段路径形态（`ops[3].expected_version`——域码映射子字段：VERSION_\*→`expected_version`、PERMISSION_DENIED→`permissions`、NOT_FOUND/ALREADY_EXISTS→`document_id`、无映射→`ops[N]`）；载荷违规（`data.blob`）与 op 守卫（`ops[i].collection_id` 等）同形态。裸 "document database error" 已消灭；infra 产出的域码 status 在 app 层经 `errors.As` 提取透传（防包装链丢 status）。

## 8.3 聚合 `documents:aggregate`（redesign §4.1 + §10.5 P1）

`DatabasesService/AggregateDocuments`（Server 面，scope `databases.read`）：`POST .../documents:aggregate`，`sum/avg/min/max` + 可选单键 `group_by`（count 已有独立 `:count` RPC 不并入）。语义：

- **D1（§11-J 已裁决）**：聚合一律在 `listPermissionFilter` 过滤后的可见行集上执行（过滤先于 GROUP BY）——不可见行不进聚合、group 键不泄露；权限 golden 集成测试锁语义（`aggregate_integration_test.go`）；最小桶/k-匿名未实现（可选产品功能，默认关）；权限变更前后聚合结果不可比属固有属性。
- 聚合目标必须是集合声明的数值属性（`integer`/`float`，System 主体一视同仁防拼列）；`group_by` 须为已声明属性，键按 text 序列化（NULL 键=属性未设置的行，归入 `group_key` 未设置的组）。
- 空集语义：`sum=0`（COALESCE，类型跟随属性）、`avg/min/max` 无值（proto oneof 未设置）；`group_by` 下空集返回空组列表。无 `group_by` 时恰返回一组。
- **结果类型化（预决策 5）**：`AggregateValue.result` 是 oneof——integer 属性的 `sum/min/max` 走 `int64_value`（`SUM(bigint)::int8` 显式收敛、`MIN/MAX` 原生 bigint，`>2^53` 精确）；`avg` 恒 `double_value`（`AVG(bigint)::float8`）；float 属性恒 `double_value`。integer 聚合超出 int64 → `InvalidArgument` / `AGGREGATE.OVERFLOW`（PG 22003 翻译，域码见 `internal/domain/databases/errors.go`）。
- **Data 的 double 精度界（维持不变）**：文档 Data（JSON `Struct`）的 number 通道是 double——业务值可能超过 2^53 时请用 **string 属性承载**（换 Struct 形态收益不成比例，redesign §4.1 类型系统既定取舍）；聚合面经 `int64_value` 已绕开该界，读写两个通道各自精确。
- 过滤算子与 ListDocuments 同形（单 typed AST）；排序/分页算子（orders、page_size、page_token、cursor、token）在 count 与 aggregate 一律 `InvalidArgument` 显式拒绝（整集语义，R9 + R9b——分页字段归一进 AST 后统一拦截）。

## 8.4 写幂等 `request_id`（redesign §4.1/§10.1）

七个写入口（Create/Update/Upsert/Delete/BulkUpdate/BulkDelete/ExecuteTransactions，Server 面 proto 字段 `request_id` + Client 面 Create/Update/Upsert/Delete；HTTP 面等价 `Idempotency-Key` 头，proto 字段优先）在 `internal/app/documents` 核层包裹：

- **键作用域** `(project_id, actor_id, request_id)`；`actor_id` 复用归因链（`Principal.StableActorID`：`user:<id>` 角色存裸 id（end user / console admin）、API key 主体 `key:<id>`、内部 System `system`）；不同 actor 同 key 不冲突。
- **指纹** = method + 请求关键字段规范序列化 sha256（批 ID/conflict_columns 排序规范化——集合语义，重试乱序不判冲突）；同 key 不同指纹 → `IDEMPOTENCY.KEY_CONFLICT`（InvalidArgument，重试无意义）。
- **语义**：只缓存成功响应（失败 Release 释放占位，重试重新执行）；重放返回原响应 + `x-torchwood-replayed: true` 响应头（gRPC response metadata，网关 outgoing matcher 透传为 HTTP 头；零 proto 响应侵入）。并发同 key：短轮询（100ms 间隔）≤2s 后仍 in-flight → `IDEMPOTENCY.IN_PROGRESS`（Aborted，retryable）。`Complete` 失败不回滚写入（best-effort 缓存，只损失重放能力）；store 故障时写请求失败（Unavailable，不静默降级为 at-least-once）。
- **存储** `public.idempotency_keys`（PK `(project_id, actor_id, request_id)` 仲裁并发认领 + `claim_token` 防过期重认领串写 + `expires_at` 索引惰性清理）；done TTL 24h（完成时刻起算）、in_flight 兜底 TTL 5min（崩溃残留行到期可重认领——期间同键重试收到 IN_PROGRESS）。execute-tx 幂等覆盖整批（PARTIAL 重放返回首次完整 per-op 结果，§11-E2）。

## 9 事务与分页一致性

`BulkUpdate/BulkDelete` 与单文档写走 `clients.Database.RunInTx`（已在事务内复用，否则开短事务，`internal/infra/documentdb/postgres_permissions.go`）；`ListDocuments` **keyset-only（C2 完成态，多键）**：首页（无 cursor）执行精确 COUNT 后主查询（非原子快照，`READ COMMITTED`），满页发 `ka:` token；续页（keyset token）跳过 COUNT（`total=0=unknown`），满页判定 has-more。**排序 = 全部排序键 + `_id` tiebreaker（方向随首键）**；keyset 谓词：方向一致（全 ASC/全 DESC）走行比较 `(k1,…,kn,_id) op (?,…,?)`，方向混合走逐键 OR 展开（`k1 OP1 ? OR (k1 = ? AND k2 OP2 ?) OR …`）——两种形态与 ORDER BY 全序严格一致（跨页不丢不重的机制保证，确定性用例锁行为）。cursor 仍以 docID 定位：服务端按 docID 查行取全部排序键值（token 只编码 `ka:/kb:`+docID）。

**NULL 排序键的已知限制**（预决策 4）：行比较谓词对 NULL 求值为 NULL（行被排除）——cursor 行的排序键含 NULL → `InvalidArgument`（消息明示"filter them out first"，先 isNull/isNotNull 过滤再分页）；数据行含 NULL 键在续页中被跳过。不做 NULLS LAST 谓词改写（行比较谓词与 NULL 的组合正确性代价不成比例）。`pkg/crud` 提供 `ParseListParams`/`BuildPaginationInfo`（`pkg/crud/list.go:57`/`pagination.go:360`），offset token（`EncodePageToken`/`DecodePageToken`，`v1` base64 JSON，TTL 24h）仅供静态表/控制面列表使用，文档面不再接受。

## 10 系统列与写入过滤

| 列 | 说明 |
|---|---|
| `_id` | 文档主键，`idgen.UUID()` 默认，`^[a-zA-Z0-9_.:-]{1,64}$`（`docIDRe`） |
| `_created_at/_updated_at` | 自动维护（`NOW()`） |
| `_created_by/_updated_by` | 归因主体：`user:<id>` 角色存裸 id；API key 主体存 `key:<keyID>`（`databases.Principal.KeyID` 由 `DocPrincipal` 投影，`userIDFromPrincipal`）；其余留空 |
| 用户输入 `_` 前缀字段 | `buildInsertParts`/`buildUpdateParts` 直接过滤，防伪造系统列 |
| `documentSecurity/disabled` | 目录层控制：`disabled=true` 时非 `BypassesDocumentACL` 一律 `PermissionDenied`（`ensureCollectionAccessible`） |

写保护：`isWriteProtectedSystemCollection` 仅对 `databaseID=="_"` 的 `users/sessions/identities` 生效，业务库同名集合不受影响。

## 11 常见用法与示例

```go
// 创建集合后增属性
docDB.CreateCollection(ctx, pid, "app", "posts", perms, false)
docDB.CreateAttribute(ctx, pid, "app", "posts", Attribute{Key:"title", Type:"string", Size:128, Required:true})

// 单 AST 查询（C7）：程序化构造器（pkg/query）+ Query 结构体字面量
ast := &query.Query{
  Filter: query.And(query.Eq("status", "published"), query.Gt("views", "100")),
  Orders: []query.Order{{Attribute: "$createdAt", Desc: true}},
  PageSize: 25,
}
docs, total, nextToken, _ := docDB.ListDocuments(ctx, pid, "app", "posts", databases.Query{AST: ast}, principal)

// 多键排序 + 游标（C2 完成态）：全部排序键 + _id tiebreaker；
// 续页把上页 nextToken 填进 PageToken
ast = &query.Query{
  Orders: []query.Order{{Attribute: "priority", Desc: true}, {Attribute: "title"}},
  PageSize: 25, PageToken: nextToken,
}

// DSL 串是客户端糖（SDK/CLI 解析为 AST 后发送，服务端零消费）
parsed, _ := query.ParseMany([]string{`equal("status","published")`, `limit(25)`})
wire := parsed.ToWireJSON() // CLI/工具直连 JSON 请求面
```

分页用 `pkg/crud`（见 `09-api-guide.md` §3）：`ParseListParams` 校验 `page_size/page_token/filter/order_by`，`BuildPaginationInfo` 产出 `HasNext/NextOffset`，handler 用 `EncodePageToken` 编码 `next_page_token`。

## 12 测试与参考

- 集成 `internal/infra/documentdb/postgres_test.go`（`testing.Short` 跳过），`internal/testutil/db.go:SetupTestDB` 按 `TORCHWOOD_TEST_DATABASE_SOURCE` 创建隔离库（`pg_terminate_backend` + `DROP DATABASE`）。
- `pkg/query/query_test.go`（DSL 糖 + 构造器）、`pkg/query/proto/proto_test.go`（每算子编解码往返）、`postgres_query_compile_test.go`（每算子 SQL + keyset 谓词形态）、`permissions_test.go` 覆盖算子/转义/白名单/敏感列/权限分支；`TestPostgresDocumentDocuments_MultiKeyCursor` 锁多键跨页不丢不重。
- **DSL 文法 parity 锁**：`pkg/query/testdata/dsl_ast_golden.json`（47 条，含 10 错误条目）是根模块解析器与 `sdk/go/query.FromDSL` 的**共同仲裁语料**——两侧 golden 测试以中立 JSON 形态比对；`root`/`sdk` override 块表达设计内的单侧契约差异（如 offset：根侧通用解析器支持、SDK 糖按文档面 keyset-only 拒绝）。改语料须在 commit message 给出理由，禁止单方删条目。
- `pkg/crud/`：AIP-132/158/160 抽象，`filter.go`/`order.go`/`pagination.go` 供静态表列表复用，动态文档优先 `pkg/query`。
- 参考：`internal/domain/databases/` 端口与 `Principal`；`internal/infra/documentdb/postgres*.go`；`pkg/query/proto/proto.go` typed AST；`db/migrations/` + `internal/infra/projectschema/`；`AGENTS.md` §数据库约定。
