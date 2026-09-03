# 06 数据库：三类库、标识与动态文档

> 面向后端开发者：围绕 Postgres 三层 schema、标识规则、DDL 约束、静态/动态表分工与查询/权限实现。
> 源码锚点：`pkg/ident/ident.go`、`internal/infra/documentdb/`、`pkg/query/query.go`、`pkg/crud/`、`internal/domain/databases/`。

## 0 子系统定义与边界

**DocumentDB 子系统 = torchwood 的文档数据存储整体方案**：以 `Databases → Collections → Documents` 三级资源模型为核心，端到端覆盖**元数据目录（catalog）、物理表管理（DDL 与项目数据面迁移）、文档 CRUD 与查询编译、权限集成（集合/文档两级 ACL + 认证期角色注入）、事件集成（事务性 outbox → realtime 按可见性扇出）**。七张系统静态表（`users/sessions/identities/groups/memberships/buckets/files`）是它的边界邻居而非组成部分——共用项目数据面 schema 与 `Principal`/角色语义，但不走动态文档路径（`IsSystemCollection` 名单仅用于 sentinel 写保护与测试重建）。

### 模块地图（范围内）

| 层 | 模块 | 职责 |
|---|---|---|
| 领域 | `internal/domain/databases/` | Document/Collection/Attribute/Index/Permission/Principal 模型，权限判定（`AllowsDocumentAccess`/`CollectionAllows` 等），`DocumentDB` 三端口（`repository.go`：Catalog / SchemaApplier / Documents） |
| 查询 | `pkg/query/` + `pkg/query/proto/` | Appwrite DSL 解析与 typed AST 编解码（双栈，同请求冲突即 `InvalidArgument`） |
| 适配器 | `internal/infra/documentdb/`（同包 7 文件） | catalog 寻址、collection DDL、文档 CRUD（OCC/Upsert/Bulk/advisor lock）、查询编译与执行、权限 SQL 下推、SQLSTATE 翻译 |
| 应用 | `internal/app/documents/`（Client/Server 共用核）+ `internal/app/server|client` 的 Databases 用例 | 用例守卫（sentinel 拒绝/标识校验/系统集合拦截/disabled）、空 ACE 种子、grant 展开/校验、错误映射 |
| 数据面 | `internal/infra/projectschema/` + `pkg/ident/` | 项目 schema 生命周期（Apply/迁移/孤儿对账/缓存失效桥接）、两段式寻址与标识规则（≤28 字符） |
| 迁移 | `db/migrations/`（public 控制面）+ `internal/infra/projectschema/migrations/`（项目数据面模板） | catalog/outbox 控制面演进；新项目一次性建面 + 存量 `EnsureAll` 自愈 |
| 事件 | `internal/infra/events/`（outbox + worker）→ `internal/infra/realtime/`（subscriber/hub/stream） | 写路径同事务落 `document_events_outbox` → Redis Pub/Sub → hub 按快照 ACL 过滤扇出（`VisibleTo`），出站帧剥 ACL |
| 分页 | `pkg/crud/pagination.go`（HMAC 签名 offset token）+ documentdb `ka:/kb:` keyset token | 两族 token：结构化 offset 续页与 keyset 游标续页 |
| 传输 | `internal/api/servergrpc|clientgrpc/databases.go` + `proto/server|client/v1/databases.proto` | 请求校验、authz 注解（`method_auth` + scope 表）、双栈参数绑定、OpenAPI 契约 |

### 范围外

Storage 对象本体（`files` 行只是元数据，对象在 S3/MinIO）、Functions 执行、账本/OAuth 目录（同在 `tw_<project>` 但独立演进）、billing 用量统计（消费 `files.SumSize`，不经过文档端口——`SumDocumentField` 目前无生产调用方）。

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
10. **查询双栈互斥**：`queries`（DSL 字符串）与 `query`（typed AST）携带冲突语义即 `InvalidArgument`；两栈算子集尚不对齐（DSL 无 `or`/`and`，AST 无 `between`/`isNull` 等；`in` 两栈均已支持）。跨 filter 绑定参数累计 ≤2000（封死 PG 65535 语句参数上限）。

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

