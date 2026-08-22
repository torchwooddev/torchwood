# 本轮残留猎漏

**HEAD**: `41b837c65741fc655ec81da228b86180eab15d75`（`refs/heads/main`）
**结论**: 有残留但不挡收尾

运行时 / 迁移 / 测试对 Wave 0–3、E-5 cut、C-1、V-4、D-7、M-4、S-4、D-6 **已闭合**：staged 事务 RPC/表/Wire 已消失；public catalog 已 DROP；系统资源热路径走 bun 静态表；worker 边界源码与 `wire_gen.go` 未拉回禁止包。残留主要是**当前态文档仍按 cut 前世界描述**，以及 E5-5 contract 未删干净的 no-op / sentinel 测试缝。这些不会在 migrate 后炸库或编不过。

未跑 `go list -deps`（本评审无 shell）；C-1 以 `cmd/worker` 生产源码 import、`wire_gen.go` 与 `import_guard_test.go` 交叉核对。

## 残留表

| 主题 | 状态 | 证据 file:line |
|------|------|----------------|
| D-6 staged transactions | 运行时已收干净 | Go/TS/proto 无 `CreateTransaction`/`TransactionRepository`/`shared.NewTransactions`；`proto/server/v1/databases.proto:115-162` 无 Transaction RPC；`cmd/server/wire_gen.go` 无相关构造；`internal/app/server/projects.go:171-191` 删项目不碰 `document_transactions`；`internal/domain/events/envelope.go:92-121` `ClientPayload` 不含 `transaction_id`（`envelope_test.go:64` 断言）；swagger 无 Transaction。允许残留：`proto/client/v1/payments.proto:146`、wechat/iosiap `transaction_id`、`db/migrations/000012_*` 与 `000021_*`、历史设计/prompts。 |
| D-7 public catalog | 运行时已收干净 | `db/migrations/000020_drop_public_document_catalog.up.sql:4-7` DROP 四表；runtime 一律 `ModelTableExpr("?.document_*", cat)`（`internal/infra/documentdb/postgres.go:122-123` 等）；`internal/app/server/projects_test.go:69-77` 用 `to_regclass` **断言 public 四表已删**，不是当它们还在 COUNT。 |
| E-5 系统表 cut | cut 完成；E5-5 contract 半留 | `EnsureSystemCollections` 为 no-op（`postgres.go:1433-1435`，接口注释 `repository.go:15-16`）；CreateProject 走 `projectschema.Apply`（`projects.go:98-103`），生产调用点已无 Ensure；Users/Sessions 热路径 bun（`wire_gen.go:64-65` `NewUserRepository`/`NewSessionRepository`，`internal/infra/bun/model/users.go` 无 `_version`）；`000009_system_tables_cut.up.sql:49-55` RENAME `sys_*`→最终名并删 catalog sentinel。静态 `users` 无 `_version`（`postgres_test.go:920-924`）。生产 Account/Users 不 `ListDocuments(users/sessions)`；剩余 ListDocuments 仅 `SeedLegacySystemDocumentCollections` 测试重建旧文档表（`occ_test.go:266`、`postgres_test.go:301`）。 |
| C-1 worker 边界 | 已收干净（源码/Wire/守卫） | `cmd/worker/import_guard_test.go:16-32` 禁 `internal/app` 桶、`app/client|console|server`、`internal/api`、`infra/auth|documentdb|server`、`genproto`；`provides.go:12-31` / `wire_gen.go:12-27` 只拉 `app/{assets,billing,functions,payments,storage,subscriptions}` 与作业适配器。生产 `*.go` 无禁止 import。 |
| V-4 Functions.Execute | 已收干净 | proto/Go 无 `Functions.Execute` / `ExecuteCommand`；保留 `sanitizeEnv`/`RuntimeImage`（`internal/app/functions/functions.go:38-49`）与领域 `executor.Execute`（`executions.go:200`）。对外是 `CreateExecution`（`proto/server/v1/functions.proto:109`）。 |
| M-4 资产五动词 | 已收干净 | 领域 `Service.Grant/Consume/Transfer/Mutate/Expire`（`internal/domain/assets/write.go:12-603`）；app 只鉴权后委托（`internal/app/assets/write.go:48-80`）。Holding 无五动词。 |
| S-4 pkg/uow | 已收干净 | `pkg/uow/uow.go:1-12` 有 `Runner.Run`；注释未声称「每个调用方都用 ctx bun.Tx 且无 Runner」。增量可留。 |
| E-8 / E-3 | 已收干净 | `ProjectResolver` 类型零命中（仅历史评审文）；`internal/domain` 生产代码不 import grpc（`service_test.go:71-94` 守卫）；`pkg/ident` 无 grpc；`ActorKind("agent")` 非法（`principal.go:10-22`，`principal_test.go:10-12`）。 |
| 当前态文档 | **撒谎** | `AGENTS.md:43` 仍写「系统文档集合」；`docs/developer/06-databases.md:13,85-108,265` 仍把 Ensure 写成播种文档集合 + sentinel + `_perms`；`05-authentication.md:160`、`09-api-guide.md:317`、`11-testing.md:86` 同。`docs/design/project-data-plane-schema.md` 状态仍 Draft，把 public catalog / Ensure 播种写成「当前」。`docs/roadmap.md:52,297,347,487` 对 D-6 已改口。`docs/design/system-tables.md:3` 已标「已落地」。 |
| DI / proto | 已收干净 | `cmd/server/wire_gen.go`、`cmd/worker/wire_gen.go` 无已删 Transaction 构造；databases proto 无 Transaction RPC。 |
| 迁移闭合 | 已收干净 | `000012` 建 tx 表 → `000020` DROP public catalog → `000021` DROP `public.document_transaction_ops` / `public.document_transactions`。`internal/infra/projectschema/migrations/` 无 `document_transactions`。 |
| 会在已 migrate DB 上炸的测试 | 未发现 | 无测试 INSERT `document_transactions`；无 `CreateTransaction` 调用；`projects_test.go:69-77` 断言 public catalog 已不存在。大量测试仍调 no-op `EnsureSystemCollections`，不依赖它建表。 |

