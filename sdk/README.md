# Torchwood TypeScript SDK

`@torchwood/sdk` 封装 Torchwood **Client API**（用户 JWT）与 **Server API**（API Key + `X-Torchwood-Project`），是 Torchwood **AI/Agent-Native** 能力的前端集成层 —— 便于 LLM Agent、自动化脚本与 MCP Tool Server 以类型安全的方式调用后端。

## Agent / 自动化集成要点

| 场景 | 推荐方式 | 说明 |
|------|----------|------|
| 管理面自动化（建用户、管文档、Storage） | **Server API** + API Key | 在 Console 或通过 `POST /v1/server/api-keys` 创建带 scope 的 Key |
| 终端用户身份流（注册/登录/会话） | **Client API** + JWT | SDK 自动持久化 access token |
| Agent 工具 schema 来源 | **OpenAPI** | `task generate-proto` 后在 `genproto/**/*.swagger.json` 获取 |
| 快速验证 | **Web 演示** | `task sdk-demo`，设置页填入 Console API Keys 页面创建的 API Key |

典型 Agent 工作流：用 scoped API Key 实例化 `Torchwood.withApiKey()` → 读取 OpenAPI 或 SDK 类型 → 调用 Server Databases/Users/Storage API → 将结构化响应回传给 LLM。

## 目录

| 路径 | 说明 |
|------|------|
| `typescript/` | SDK 包 `@torchwood/sdk`（TypeScript） |
| `go/` | 官方 Go SDK（模块 `github.com/torchwooddev/torchwood/sdk/go`） |
| `demo/` | Web 演示站点（注册/登录 + SDK 功能演示） |

## Go SDK

Go SDK 是 gRPC 直连的薄封装（默认本地走 insecure，生产用 `WithDialOptions` 配置 TLS），
拆分为 `sdk/go/client`（Client API，Bearer JWT，自动刷新 token）与
`sdk/go/server`（Server API，API Key，含 InvokeJSON 动态分发）两个子包。

```go
import (
    "context"

    "github.com/torchwooddev/torchwood/sdk/go/client"
    "github.com/torchwooddev/torchwood/sdk/go/server"
)

// Server API：以 API Key 管理用户/用户组/文档库
srv, err := server.New("127.0.0.1:9060",
    server.WithAPIKey(apiKey),
    server.WithDatabaseID("app"),
)

// 为 Agent 账号签发 client token
tok, err := srv.Users.CreateUserToken(ctx, "user-1")

// 文档 upsert（按唯一索引冲突列）
doc, err := srv.Databases.UpsertDocument(ctx, "members", "m1",
    map[string]any{"channel_id": "ch1", "last_read_seq": 42},
    []string{"channel_id", "user_id"}, nil)

// 逃生舱：按方法名 + JSON 调用任意 Server API unary 方法
respJSON, err := srv.InvokeJSON(ctx, "/torchwood.server.v1.UsersService/ListUsers", []byte(`{"pageSize":10}`))

// Client API：注册/登录自动保存 token，过期自动刷新
store := client.NewFileTokenStore("~/.torchwood/tokens.json")
c, err := client.New("127.0.0.1:9060",
    client.WithProjectID("default"),
    client.WithTokenStore(store),
)
_, err = c.Account.SignIn(ctx, "u@example.com", "Pass@123")
me, err := c.Account.Me(ctx)
```

- 文档 API 默认绑定 `WithDatabaseID` 指定的库，可用 `c.UseDatabase(id)` 切换副本。
- client 包 TokenStore 支持内存与 JSON 文件两种实现；`OnTokensChanged` 回调感知登录/刷新/清空。
- 所有调用返回 gRPC status 错误，用 `status.Code(err)` 判别（`codes.NotFound` 等）。
- SDK 测试通过 bufconn 内存 gRPC 服务运行，无需外部依赖；`task test` / `task lint-go` 已覆盖 `sdk/go`。

## 快速开始

```bash
# 安装依赖并编译 SDK
task sdk-install
task sdk-build

# 启动本地 Torchwood（另开终端）
task up
task migrate
task dev-server
# 全新数据库上打开 http://127.0.0.1:9080/console/ 完成首次部署引导，
# 注册第一个管理员（填写 project id / database id），登录后到 API Keys 页面创建密钥

# 启动 Web 演示（默认 http://localhost:5174）
task sdk-demo
```

复制 `sdk/demo/.env.example` 为 `sdk/demo/.env` 可调整默认 Endpoint / Project ID。

## Web 演示站点

演示站点提供完整的前端体验：

| 页面 | 说明 |
|------|------|
| `/register` `/login` | 用户注册与登录（Client Account SDK） |
| `/app/account` | me / prefs / sessions / refresh |
| `/app/databases` | Server + Client Databases API 全功能验证 |
| `/app/groups` | 创建用户组、刷新 Token、邀请成员 |
| `/app/server` | Health / Projects / Users / Groups / Databases |
| `/app/settings` | Endpoint、Project ID、API Key 配置 |

Server API 相关功能需在设置页填写 Console API Keys 页面创建的 API Key。

## SDK 用法

```typescript
import { Torchwood } from "@torchwood/sdk";

// Server API
const admin = Torchwood.withApiKey("http://localhost:9080", "default", apiKey);
await admin.server.health.check();

// Client API（注册后自动保存 access token）
const client = Torchwood.create({ endpoint: "http://localhost:9080", projectId: "default" });
await client.account.signUp({ email: "u@example.com", password: "Pass@123", name: "User" });
await client.databases.createDocument("app", "notes", { data: { title: "Hi" } });
```

## 已实现 API surface

**Client：** Account（注册/登录/会话/偏好）、Databases（文档 CRUD + count）、Groups 与 Memberships。

**Server：** Health、Projects、Users、Groups、Databases（库/集合/属性/索引/文档/Bulk）、API Keys、Storage（Bucket/File）。
