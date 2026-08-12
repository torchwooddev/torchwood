# 复审任务（Round 2）：02 - 动态文档层（Postgres adapter + 查询 DSL）

## 背景

- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色

你是资深 Go + PostgreSQL 安全/数据库代码审查专家，对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「动态文档层」做 Round 2 复审。同时你是修复验证者，需对照 `fix-plan.md` 逐条核实修复是否真正落地、是否完整、是否引入回归。

## 第一步：建立基线

- 读 `docs/review/prompts/02-documentdb.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 **F3 全部章节**（含 §12 说明 F4-2 已并入 F3-5 处理）与 **F4-1 章节**：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- internal/infra/documentdb/ pkg/query/ internal/app/server/databases.go internal/app/client/databases.go internal/app/server/users.go internal/app/server/teams.go` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/archived/databases-security-fix-plan.md`、`docs/archived/databases-fix-plan.md`（历史安全修复背景）
- 架构：`internal/infra/documentdb` 是动态文档适配器：每个 project 一个 PostgreSQL schema，每个 collection 一张真实表，含 `_tenant`、`_perms`、`_createdAt`、`_updatedAt` 与动态 attribute 列
- 权限模型：`_perms` 实现角色权限（`read:any`/`write:any`/`read:users`/`write:users`/`keys` 角色等）；API Key 以 `keys` 角色参与，不默认 bypass；admin 可绕过
- 查询使用 Appwrite 风格 DSL（`pkg/query`）：`equal`、`greaterThan`、`contains`、`orderDesc`、`limit` 等
- 已知修复（2026-08）：upsert ON CONFLICT 提权漏洞（commit cd565f5）——注意检查是否回归

## 复审重点 A：修复验证（逐条核实）

对 fix-plan 中本模块的**每一个修复项**，按以下格式逐条列出（编号 + 一句话标题 + 文件锚点），并逐条核实：

1. **F3-1 UpsertDocument TOCTOU 竞态提权**（P0）—— `internal/infra/documentdb/postgres.go:481-507,533-540`
   - 核实：是否把「预查 + INSERT ON CONFLICT」包进 `p.db.RunInTx`；预查是否改为 `SELECT _id ... FOR UPDATE` 或 `pg_advisory_xact_lock` 串行化；是否补并发集成测试。
2. **F3-2 ListDocuments page_size 参数失效**（P1）—— `pkg/query/query.go:205-207`、`postgres.go:729-735`
   - 核实：`ParseMany` 是否不再恒注入默认 50；ListDocuments 是否在 DSL 未显式指定 limit 时使用 `q.PageSize` 并保留上限 clamp；`TestListDocuments_PaginationGuards` 是否被重写并断言真实行为。
3. **F3-3 CreateDocument 尾随读回半完成状态**（P1）—— `internal/app/server/databases.go:333-336`、`internal/app/client/databases.go:140-143`
   - 核实：app 层是否删除冗余 principal 重读，直接返回 adapter 的 `created`；若保留尾随读，是否显式处理 PermissionDenied。
4. **F3-4 文档写入与 _perms 非原子**（P1）—— `postgres.go:424-432`（Create）、`:618-635`（Update）、`:664-667`（Delete）
   - 核实：数据语句与 setPermissions/clearPermissions 是否包进同一 `p.db.RunInTx`；嵌套事务是否安全（参考 Bulk 已有先例与 `clients.InTx` 嵌套防护）。
5. **F3-5 DDL 与元数据非事务（含 F4-2 DeleteDatabase/DeleteCollection 不清理元数据）**（P2）—— `postgres.go:334-352`（CreateAttribute）、`:361-375`（CreateIndex）、`:244-252`（DeleteAttribute）、`:278-286`（DeleteCollection）、`:166-175`（CreateCollection）
   - 核实：DDL + 元数据写入是否包进同一 `RunInTx`；DeleteDatabase/DeleteCollection 是否同步删除 `document_collections`/`document_attributes`/`document_indexes` 对应行；删库/删集合后重建同名资源是否成功。
6. **F3-6 错误分类与校验补强**（P2）—— `internal/app/shared/docdb_errors.go:19-31`、`postgres.go:639-645`、`postgres.go:378-385`、`postgres.go:914-953`、`postgres.go:1722-1730`、`postgres.go:740-744`、`postgres_permissions.go:66-68`
   - 核实：`docdbErrorSQLStates` 是否补 42P10/23505；UpdateDocument 目标不存在是否映射 NotFound；CreateDocument 是否补 `validateDocID`；SumDocumentField 是否加字段白名单+类型校验；contains/startsWith/endsWith 是否转义 `%`/`_` 并带 `ESCAPE '\'`；DecodePageToken 失败是否显式报 InvalidArgument；权限检查路径 N+1 是否复用调用方已取的 coll。
7. **F4-1 用户/团队级联删除被 50 条截断**（P0，涉及 documentdb 分页能力）—— `internal/app/server/users.go:287-326`、`internal/app/server/teams.go:455-463`
   - 核实：级联 ListDocuments 是否设 `PageSize: 1000` 并循环直至 `NextPageToken` 为空；DeleteUser 的 sessions/identities/memberships 三集合、DeleteTeam 的 memberships 是否均同处理；是否补 >50 会话/成员集成测试。

对每条修复项，检查：
1. 修复是否已落地（代码中能否找到对应改动）；
2. 修复是否正确完整——有无绕过路径、边界遗漏（例如只改了入口 A 没改入口 B、校验可加在错误层、并发场景仍可乘）；
3. 修复是否引入新问题（接口/行为变化是否同步到全部调用方与前端/SDK）；
4. 承诺的测试是否真实存在且断言的是真实行为（不是恰好通过的假断言）。

## 复审重点 B：回归与新问题排查

- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归），特别是 `pkg/query` 的 limit 默认值移除后是否影响其他调用方。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（注入、越权、信息泄露、输入校验）、正确性（错误处理、并发、事务边界）、一致性（与 AGENTS.md 约定、proto 注解、`internal/domain/databases` 端口签名）、测试质量。
- **本模块修复后特有风险点**：
  1. F3-1/F3-4 事务化后重查死锁/嵌套事务：`RunInTx` 与 `clients.InTx` 嵌套是否安全，批量操作是否出现长事务持有锁； upsert 加 `FOR UPDATE` 后高并发下是否产生死锁。
  2. F3-5 DDL 事务化后重查元数据一致性：DDL + 元数据写入同一事务，失败回滚后是否遗留中间状态；DeleteDatabase/DeleteCollection 同步清理元数据后，sequence/index/constraint 是否也被清理，重建同名资源是否成功。
  3. F3-2 分页改造后重查 count 与列表权限一致性：`ParseMany` 不再注入默认 limit 后，count 查询是否仍与列表查询使用完全一致的权限过滤条件，是否出现能 count 不能 list 或反之的偏差。
  4. F3-6 错误映射补强后重查调用方契约：新增的 `ErrDocumentNotFound`/`InvalidArgument` 等错误是否被所有 gRPC handler 正确转换，是否破坏 SDK/Console 对错误码的预期。

## 输出要求

用简体中文输出复审报告，三节结构：

1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束

- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./internal/infra/documentdb/... ./pkg/query/...` 与无外部依赖的纯单元测试辅助验证。
