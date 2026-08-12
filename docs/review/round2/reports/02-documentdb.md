# 复审报告（Round 2）：02 - 动态文档层（Postgres adapter + 查询 DSL）

> 审查基准：当前工作区代码（commit 1288705..HEAD，工作区无未提交代码改动）。
> 审查范围：`internal/infra/documentdb/`、`pkg/query/`、`internal/app/server/databases.go`、`internal/app/client/databases.go`、`internal/app/server/users.go`、`internal/app/server/teams.go`、`internal/app/shared/docdb_errors.go`。
> 辅助验证：`go vet ./internal/infra/documentdb/... ./pkg/query/... ./internal/app/server/... ./internal/app/client/... ./internal/app/shared/... ./internal/domain/databases/... ./internal/infra/clients/...` 通过；`go test ./pkg/query/... ./internal/domain/databases/... ./internal/app/shared/...` 通过；`go test -short ./internal/infra/documentdb/...` 通过（集成测试因需 Postgres 被 `-short` 跳过）。

---

## 1. 修复验证结论表

| 编号 | 修复项 | 结论 | 证据（文件路径:行号）与说明 |
|------|--------|------|----------------------------|
| F3-1 | UpsertDocument TOCTOU 竞态提权 | ✅ 已修复 | `internal/infra/documentdb/postgres.go:504-533` 外层包 `RunInTx`；`:540-650` `upsertDocument` 在 `:556-559` 对 `(_tenant, 冲突列值)` 取 `pg_advisory_xact_lock(hashtext(?), hashtext(?))` 串行化；`:564-571` 预查目标行；`:587-593` 按命中目标行做 `update` 权限检查、未命中则检查集合级 `create`。`postgres_test.go:162-217` `PrivilegeEscalationRejected` 与 `:224-283` `ConcurrentRace` 覆盖单并发与并发场景。 |
| F3-2 | ListDocuments page_size 参数失效 | ✅ 已修复 | `pkg/query/query.go:184-208` `ParseMany` 不再注入默认 limit；`internal/infra/documentdb/postgres.go:877-886` 在 DSL 未显式指定 limit 时使用 `q.PageSize`，`<=0` 回退 50，并 clamp 到 `maxQueryLimit`；`:888-891` 非法 `PageToken` 显式报 `InvalidArgument`。`postgres_test.go:681-773` `PaginationGuards` 重写，真实断言 page_size、token 续页、负数回退、DSL limit 优先级、offset 上限。 |
| F3-3 | CreateDocument 尾随读回半完成状态 | ✅ 已修复 | `internal/app/server/databases.go:367-373` 直接返回 adapter 结果，注释说明不再以调用方 principal 重读；`internal/app/client/databases.go:140-149` 同样直接返回。adapter 内部 `postgres.go:488-495` 用 `SystemPrincipal` 读回。 |
| F3-4 | 文档写入与 _perms 非原子 | ✅ 已修复 | `postgres.go:411-427` `CreateDocument` 包 `RunInTx`；`:431-496` `createDocument` 中 INSERT 与 `:485` `setPermissions` 共事务。`:701-717` `UpdateDocument` 包 `RunInTx`；`:720-782` `updateDocument` 中 UPDATE 与 `:764-769` `clearPermissions`/`setPermissions` 共事务。`:784-794` `DeleteDocument` 包 `RunInTx`；`:796-815` `deleteDocument` 中 `:810` `clearPermissions` 与 DELETE 共事务。`internal/infra/clients/tx.go:24-28` 提供 `InTx`；`postgres_permissions.go:153-178`/`202-223` Bulk 操作通过 `InTx` 避免嵌套事务。`postgres_test.go:900-935` `BulkUpdateDocuments_RollbackOnFailure` 覆盖中途失败整体回滚。 |
| F3-5 | DDL 与元数据非事务（含 F4-2 清理） | ⚠️ 部分修复 | ✅ `postgres.go:180-194` `CreateCollection`、`:363-384` `CreateAttribute`、`:392-403` `CreateIndex`、`:250-260` `DeleteAttribute`、`:296-315` `DeleteCollection`、`:149-168` `DeleteDatabase` 均已包入 `RunInTx` 并同步清理 `document_collections`/`document_attributes`/`document_indexes`。`postgres_test.go:389-448` 覆盖删集合后 `_perms` 清理与同名重建。❌ `DeleteIndex`（`:263-279`）仍直接 `DROP INDEX` 后删 `document_indexes`，未包 `RunInTx`，见「新发现问题 P1-1」。`CreateDatabase`（`:83-104`）元数据 INSERT 使用 `p.db.NewInsert()` 未接入事务连接，见「新发现问题 P1-2」。 |
| F3-6 | 错误分类与校验补强 | ✅ 已修复 | `internal/app/shared/docdb_errors.go:19-33` 已补 `42P10`/`23505` 等 SQLSTATE 映射；`postgres.go:778-779` `UpdateDocument` 目标不存在返回 `databases.ErrDocumentNotFound`；`:436-439` `CreateDocument` 调 `validateDocID`；`:1064-1115` `SumDocumentField` 有字段白名单 + 数值类型校验；`:655-659` `escapeLikePattern` 配合 `:1886-1893` `ESCAPE '\'` 转义 `%`/`_`；`:888-891` `DecodePageToken` 失败显式 `InvalidArgument`；`postgres_permissions.go:58-87` `checkDocumentPermission` 复用调用方传入的 `coll`。 |
| F4-1 | 用户/团队级联删除被 50 条截断 | ✅ 已修复 | `internal/app/server/users.go:335-416` `deleteUserCascade` 对 `sessions`/`identities`/`memberships` 均调 `cascadeListAll`；`:418-436` `cascadeListAll` 设 `PageSize: cascadePageSize`（`:414` 定义 `cascadePageSize = 1000`）并循环至 `NextPageToken` 为空。`internal/app/server/teams.go:467-469` `listMembershipDocs` 同样使用 `cascadeListAll`。`users_integration_test.go:192-254` `TestServerUsers_DeleteUser_CascadeBeyondDefaultPage` 直插 61 条 sessions + 61 条 memberships 验证级联清理。 |

