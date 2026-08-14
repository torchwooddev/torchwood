# Round 3 审查报告：02 动态文档层

> 审查基准：当前工作区只读（未改任何源代码）。
> 范围：`internal/infra/documentdb/`、`pkg/query/`，交叉 `internal/domain/databases/`、`db/migrations/`、`internal/infra/clients/`；对照 app 层 Databases 用例确认入口校验。
> 辅助验证：未跑需 Postgres 的集成测试；本会话无 shell，`go vet ./internal/infra/documentdb/... ./pkg/query/...` 未执行（静态阅读未见明显 vet 级问题）。

---

## 摘要

动态文档层在 Round 1/2 与 B1/B2 之后的主安全面（SQL 注入、`_tenant`/`_perms` 过滤、keys 不 bypass、upsert ON CONFLICT 提权、分页/limit 上限、文档与权限同事务）**当前代码中未见回归**。adapter 对标识符一律 `quoteIdent`、值一律绑定参数；`keys` 只是普通角色；`PlatformAdmin` 仅 console 的 owner/admin 置位，API Key 不会拿到该标志。

本轮未发现新的 P0/P1。剩余问题集中在：array 属性物理列未落地、删列不清理索引元数据、列表不回传文档权限、upsert 冲突列未与唯一索引对齐、DDL/缺表错误映射不完整。这些影响一致性与 API 契约，不构成新的越权读写。

---

## 已核实为健康 / 已修复不再复报

下列 Round 1/2 与历史方案项已对照**当前代码**核实，不再作为新问题复报。

