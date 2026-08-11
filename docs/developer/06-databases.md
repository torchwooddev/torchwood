# Torchwood 动态文档数据库（Databases / Documents）

> 本文描述 Torchwood 的**动态文档层**：运行时创建数据库、集合（collection）、属性与索引，并以 Appwrite 风格查询 DSL 读写文档，全程无需手工迁移。
> 相关代码：`internal/infra/documentdb/`、`pkg/query/`、`pkg/crud/`、`internal/domain/databases/`、`db/migrations/`。

---

## 1. 架构总览

Torchwood 采用 **schema-per-database** 的 PostgreSQL 映射：

- 每个 `(project, database)` 对应一个真实 PostgreSQL schema；
- 每个 collection 对应 schema 内的一张**真实表**（不是 JSONB 堆表）；
- 表名 / schema 名均经过严格标识符白名单校验（`^[a-zA-Z_][a-zA-Z0-9_]*$`）与引用转义，杜绝 SQL 注入。

### 1.1 命名规则

| 对象 | 命名 | 示例 |
|------|------|------|
| Schema | `TORCHWOOD_<projectInternalID>_<databaseID>` | `TORCHWOOD_1_default`、`TORCHWOOD_1_app` |
| 集合表 | `<schema>.<collectionID>` | `TORCHWOOD_1_default.users` |
| 权限表 | `<schema>._perms` | `TORCHWOOD_1_default._perms` |

`internalID` 取自 `projects.internal_id`（`internal/infra/documentdb/postgres.go` 的 `resolveInternalID`，进程内缓存）；`CreateDatabase` 即 `CREATE SCHEMA IF NOT EXISTS`，`DeleteDatabase` 即 `DROP SCHEMA ... CASCADE`。

### 1.2 集合表结构

```sql
CREATE TABLE IF NOT EXISTS "TORCHWOOD_1_default"."posts" (
    _id          TEXT NOT NULL,
    _tenant      BIGINT NOT NULL DEFAULT 1,
    _created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _created_by  TEXT,
    _updated_by  TEXT,
    -- ...每个 attribute 一列...
    PRIMARY KEY (_tenant, _id)
);
```

### 1.3 系统列

| 列 | 说明 |
|----|------|
| `_id` | 文档 ID（客户端缺省由 `idgen.UUID()` 生成；合法字符 `[a-zA-Z0-9_.:-]{1,64}`） |
| `_tenant` | 项目隔离键（`projects.internal_id`），所有查询强制带 `d._tenant = ?` |
| `_created_at` / `_updated_at` | 时间戳，自动维护 |
| `_created_by` / `_updated_by` | 审计列，取自调用方 principal 的第一个 `user:<id>` 角色；仅含 keys 角色的主体留空 |
| `_perms`（独立表） | 文档级权限（见 §3） |

写入时 `_` 前缀字段一律被过滤（`buildInsertParts` / `buildUpdateParts`），用户数据无法伪造系统列。

### 1.4 权限表 `_perms`

```sql
CREATE TABLE IF NOT EXISTS "TORCHWOOD_1_default"."_perms" (
    _id         BIGSERIAL PRIMARY KEY,
    _tenant     BIGINT NOT NULL,
    _collection TEXT NOT NULL,
    _document   TEXT NOT NULL,
    _type       TEXT NOT NULL,      -- read / create / update / delete
    _permission TEXT NOT NULL,      -- 角色（any / users / user:<id> / keys / ...）
    _created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (_tenant, _collection, _document, _type, _permission)
);
-- 索引：idx_perms_lookup (_tenant,_collection,_document,_type)、idx_perms_role (_tenant,_collection,_type,_permission)
```

删除 collection 时同步清理该 collection 的 `_perms` 行（`DeleteCollection`），避免同名重建后旧权限泄漏到新文档。

---

## 2. 元数据静态表与动态文档层的分工

元数据与业务数据的“动静分离”由两层承担：

| 层 | 技术 | 职责 | 表 |
|----|------|------|-----|
| 元数据静态层 | bun + golang-migrate（`db/migrations/`） | 项目、API Key、库/集合目录、审计等平台元数据 | `projects`、`api_keys`、`admins`、`document_databases`、`document_collections`、`document_attributes`、`document_indexes`、`audit_logs`、`admin_projects`、`project_oauth_providers`、`functions` 等 |
| 动态文档层 | schema-per-database 原生 SQL/JSONB | 系统资源（users、sessions、files、buckets、teams 等）与用户动态集合的真实数据 | `TORCHWOOD_<internalID>_<dbID>.*` |

