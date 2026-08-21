# Agent 默认工具箱

> Overlay，不是新的产品 API。完整 API 仍是 **201 个 RPC**（Client 69 + Server 122 + Console 10）；本表是 Agent 用 API Key 打 Server 时的默认约 18 个动词。
> 权威映射：`sdk/go/server/tools.go`、`sdk/typescript/src/server/tools.ts`。规格：`docs/review/wave3-e7-tool-catalog.md`。

---

## 1. 定位

- **不是**第 202 个 RPC，也**不是**把产品面砍到 20 个动词。
- Agent / 自动化默认只看见下表；Console、CLI、SDK 仍走完整 Server API。
- 逃生舱仍是 `InvokeJSON(fullMethod, protojson)`：覆盖全部 `torchwood.server.v1` unary，**继续排除 `APIKeysService`**。
- 本 catalog **不含** create / list / get / delete API key。密钥只在 Console 或带合适 scope 的管理流程里创建，不交给普通 Agent 工具面。

## 2. 默认 18 个工具

| 工具名 | Server RPC | gRPC FullMethod | 输入要点 |
|--------|------------|-----------------|----------|
| `list_users` | Users.ListUsers | `/torchwood.server.v1.UsersService/ListUsers` | `page_size` / `page_token` / `queries[]string` |
| `get_user` | Users.GetUser | `/torchwood.server.v1.UsersService/GetUser` | `id` |
| `create_user` | Users.CreateUser | `/torchwood.server.v1.UsersService/CreateUser` | `email`, `password`；可选 `name` / `status` / `labels` / `prefs` |
| `query_documents` | Databases.ListDocuments | `/torchwood.server.v1.DatabasesService/ListDocuments` | 必填 `database_id`、`collection_id`。**优先** `query`（E-4 `shared.v1.Query` AST）；仍接受 `queries[]string` + `page_size` / `page_token`。两者同时提供且冲突 → `InvalidArgument` |
| `get_document` | Databases.GetDocument | `/torchwood.server.v1.DatabasesService/GetDocument` | `database_id`, `collection_id`, `document_id` |
| `create_document` | Databases.CreateDocument | `/torchwood.server.v1.DatabasesService/CreateDocument` | `database_id`, `collection_id`, `document_id`, `data`；可选 `permissions` |
| `update_document` | Databases.UpdateDocument | `/torchwood.server.v1.DatabasesService/UpdateDocument` | 同上 id 三元组；可选 `data` / `permissions` / `increment`。用户集合须带 `version`（OCC） |
| `upsert_document` | Databases.UpsertDocument | `/torchwood.server.v1.DatabasesService/UpsertDocument` | id 三元组 + `data`；可选 `permissions`、`conflict_columns` |
| `delete_document` | Databases.DeleteDocument | `/torchwood.server.v1.DatabasesService/DeleteDocument` | id 三元组。用户集合须带 `version`（OCC） |
| `list_collections` | Databases.ListCollections | `/torchwood.server.v1.DatabasesService/ListCollections` | `database_id`；可选 `queries[]string`、`page_size`、`page_token` |
| `get_collection` | Databases.GetCollection | `/torchwood.server.v1.DatabasesService/GetCollection` | `database_id`, `collection_id` |
| `invoke_function` | Functions.CreateExecution | `/torchwood.server.v1.FunctionsService/CreateExecution` | `function_id`；可选 `deployment_id`、`data`、`async` |
| `list_files` | Storage.ListFiles | `/torchwood.server.v1.StorageService/ListFiles` | `bucket_id`；可选 `queries[]string`、`page_size`、`page_token` |
| `get_file` | Storage.GetFile | `/torchwood.server.v1.StorageService/GetFile` | `bucket_id`, `file_id` |
| `grant_asset` | Assets.Grant | `/torchwood.server.v1.AssetsService/Grant` | `owner_id`, `def_code`, `quantity`, `idempotency_key` |
| `list_user_assets` | Assets.ListUserAssets | `/torchwood.server.v1.AssetsService/ListUserAssets` | `owner_id`；可选 `page_size`、`page_token` |
| `get_order` | Payments.GetOrder | `/torchwood.server.v1.PaymentsService/GetOrder` | `order_id` |
| `get_health` | Health.Check | `/torchwood.server.v1.HealthService/Check` | 无入参 |

上传分片、OAuth 回调、Realtime WebSocket 仍是自定义 HTTP，不在本表（也不可经 `InvokeJSON`）。

## 3. `query_documents` 双栈

`ListDocumentsRequest` 同时有：

- `optional shared.v1.Query query`：filter 树 + orders + `page_size` / `page_token`（权威分页是 `page_token`）；
- 旧栈 `queries[]string` + `page_size` / `page_token`（Appwrite DSL，如 `equal("status","active")`）。

Agent 默认填 AST。`InvokeJSON` 用 protojson（camelCase）：

```json
{
  "databaseId": "app",
  "collectionId": "notes",
  "query": {
    "filter": { "eq": { "attribute": "status", "values": ["active"] } },
    "pageSize": 20
  }
}
```

旧客户端只填 `queries` 仍然有效。HTTP JSON（grpc-gateway `UseProtoNames`）则为 snake_case 字段名。

## 4. 调用方式

Go：

```go
t, ok := server.LookupTool("query_documents")
out, err := srv.InvokeTool(ctx, "query_documents", reqJSON)
// 等价于 srv.InvokeJSON(ctx, t.FullMethod, reqJSON)
```

TypeScript：`import { agentTools, lookupAgentTool } from "@torchwood/sdk"`，按 `fullMethod` 对照 OpenAPI / 用 `tw.server.databases.listDocuments(...)` 等已有 SDK 方法。TS SDK 不提供 `InvokeJSON`；catalog 只给名字与路径。

其余 183 个 RPC 仍可通过 SDK 方法或 `InvokeJSON`（Server unary、非 APIKeys）调用，只是不在 Agent 默认工具箱里。
