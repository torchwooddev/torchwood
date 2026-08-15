# PR1 审查修复派发稿（给第三方 agent / opencode）

把本文从「总则」到文末整段复制到**现有 PR1 工作树**的新 session。只修审查列出的问题，不要开 PR2，不要重开产品决策。

规格：`docs/design/v2-events-realtime-transactions.md` §1  
审查结论：owner 对 PR1 的严格审查（cache 回滚 + demo 签名 + 若干建议项）

---

## 总则

1. 先读 `AGENTS.md`，再读本文件列出的每一条 Finding。
2. 工作树已有 PR1 OCC 改动。在此之上修，不要 revert 无关代码。
3. 禁止手改 `genproto/**`。改 proto 才跑 `task generate-proto`。本轮**不应改 proto**。
4. 改 Console 后 `task console-build`。改 `sdk/typescript` 签名的调用方后，在 `sdk/demo` 跑 `tsc -b`（或 demo 的 build）。
5. 对话与 commit 用简体中文。
6. 做完用文末回执模板回复。不要声称「已审查通过」。

---

## 必须修（合入阻断）

### F1 — `ensureVersionColumn` 不得在未提交事务里写 cache

**文件**：`internal/infra/documentdb/postgres.go`（`ensureVersionColumn`、`versionColumnReady`、`updateDocument` / `createDocument` / `upsertDocument` / `deleteDocument`）

**现状**：`ensureVersionColumn` 在 `ALTER TABLE … ADD COLUMN` 成功后立刻 `versionColumns.Store`。调用点全在 `RunInTx` 内，且排在 `checkDocumentPermission` **之前**。PG 事务内 ALTER 会随 `ROLLBACK` 撤销列，cache 仍认为列已就绪。

**后果**：存量表第一次写若权限失败 / 唯一键冲突 → 列被回滚 → 下次 Update 拼 `_version = _version + 1` 得 **42703**；`$version` 查询走 `versionColumnReady` cache hit 同样 42703。规格禁止读路径落到 42703。

**锁定修法**（不要另发明）：

- **只有** `information_schema` 已经查到 `udt_name == int8`（列在本事务开始前就存在）时才 `versionColumns.Store`。
- 本事务刚执行的 `ALTER ADD COLUMN`：**不要** Store。下一次写/读再查 catalog；若上次已提交，会看到 int8 再缓存；若已回滚，会再次 ALTER。
- `versionColumnReady` 与 `ensureVersionColumn` 共用同一规则，禁止在 InTx 的 ALTER 路径污染读路径 cache。
- `InTx` 时禁止把「本事务新建的列」写入进程 cache。

**测试**（`internal/infra/documentdb/occ_test.go`，集成，`testing.Short` 跳过）：

1. 用户集合**尚无** `_version` 列（新建后可手动 `DROP COLUMN _version`，或用写路径尚未触发过 ALTER 的旧表）。
2. 用**无 update 权限**的 principal 调 `UpdateDocument`（带合法 ExpectedVersion）→ 权限失败。
3. 断言表上**仍然没有** `_version` 列（`information_schema`）。
4. 用有权限的 principal 再 Update（ExpectedVersion=1）→ **成功**，不得 42703；行 `_version` 变为 2。
5. 再对同一集合发 `equal("$version", 2)` 的 List → 成功，不得 `version_column_unavailable`，不得 42703。

### F2 — `sdk/demo` 跟上 OCC 签名

**文件**：`sdk/demo/src/pages/DatabasesPage.tsx`（至少 L217–221、L247–251、L267–271、L560–564、L573–574、L644–648、L657–658）

**现状**：`updateDocument` 未传 `version`；`deleteDocument` 仍三参数。`sdk/demo` 依赖 `file:../typescript`，`tsc -b` 会挂。

**要求**：

- 所有 `updateDocument` / `deleteDocument`（client 与 server、向导步骤与手动按钮）传入当前文档 version。
- 流程：先 `getDocument` 或使用刚 `createDocument` 返回的 `version`，再 update/delete。create 响应带 `version`（一般为 1）；update 后用响应里的新 version 再 delete。
- Bulk 路径保持不传 version（Bulk 仍 LWW）。
- `sdk/demo` 的 TypeScript 编译必须过。