| 项 | 结论 | 当前证据 |
|----|------|----------|
| Upsert ON CONFLICT 提权 / TOCTOU（cd565f5 / F3-1） | ✅ 未回归 | `postgres.go:539-598` 同事务 + `pg_advisory_xact_lock`；命中行走文档级 `update`，未命中走集合级 `create`；权限替换用 `effectiveID` 而非调用方 `_id`。`conflictLockKey` 已改 JSON（`:666-678`），不再用 `\x00` 拼接。 |
| 冲突列 SQL 注入 | ✅ | `:516-520` `safeNameRe` 且禁止 `_` 前缀，`quoteIdent` 后拼进 `ON CONFLICT`。 |
| `_perms` 缺 `_tenant`（W5 / A5） | ✅ | `postgres_permissions.go:114-127` 两个 EXISTS/NOT EXISTS 均有 `p._tenant = d._tenant`。 |
| 文档级优先（B1） | ✅ | `permissions.go:83-138` 用户集合覆盖、系统集合 OR（D1）、`SkipDocumentPermissionFilter` 仅系统/`!DocumentSecurity`。 |
| keys 角色不 bypass | ✅ | `IsSystem()` 不含 `keys`（`access.go:40-42`）。`permissions_test.go:182-220` `TestPermissions_KeysNotBypass`：无私有文档 read:keys 时 Get 拒。敏感系统集合写被 `systemCollectionsWriteProtected` + `cleanupKeysWritePerms` 收窄（`:40-52`、`:1239-1253`）。 |
| API Key ≠ 超级用户 | ✅ | 拦截器给 API Key 的是 `Roles: ["keys"]`，`IsPlatformAdmin` 仅 `validator.go:154` 对 console `owner`/`admin` 置位。授权靠集合/文档上的 `keys` 角色匹配 + 方法级 scope（B2，本模块外）。 |
| admin 绕过是否过宽 | ✅ 可接受 | `PlatformAdmin` ⇒ `IsSystem()` 跳过文档 ACL 与写保护，符合 Console 超管语义；Server Databases **写**路径在 app 层对全部系统集合直接拒绝（`internal/app/server/databases.go:275-277`），不依赖 adapter 守门。读路径超管可看高敏集合，属产品设计。 |
| 分页 / 负数 limit（N1 / F3-2） | ✅ | `pkg/query/query.go:132-147` 负数 fail-fast；`postgres.go:891-913` `PageSize` 回退 + `maxQueryLimit=100` + `maxQueryOffset=10000`。 |
| 查询输入上限（A2） | ✅ | `maxQueryCount/maxQueryStringLen/maxFilterValues`（`postgres.go:59-62`、`:1837-1848`、`:1862-1878`）。 |
| `contains`/`startsWith`/`endsWith` 转义 | ✅ | `escapeLikePattern` + `ESCAPE '\'`（`:657-664`、`:1901-1909`）。 |
| 字段白名单 / 敏感列 / search 需 fulltext（A7） | ✅ | `validateQueryFields`（`:1778-1835`）；非 System 路径未知列 → InvalidArgument。 |
| 文档与 `_perms` 同事务（F3-4） | ✅ | Create/Update/Delete/Upsert/Bulk 均 `RunInTx` + `conn(ctx)`；`clients.InTx` 防嵌套（`tx.go:24-40`）。 |
| 增量原子性 | ✅ | `buildIncrementParts`（`postgres_permissions.go:142-150`）为 `COALESCE(col,0) + ?`，非 read-modify-write。 |
| `_created_by`/`_updated_by` 不信任客户端 | ✅ | `buildInsertParts`/`buildUpdateParts` 跳过 `_` 前缀（`postgres.go:1619-1622`、`:1655-1658`）；审计列来自 `userIDFromPrincipal`。 |
| 系统集合保护 | ✅ | adapter 写保护 users/sessions/identities；app Databases 写禁全部系统集合、读禁高敏集合；`IsSystem` 仅 default 库按名单判定（`system_collections.go:45-47`）。reconcile 只 **补齐缺失属性**（`:1215-1236`），不改已有列/数据。 |
| R02-P1-1 DeleteIndex 未事务化 | ✅ 已修 | `postgres_permissions.go:263-282` 已 `RunInTx`；`postgres_test.go:1142-1183` 覆盖同名重建。 |
| R02-P1-2 CreateDatabase 元数据未进事务 | ✅ 已修 | `postgres.go:83-107` schema/`_perms`/INSERT 同事务；`:1186-1222` 覆盖回滚。 |
| R02-P1-3 ListCollections 未走 `conn(ctx)` | ✅ 已修 | `:231-241` 已用 `p.conn(ctx)`。 |
| R02-P1-4 仅改权限不刷新审计列 | ✅ 已修 | `:759-767`；测试 `:1224-1267`。 |
| CreateCollection 尾随 GetCollection（R02-P3-1） | ✅ 已修 | handler 直接构响应（`internal/api/servergrpc/databases.go:125-133`）。 |
| 租户/项目隔离 | ✅ | schema=`TORCHWOOD_<internalID>_<db>` + 每条文档 SQL 带 `_tenant = ?`；`resolveInternalID` 按 `projects.id` 解析。跨项目取决于调用方传入的 `projectID`（handler 取自拦截器上下文）。 |
| DSL 非法输入 | ✅ 不 panic | 未知算子 / 括号不平衡 / 非整数 limit → `error`（`query.go:176-178`、`:256-258`、`:128-134`）。order 键经 `safeNameRe` + 白名单。 |
| W8 存在性探测（Get 先取行再鉴权） | ⚠️ 残留、Phase C 已接受 | `postgres.go:689-698`：不存在 → `nil`（app 映射 404）；存在无权限 → `ErrPermissionDenied`（403）。不升级为新 P0。 |
| W3 空 permissions → 默认全公开 | ⚠️ Phase C 已接受 | `ParsePermissionStrings` 空列表回落 `DefaultCollectionPermissions()`（含 `read:any`）。 |
| W7 Update 路径无行锁 | ⚠️ Phase C 已接受 | Update/Delete 同事务内检查-写入仍无 `FOR UPDATE`；upsert 已用 advisory lock。 |

---

## 问题

### P0 严重

无。

### P1 高

无。

### P2 中

1. **`array=true` 属性只写元数据，物理列仍是标量**
   - 位置：`internal/infra/documentdb/postgres.go:1384-1401`（`attributeColumnSQL` 用 `pgTypeFor` 后从未追加 `[]`）；`:371-386` / `:1490-1507` 仅把 `IsArray` 写入 `document_attributes`。
   - 描述：Proto/API 已暴露 `Attribute.array`（`proto/server/v1/databases.proto:235`），但 `CREATE TABLE`/`ADD COLUMN` 生成的是 `VARCHAR`/`BIGINT` 等标量。`attr.Array` 只影响「不要 DEFAULT」。
   - 影响：调用方按数组写入会触发 PG 类型错误（常见 `42804`，见下条，映射表也未覆盖）；读回无法得到数组语义。功能与契约不一致，不是注入。
   - 建议：`attr.Array == true` 时生成 `BIGINT[]` / `TEXT[]` 等；写入路径对切片走 array 绑定；补集成测试。

