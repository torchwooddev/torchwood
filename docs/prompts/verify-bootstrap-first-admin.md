# 验收 Prompt：首个管理员 Bootstrap（严格验收）

> 将本文件整体作为验收任务分派给验收 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 完整设计文档：`docs/implementation-bootstrap-and-cli.md` §3、§6（**先通读再动手**，以代码现状为准）
> 验收对象：`docs/prompts/bootstrap-first-admin.md` 声称已完成的实现（即当前工作区状态）

---

## 任务目标

对「移除 cmd/seed + Console 首个管理员 Bootstrap」的实现做**严格验收**：
逐项核对代码与设计文档 §3 的一致性，并实测全部验收项。

- **只读验收**：不得修改任何代码/文档文件（临时验证数据库除外）；发现偏差只报告，不修复。
- **证据要求**：每项结论必须附带可核查证据 —— 代码位置（`文件:行号`）、命令输出摘要、
  或可复现的 HTTP/gRPC 请求与响应。禁止仅凭「读起来没问题」下结论。
- **严格程度**：存在设计文档要求但实现缺失/偏差即判「失败」；实现超出设计但行为等价的可判「通过」并注明。

## 验收前准备

```bash
task up            # Postgres / Redis / MinIO
task test          # Taskfile dotenv 自动加载 .env（含 TORCHWOOD_TEST_DATABASE_SOURCE 等）
```

## 一、代码层面核对

### A1. Proto（`proto/console/v1/auth.proto` 与 `genproto/console/v1/`）

| 项 | 期望 |
|----|------|
| `GetSetupStatus` | `GET /v1/console/auth/setup-status`；`GetSetupStatusRequest{}`、`GetSetupStatusResponse{ bool needs_setup = 1 }` |
| `SignUp` | `POST /v1/console/auth/sign-up, body: "*"`；`SignUpRequest{ email, password }`、`SignUpResponse{ Admin admin=1, access_token=2, refresh_token=3, default_api_key_secret=4 }` |
| 注解 | 两个 RPC **无** method 级 authz 注解，依赖服务级 `ACCESS_PUBLIC`（与 `SignIn` 一致） |
| 生成 | `genproto/console/v1/auth.*` 由 buf 生成（`task generate-proto`），不得是手改产物；`Admin` 消息复用 `console/v1/admins.proto` |

### A2. 重构（`internal/app/server/projects.go`）

- `CreateProject`：principal 校验**原样保留**（`ActorKind == admin && IsPlatformAdmin`，否则 `PermissionDenied`，安全评审 M7）。
- `CreateProjectInternal`：**无** principal 检查，注释注明「bootstrap 等系统路径专用，调用方负责授权」；name/description 校验、id 派生、`RunInTx` + `EnsureSystemCollections` 逻辑与重构前一致。
- `CreateProject` 校验通过后委托 `CreateProjectInternal`，对现有调用方行为零变化。

### A3. Use-case（`internal/app/console/setup.go`）

- `GetSetupStatus`：`console_admins` 为空 → `true`。
- `SignUp` 步骤顺序（对照设计文档 §3.2）：
  1. 首次性检查：已存在任何 admin → `FailedPrecondition("setup already completed")`；
  2. `Admins.Create(ctx, {Email, Password, Role: "owner"})`（复用邮箱唯一/密码强度/角色校验）；
  3. `CreateProjectInternal({Name: "Default"})` → 派生 id=`default`；
  4. `APIKeys.Create({ProjectID: "default", Name: "Default API Key", Scopes: ["all"]})` → 明文 secret；
  5. `GrantProjectAccess(admin.ID, "default")`；
  6. `Auth.SignIn`（**必须用规范化后的 email**，即 `Admins.Create` 返回的 `admin.Email`，否则大写输入会登录失败）。
- 失败补偿：步骤 3–6 任一步失败时 best-effort 回删已建资源（至少 admin；理想含 project/api-key），**回删不得走 `Admins.Delete`**（会触发「最后一个 owner」保护而失败），应直接调 repo 删除；补偿失败只记日志。
- 首次性保证在 use-case 层完成，不依赖拦截器。

### A4. Handler + Wire

- `internal/api/consolegrpc/auth.go`：`GetSetupStatus` / `SignUp` 两个 handler 存在；`SignUp` 成功后调用现有 `setSessionCookies`；响应映射 `admin/access_token/refresh_token/default_api_key_secret` 正确。
- `internal/app/provides.go` 注入 `console.NewSetup`；`cmd/server/wire_gen.go` 由 `task wire-all` 重新生成（`NewAuthService` 新签名被全部调用点适配）。

### A5. 安全约束未放宽（重点抽查）

- `ConsoleAdminsService.CreateAdmin` 的 proto 注解仍为 `permissions: ["owner"]`；
- `APIKeysService` 仍拒绝 API key 凭证调用（`IsAPIKeysServiceMethod`）；
- 未新增任何绕过 `CreateProject` 校验的公开入口（`CreateProjectInternal` 仅系统路径 reachable）；
- `SignUp` 是公开端点：确认首次性检查在 use-case 层，拦截器配置无法绕过。