## 6 查询 DSL（`pkg/query`）

`pkg/query/query.go:184` `Parse` 以 `^(\w+)\((.*)\)$` 匹配算子，`splitArgs` 处理引号/转义/括号嵌套；`ParseMany` 合并多串为 `Query{Filter, Filters, Orders, Selects, Limit, Offset, CursorAfter/Before, PageSize, PageToken}`，`Filter` 为 `OpAnd` 树。

| 类 | 算子 | 示例 | SQL |
|---|---|---|---|
| 过滤 | `equal`/`notEqual`/`in` | `equal("status","a")` / `in("status",["a","b"])` | `=` / `IN` / `NOT IN` |
|  | `lessThan`/`greaterThan`/`between` | `between("age",18,60)` | `<`/`>`/`BETWEEN ? AND ?` |
|  | `contains`/`startsWith`/`endsWith` | `contains("name","jo")` | `ILIKE '%v%' ESCAPE '\'`（`escapeLikePattern` 转义 `%_\'） |
|  | `search` | `search("title","hello")` | `to_tsvector('simple',col::text) @@ plainto_tsquery('simple',?)` |
|  | `isNull`/`isNotNull` | `isNull("deleted_at")` | `IS NULL` |
| 排序 | `orderAsc`/`orderDesc` | `orderDesc("$createdAt")` | `ORDER BY d."field" ASC/DESC, d._id <dir>`（与 cursor 续页路径同构的 `_id` tiebreaker） |
| 分页 | `limit`/`offset` | `limit(25)` | `LIMIT/OFFSET` |
|  | `cursorAfter`/`cursorBefore` | `cursorAfter("doc-id")` | keyset 谓词（与 `offset` 互斥） |
| 投影 | `select` | `select(["name","age"])` | 返回后裁剪 `Data` |

别名 ` $id→_id`、`$createdAt→_created_at`、`$updatedAt→_updated_at`、`$version→_version`（`mapQueryField`）。程序化拼串用 `BuildFilter`/`BuildEqual`/`BuildLimit`（自动转义 `"`/`\`）。

**输入上限**（`internal/infra/documentdb/postgres.go:46`）：`queries≤100`、`单串≤4096`、`equal 多值≤1000`、**跨 filter 绑定参数累计 ≤2000**（`maxTotalFilterParams`，封死 PG 65535 语句参数上限）、`maxQueryLimit=100`、`maxQueryOffset=10000`（`validateQueryInput` + `buildAppwriteQuery` 出口）。**写入载荷上限**（`internal/app/documents`）：总量 ≤1 MiB、单属性值 ≤256 KiB，超限 `DOCUMENT.TOO_LARGE`（InvalidArgument）。

**编译与校验**（`postgres_query_compile.go`）：`astFrom` 优先 `Query.AST`（`shared.v1.Query` typed 形态，与 `queries` 互斥），否则 `ParseMany`；`validateQueryFields` 白名单=系统列+已声明 attribute，`search` 需命中 `fulltext` 索引，`_version` 缺列返回 `version_column_unavailable`（`InvalidArgument`），系统集合敏感列（`users.password_hash/prefs/labels` 等）黑名单仅按 `IsSystemCollection` 生效（`pkg/query/proto` 双栈见 `docs/review/wave2-e4-query-ast.md`）。

## 7 权限模型（`_perms`）

条目 `type:role`，`type∈{read,create,update,delete}`（`write` 展开为三写）。角色：`any`（合成，仅 read 可授予）/`users`/`user:{id}`/`group:{id}`/`keys`/`admin`/`guests`/`__system__`。`ExpandPermissionRoles` 无条件注入 `any`；`ExpandPermissionTemplates` 展开 `user:`/`group:` 模板。

`_perms` 按 `_type='read'` 匹配；`documentSecurity=false` 只看集合级，`true` 时文档有 `_perms` 则覆盖，无行回集合级（`AllowsDocumentAccess`）。

