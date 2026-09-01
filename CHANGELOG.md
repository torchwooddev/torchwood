# Changelog

本文件记录独立分发的子模块版本。模块遵循 Go nested-module tagging：
`genproto/vX.Y.Z` tag 承载 `github.com/torchwooddev/torchwood/genproto`，
`sdk/go/vX.Y.Z` tag 承载 `github.com/torchwooddev/torchwood/sdk/go`。
TypeScript SDK 以 npm 包 `@torchwood/sdk` 分发（`sdk/typescript/`，`task sdk:publish` 发布）。
发布流程见 `.github/workflows/release.yml`（workflow_dispatch）。

## @torchwood/sdk

### v0.1.0 — 2026-09-01

首个 npm 发布版本。ESM-only（Node >= 18），零运行时依赖，`tsc` 直出 `dist/`
（发布产物不含 `__tests__`，`prepublishOnly` 自动跑测试与干净构建）。

- Client API 客户端（Bearer JWT，token 自动持久化/刷新）：account（注册/
  登录/会话/偏好）、databases（文档 CRUD + count）、groups / memberships、
  realtime（WebSocket 订阅）、assets、payments、subscriptions；
- Server API 客户端（API Key + `X-Torchwood-Project`）：health / projects /
  users / groups / databases（库/集合/属性/索引/文档/Bulk）/ apiKeys /
  oauthProviders / storage / functions / payments / assets / subscriptions /
  billing / outbox；
- Agent 默认工具目录：18 个动词 → Server RPC 的映射（`agentTools` /
  `lookupAgentTool` / `TOOL_*` 常量），供 LLM Agent 与 MCP Tool Server 使用；
- 门面 `Torchwood.withApiKey()` / `Torchwood.withAccessToken()` /
  `Torchwood.create()` 三种实例化方式。

## sdk/go

### v0.1.2 — 2026-08-26

流水线端到端验证发布（tag `sdk/go/v0.1.2`，genproto @ `genproto/v0.1.2`）：
release.yml 修复后**首次全程绿灯**（rewrite → tidy → build → tag → CI 内
干净目录下游验收）。模块内容与 v0.1.1 一致（其间无 sdk/go 变更），无功能
差异；随附流水线验收脚本补 `go mod tidy`（`go get pkg@ver` 只写根模块
go.sum 条目，直接 build 缺传递 sum）。下游可按需停留在 v0.1.1。

### v0.1.1 — 2026-08-26

首次经 `.github/workflows/release.yml` 成功发布的版本（tag `sdk/go/v0.1.1`，
genproto @ `genproto/v0.1.1`）：require 改写为真实 genproto 版本并移除本地
相对路径 replace，下游 `go get github.com/torchwooddev/torchwood/sdk/go@v0.1.1`
可正常解析编译（干净目录验收通过）。

- 修复 v0.1.0 的分发断裂：v0.1.0 为手动 tag，go.mod 仍含本地 replace 与
  伪版本，下游无法解析——请直接使用 v0.1.1；
- client SDK `CountDocuments` 改用独立 `CountDocumentsRequest`（R4 P3-9
  proto 变更的漏改，v0.1.1 发布流水线首次完整编译 sdk/go 时暴露并修复）；
- 包含自 v0.1.0 tag（2026-08-12）以来的 SDK 增强：默认 30s 超时兜底与
  `Unavailable` 指数退避重试、`InvokeTool` Agent 工具目录、Client SDK
  Realtime 订阅 API、Outbox dead-letter list/replay、`DocumentsPager`
  分页迭代器、storage 传输 helper、错误 helper 全家桶；
- 发布流水线三项修复随附：go.mod require/验收命令改用纯版本号（nested
  module tag 前缀由 Go 解析）、tidy/验收改走默认 module proxy（vanity
  站点不稳定不再阻塞发布）、验收探针 `Health.Check` 双返回值修正。

### v0.1.0 — 2026-08-24（tag 实际打于 2026-08-12，手动）

首个 tag。**不可用于下游解析**：go.mod 仍含本地 replace 与伪版本，
功能上由 v0.1.1 完整取代（v0.1.0 小节历史描述的增强实际随 v0.1.1 发布）。

- Server API 客户端（`x-api-key` + `x-torchwood-project`）：Health / Users /
  Groups / Databases / Projects / Storage / Functions / OAuthProviders /
  Payments / Assets / Subscriptions / Billing / Outbox 13 个类型化服务封装；
- Client API 客户端（Bearer JWT 自动刷新 + FileTokenStore 原子持久化）；
- `InvokeJSON` 动态分发：覆盖全部 `torchwood.server.v1.*` unary（排除
  APIKeysService），proto 新增方法零登记自动可用。

## genproto

### v0.1.2 — 2026-08-26

跟随 sdk/go v0.1.2 发布；内容与 v0.1.1 一致（流水线验证）。

### v0.1.1 — 2026-08-26

跟随 sdk/go v0.1.1 发布（tag `genproto/v0.1.1`）。相对 v0.1.0：Document
proto 合并至 shared.v1、assets 幂等键语义注释、CountDocuments 独立
Request、若干 `reserved` 补齐与 OpenAPI 修正（详见 git log
`genproto/v0.1.0..genproto/v0.1.1`）。

### v0.1.0 — 2026-08-24（tag 实际打于 2026-08-12，手动）

首个发布版本：client / console / server / shared 四组 protobuf 的 Go
生成代码与 OpenAPI 文档。
