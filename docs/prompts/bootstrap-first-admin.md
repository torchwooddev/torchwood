# 实施 Prompt A：移除 cmd/seed + 首个管理员 Bootstrap

> 将本文件整体作为任务说明分派给实施 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 完整设计文档：`docs/implementation-bootstrap-and-cli.md` §1-§3、§6（**先通读再动手**，以代码现状为准）

---

## 任务目标

1. 移除离线脚本 `cmd/seed`。
2. Console 新增公开的首个管理员注册能力：仅当 `admins` 表为空时可用，第一个注册的管理员自动成为超管（`owner`），并在注册流程中自动创建默认 project（id=`default`）和默认 API Key（scope=`all`，明文 secret 仅此一次返回）。
3. Console 前端登录页在未初始化时切换为「初始化设置」表单。
4. 同步清理所有文档中的 seed 引用。

## 关键现状（已调研核实，实施时复核）

- `proto/console/v1/auth.proto`：`ConsoleAuthService` 服务级 `ACCESS_PUBLIC`，仅 `SignIn/RefreshToken/SignOut`。
- `internal/app/console/admins.go` `Admins.Create`：**无 principal 检查**，含邮箱唯一/密码强度/角色校验，可直接复用。
- `internal/app/server/apikeys.go` `APIKeys.Create`：**无 principal 检查**，返回 `(*projects.APIKey, secret明文, error)`，可直接复用。
- `internal/app/server/projects.go` `Projects.CreateProject`：**有平台 admin principal 校验**（安全评审 M7，不得移除），需按设计文档 §3.3 最小重构拆出 `CreateProjectInternal`。
- 有效 scope 校验：`interceptor.ValidAPIKeyScope`，`all` 表示全量放行。
- handler 层 `internal/api/consolegrpc/auth.go` 的 `SignIn` 成功后会 `setSessionCookies`；`SignUp` 复用该逻辑。
- `admin_projects` 关联表模型在 `internal/infra/bun/model/audit.go`；如 repo 层无写入方法需新增。
- Console 前端无注册页：`console/src/routes/Login.tsx`、`console/src/api/auth.ts`。

## 实施步骤

1. **Proto**：`proto/console/v1/auth.proto` 增加 `GetSetupStatus`（`GET /v1/console/auth/setup-status`）与 `SignUp`（`POST /v1/console/auth/sign-up`），消息定义见设计文档 §3.1。执行 `task generate-proto`，不得手改 `genproto/`。
2. **重构**：`internal/app/server/projects.go` 拆出 `CreateProjectInternal`（无 principal 检查，注释注明系统路径专用）；`CreateProject` 保留原校验并委托，行为零变化。
3. **Use-case**：新增 `internal/app/console/setup.go`，按设计文档 §3.2 实现 `GetSetupStatus` / `SignUp`，含失败补偿（步骤失败回删已建 admin）与首次性检查（`FailedPrecondition`）。
4. **Handler + Wire**：`internal/api/consolegrpc/auth.go` 增加两个 handler；`SignUp` 成功后 `setSessionCookies`。provider 变更后执行 `task wire-all`。
5. **前端**：`console/src/api/auth.ts` 增加 `getSetupStatus`/`signUp`；`console/src/routes/Login.tsx` 挂载时查 `setup-status`，未初始化时渲染初始化表单；注册成功后展示默认 API Key secret 一次（可复制对话框），不持久化。
6. **移除 seed**：删除 `cmd/seed/`；全仓 grep `cmd/seed`、`go run ./cmd/seed`、`seed`，更新设计文档 §2 列出的全部文档（README×2、developer 01/02/03/05/12/13、roadmap §2.9、manual-acceptance-checklist 0.4），快速开始改为 Console 引导流程。
7. **测试**：按设计文档 §3.7 补单元测试与集成测试。

## 约束

- 遵守 `AGENTS.md`：Clean Architecture 分层、proto authz 注解、最小改动、中文注释风格与现有代码一致。
- 不引入新第三方依赖。
- `SignUp` 是公开端点，首次性保证必须在 use-case 层，不依赖拦截器。
- 不得放宽 `AdminsService.CreateAdmin` 的 `permissions: ["owner"]`。
- 并发首次注册窗口按设计文档处理（MVP 接受，advisory lock 为可选增强）。

## 完成验收

- 全新数据库：启动 server → `/console/` 显示初始化引导 → 注册后即为 owner 并直接进入 Console → 页面展示一次默认 API Key secret。
- 用该 secret 以 `x-api-key` metadata 调用 `UsersService/ListUsers` 成功（`all` scope 生效）。
- 二次 `sign-up` 返回 `FailedPrecondition`；`setup-status` 返回 `needs_setup=false`。
- `cmd/seed` 目录及文档引用全部清除。
- `task test`、`task console-build && task build` 全部通过。
- 完成后输出：变更文件清单、每个验收项的验证方式与结果、遗留问题。
