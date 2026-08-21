# E-6 拆 DocumentDB 接口（一页规格）

> Wave 2。对应 `docs/review/first-principles-plan.md` E-6。依赖 E-4 已合入（Documents.List 吃 AST）。  
> 日期：2026-08-21。**可施工。**

## 锁定

1. **三个接口**，`DocumentDB` 嵌入它们，现有调用方多数不用改签名：

```go
type Catalog interface {
    GetDatabase(...) (*Database, error) // 不再返回 *Collection
    ListDatabases(...) ([]Database, error)
    GetCollection(...) (*Collection, error)
    ListCollections(...)
    // 只读 catalog，不跑 DDL、不 Apply 项目迁移
}

type SchemaApplier interface {
    CreateDatabase / DeleteDatabase
    CreateCollection / UpdateCollection / DeleteCollection
    CreateAttribute / DeleteAttribute
    CreateIndex / DeleteIndex
    EnsureSystemCollections
    EnsureCatalog(ctx, projectID) error // 唯一允许 projectschema.Apply 的读旁路出口：启动/建项/EnsureSystem；GetCollection 禁止调用
}

type Documents interface {
    // CRUD / List / Count / Sum / Bulk — 仍带 databases.Principal 作为 ACL 主体（不是身份袋）。
    // List 继续 SQL 下推 _perms（listPermissionFilter 不得改成 fetch-then-Check）。
}

type DocumentDB interface { Catalog; SchemaApplier; Documents }
```

2. **`Database` 类型**（M-5）：`ID, Name, ProjectID, CreatedAt, UpdatedAt`。`GetDatabase`/`ListDatabases` 返回它。app/handler 映射到 proto `Database`。
3. **GetCollection 不再 `ensureProjectCatalog`→Apply**。缺 catalog schema 时返回 NotFound（或明确 InvalidArgument），由 CreateProject / EnsureSystemCollections / 启动 EnsureAll 负责 Apply。写 DDL 路径（CreateCollection 等）仍可 Apply。
4. **不删** sentinel、`businessSchema`、系统集合守卫、`RejectExternalDatabaseID`。
5. **测试 fake**：`fakeDocDB` / `stubDocDB` 继续实现嵌入后的 `DocumentDB`（K-21）。不要逼 Account 单测起 PG。
6. **本波不做**：Documents 去 Principal（T-3 完整形状留给系统表化后的 ACL 重写）；删 public 幽灵 catalog（D-7）。

## 验收

- `rg ensureProjectCatalog` 不得出现在 `GetCollection` / `ListCollections` / `GetDatabase` / `ListDatabases`。
- `GetDatabase` 返回 `*databases.Database`，测试与 handler 更新。
- 现有文档 OCC / 权限集成测仍绿。
- 新建项目后第一次 GetCollection 在 Ensure 已跑的前提下成功；未 Ensure 的项目 GetCollection 失败且不跑 DDL。