2. **`DeleteAttribute` 不清理依赖的 `document_indexes` 元数据**
   - 位置：`internal/infra/documentdb/postgres.go:241-260`（只 `DROP COLUMN` + 删 `document_attributes`）。
   - 描述：PG 会随列丢掉依赖索引，但 catalog 仍保留 `document_indexes` 行。`GetCollection` 继续报告已不存在的 unique/fulltext；`validateQueryFields` 会认为某列仍有 fulltext（`:1789-1821`）；同 ID `CreateIndex` 会撞复合主键。
   - 影响：元数据与物理对象分叉；search 白名单误放行后变成无索引 seqscan；重建索引失败。
   - 建议：删列前查出 `Attributes` 含该 key 的索引，同事务删 `document_indexes`（或拒绝「仍被索引引用」的删列）。

3. **`ListDocuments` / `CountDocuments` 不装配文档 `_perms`，列表里 `permissions` 恒空**
   - 位置：`internal/infra/documentdb/postgres.go:979-1013`（scan 后只做 in-memory `select` 投影，无 `attachDocumentPermissions`）；对比 Get/Create/Update `:700-702`、`:492`、`:790`。
   - 描述：gRPC `mapDocument` 原样输出 `doc.Permissions`（`internal/api/servergrpc/databases.go:566-572`），因此 List 响应的 permissions 始终为空。调用方只能再 Get 才能看到 ACL。
   - 影响：与 Get 契约不一致，客户端无法按列表结果做授权 UI / 二次校验。权限过滤本身仍正确。
   - 建议：列表批量 `IN (_document)` 一次取出 `_perms` 再按文档分组，避免 N+1。

4. **Upsert `conflictColumns` 仍完全由客户端指定，未与集合唯一索引对齐**
   - 位置：`internal/infra/documentdb/postgres.go:508-521`（只校验标识符）；app 层 `internal/app/server/databases.go:451-461` 同样只要求非空。
   - 描述：提权路径已关闭（命中行必须过 `update` 检查）。但任意通过 `safeNameRe` 的列都可进入 `ON CONFLICT`：不匹配唯一约束 → `42P10`；碰巧等于某条 unique 则对该行走更新语义。
   - 影响：不是提权回归。防御纵深不足：调用方可对「自己刚建的低基数字段 unique」做 upsert；错误码可探测哪些列上有唯一约束。
   - 建议：`GetCollection` 后要求 `conflictColumns` 精确等于某条 `type=unique` 的 `Indexes.Attributes`（含顺序），否则 InvalidArgument。

5. **`UpdateCollection` 权限与其它字段非原子，且仅改权限不碰 `updated_at`**
   - 位置：`internal/infra/documentdb/postgres.go:322-354`。
   - 描述：`setCollectionPermissions` 与后续 `UPDATE name/document_security/disabled` 不在同一 `RunInTx`；若第二步失败，权限已改、名称未改。仅 patch permissions 时 `len(sets)==0` 直接返回，集合 `updated_at` 不变。
   - 影响：与 F3-5「DDL/元数据同事务」原则不一致；审计时间戳失真。需 PlatformAdmin，利用面有限。
   - 建议：整段包 `RunInTx`；权限变更也 `SET updated_at = now()`。

6. **缺表 / 类型不匹配的 SQLSTATE 未映射，可能泄露 schema 名**
   - 位置：`internal/app/shared/docdb_errors.go:19-33`（有 `42703`/`42P10`/`23505` 等，**无 `42P01`/`42804`**）；`postgres.go:853-861` 非 System 会先 404，但 **System 路径与「元数据在、表已丢」** 会把原始 `relation "TORCHWOOD_<n>_<db>"."<coll>" does not exist` 交给 gRPC Internal。
   - 描述：adapter 文档 CRUD 对非唯一约束错误多是 `fmt.Errorf("...: %w", err)` 保留 `pgdriver.Error`；映射表漏了 `undefined_table` / `datatype_mismatch`。部分 DDL（`CreateDatabase` `:105-106`）连 `ErrDuplicateKey` 都不包，直接返回 23505。
   - 影响：错误文本可暴露内部 schema 命名与项目 `internal_id`；客户端看到 500 而非 404/409。
   - 建议：补 `42P01`→NotFound、`42804`→InvalidArgument；`CreateDatabase` 唯一冲突改为 `ErrDuplicateKey`；DDL 出口统一 `MapDocumentDBError`。

7. **`maxQueryCount × maxFilterValues` 可超过 PG 绑定参数上限**
   - 位置：`postgres.go:59-62`、`:1839-1841`、`:1862-1878`。
   - 描述：100 条 `equal` × 每条 1000 值 = 10 万占位符，超过 PG 65535。`54000` 虽映射为 ResourceExhausted，仍会在解析/绑定时制造大 SQL 与 CPU/内存尖峰。
   - 影响：认证且对集合有 list 权的调用方可做廉价 DoS。不是注入。
   - 建议：对全部 filter 值累计计数，超过例如 2000 即 InvalidArgument。

