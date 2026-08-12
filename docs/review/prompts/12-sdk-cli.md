# 审查任务：12 - SDK（Go / TypeScript）与 CLI

## 角色

你是资深 Go 与 TypeScript 代码审查专家（SDK 设计）。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「SDK 与 CLI」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定，特别是 CLI 约定）与 `sdk/README.md`、`docs/developer/12-sdk.md`
- 产品定位：SDK 是 AI/Agent-Native 的对外接口（Agent 工作流/工具集成），Server API 通过 scoped API Key 调用
- `sdk/go/`：Go SDK，拆分为 `client/`（用户端）与 `server/`（管理面）；`server` 包提供 `InvokeJSON` 以 API Key 调用 Server API；CLI 通过 sdk/go 调用，**CLI 源码不直接 import genproto/grpc**（有 import_guard_test 兜底）
- `sdk/typescript/`：TypeScript SDK（`src/client/`、`src/server/`）；`sdk/demo/`：web demo（http://localhost:5174）
- `cmd/client/`：Torchwood CLI（cobra，`bin/torchwood`），命令通过 JSON map 请求 / InvokeJSON 实现；新增 RPC 无需在 CLI 登记，覆盖完整性由 sdk/go/server 测试保证
- 已知改动（2026-08）：CLI 重构为 `cmd/client/cmd` 包；`FileTokenStore` 在 POSIX 用原子 rename、Windows 先 pre-remove（commit 539d956）；`json.Number` 保 int64 精度（commit df11068）

## 审查范围

- `sdk/go/`（全部 `*.go`，含测试）：`client/`、`server/`、`internal/conn/`
- `sdk/typescript/src/`（全部 `.ts`）
- `cmd/client/`（全部 `*.go`，含测试与 import_guard_test）
- `sdk/demo/`（只读了解用法，不逐行）
- 交叉引用（只读）：`proto/`（API 契约）、`genproto/`（生成的类型定义，供对照）、`sdk/typescript/package.json`

## 审查重点

1. **Go SDK 设计**（`sdk/go/server`、`client`）：`InvokeJSON` 的请求构造（path/query/body/header）、错误解析（`IsPermissionDenied` 等 helper）与结构化错误映射、API Key 传递（metadata 还是 header——与 server 拦截器约定一致）、超时与重试策略、泛型封装类型安全。
2. **类型/契约一致性**：SDK 暴露的方法与 proto RPC 集合是否一致（有无遗漏或多余）；JSON 字段名映射（snake_case ↔ camelCase）；int64 精度处理（JSON number vs string）；`google.protobuf.Struct` 的处理。
3. **FileTokenStore**（`sdk/go/internal/conn` 或相关）：token 缓存文件的原子写（POSIX rename / Windows pre-remove）、并发访问安全、权限位设置（0600）、清理。
4. **CLI**（`cmd/client/cmd`）：cobra 命令结构与 `--help` 质量；flag 解析与 JSON map 请求转换（`json.Number` 精度）；API Key 来源（环境变量/flag/配置文件）与提示（不打印 secret）；错误输出与退出码约定；import_guard_test 的有效性（防直接依赖 genproto）。
5. **TypeScript SDK**：请求封装（fetch/axios）、错误解析（结构化错误 → 类型化错误类）、类型导出完整性、浏览器兼容性（demo 使用）、认证 header 注入。
6. **SDK 测试质量**：`sdk/go/server` 的测试是否真正覆盖「新增 RPC 无需 CLI 登记」的保证机制；测试是否需要真实服务器（mock 方式）。
7. **安全**：SDK/CLI 中 token/API Key 的处理（不回显、不落盘明文、日志不打印）；redirect/URL 构造注入。
8. **文档与示例**：`sdk/README.md` 与实际 API 的同步性；demo 是否正确演示（cookie 处理 vs localStorage）。

## 通用检查项

1. 一致性：与 proto/服务端契约、与 AGENTS.md 约定
2. 错误处理：错误类型化、上下文信息、网络错误区分
3. 兼容性：SDK 版本与服务器版本不匹配时的行为
4. 可维护性：代码生成 vs 手写（TS SDK 是否生成或同步自 proto）、重复代码
5. 测试：核心逻辑（错误映射、token 存储、JSON 精度）是否有测试

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：凭据泄露、错误调用导致数据损坏（精度/字段映射错误）
- 🟠 **P1 高**：功能缺陷、契约不一致、平台兼容问题（Windows）
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（Agent 集成友好度、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./sdk/go/... ./cmd/client/...` 辅助检查
- 纯单元测试（不依赖外部服务）可运行 `go test ./sdk/go/...` 验证；TS SDK 可运行 `npx tsc --noEmit`（`sdk/typescript/` 目录）
- 不要运行需要真实 Torchwood 服务器的测试
