# 修复任务 F3：动态文档层修复（documentdb + query）

## 角色

你是资深 Go + PostgreSQL 工程师，负责修复 Torchwood 动态文档层（Postgres adapter + 查询 DSL）
的审查发现。方案详见 `docs/review/fix-plan.md` §3（F3 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`、`docs/review/fix-plan.md` §3
- 审查报告（背景）：`docs/review/` 下的 02 报告（含 F4-2 并入项见下）

## 修复清单

1. **UpsertDocument TOCTOU 竞态提权**（P0）：`internal/infra/documentdb/postgres.go:481-507`
   权限预查（SELECT 冲突行）与 `:533-540` 的 INSERT ... ON CONFLICT 非原子——预查无行走
   create 权限放行，随后 ON CONFLICT DO UPDATE 可改写并发插入的他人行。
   修复：将「预查 + upsert」包进 `p.db.RunInTx`；预查改为
   `SELECT _id ... FOR UPDATE`（锁住冲突目标行后再检查权限，INSERT 在同一事务内执行）；
   或对 `(_tenant, 冲突列值)` 加 `pg_advisory_xact_lock` 串行化。
   补并发集成测试：victim 与 attacker 并发插入同一冲突值，断言 attacker 无法改写。
2. **ListDocuments page_size 参数失效**（P1）：`pkg/query/query.go:205-207` `ParseMany`
   对任何输入（含空）都在 `merged.Limit == 0` 时注入默认 50，导致
   `postgres.go:729-735` 的 `if limit == 0 { limit = q.PageSize }` 成为死代码。
   修复：`ParseMany` 不注入默认 limit（默认值交由 adapter 决定）；ListDocuments 改为
   「DSL 未显式指定 limit 时用 q.PageSize」，保留上限 clamp（maxQueryLimit）；
   重写 `postgres_test.go:638-643` 的 `TestListDocuments_PaginationGuards`（现断言
   因恒 50 恰好通过，掩盖缺陷）；补 `page_size=5` + 查询断言返回 5 条。
3. **CreateDocument 尾随读回半完成状态**（P1）：`internal/app/server/databases.go:333-336`
   与 `internal/app/client/databases.go:140-143` 在 adapter 已用 SystemPrincipal 读回后，
   又用调用方 principal 重新 GetDocument，权限不含调用方时返回 403（数据已落库）。
   修复：删除 app 层冗余重读，直接返回 adapter 的 `created`。
4. **文档写入与 _perms 非原子**（P1）：`postgres.go:424-432`（Create）、`:618-635`（Update）、
   `:664-667`（Delete）中数据语句与 setPermissions/clearPermissions 分离，第二步失败时
   权限丢失退化 fail-open。修复：包进 `p.db.RunInTx`（参考 Bulk 已有先例，
   `clients.InTx` 嵌套防护可复用）。
5. **DDL 与元数据非事务**（P2，含 F4-2）：`postgres.go:334-352`（CreateAttribute）、
   `:361-375`（CreateIndex）、`:244-252`（DeleteAttribute）、`:278-286`（DeleteCollection）、
   `:166-175`（CreateCollection）DDL 与 `document_*` 元数据写入分离。
   修复：包进同一 `RunInTx`（PG 支持事务内 DDL）；**DeleteDatabase（:143-154）与
   DeleteCollection 必须级联删除 `document_collections`/`document_attributes`/
   `document_indexes` 对应行**（按 project_id/database_id[/collection_id] 过滤），
   否则「删了建不回来」。
6. **错误分类与校验补强**（P2）：
   - `internal/app/shared/docdb_errors.go:19-31` 补 42P10（无唯一索引的 ON CONFLICT）与
     23505（元数据重复键）映射；
   - `postgres.go:639-645` UpdateDocument 目标不存在 → 定义 domain ErrDocumentNotFound
     并映射 codes.NotFound（保持 Bulk 回滚依赖的错误行为）；
   - `postgres.go:378-385` CreateDocument 补 `validateDocID`；
   - `postgres.go:914-953` SumDocumentField 校验字段 ∈ 集合声明属性且为数值类型；
   - `postgres.go:1722-1730` contains/startsWith/endsWith 对值做 `%`/`_`/`\` 转义并加
     `ESCAPE '\'` 子句；
   - `postgres.go:740-744` ListDocuments 的 DecodePageToken 失败显式返回 InvalidArgument
     （对齐 ListCollections）；
   - `postgres_permissions.go:66-68` 权限检查 N+1 → 复用调用方已取的 coll。

## 约束

- 不修改 proto；不修改 `internal/app/server/teams.go`、`users.go`（级联删除在 F4 批次）
- 保持现有代码风格与错误码约定；不引入新依赖；除必要外不新增注释
- 不运行需要本地 Postgres 的集成测试（可静态阅读现有测试并更新其中被本批次破坏的断言）

## 验证

- `go vet ./internal/infra/documentdb/... ./pkg/query/... ./internal/app/server/databases.go ./internal/app/client/databases.go ./internal/app/shared/...`
- `go test ./pkg/query/...`（纯单元测试）
- `go build ./...`
- 更新受影响的现有测试断言（如 pagination guards、upsert 权限测试）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；说明哪些现有测试
被修改及原因；列出需要 CI（本地 Postgres）才能验证的项。
