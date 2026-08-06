# Torchwood Databases Transactions 实现 Prompt

> 将本文件整体复制到新 Cursor session 作为实现任务说明。  
> 仓库路径：`D:/Codes/qiulin/Torchwood`  
> Appwrite 参考：`D:/Codes/baas/appwrite`（`Transactions` HTTP 模块 + `utopia-php/database` 事务权限）

---

## 背景

Torchwood 已完成 Databases 权限 P0/P1（`documentSecurity` OR 逻辑、`_perms` 表、guests 公开读等）。**尚未实现** Appwrite 风格的 **Database Transactions**：在单个事务内批量创建/更新/删除文档，提交前全部校验权限，提交后原子生效。

当前架构：
- 元数据：`document_databases` / `document_collections`（bun + migrate）
- 数据平面：PostgreSQL `schema-per-database` + 每 schema 的 `_perms` 表
- 权限：`internal/domain/databases/permissions.go` + `internal/infra/documentdb/postgres_permissions.go`
- API：Server `proto/server/v1/databases.proto`（API Key）；Client `proto/client/v1/databases.proto`（JWT/guests 读）

---

## 目标

实现 Server API 事务能力，语义对齐 Appwrite TablesDB/DocumentsDB Transactions：

1. **创建事务** → 返回 `transactionId`，默认授予创建者对该事务的 read/update/delete
2. **事务内操作**：create / update / delete / upsert 文档（可跨同一 database 下多个 collection）
3. **提交事务** → 原子执行所有 staged 操作 + 权限写入
4. **回滚/过期** → 丢弃 staged 操作
5. **权限**：每个 staged 操作遵循 collection `create` 与 document `read/update/delete` 规则；API Key / PlatformAdmin 可绕过

---

## Appwrite 参考要点

阅读以下文件（Appwrite 仓库）：

| 文件 | 说明 |
|------|------|
| `src/Appwrite/Platform/Modules/Databases/Http/Databases/Transactions/Create.php` | 创建事务，默认 permissions |
| `.../Transactions/Update.php` | 提交/回滚 |
| `.../Transactions/Operations/Create.php` | staged create + 权限合并 |
| `utopia-php/database` `Database.php` | `createTransaction` / `commit` / `rollback` |

关键语义：
- 事务有 **TTL**（如 60s），过期自动失效
- Staged 文档在提交前 **不可被外部读取**
- `documentSecurity=true` 时，staged 权限与 collection 权限 OR
- Update 操作需先具备 **read + update**
- 提交失败则整批回滚

---

## Torchwood 建议设计

### 1. 存储

**方案 A（推荐 MVP）**：Redis 或 Postgres 元数据表存 staged ops

```sql
CREATE TABLE document_transactions (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    database_id  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending|committed|rolled_back|expired
    expire_at    TIMESTAMPTZ NOT NULL,
    created_by   TEXT NOT NULL,  -- user id or api key id
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE document_transaction_ops (
    id              TEXT PRIMARY KEY,
    transaction_id  TEXT NOT NULL REFERENCES document_transactions(id) ON DELETE CASCADE,
    op_type         TEXT NOT NULL, -- create|update|delete|upsert
    collection_id   TEXT NOT NULL,
    document_id     TEXT,
    data            JSONB,
    permissions     TEXT[],
    seq             INT NOT NULL
);
```

**方案 B**：纯内存（仅单实例 dev，不推荐生产）

### 2. Proto（`proto/server/v1/databases.proto`）

```protobuf
rpc CreateTransaction(CreateTransactionRequest) returns (Transaction);
rpc GetTransaction(GetTransactionRequest) returns (Transaction);
rpc DeleteTransaction(GetTransactionRequest) returns (shared.v1.Empty); // rollback

rpc CreateTransactionDocument(CreateTransactionDocumentRequest) returns (TransactionDocument);
rpc UpdateTransactionDocument(UpdateTransactionDocumentRequest) returns (TransactionDocument);
rpc DeleteTransactionDocument(DeleteTransactionDocumentRequest) returns (shared.v1.Empty);

rpc CommitTransaction(GetTransactionRequest) returns (CommitTransactionResponse);
```

消息需含：`transaction_id`、`database_id`、`collection_id`、`document_id`、`data`、`permissions`、`expire_at`。

### 3. 领域层

