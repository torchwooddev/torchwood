# Torchwood 开发者文档

面向开发者的完整技术文档，覆盖架构、环境搭建、配置、代码生成、认证授权、核心子系统、开发指南、测试与运维。所有文档均以仓库当前代码为准。

> 惯例：文档使用简体中文；如与代码不一致，以代码为准（`AGENTS.md` 为开发约定总纲）。
> 索引与阅读路径：2026-08-12 随各章节一起按代码复核更新。

## 章节索引

| 章节 | 内容 | 适合读者 |
|------|------|----------|
| [01-overview.md](01-overview.md) | 架构总览：产品定位、技术栈、Clean Architecture 分层、目录树、典型调用链、设计原则、三大运行入口 | 所有开发者 |
| [02-quickstart.md](02-quickstart.md) | 环境搭建与快速开始：前置条件、本地基础设施、分步启动（含 bootstrap）、端点、任务速查、常见问题、CLI | 新加入的开发者 |
| [03-configuration.md](03-configuration.md) | 配置体系：config.proto schema、环境变量映射（TORCHWOOD_ 前缀）、加载顺序、特殊配置项（setup token / 可信代理 / cookie / 测试 DSN） | 所有开发者 |
| [04-codegen.md](04-codegen.md) | 代码生成与工具链：Task、Buf proto 生成、config 生成、Wire 依赖注入、生成流程顺序 | 后端开发者 |
| [05-authentication.md](05-authentication.md) | 认证与授权：JWT / session cookie / API Key / admin session、Principal 注入、scope、authz 注解 | 后端开发者 |
| [06-databases.md](06-databases.md) | 动态文档数据库：schema-per-database、_tenant / _perms 权限模型、Appwrite 风格查询 DSL | 后端开发者 |
| [07-storage.md](07-storage.md) | 存储子系统：S3/MinIO、multipart 上传下载、分片上传、File Token、公开 bucket、预览缩略图、Usage 统计 | 后端开发者 |
| [08-functions.md](08-functions.md) | 函数执行：Docker 执行器（构建/运行）、异步 worker（含重试语义）、部署/执行生命周期、安全基线 | 后端开发者 |
| [09-api-guide.md](09-api-guide.md) | 后端 API 开发指南：新增 gRPC 方法的完整流程、错误映射、列表分页约定、强制约束 | 后端开发者 |
| [10-console.md](10-console.md) | Console 前端开发：目录结构、API client 封装、会话 cookie、新增管理页面流程 | 前端开发者 |
| [11-testing.md](11-testing.md) | 测试与质量：测试分层、testutil、CI 流水线、Lint、健康检查与可观测性 | 所有开发者 |
| [12-sdk.md](12-sdk.md) | 官方 SDK：TypeScript SDK（包结构、入口类、Client/Server API、错误处理、demo）与 Go SDK（client/server 子包、InvokeJSON） | SDK 用户 / Agent 集成方 |
| [13-operations.md](13-operations.md) | 部署与运维：运行形态、构建发布、生产配置要点（含 setup token）、健康检查、备份与升级 | 运维 / 部署负责人 |
| [14-agent-tools.md](14-agent-tools.md) | Agent 默认工具箱：18 个 overlay 动词映射现有 Server RPC；完整 API 仍是 185 RPC（Client 61 + Server 114 + Console 10），不含 API key 管理 | Agent / SDK 集成方 |

## 推荐阅读路径

- **新开发者**：01 → 02 → 04 → 09 → 11
- **后端功能开发**：03 → 04 → 05 → 06 → 09
- **前端页面开发**：10（配合 05 §3 与 09 §8）
- **Agent / SDK 集成**：12 → 14（配合 05 §4 与 roadmap §0）
- **部署上线**：13（配合 03 §5 与 11 §6）

## 相关文档

- `README.md` / `README_ZH.md` — 项目总览与快速开始（中文/英文）
- `AGENTS.md` — 开发约定（分层、生成、配置、数据库约定，必读）
- `docs/roadmap.md` — 开发路线图（含 AI/Agent-Native 战略与验收标准）
- `docs/tech-decision.md` — 技术决策记录
- `docs/implementation-*.md` — 各功能实现说明（bootstrap-and-cli、health-observability、functions-executor、storage-chunked-upload、groups-prefs、account-completion、settings-page 等）
- `docs/archived/` — 归档设计文档（P0 设计、安全评审、修复计划等）
