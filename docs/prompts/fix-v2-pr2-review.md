# PR2 审查修复派发稿（给第三方 agent / opencode）

把本文从「总则」到文末整段复制到**现有 PR2 工作树**的新 session。只修审查列出的问题，不要开 PR3/PR4，不要重开产品决策。

规格：`docs/design/v2-events-realtime-transactions.md` §2 / §3  
审查结论：owner 对 PR2 的严格审查（生产 pgdriver 4KB 缓冲会回滚普通文档写）

---

## 总则

1. 先读 `AGENTS.md`，再读本文件每一条 Finding。
2. 工作树已有 PR2 outbox 改动。在此之上修，不要 revert 无关代码。
3. 禁止手改 `genproto/**`。本轮**不应改 proto**。
4. 对话与 commit 用简体中文。
5. 做完用文末回执模板回复。不要声称「已审查通过」。
6. 不要开始 Realtime / 事务 API / Stream worker。

---

## 必须修（合入阻断）

### F1 — 生产数据库连接必须放大 pgdriver 写缓冲

**文件**：`internal/infra/clients/database.go` 的 `newDatabase`（约 L75）

**现状**：

```go
sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(source)))
```

bun `pgdriver` 默认写缓冲 **4KB**。PR2 之后每次用户集合写都会把**整份信封 JSON** 当作一个 JSONB 参数插入 `document_events_outbox`。以前字段拆开绑定，一篇 10KB 文档可以成功；现在 outbox 一次塞进 >4KB 的 blob，`Publish` 报 `bufio: buffer full`，整段 `RunInTx` **回滚，文档写失败**。

测试库已在 `internal/testutil/db.go` 设 `WithBufferSize(2<<20)`，所以集成测试是绿的，**盖不住生产**。

**锁定修法**：

- 生产 `newDatabase` 与测试对齐：`pgdriver.WithBufferSize(2 << 20)`（2MiB，覆盖设计 256KiB 上限 + 余量）。
- 不要做成可配环境变量（P2 配额/TTL 用常量，减少 config.proto 噪音）。
- 在 `newDatabase` 旁用一句注释说明：outbox 信封是单参数 JSONB，默认 4KB 会回滚业务写。

**测试**：

- 若现有 `internal/infra/clients` 有数据库构造测试，补一条断言 connector 使用了放大缓冲（能测到就测；测不到构造细节则至少保证 `go test ./internal/infra/clients/...` 仍绿）。
- 复跑：

```
go test ./internal/infra/events/... ./internal/infra/documentdb/... ./internal/domain/events/...
```

（有 `.env` / docker PG 时跑非 `-short`，确认截断与普通写仍过。）

不要把「只改 testutil」当成修复。

---

## 建议修（本轮一并做完）

### F2 — 不要提交仅 CRLF 变化的 `go.mod`

`git diff HEAD -- go.mod` 若无内容差异、只有行尾，**不要**把 `go.mod` 推进本 PR。`go.sum` 若有 wire/`go mod tidy` 的真实新增行可以保留。

检查：`git diff HEAD -- go.mod` 为空或只有 `^M` → `git checkout -- go.mod`。

### F3 — `deleteDocument` 双路径加一句注释

**文件**：`internal/infra/documentdb/postgres.go` `deleteDocument`

用户集合且 `p.pub != nil` 时走「抓拍 _perms → DELETE → Publish」后 `return`；否则走后面的 `clearPermissions` + `DELETE`。两路都必须清 `_perms`、都必须删行。

在分叉处加**一句**注释：为何分叉（有 publisher 时要在清权限前抓拍写前 ACL，并带删除前 version 发事件），以及两路都要清 `_perms`。不要重构整函数。

---

## 不要做

- 不要开始 PR3（WS / Redis Stream / Hub）。
- 不要开始 PR4（事务 API）。
- 不要改 proto、不要手改 `genproto`。
- 不要把 outbox 改成异步 fire-and-forget（缓冲失败必须修连接，不能吞 Publish 错误）。
- 不要把 2MiB 做成「截断后再写 4KB 缓冲」的绕法。
- 不要扩大 `clients.Database` 重构范围。

---

## 验收

```
go vet ./...
go test -short ./internal/infra/clients/... ./internal/infra/events/... ./internal/infra/documentdb/... ./internal/domain/events/...
```

有本地 PG 时：

```
# 先加载 .env 中的 TORCHWOOD_TEST_* 
go test ./internal/infra/events/... ./internal/infra/documentdb/... ./internal/domain/events/... -count=1
```

`newDatabase` 源码必须能搜到 `WithBufferSize(2<<20)` 或等价的 `2 * 1024 * 1024`。

---

## 回执模板

```
分支/工作树：
已修 Finding：F1 F2 F3（列出实际完成的）
偏差：
自测命令与退出码：
- go vet
- go test -short <上述包>
- go test 非 short（是否跑、结果）
newDatabase 是否含 WithBufferSize：
go.mod 是否仍仅有 CRLF 变更：
未跑的测试与原因：
```