新增 `internal/domain/databases/transaction.go`：

```go
type Transaction struct { ID, DatabaseID, ProjectID, Status, ExpireAt, CreatedBy string }
type TransactionOp struct { ID, Type, CollectionID, DocumentID string; Data map[string]any; Permissions []Permission }
type TransactionRepository interface {
    Create(ctx, tx Transaction) error
    AppendOp(ctx, op TransactionOp) error
    ListOps(ctx, txID string) ([]TransactionOp, error)
    Commit(ctx, txID string, fn func(ctx) error) error
    Rollback(ctx, txID string) error
}
```

### 4. 提交逻辑（`internal/infra/documentdb/transaction.go`）

```
BEGIN;
for each op in ops ordered by seq:
  switch op.Type:
    create  → check collection create → INSERT doc → setPermissions
    update  → check read+update → UPDATE → optional perms replace
    delete  → check delete → DELETE → clearPermissions
    upsert  → exists? update branch : create branch
COMMIT;
```

- 复用 `checkDocumentPermission`、`CollectionAllows`、`ValidateGrantablePermissions`
- 使用 `principal` 自 `CreatedBy` 还原，或提交时传入当前 principal 并校验与创建者一致
- `ErrPermissionDenied` → 整事务 abort

### 5. App 层（`internal/app/server/transactions.go` 或扩展现有 `Databases`）

- `CreateTransaction`：生成 ULID，`expire_at = now + 60s`，默认 staged 权限 `read/update/delete:user:<id>` 或 API key 全量
- `CommitTransaction`：校验未过期、status=pending、principal 有权限提交
- 错误映射：`shared.MapDocumentDBError`

### 6. 与现有 Bulk API 的关系

已有 `BulkUpdateDocuments` / `BulkDeleteDocuments` 是 **立即执行、非原子**。Transactions 是 **staging + 单次提交**。不要在 bulk 内伪造成事务；保持两套 API。

### 7. Console（可选 P2）

- 事务调试 UI 非必须；MVP 仅 Server API + 集成测试

---

## 实现步骤（建议顺序）

1. Migration：`document_transactions` + `document_transaction_ops`
2. Domain ports + infra adapter（Postgres，提交时用 `db.RunInTx` 或 raw `BEGIN/COMMIT`）
3. Proto + `task generate-proto`
4. `internal/app/server` use-cases
5. `internal/api/servergrpc` handlers
6. 集成测试 `internal/app/server/transactions_integration_test.go`：
   - 事务内 create 2 个文档，commit 后均可读
   - 事务内 update + delete，commit 后状态正确
   - 中途 rollback，数据不变
   - 权限不足时 commit 失败，无部分写入
   - 过期事务 commit 返回 FailedPrecondition
7. 更新 `docs/roadmap.md` 将 Transactions 标为完成

---

## 验收标准

- [ ] `POST /v1/server/databases/{db}/transactions` 创建事务
- [ ] 事务内 CRUD staged 操作不立即落库
- [ ] `POST .../transactions/{id}/commit` 原子提交
- [ ] `DELETE .../transactions/{id}` 回滚
- [ ] 权限与 `documentSecurity` OR 逻辑一致
- [ ] API Key / PlatformAdmin 绕过与现有文档层一致
- [ ] `go test ./... -short` 通过

---

## 约束

- 遵循 `AGENTS.md`：Clean Architecture、Wire 注入、proto authz 注解
- 不要编辑 `genproto/*.pb.go`
- 修改 proto 后执行 `task generate-proto`；若改 Wire provider 执行 `task wire-all`
- 对话与 commit message 使用简体中文
- 保持最小 diff，不重构无关代码

---

## 已有代码入口

| 路径 | 用途 |
|------|------|
| `internal/infra/documentdb/postgres_permissions.go` | 权限检查、list 过滤 |
| `internal/domain/databases/permissions.go` | `AllowsDocumentAccess`、`ValidateGrantablePermissions` |
| `internal/app/server/databases.go` | Server 文档 use-case 模式参考 |
| `internal/infra/clients/tx.go` | bun `RunInTx` 辅助 |
| `db/migrations/` | 新 migration `000006_document_transactions.up.sql` |

---

## 不在本次范围

- Client API 事务（仅 Server / API Key）
- 跨 database 事务
- 实时通知 / Webhook
- Console UI