**统计**：✅ 5 项、⚠️ 1 项（F3-5 部分修复）、❌ 0 项、🔴 0 项。

---

## 2. 新发现问题

### 🔴 P0 严重

本次复审未发现新的 P0 问题。F3-1 TOCTOU 修复已落地，并发提权路径被关闭；SQL 注入、越权读写、跨租户泄露等 Round-1 高风险项未出现回归。

### 🟠 P1 高

1. **DeleteIndex 的 DDL 与元数据删除未事务化**
   - 位置：`internal/infra/documentdb/postgres.go:263-279`
   - 问题：`DROP INDEX IF EXISTS` 与 `document_indexes` 元数据删除之间无 `RunInTx`。若元数据删除失败（如连接中断），会出现物理索引已不存在但 catalog 仍保留记录；后续重建同名索引时，`CREATE INDEX IF NOT EXISTS` 可重建物理索引，但元数据唯一约束可能因残留旧行而冲突，且 `GetCollection` 会返回已不存在的索引定义。
   - 影响：与 F3-5 同性质的一致性问题，删集合/删库场景已修复，唯独删索引遗漏。
   - 修复建议：参照 `DeleteAttribute`/`DeleteCollection`，将 `DeleteIndex` 整体包入 `p.db.RunInTx(ctx, ...)`。

2. **CreateDatabase 元数据写入未接入事务连接**
   - 位置：`internal/infra/documentdb/postgres.go:83-104`
   - 问题：`CREATE SCHEMA IF NOT EXISTS` 与 `ensurePermsTable` 走 `p.conn(ctx)`，但 `document_databases` INSERT 使用 `p.db.NewInsert()`，且整个函数未包 `RunInTx`。若 schema/permissions 表创建成功但元数据行写入失败，会出现 schema 存在而 `ListDatabases`/`GetDatabase` 看不到该库；再次创建同名库时 schema 已存在虽幂等，但元数据仍缺失，需人工清理。
   - 影响：元数据与物理对象不一致，与 F3-5 目标相悖。
   - 修复建议：将 `CreateDatabase` 整体包入 `RunInTx`，并把 INSERT 改为 `p.conn(txCtx).NewInsert()`。

