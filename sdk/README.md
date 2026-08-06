# Torchwood TypeScript SDK

`@torchwood/sdk` 封装 Torchwood **Client API**（用户 JWT）与 **Server API**（API Key + `X-Torchwood-Project`），是 Torchwood **AI/Agent-Native** 能力的前端集成层 —— 便于 LLM Agent、自动化脚本与 MCP Tool Server 以类型安全的方式调用后端。

## Agent / 自动化集成要点

| 场景 | 推荐方式 | 说明 |
|------|----------|------|
| 管理面自动化（建用户、管文档、Storage） | **Server API** + API Key | 在 Console 或通过 `POST /v1/server/api-keys` 创建带 scope 的 Key |
| 终端用户身份流（注册/登录/会话） | **Client API** + JWT | SDK 自动持久化 access token |
| Agent 工具 schema 来源 | **OpenAPI** | `task generate-proto` 后在 `genproto/**/*.swagger.json` 获取 |
| 快速验证 | **Web 演示** | `task sdk-demo`，设置页填入 seed 输出的 API Key |

典型 Agent 工作流：用 scoped API Key 实例化 `Torchwood.withApiKey()` → 读取 OpenAPI 或 SDK 类型 → 调用 Server Databases/Users/Storage API → 将结构化响应回传给 LLM。

## 目录

| 路径 | 说明 |
|------|------|
| `typescript/` | SDK 包 `@torchwood/sdk` |
| `demo/` | Web 演示站点（注册/登录 + SDK 功能演示） |

## 快速开始

```bash
# 安装依赖并编译 SDK
task sdk-install
task sdk-build

# 启动本地 Torchwood（另开终端）
task up
task migrate
go run ./cmd/seed   # 记下输出的 api_key
task dev-server

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
| `/app/teams` | 建队、刷新 Token、邀请成员 |
| `/app/server` | Health / Projects / Users / Teams / Databases |
| `/app/settings` | Endpoint、Project ID、API Key 配置 |

Server API 相关功能需在设置页填写 `go run ./cmd/seed` 输出的 API Key。

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

**Client：** Account（注册/登录/会话/偏好）、Databases（文档 CRUD + count）、Teams 与 Memberships。

**Server：** Health、Projects、Users、Teams、Databases（库/集合/属性/索引/文档/Bulk）、API Keys、Storage（Bucket/File）。