- `document_databases` / `document_collections` / `document_attributes` / `document_indexes` 构成**目录**：属性/索引的声明（含 `is_system`、`document_security`、`permissions` 等）。
- 动态层表结构由目录驱动：`CreateCollection` 先建表 + 索引，再写目录元数据（用户集合重复创建返回 `ErrDuplicateKey` → `AlreadyExists`；系统集合幂等成功）。

### 2.1 系统集合

`internal/domain/databases/system_collections.go` 是系统集合名单的单一事实来源（仅对 `default` 库生效）：

| 集合 | 属性（节选） | 索引 | 独占管理服务 |
|------|-------------|------|-------------|
| `users` | email、password_hash、name、status、email_verified、phone、labels、prefs、factors | email 唯一、phone | Account / Server Users |
| `sessions` | user_id、secret_hash、provider、user_agent、ip、expire_at | user_id | Account |
| `identities` | user_id、provider、provider_uid、provider_email、provider_data | user_id、(provider,provider_uid) 唯一 | OAuth / OTP |
| `teams` | name、permissions、total、prefs | name | Teams |
| `memberships` | team_id、user_id、email、roles、status、invited_at、joined_at | team_id、user_id、email | Teams |
| `buckets` | name、permissions、public | name | Storage |
| `files` | bucket_id、name、mime_type、size、metadata | bucket_id、name fulltext | Storage |

- `SensitiveSystemCollectionIDs`（users / sessions / identities）：Server API 仅 PlatformAdmin 可读（返回前脱敏），Client API 一律拒绝（走 Account 专用 API）。
- `isWriteProtectedSystemCollection`（纵深防御）：`default` 库的 users/sessions/identities 禁止非 System 主体直接写；更新路径对文档 owner（`user:<id>` 匹配）放行，以支持 `UpdateAccount` / `UpdatePrefs` 自助路径。
- `EnsureSystemCollections` 在项目首次使用时引导创建全部系统集合（幂等，进程内缓存 + `DO NOTHING`），并执行存量 `keys` 角色写权限收窄清理（`cleanupKeysWritePerms`：移除 users/sessions/identities 的 `update:keys` / `delete:keys`，teams/memberships 保留）；对**已存在**的存量集合按 spec 幂等补齐缺失属性（`reconcileSystemCollectionAttrs`：直接调 `CreateAttribute` 补物理列 + `document_attributes` 元数据，按属性 Key 比对，并发元数据 INSERT 撞唯一约束 23505 忽略）。

---

## 3. 权限模型（_perms）

权限条目格式为 `type:role`，`type` 为 `read` / `create` / `update` / `delete`（`write` 在解析时展开为 create + update + delete）。

### 3.1 角色清单（以 `permissions.go` 与系统集合 spec 为准）

| 角色 | 含义 | 授予规则 |
|------|------|---------|
| `any` | 公开（匿名） | **合成角色**：`ExpandPermissionRoles` 无条件注入；只允许 read 类授予，写类授予一律拒绝（`syntheticRoles` 校验） |
| `users` | 任何已认证终端用户 | 仅当调用方持有 `users` 角色时注入 |
| `user:{id}` | 指定用户（模板，落库前展开为 `user:<uuid>`） | `ExpandPermissionTemplates` 按调用方首个 `user:` 角色替换 |
| `team:{id}` | 指定团队（模板，同上） | 按调用方首个 `team:` 角色替换 |
| `keys` | API Key / 自动化主体 | 不默认 bypass；按 scope 限权 |
| `admin` | 平台管理员（Console admin） | PlatformAdmin 走 `IsSystem()` 完全绕过 |
| `guests` | 匿名 Client API 读（`GuestPrincipal`） | 用于公开 bucket 匿名读等场景 |
| `__system__` | 内部基础设施主体（`SystemPrincipal`） | 绕过全部文档级权限检查 |

### 3.2 documentSecurity 语义（B1，`AllowsDocumentAccess`）

- `documentSecurity = false`：只按**集合级**权限判定；
- `documentSecurity = true`：
  - 文档无 `_perms` 行 → 集合级权限兜底；
  - 文档有 `_perms` 行 → 用户集合**文档权限覆盖集合权限**（私有文档）；系统集合保持 **OR** 语义（D1 豁免，匿名读 teams/buckets 依赖此行为）。

### 3.3 各操作的检查点