### A6. 前端（`console/src/routes/Login.tsx`、`console/src/api/auth.ts`）

- 挂载时查 `GET /v1/console/auth/setup-status`；`needs_setup=true` 渲染「初始化设置」表单（email/password/确认密码，文案区分）；探测失败回退登录表单。
- 注册成功后展示默认 API Key secret **一次**（可复制对话框/区块），**不持久化**（检查无 localStorage/sessionStorage 写入该 secret）。
- 注册成功后直接进入 Console（cookie 已由服务端下发；检查前端认证状态同步逻辑）。
- `task console-build` 无 TS 错误、`task lint-console` 通过。

### A7. 移除 seed + 文档

- `cmd/seed/` 目录已删除（`Test-Path cmd/seed` 为 False）。
- 全仓 grep（排除 `genproto/`、`console/node_modules/`、`console/dist/`、`.git/`、`docs/archived/`、设计文档与 prompts 自身）无 `cmd/seed` / `go run ./cmd/seed` 残留。
- 按设计文档 §2 清单逐份核对：README×2（快速开始改为 Console 引导流程 + 目录树）、developer 01/02/03/05/12/13、roadmap §2.9（标记「已由首个管理员 bootstrap 取代」）、manual-acceptance-checklist 0.4（改为 bootstrap 验收步骤）。

### A8. 测试存在性

- `internal/app/console/setup_test.go`：首次成功（owner / project id=`default` / secret 非空 / tokens 非空）、二次 `FailedPrecondition`、project 创建失败 → admin 被回删（mock 注入失败）。
- 集成测试：经 gateway 的 `POST /v1/console/auth/sign-up` 全流程（含 `x-api-key` 调 `UsersService/ListUsers` 验证 `all` scope）—— 检查集成测试确实连接真实数据库运行（非 `t.Skip` 短路）。

## 二、行为验证（实测，全新数据库）

建议用临时库（如 `torchwood_verify_<pid>`）执行 `task migrate` 后启动 `task dev-server`（或 `task build && ./bin/server.exe`），全程用 HTTP 验证；也可复用 `internal/api/consolegrpc` 的集成测试方式，但必须给出实测输出。

1. **初始化前**：`GET /v1/console/auth/setup-status` → `{"needs_setup":true}`。
2. **首次注册**：`POST /v1/console/auth/sign-up` `{"email":"owner@torchwood.local","password":"Pass@1234"}`
   → 200；`admin.role == "owner"`；`access_token`/`refresh_token` 非空；`default_api_key_secret` 非空；
   `Set-Cookie` 含 `TORCHWOOD_session_console`（Path=/）与 `TORCHWOOD_console_refresh`（Path=/v1/console/auth）。
3. **默认资源落库**（psql 核对）：
   - `console_admins` 恰 1 行且 `role='owner'`；
   - `projects` 存在 `id='default'`；
   - `api_keys` 存在 `project_id='default'` 且 `scopes` 含 `"all"`；
   - `console_admin_projects` 存在 `(admin_id, 'default')` 关联。
4. **API Key 生效**：`GET /v1/server/users` 带 `x-api-key: <default_api_key_secret>` → 200（`all` scope 放行 `ListUsers`）。
5. **二次注册**：再次 `POST /v1/console/auth/sign-up` → HTTP 400，message 含 `setup already completed`。
6. **初始化后状态**：`GET /v1/console/auth/setup-status` → 响应中 `needs_setup` 不为 true
   （注意：生产 marshaler `EmitUnpopulated=false` 会省略 false 字段，响应体可能是 `{}`，语义即 `needs_setup=false`，判为通过并注明）。
7. **预置 admin 场景**：向全新库手工插入一个 admin 行后调 `sign-up` → 仍返回 `FailedPrecondition`（首次性检查与表是否为空无关的行为一致性）。

## 三、工程化

- `task test` 全部通过（含集成测试，非 `-short`）；
- `task console-build && task build` 通过；
- `gofmt -l .` 无输出、`go vet ./...` 通过。

## 四、输出要求

验收报告（markdown），包含：

1. **逐项结论表**：`# | 验收项 | 结论（通过/失败/警告） | 证据（文件:行号 或 命令输出摘要）`，覆盖 A1–A8、行为验证 1–7、工程化 3 项。
2. **失败项清单**：按严重程度排序（阻断/非阻断），每条附：期望 vs 实际、复现步骤、最小修复建议（建议即可，不修改代码）。
3. **与设计文档的偏差清单**：列出所有实现与设计文档的差异，并逐条标注「设计接受」（如 setup-status 空对象语义、并发窗口无 advisory lock）或「需修复」。
4. **最终结论**：整体是否通过验收；若不通过，列出必须修复项（按优先级）。
