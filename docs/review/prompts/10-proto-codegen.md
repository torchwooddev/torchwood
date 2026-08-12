# 审查任务：10 - Proto 定义与代码生成（proto / buf / genproto 一致性）

## 角色

你是资深 Protobuf/API 设计审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Proto 定义与代码生成」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/roadmap.md` §0（AI/Agent-Native：protobuf 是 API 单一事实来源，自动生成 OpenAPI/Swagger，供 Agent 以 API Key 调用）
- 生成管线：`buf.yaml`、`buf.gen.yaml` 驱动 `buf generate` → `genproto/`（gRPC 桩 + grpc-gateway handler + OpenAPI spec）；**不要手工编辑 `*.pb.go`**
- 关键约定：每个 gRPC 方法必须带 authz 注解（`proto/shared/v1/authz.proto` 的 `method_auth`，缺失会导致 `collectMethodsByAccess` 报错）；REST 映射经 `google.api.http` 注解
- 目录：`proto/client/v1/`（终端用户 API）、`proto/server/v1/`（Agent/自动化 API）、`proto/console/v1/`（Admin Console API）、`proto/shared/v1/`（common/authz/error）

## 审查范围

- `proto/`（全部 `.proto`）
- `buf.yaml`、`buf.gen.yaml`、`buf.lock`
- `genproto/`（抽查，不逐行）
- 交叉引用（只读）：`internal/api/servergrpc/`、`internal/api/clientgrpc/`、`internal/api/consolegrpc/`（handler 实现）、`sdk/typescript/src/`（SDK 类型对照）

## 审查重点

1. **authz 注解完整性**：遍历所有 RPC，核对每个方法都有 `method_auth`（access level + permissions）；access level 选择是否合理（公开端点是否真该公开）；permission 命名是否与 scope 体系一致（`*.read`/`*.write`/`*.delete`/`*.all`）；Service 级 `service_auth` 默认值与方法级覆盖是否正确。
2. **REST 映射**（`google.api.http`）：每个 RPC 是否有 http 注解；路径与方法是否符合 REST 惯例（`/v1/server/...`、`/v1/account/...`）；body 字段选择是否正确（避免 query 传对象）；路径参数与 proto 字段一一对应；是否存在路径冲突（同 path 不同 method）。
3. **API 一致性**：命名一致性（资源复数、`{id}` 命名、字段 snake_case）；字段编号稳定性（删除字段应 reserved）；request/response 消息的可扩展性（是否都包 message 而非裸标量）；分页约定（page/limit/cursor 字段命名与 `pkg/crud` 语义一致）。
4. **错误模型**：`proto/shared/v1/error.proto` 的结构化错误字段设计；handler 是否按此模型返回。
5. **版本管理**：package/version 目录（v1）使用是否一致；破坏性变更的预留（reserved 字段/编号）；buf breaking 检查配置（buf.yaml 是否启用）。
6. **生成配置**（`buf.gen.yaml`）：插件、输出路径、M 选项映射（go_package 一致性）；`buf.lock` 与 `buf.yaml` 依赖版本固定。
7. **实现与定义一致性**（抽查）：`genproto` 是否与 proto 同步（有无过期生成）；handler 实现是否遗漏新定义的 RPC（gRPC 未实现会返回 Unimplemented，检查是否有 stub）；SDK（TS）类型是否与 proto 同步。
8. **字段语义安全**：敏感字段（secret、token）在 proto 注释中是否标注不回显；`google.protobuf.Struct`/`Value` 的使用边界（prefs/metadata 是否有大小限制）。

## 通用检查项

1. 设计：API 是否对 Agent 友好（可机器读取、错误可编程处理、幂等语义）
2. 一致性：跨 client/server/console 的命名与语义统一
3. 可维护性：注释质量（字段说明、权限说明）
4. 测试：无（本模块无测试，审查静态定义）

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：缺失 authz 注解导致越权、REST 路径冲突、定义与实现不一致
- 🟠 **P1 高**：错误映射缺失、字段设计缺陷（不可扩展/命名冲突）
- 🟡 **P2 中**：设计不一致、注释缺失、生成配置问题
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`proto 文件:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（Agent-Native 就绪度、最需优先修复的 3 项）。

## 验证方式

- 可运行 `buf lint` 或 `task generate-proto` 的 dry-run 判断配置正确性（**不要实际重新生成覆盖 genproto**，避免改动生成文件；如需验证，先确认 git 状态干净再恢复）
- 用 grep 统计 RPC 总数 vs 带 method_auth 数量核对覆盖率