---

## 建议修（本轮一并做完）

### F3 — Console 不要 `version ?? 0`

**文件**：`console/src/api/databases.ts`、`console/src/routes/databases/pages.tsx`（L1264 列表删除、L1681 详情保存、L1695 详情删除）

**要求**：

- 在 `listDocuments` / `getDocument` 把 `version` 收成 number（网关 int64 常为 string）。`Number(doc.version)`，`Number.isFinite` 且 `> 0` 才可用。
- 页面侧非法/缺失 version 时在 UI 拦截并 toast，**禁止**静默送 `0`（0 会变成 `version_required`）。
- Bulk 对话框继续不传 version。

### F4 — 读路径非 bigint `_version` 用 `version_column_conflict`

**文件**：`internal/infra/documentdb/postgres.go` `versionColumnReady` / `validateQueryFields`

**现状**：列存在但 `udt_name != int8` 时 `versionColumnReady` 返回 false，查询报 `version_column_unavailable`。写路径同类情况是 `version_column_conflict`。

**要求**：列存在且非 bigint → 读路径也返回 `ErrVersionColumnConflict`（`FailedPrecondition` / `version_column_conflict`）。仅「列不存在」才是 `version_column_unavailable`。补测试。

### F5 — CLI delete 缺 `--version` 单测

**文件**：`cmd/client/cmd/databases_test.go`

对 `documents delete` 无 `--version` / `--version 0` 断言错误，与 `buildUpdateDocumentReq` 的缺 version 表驱动对齐。

### F6 — 开发者文档写 OCC breaking

**文件**：`docs/developer/06-databases.md`

补一小节（或改 §3.3 操作表）：

- 用户集合文档有顶层 `version`（整型，从 1 起）。
- Update / Delete / Increment **必须**带当前 `version`；对不上 `version_mismatch`，未带 `version_required`（均为 FailedPrecondition）。
- Bulk / Upsert / Create 不带 version。
- 查询：`$version` / `_version`；未 ALTER 的旧表查询该字段 → `version_column_unavailable`。
- 系统集合无此列，出站 `version=0`，禁止 `$version` 查询。

不要写成长文，短表 + 错误码即可。

### F7 — adapter `CreateAttribute` 拒绝保留列

**文件**：`internal/infra/documentdb/postgres.go` `CreateAttribute`

复用 `databases.ReservedAttributeKeys`（含 `_version`）。直调 adapter 也不得 `ADD COLUMN _version` 当用户属性。app 层已有校验，这是 fail-closed 第二道。

---

## 不要做

- 不要开始 PR2（outbox / 事件 / Realtime / 事务）。
- 不要改 proto 字段号，不要手改 `genproto`。
- 不要把系统集合改成带 `_version` 列。
- 不要为了「adapter 也归零 Version」大改 Users/Teams/Storage；系统集合出站契约维持 Get/List 归零即可。可在 `scanDocumentJSON` 旁保留现有注释。
- 不要扩大 Console / SDK 重构范围。

---

## 验收

```
go vet ./...
go test -short ./internal/infra/documentdb/... ./internal/app/shared/... ./internal/app/client/... ./internal/app/server/... ./cmd/client/cmd/...
go test ./sdk/go/client/... ./sdk/go/server/...   # 在 sdk/go 模块目录
cd sdk/typescript && npm test
cd sdk/demo && npx tsc -b --pretty false
task console-build
```

F1 的新集成测试在非 `-short` 下必须覆盖「无权限 Update 回滚后，有权限 Update 仍成功」。本地有 docker PG 时再跑：

```
go test ./internal/infra/documentdb/ -run TestUpdateDocument_EnsureVersionRollbackDoesNotPoisonCache -count=1
```

（测试名可自定，语义必须是上述窗口。）

---

## 回执模板

```
分支/工作树：
已修 Finding：F1 F2 F3 F4 F5 F6 F7（列出实际完成的）
偏差：
自测命令与退出码：
- go vet
- go test -short <上述包>
- sdk/go
- sdk/typescript npm test
- sdk/demo tsc -b
- task console-build
F1 集成测试（是否跑、结果）：
未跑的测试与原因：
```
