# Changelog

本文件记录独立分发的子模块版本。模块遵循 Go nested-module tagging：
`genproto/vX.Y.Z` tag 承载 `github.com/torchwooddev/torchwood/genproto`，
`sdk/go/vX.Y.Z` tag 承载 `github.com/torchwooddev/torchwood/sdk/go`。
发布流程见 `.github/workflows/release.yml`（workflow_dispatch）。

## sdk/go

### v0.1.0 — 2026-08-24

首个发布版本（tag `sdk/go/v0.1.0`）。发布时 go.mod 的 require 已改写为真实
genproto 版本并移除本地相对路径 replace，下游 `go get
github.com/torchwooddev/torchwood/sdk/go@v0.1.0` 可正常解析编译。

- Server API 客户端（`x-api-key` + `x-torchwood-project`）：Health / Users /
  Groups / Databases / Projects / Storage / Functions / OAuthProviders /
  Payments / Assets / Subscriptions / Billing / Outbox 13 个类型化服务封装；
- Client API 客户端（Bearer JWT 自动刷新 + FileTokenStore 原子持久化）；
- `InvokeJSON` 动态分发：覆盖全部 `torchwood.server.v1.*` unary（排除
  APIKeysService），proto 新增方法零登记自动可用；`InvokeTool` Agent 工具箱；
- 默认单次调用 30s 超时兜底（`WithTimeout` 可调，尊重调用方 deadline）；
- 默认对 `Unavailable` 指数退避重试（最多 4 次，`WithRetryDisabled` 关闭）;
- 错误 helper：`ErrorCode` / `HTTPErrorClass` / `IsPermissionDenied` /
  `IsUnauthenticated` / `ExtractRetryAfter`。

## genproto

### v0.1.0 — 2026-08-24

首个发布版本（tag `genproto/v0.1.0`）：client / console / server / shared
四组 protobuf 的 Go 生成代码与 OpenAPI 文档，对应服务端当前 RPC 面
（Client 61 + Server 114 + Console 10）。