| 操作 | 检查 |
|------|------|
| `CreateDocument` | 集合级 `create` 权限 + 写保护集合拦截 |
| `GetDocument` | 文档级 `read`（`checkDocumentPermission`） |
| `UpdateDocument` | 仅检查文档级 `update`（D3：不再强制 read 预检；独立 update 策略） |
| `DeleteDocument` | 文档级 `delete` |
| `ListDocuments` / `CountDocuments` | 集合级 `read` 拒绝（`ListAccessDenied`）→ 逐文档 SQL 过滤（`listPermissionFilter`，见 §4.2）；系统集合 + 集合级 read 可跳过文档级过滤（D1） |
| `SumDocumentField` | 同 List 的 read 过滤（storage usage 只统计可见文档） |

### 3.4 授权授予约束

`ValidateGrantablePermissions`：非特权主体（普通用户）不能授予自己未持有的角色，且不能授予 `any` 的写类权限（`create` 类型豁免）；API Key（keys 角色 + scope）、System、PlatformAdmin 跳过校验。

---

## 4. Appwrite 风格查询 DSL（pkg/query）

### 4.1 算子完整清单（以 `pkg/query/query.go` `Parse` 与 `postgres.go` `buildAppwriteQuery` 为准）

| 类别 | 算子 | 语法 | SQL 映射 |
|------|------|------|---------|
| 过滤 | `equal` | `equal("field","v")` 或 `equal("field",["a","b"])` | 单值 `=`；多值 `IN (...)` |
| 过滤 | `notEqual` | `notEqual("field","v")` | 单值 `!=`；多值 `NOT IN (...)` |
| 过滤 | `lessThan` | `lessThan("field",18)` | `<` |
| 过滤 | `lessThanEqual` | `lessThanEqual("field",18)` | `<=` |
| 过滤 | `greaterThan` | `greaterThan("field",18)` | `>` |
| 过滤 | `greaterThanEqual` | `greaterThanEqual("field",18)` | `>=` |
| 过滤 | `contains` | `contains("name","john")` | `ILIKE '%v%'` |
| 过滤 | `startsWith` | `startsWith("name","jo")` | `ILIKE 'v%'` |
| 过滤 | `endsWith` | `endsWith("name","hn")` | `ILIKE '%v'` |
| 过滤 | `search` | `search("title","hello")` | `to_tsvector('simple', col::text) @@ plainto_tsquery('simple', ?)`（**需 fulltext 索引列**） |
| 过滤 | `isNull` | `isNull("field")` | `IS NULL` |
| 过滤 | `isNotNull` | `isNotNull("field")` | `IS NOT NULL` |
| 过滤 | `between` | `between("age",18,60)` | `BETWEEN ? AND ?` |
| 排序 | `orderAsc` / `orderDesc` | `orderAsc("$createdAt")` | `ORDER BY col ASC/DESC`（默认 `_created_at DESC` 兜底） |
| 分页 | `limit` | `limit(25)` | `LIMIT ?` |
| 分页 | `offset` | `offset(10)` | `OFFSET ?` |
| 分页 | `cursorAfter` / `cursorBefore` | `cursorAfter("doc-id")` | keyset 谓词 `(col, _id) >/< (?, ?)` + 同构 `ORDER BY`（与 offset 互斥，cursor 优先） |
| 投影 | `select` | `select(["name","age"])` | 返回后裁剪 `Data`（系统字段始终保留） |

字段别名：`$id` ↔ `_id`、`$createdAt` ↔ `_created_at`、`$updatedAt` ↔ `_updated_at`。

### 4.2 解析与校验机制

1. **语法层**（`pkg/query`）：正则 `^(\w+)\((.*)\)$` 匹配算子，`splitArgs` 处理引号/转义/嵌套括号；未知算子、参数个数错误、`limit(-1)` 等在解析期 fail-fast。
2. **输入上限**（`postgres.go` `validateQueryInput`，A2）：
   - queries 条数 ≤ 100；单条查询串 ≤ 4096 字符；`equal`/`notEqual` 多值 ≤ 1000 个。
3. **翻页防护**（A1）：`page_size`/`limit` 上限 100（`maxQueryLimit`），默认 50；`offset` 上限 10000（`maxQueryOffset`），超限返回 `InvalidArgument`。
4. **字段白名单**（A7，仅非 System 路径）：过滤/排序/投影字段必须是系统列（`_id`/`_created_at`/`_updated_at`）或已声明 attribute；`search` 必须命中 fulltext 索引列；系统集合另有**敏感列黑名单**（`users.password_hash`/`prefs`/`labels`、`sessions.secret_hash`、`identities.provider_data` 禁止作为过滤条件；自定义库同名集合不受影响）。
5. **列表过滤**：非 System 主体先经 `ListAccessDenied` 拒绝，再生成 `EXISTS (SELECT 1 FROM ..._perms p WHERE p._tenant = d._tenant AND ...)` 子查询按 `_type='read'` 过滤；`documentSecurity=true` 且集合级有 read 时，无 `_perms` 行的文档由集合级权限兜底。