## Issues

### Issue 1 -- Severity: suggestion
- **File**: AGENTS.md:43
- **Description**: 当前态宪法仍写项目数据面「容纳**系统文档集合**」，并指向 `docs/design/project-data-plane-schema.md`。E-5 cut 后 `tw_<project>.users` 等是 bun 静态表（无 `_id`/`_perms`/`_version`），catalog 无 `database_id='_'`。新贡献者会按文档集合实现 Account/Storage。
- **Suggestion**: 改成「系统静态表（users/sessions/… bun）+ 文档目录 + 账本 / Functions / OAuth」；sentinel `_` 只作为对外拒绝的内部寻址，不再叫系统文档库。
- **Status**: open

### Issue 2 -- Severity: suggestion
- **File**: docs/developer/06-databases.md:13 ; docs/developer/06-databases.md:85-88 ; docs/developer/06-databases.md:90-108 ; docs/developer/06-databases.md:265
- **Description**: 开发者当前态指南仍描述：`tw_<project>` 容纳系统**文档**集合；系统资源「均含 `_perms`」；`EnsureSystemCollections` 引导 catalog sentinel `document_databases(id='_')`、七集合 spec、`cleanupKeysWritePerms`、`reconcileSystemCollectionAttrs`；§6 仍说引导依赖 `DO NOTHING` 幂等而非事务。代码里 Ensure 已是 `return nil`（`postgres.go:1433-1435`），`cleanupKeysWritePerms` / `system_collection_specs.go` 已不存在，CreateProject 用 `projectschema.Apply`。同文件 §6 已正确写 D-6 删除 staged API，E-5/D-7 段未跟。
- **Suggestion**: 按 cut 后事实重写 §1–2.1 与 §6 末条：系统资源 bun + FK；catalog 只服务用户 collection；Ensure 为保留签名的 no-op。
- **Status**: open

