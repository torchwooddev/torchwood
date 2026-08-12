# 审查任务：06 - Server/Console 用例层（internal/app/server + console + shared）

## 角色

你是资深 Go 后端代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Server/Console 用例层」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/roadmap.md` §2.2/§2.3（Server Users/Teams 管理验收标准）
- 架构：`internal/app/server` 是管理面用例层（Projects/API Keys/Users/Teams/Databases/Collections/Attributes/Indexes/Documents/Functions/OAuth Providers/Storage 元数据）；`internal/app/console` 是 Console 用例层（Admins、bootstrap）；`internal/app/shared` 为共享用例逻辑
- 鉴权：API Key（scope：`users.write`、`databases.write` 等）或 Console admin 会话；admin 通过 `X-Torchwood-Project` 指定项目
- 约定：列表查询复用 `pkg/crud`；动态文档操作委托 `internal/infra/documentdb`；元数据表用 bun repo

## 审查范围

- `internal/app/server/`（全部 `*.go`，含测试）
- `internal/app/console/`（全部 `*.go`，含测试）
- `internal/app/shared/`（全部 `*.go`）
- 交叉引用（只读）：`internal/domain/projects/`、`internal/domain/users/`、`internal/domain/teams/`、`internal/domain/databases/`（端口）、`internal/infra/bun/model/`、`proto/server/v1/*.proto`、`proto/console/v1/*.proto`

## 审查重点

1. **项目归属校验**：所有资源（project/user/team/bucket/function/database）查询是否都按 `projectID` 过滤；`X-Torchwood-Project` 指定的项目是否校验 admin 有权访问（admin_projects 归属）；API Key 是否绑定项目。
2. **API Key 管理**：创建时的 scope 校验（不能超出自身 scope 创建更宽 scope）、secret 只显示一次、删除/轮换的副作用（已有 token 失效）。
3. **用户管理**：`POST /v1/server/users` 创建用户（密码策略、email 唯一性）、`PATCH` 字段白名单（labels/status/prefs 类型校验）、删除用户级联（sessions/tokens/documents？）、模拟登录 token 的范围限制（仅调试用途，是否带警告/审计）。
4. **Teams/Memberships**（对照 roadmap §2.3 验收标准）：角色状态机（invite → pending → accept/reject）、owner 保护（最后一个 owner 不能降级/删除）、成员只能操作自己的 membership、删除团队级联 memberships、邀请幂等性。
5. **Admins（console 用例）**：owner 权限保护（第一个 admin 是 owner；owner 不能删除自己/最后一个 owner）、admin 角色变更边界、bootstrap 流程的幂等性（并发注册第一个 admin 的竞态）。
6. **Databases/Collections/Attributes/Indexes 管理用例**：collection 创建/更新（name/permissions）、attribute/index 的 DDL 编排与元数据一致性（失败回滚）、系统集合只读保护、删除操作的级联（删 collection 删表与元数据、删 database 删 schema）。
7. **Functions/Deployments/Variables/Executions 用例**：变量值的存储与脱敏（GET 时不回显 secret）、部署状态机、执行触发的队列入队与幂等。
8. **Storage 元数据用例**：bucket public 标志的影响面、文件元数据更新的字段白名单、usage 统计的正确性。
9. **错误与事务**：用例层错误是否明确分类（NotFound/Conflict/PermissionDenied）；复合操作的事务边界；上下文取消传播。

## 通用检查项

1. 安全：跨项目越权、水平越权、信息泄露（列表是否泄露 scope/secret/变量值）、输入校验
2. 错误处理：错误吞掉、错误分类不当、panic
3. 并发：check-then-act 竞态、事务边界
4. 性能：N+1、不必要的全量加载
5. 一致性：与端口签名一致、与 proto/AGENTS 约定一致（复用 pkg/crud）
6. 测试：关键路径（级联删除、owner 保护、项目隔离）是否有测试

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：跨项目越权、提权、级联删除损坏数据
- 🟠 **P1 高**：功能缺陷、状态机漏洞、边界条件错误
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（隔离与级联正确性、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./internal/app/server/... ./internal/app/console/... ./internal/app/shared/...` 辅助检查
- 集成测试需要本地 Postgres/Redis，**不要运行**