### P3 低

1. **cursor 文档存在性探测（W8 的新向量）**
   - 位置：`postgres.go:944-951`。
   - 描述：`cursorAfter`/`cursorBefore` 按 `_id+_tenant` 取排序键，**不做 read 鉴权**。存在 → 继续列表；不存在 → `cursor document not found`。可枚举集合内任意文档 ID。
   - 影响：与已接受的 W8 同类（存在性，非内容）。
   - 建议：找不到或无 read 权时统一返回 InvalidArgument（不区分原因），或要求 cursor 落在当前权限过滤结果内。

2. **`PlatformAdmin` 走 System 路径，跳过查询白名单**
   - 位置：`access.go:40-42`；`postgres.go:853-880`。
   - 描述：console owner/admin 的 List/Count 不跑 `validateQueryFields`，可对 `password_hash` 做等值过滤（响应侧仍脱敏）。主体本身已是超管，不构成外部越权。
   - 建议：若要最小权限，仅 `__system__` 跳过白名单，`PlatformAdmin` 仍校验敏感列。

3. **`reconcileSystemCollectionAttrs` 不补索引**
   - 位置：`postgres.go:1185-1200`、`:1215-1236`。
   - 描述：已存在的系统集合只补缺失属性。spec 后续增加的 unique/fulltext（如 `users.email`）不会补到存量项目。
   - 建议：按 `(type, attributes)` 幂等 `CreateIndex`。

4. **`limit(0)` 在 `ParseMany` 中被当成「未指定」**
   - 位置：`pkg/query/query.go:194-196`（`if q.Limit != 0`）。
   - 描述：解析允许 0，合并时丢弃，adapter 回退默认 50。无法表达「返回 0 行」。
   - 建议：用 `*int` / `hasLimit` 区分未设置与显式 0。

5. **`pkg/query` 测试偏薄**
   - 位置：`pkg/query/query_test.go`（仅 happy path + 负数 limit/offset）。
   - 描述：缺未知算子、不平衡引号、`contains` 通配符、`BuildFilter` 往返、超长/非法字段等用例。实现本身是 fail-fast 的。
   - 建议：补解析失败与转义回归。

6. **其它小项**
   - `pgTextArray`（`postgres.go:1276-1282`）手搓数组字面量，角色来自 JWT/labels；当前双引号转义可挡住注入，更稳妥是 `pgdialect.Array`。
   - Bulk 条数上限只在 app 层（`maxBulkOperations=1000`），adapter 自身不封顶；内部 `session_service` 走 SystemPrincipal 直调。
   - `select` 投影在取回 `to_jsonb(d.*)` 之后做（`:991-1002`），大 JSONB 仍全量传输。
   - `systemCollectionSpecs(projectID)` 未使用参数（`system_collection_specs.go:14`）。
   - `buildIncrementParts` 不校验目标列为 integer/float（对比 `SumDocumentField:1100-1113`）。
   - 自定义 `order` 只追加 `_created_at`、无 `_id` tiebreaker（`:1943-1945`）；默认排序已有（`:1928-1929`）。
   - `CREATE INDEX` 非 `CONCURRENTLY` 且在事务内，大表会锁写。
   - `cleanupKeysWritePerms` 对 `document_collections` 的 UPDATE 无 `project_id`（`:1247-1249`），每个项目首次 bootstrap 都会全表扫一次；语义幂等，仅为噪音/锁范围。

---

## 模块结论

- **权限模型**：用户集合文档级优先、系统集合 OR、keys 按角色匹配、超管显式 bypass——与 B1/B2 及测试矩阵一致，**未见提权回归**。
- **隔离水平**：schema-per-project + 全路径 `_tenant` 谓词 + 标识符 quoting + 值参数化，隔离扎实。
- **最需优先处理的 3 项**（均为 P2，无安全紧急项）：
  1. 落地 array 列类型（或从 API 撤回该字段），避免静默错误数据。
  2. 删属性时同步清理索引元数据，避免 catalog 撒谎。
  3. List 批量回传 `_perms`，并给 upsert 冲突列加唯一索引白名单。

本模块 Round 3 **可以关闭安全主线**；上述 P2 建议排进常规迭代，不必再为此开一轮全量安全复审。
