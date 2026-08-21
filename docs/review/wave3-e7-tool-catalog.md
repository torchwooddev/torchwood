# E-7 Tool catalog overlay（开工清单）

> Wave 3。依赖 E-2a（已合）+ E-4（Query AST）。201 RPC **全部保留**。  
> 日期：2026-08-21。**可施工。** 计数以 `docs/developer/14-agent-tools.md` 为准（D-6 后少于 201）。

## 锁定

1. **不是**新的产品 API 面，是 Server API 上的 overlay：一组「高杠杆」工具描述 + Go/TS 辅助，便于 Agent 只看见 ~20 个动词。
2. **禁止**暴露 key 管理：`InvokeJSON` 继续排除 `APIKeysService`；本 catalog 不含 create/list/delete API key。
3. 落点：`sdk/go/server/tools.go`（工具名 → 已有 RPC）+ `sdk/typescript` 对等导出；可选 `docs/developer/14-agent-tools.md` 人读清单。不新增 gRPC service（避免第 202 个 RPC）。
4. 查询类工具优先走 E-4 `Query` AST，同时接受 `queries[]string` 双栈。

## 工具清单（锁定 18 个）

| 工具名 | 映射 |
|---|---|
| `list_users` | Users.ListUsers |
| `get_user` | Users.GetUser |
| `create_user` | Users.CreateUser |
| `query_documents` | Databases.ListDocuments |
| `get_document` | Databases.GetDocument |
| `create_document` | Databases.CreateDocument |
| `update_document` | Databases.UpdateDocument |
| `upsert_document` | Databases.UpsertDocument |
| `delete_document` | Databases.DeleteDocument |
| `list_collections` | Databases.ListCollections |
| `get_collection` | Databases.GetCollection |
| `invoke_function` | Functions.CreateExecution |
| `list_files` | Storage.ListFiles |
| `get_file` | Storage.GetFile |
| `grant_asset` | Assets.Grant |
| `list_user_assets` | Assets.ListUserAssets |
| `get_order` | Payments.GetOrder |
| `get_health` | Health.Check |

## 验收

- 18 个名字均可 `InvokeJSON` 或 SDK 方法打到现有 unary（key 管理不在列）。
- 文档写明：完整 API 仍是 201 RPC；本表是 Agent 默认工具箱。