### Issue 3 -- Severity: suggestion
- **File**: docs/developer/05-authentication.md:160 ; docs/developer/09-api-guide.md:310-321 ; docs/developer/11-testing.md:86
- **Description**: 其它当前态开发文档仍把 `EnsureSystemCollections` 写成活路径：认证文称它会跑 `cleanupKeysWritePerms`；API 指南把 CreateProject 事务后半段画成 `EnsureSystemCollections`（真实是 `projectschema.Apply` + `CreateDatabase`，`projects.go:87-104`）；测试指南示例仍要求 Ensure 才能 CRUD。
- **Suggestion**: 三处按 `projects.go` / `testutil.CreateTestProject` 改样例；认证节删掉 Ensure/cleanupKeys 叙事。
- **Status**: open

### Issue 4 -- Severity: suggestion
- **File**: docs/design/project-data-plane-schema.md:4-18 ; docs/design/project-data-plane-schema.md:24-66 ; docs/design/project-data-plane-schema.md:81-83
- **Description**: 猎漏清单把它当当前态；文首状态仍是 **Draft**，「当前物理布局」仍列 `public.document_databases/collections`，Goals 仍写系统集合「仍是文档」。AGENTS.md 把它当三层模型出处。D-7/E-5 落地后这段是过期「今日」。`system-tables.md:3` 已标「已落地」，本文没有同等过期横幅。
- **Suggestion**: 加 stale/superseded 横幅（D-7 catalog 已迁走并 DROP public；E-5 已表化），或把 Overview/Goals 改成落地后事实并改状态。
- **Status**: open

### Issue 5 -- Severity: suggestion
- **File**: proto/shared/v1/document.proto:17 ; internal/domain/databases/document.go:41 ; internal/infra/documentdb/postgres.go:2184
- **Description**: E-5 验收旧号 8 要求 `Document.Version` 不再出现「系统集合恒为 0」分支。注释仍写「系统集合恒为 0」；adapter 仍写「由 app 层按 IsSystemCollection 归零」，app 文档核未见归零。运行时用户集合 OCC 不受影响；易让人以为系统表还走 Documents version=0。
- **Suggestion**: proto/领域注释改为「仅用户 collection OCC；系统资源不经 Document.Version」。删掉「app 归零」误导句。
- **Status**: open

### Issue 6 -- Severity: nit
- **File**: internal/domain/databases/repository.go:26 ; internal/infra/documentdb/postgres.go:1504-1517 ; pkg/ident/ident.go:15-18
- **Description**: E5-5 contract（`system-tables.md:699`）「可同 cut 若 grep 已零」：删 Ensure 调用点与 spec、删 `documentSchema` sentinel 分叉。Cut 已完成（Ensure no-op，catalog 无 `_`）。E5-4 明确不拆 `RejectExternalDatabaseID`/`businessSchema`，故对外拒 `_` 不是半切事故。留下的是：接口仍挂 Ensure；`documentSchema` 仍把 `_` 映射到 `tw_<project>`，供 `SeedLegacySystemDocumentCollections` 测拷贝/OCC；大量测试仍调用 no-op Ensure。不会在已 migrate DB 上失败。
- **Suggestion**: 收口 PR：测试改为直接 `CreateTestProject`/`SeedLegacy…`；确认无生产调用后删 SchemaApplier.EnsureSystemCollections；sentinel 分叉若只服务遗留测再决定是否缩进测试 helper。
- **Status**: open

---

运行时与迁移可以宣布本轮收尾。对外宣称「宪法已跟上 E-5/D-7」之前，至少改 `AGENTS.md` 与 `docs/developer/06-databases.md`。