| 操作 | 检查点 |
|---|---|
| `CreateDocument` | 集合 `create` + `isWriteProtectedSystemCollection` 拦截（`users/sessions/identities` + `databaseID=="_"`） |
| `GetDocument` | 文档 `read`（`checkDocumentPermission`） |
| `UpdateDocument` | 仅文档 `update`（不强制 `read`） |
| `DeleteDocument` | 文档 `delete` |
| `ListDocuments`/`CountDocuments`/`Sum` | `ListAccessDenied` 预拒 → `listPermissionFilter` 生成 `EXISTS(SELECT 1 FROM _perms p WHERE p._tenant=d._tenant AND ... p._type='read' AND p._permission=ANY(?::text[]))`；由集合 `read` 兜底分支时追加 `OR NOT EXISTS(...)` |

`ValidateGrantablePermissions`：普通用户不可授予未持有角色与 `any` 写权限（`keys`/`System`/`PlatformAdmin` 跳过）。

## 8 OCC（`_version`）

用户集合 `BIGINT NOT NULL DEFAULT 1`，`Create/Upsert/Bulk` 盲写但 `_version+1`（`Bulk` `SkipVersion=true` LWW），`Update/Delete` 必填且等于当前值（行锁下比较，`versionColumnReady` 校验 `bigint`），成功 `+1`。错误 `version_required`/`version_mismatch`/`version_column_conflict`（`FailedPrecondition`），`version_column_unavailable`（`InvalidArgument`）。`_version` 可作过滤/排序/投影；系统表无此列。`Upsert` 的 `conflictColumns` 必须无序命中集合一个 unique 索引（非 Bypass 主体前置校验 `validateConflictColumns`，否则 InvalidArgument；Bypass 主体靠 PG 42P10 兜底）。

## 9 事务与分页一致性

`BulkUpdate/BulkDelete` 与单文档写走 `clients.Database.RunInTx`（已在事务内复用，否则开短事务，`internal/infra/documentdb/postgres_permissions.go:195`）；`ListDocuments` 先 `COUNT` 后主查询，非原子快照（`READ COMMITTED`，与 Appwrite 一致，以 `nextPageToken` 续页）。`pkg/crud` 提供 `ParseListParams`/`BuildPaginationInfo`（`pkg/crud/list.go:57`/`pagination.go:360`），游标 `EncodePageToken`/`DecodePageToken`（`v1` base64 JSON，TTL 24h，`filterDigest`/`orderBy` 一致性校验）。

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

// Appwrite DSL 查询（ListDocuments 的 queries）
queries := []string{
  query.BuildEqual("status", "published"),
  `greaterThan("views", 100)`,
  `orderDesc("$createdAt")`,
  `limit(25)`,
}
docs, total, nextToken, _ := docDB.ListDocuments(ctx, pid, "app", "posts", Query{Queries: queries, PageSize: 25}, principal)

// Typed 双栈（shared.v1.Query AST 与 queries 互斥）
ast := sharedv1.Query{Filter: &sharedv1.Filter{Op:"and", Children: ...}}
docDB.ListDocuments(ctx, pid, "app", "posts", Query{AST: &ast}, principal)
```

分页用 `pkg/crud`（见 `09-api-guide.md` §3）：`ParseListParams` 校验 `page_size/page_token/filter/order_by`，`BuildPaginationInfo` 产出 `HasNext/NextOffset`，handler 用 `EncodePageToken` 编码 `next_page_token`。

## 12 测试与参考

- 集成 `internal/infra/documentdb/postgres_test.go`（`testing.Short` 跳过），`internal/testutil/db.go:SetupTestDB` 按 `TORCHWOOD_TEST_DATABASE_SOURCE` 创建隔离库（`pg_terminate_backend` + `DROP DATABASE`）。
- `pkg/query/query_test.go`、`postgres_query_compile_test.go`、`permissions_test.go` 覆盖算子/转义/白名单/敏感列/权限分支。
- `pkg/crud/`：AIP-132/158/160 抽象，`filter.go`/`order.go`/`pagination.go` 供静态表列表复用，动态文档优先 `pkg/query`。
- 参考：`internal/domain/databases/` 端口与 `Principal`；`internal/infra/documentdb/postgres*.go`；`pkg/query/proto/proto.go` typed AST；`db/migrations/` + `internal/infra/projectschema/`；`AGENTS.md` §数据库约定。