### 4.3 列表分页

`pkg/crud` 提供 AIP-132 风格的游标编码（`crud.EncodePageToken` / `DecodePageToken`，offset 整数编码），List 返回 `totalCount` + `nextPageToken`；collection 列表与 document 列表复用同一抽象。

---

## 5. Attribute / Index 动态管理

| 操作 | 行为 |
|------|------|
| `CreateAttribute` | `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` + 写入 `document_attributes` 目录 |
| `DeleteAttribute` | `ALTER TABLE ... DROP COLUMN IF EXISTS` + 删除目录行（`DELETE /v1/server/databases/{db}/collections/{coll}/attributes/{key}`） |
| `CreateIndex` | 建真实索引 + 写 `document_indexes` 目录 |
| `DeleteIndex` | `DROP INDEX idx_<collection>_<indexID>` + 删除目录行 |

### 5.1 属性类型 → PostgreSQL 类型映射（`pgTypeFor`）

| 类型 | PostgreSQL |
|------|-----------|
| `string` / `email` / `url` | `VARCHAR(n)`（`size` 1..64000）否则 `TEXT` |
| `integer` | `BIGINT` |
| `float` | `DOUBLE PRECISION` |
| `boolean` | `BOOLEAN` |
| `datetime` | `TIMESTAMPTZ` |
| `json` | `JSONB` |
| 其他 | `TEXT`（兜底） |

`required` → `NOT NULL`；`default` → `DEFAULT <literal>`（boolean/integer/float 做类型化校验）。

### 5.2 索引类型（`createCollectionIndex`）

| 类型 | SQL |
|------|-----|
| `key`（默认） | 普通 btree：`CREATE INDEX idx_<coll>_<idx> ON <tbl> (cols...)` |
| `unique` | `CREATE UNIQUE INDEX ...` |
| `fulltext` | `USING gin(to_tsvector('simple', col1 || ' ' || col2))`（`search` 算子依赖） |

---

## 6. 事务处理注意点

- **无 Appwrite 风格 staging 事务**：`docs/prompts/databases-transactions.md` 规划的 Transactions（staged ops + commit/rollback + TTL）**尚未实现**，`document_transactions` 相关代码不存在。
- **批量操作原子化**：`BulkUpdateDocuments` / `BulkDeleteDocuments` 在未处于外层事务时整体包在 `clients.RunInTx` 中，中途失败整体回滚（行为从“部分成功”收紧为“原子”）；已在外层事务（`clients.InTx` 检测）时直接复用外层事务，不嵌套。
- 每个单文档写操作本身不强制事务：`CreateDocument` = INSERT + `setPermissions`；`UpdateDocument` 的字段更新与 `_perms` 替换（clear + set）顺序执行。
- 系统集合引导（`EnsureSystemCollections`）依赖幂等设计（`DO NOTHING` + 行数判断）而非事务。

---

## 7. 测试

- 集成测试：`internal/infra/documentdb/postgres_test.go`（跳过条件 `testing.Short()`）。
- 数据库由 `internal/testutil.SetupTestDB` 管理：从 `TORCHWOOD_TEST_DATABASE_SOURCE`（基础 DSN，默认库名前缀 `TORCHWOOD_test`）与 `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE`（维护库）读取；每个测试创建独立数据库（`<前缀>_<pid>_<序号>`）、按序执行 `db/migrations/*.up.sql`、结束后 `pg_terminate_backend` + `DROP DATABASE`。两个环境变量缺失时 fail-fast（通过 `task test` 加载 `.env` 运行）。
- 覆盖：CRUD、权限正反例、多项目隔离、批量回滚、cursor 翻页、输入上限、字段白名单、敏感列黑名单、审计列、系统集合幂等等。

---

## 8. 参考

- `internal/domain/databases/`：端口（`DocumentDB`）、Principal、权限语义、系统集合名单。
- `internal/infra/documentdb/postgres.go` / `postgres_permissions.go`：PostgreSQL 适配器实现。
- `pkg/query/`：DSL 解析器与安全构造工具（`BuildFilter` / `BuildEqual` / `BuildLimit`，供程序化拼查询）。
- `pkg/crud/`：列表分页游标抽象。
- `db/migrations/`：静态元数据表迁移（含安全修复迁移 000007/000008/000009）。
- `docs/prompts/databases-transactions.md`：Transactions 功能规划（未实现）。