3. **ListCollections 查询未接入事务连接**
   - 位置：`internal/infra/documentdb/postgres.go:227-238`
   - 问题：`COUNT(*)` 与主查询均使用 `p.db.NewSelect()`，而非 `p.conn(ctx)`。当前无调用方在事务内调用，但若未来有（如 DDL 批量迁移），会逃逸外层事务，读到未提交状态或破坏原子性。
   - 影响：事务语义不统一，与 F3-4/F3-5 倡导的 `conn(ctx)` 统一原则不一致。
   - 修复建议：改为 `p.conn(ctx).NewSelect()`。

4. **UpdateDocument 仅更新 permissions 时不刷新审计列**
   - 位置：`internal/infra/documentdb/postgres.go:746-770`
   - 问题：`buildUpdateParts`（`:1638-1656`）在 `doc.Data` 无有效字段时返回空 `setParts`；当请求仅更新 `permissions` 时，`:753-762` 的 UPDATE 语句被跳过，`:764-769` 仅执行权限替换，`_updated_at`/`_updated_by` 不会被更新。
   - 影响：权限变更后文档的「最后修改时间/操作者」信息失真，审计与缓存失效策略可能受影响。
   - 修复建议：在仅更新 permissions 分支也显式追加 `_updated_at = NOW()` 与 `_updated_by` 的 SET。

### 🟡 P2 中

1. **CountDocuments 与 ListDocuments 非原子快照**
   - 位置：`internal/infra/documentdb/postgres.go:948-957`（List 的 COUNT + 主查询），`CountDocuments:1056-1059` 为独立查询。
   - 问题：两次查询在 `READ COMMITTED` 下非同一快照，并发写入时 `total` 与返回行数可能不一致（能 count 不能 list 或反之）。当前实现与 Appwrite 行为一致，但属于已知的弱一致性。
   - 影响：低；客户端续页已基于 offset token，不影响正确性。
   - 修复建议：文档化该行为；如需强一致，可在同一事务内使用 `REPEATABLE READ` 快照。

2. **UpsertDocument advisory lock 键存在潜在碰撞**
   - 位置：`internal/infra/documentdb/postgres.go:664-672`
   - 问题：`conflictLockKey` 用 `fmt.Sprint(v)` 与 `\x00` 分隔拼接冲突值，极端情况下不同值组合可能生成相同字符串（如值本身含 `\x00`），导致无关冲突值被串行化。
   - 影响：仅性能降级（不必要的串行），不影响正确性；且 SQL 参数通常为标量字符串/数字，风险低。
   - 修复建议：使用带长度前缀的序列化（如 JSON、protobuf）或 PostgreSQL `hash_record`。

### 🟢 P3 低

1. **CreateCollection handler 尾随 GetCollection 重查**
   - 位置：`internal/api/servergrpc/databases.go:122-132`
   - 问题：`CreateCollection` 成功后立即 `GetCollection` 返回响应，多一次元数据查询；若重查失败则返回 `Internal`。adapter 层 `CreateCollection` 已完成元数据写入，可直接返回或统一错误映射。
   - 影响：额外查询与错误码不一致。
   - 修复建议：复用 `CreateCollection` 结果或移除冗余重查。

---

## 3. 模块总体结论

- **修复完成度估计**：约 **90%**。F3-1、F3-2、F3-3、F3-4、F3-6、F4-1 已完整落地并配套测试；F3-5 主体（Create/Delete Collection/Attribute、Create Index、Delete Database 元数据清理）已完成，但 `DeleteIndex` 与 `CreateDatabase` 的事务化遗漏导致元数据一致性仍有缺口。
- **剩余风险 Top 3**：
  1. `DeleteIndex` 未事务化，删索引失败时 catalog 与物理索引不一致。
  2. `CreateDatabase` 元数据写入未接入事务，schema 与 catalog 可能不同步。
  3. `UpdateDocument` 仅改权限时不更新审计列，时间戳语义不完整。
- **是否建议关闭本模块审查**：**不建议立即关闭**。建议在补齐上述 3 项 P1 修复（至少 P1-1、P1-2 两项事务化缺口）并补充相应集成测试后，再进行一次轻量复审确认，方可关闭动态文档层 Round-2 审查。
