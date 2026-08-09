# Torchwood Console 前端开发指南

> 本文面向需要在 Admin Console 新增页面/功能的开发者。Console 是 React 19 + Vite + TanStack Query +
> shadcn/ui 风格的管理后台，构建产物通过 `go:embed` 打进 Go 二进制，经 `/console/` 路径由 server 直接 serve。
> 目标读者：前端开发者（新增管理页面）。
> 关联：`AGENTS.md`（开发约定）、`docs/developer/09-api-guide.md`（后端 API 约定）。
> 修订记录：2026-08-09 初版（目录结构、API client、会话 cookie、新增页面流程按代码核实）。

---

## 1. 技术栈（以 `console/package.json` 为准）

| 依赖 | 版本（^） | 用途 |
|------|-----------|------|
| react / react-dom | 19.x | UI 框架 |
| react-router-dom | 7.x | 路由（`BrowserRouter` + 嵌套路由） |
| @tanstack/react-query | 5.x | 服务端状态管理（`useQuery` / `useMutation` / `useQueryClient`） |
| axios | 1.x | HTTP client（`console/src/api/client.ts` 封装） |
| sonner | 2.x | toast 提示 |
| lucide-react | 1.x | 图标 |
| tailwindcss + tailwindcss-animate | 3.x | 样式 |
| class-variance-authority + clsx + tailwind-merge | — | `cn()` 工具（`lib/utils.ts`） |
| @radix-ui/react-* | 1.x | 无头组件（dialog/select/label/avatar 等） |
| typescript | ~6.x | 构建时类型检查（`tsc -b`） |
| vite | 8.x | 构建/开发服务器 |
| pnpm | 11.20.0 | 包管理器（`packageManager` 字段锁定） |

---

## 2. 目录结构

```
console/
├── embed.go                    # //go:embed dist → console.Dist（Go 侧嵌入）
├── package.json / vite.config.ts / tailwind.config.js / eslint.config.js
├── dist/                       # pnpm build 产物（已构建，随 Go 二进制发布）
└── src/
    ├── main.tsx                # 入口：createRoot + StrictMode
    ├── App.tsx                 # QueryClientProvider + AuthProvider + BrowserRouter + 路由表
    ├── api/                    # 每个资源的 API 封装（axios）
    │   ├── client.ts           # axios 实例 + 拦截器（cookie/refresh/项目头/错误 toast）
    │   ├── auth.ts / admins.ts / projects.ts / users.ts / teams.ts
    │   ├── apiKeys.ts / databases.ts / storage.ts / functions.ts / oauthProviders.ts
    ├── components/
    │   ├── Layout.tsx          # 侧边栏 + 顶部栏 + 项目选择器 + 路由出口
    │   ├── ProjectBootstrap.tsx # 自动选择默认项目（保证 X-Torchwood-Project 就绪）
    │   ├── ProjectSelector.tsx / PageHeader.tsx / EmptyState.tsx / LoadingTable.tsx
    │   ├── ConfirmDialog.tsx / FormPage.tsx
    │   ├── list/               # DataTable / ListToolbar / ResourceListPage（通用列表页）
    │   ├── resource/           # PermissionEditor / permission-presets / shared（行操作按钮）
    │   └── ui/                 # shadcn/ui 风格基础组件
    │       ├── button.tsx / card.tsx / input.tsx / label.tsx / select.tsx
    │       ├── dialog.tsx / badge.tsx / table.tsx / checkbox.tsx / skeleton.tsx
    ├── hooks/
    │   ├── useAuth.tsx         # 会话状态（refresh 探测 + 登录/登出/选项目）
    │   ├── useListParams.ts    # 列表页 URL 参数（q/page/pageSize）读写
    │   └── useRowSelection.ts  # 行选择
    ├── lib/utils.ts            # cn()（clsx + tailwind-merge）
    └── routes/                 # 按资源分目录，每目录一个 pages.tsx
        ├── Login.tsx / Dashboard.tsx
        ├── admins/  api-keys/  databases/  functions/  projects/
        ├── settings/  storage/  teams/  users/
```

约定：

- **路由页面按资源分目录**（`routes/<resource>/pages.tsx`），一个页面一个导出组件；
- **通用组件**放 `components/`，shadcn 风格原语必须放 `components/ui/`；
- **API 封装**放 `api/`，页面不得直接 `axios`。

---

## 3. API client 封装（`console/src/api/client.ts`）

### 3.1 实例与请求拦截器

```ts
export const api = axios.create({
  baseURL: "/v1",
  headers: { "Content-Type": "application/json" },
});

api.interceptors.request.use((config) => {
  const projectID = getProjectID();          // localStorage: TORCHWOOD_console_project
  if (projectID) {
    config.headers["X-Torchwood-Project"] = projectID;
  }
  return config;
});
```

要点：

- 会话凭证**不在 JS 里**：登录态由 HttpOnly cookie（`TORCHWOOD_session_console` /
  `TORCHWOOD_console_refresh`）携带，同源 XHR 自动附带；前端不再读写 token
  （`client.ts` 启动时会清理 localStorage 中迁移前残留的旧 token）；
- 项目上下文：`X-Torchwood-Project` 头从 localStorage 读取，由 `setProjectID` / `getProjectID` 管理；
- baseURL 为 `/v1`，与后端 grpc-gateway 路由前缀一致（如 `/v1/server/projects`）。

### 3.2 响应拦截器：401 自动刷新（single-flight）

```ts
// 并发 401 只触发一次刷新；用裸 axios 绕过拦截器避免递归。
export function refreshAuthTokenSingleFlight(): Promise<void> {
  if (!refreshPromise) {
    refreshPromise = axios
      .post("/v1/console/auth/refresh")   // 空 body，服务端读 refresh cookie
      .finally(() => { refreshPromise = null; });
  }
  return refreshPromise;
}
```

401 处理流程（`client.ts` 的 response interceptor）：

1. 登录请求（`/console/auth/sign-in`）401 → 直接 toast 错误；
2. 其他请求 401 → 先 `refreshAuthTokenSingleFlight()` 刷新一次，成功后重试原请求（带
   `__authRetried` 标记防死循环）；刷新失败 → `forceReLogin()` 跳转 `/console/login`；
3. 业务错误（≥400）→ 从 `error.response.data.error.message` 取后端错误消息并 `toast.error`，
   最后 `Promise.reject(error)` 交给调用方。

> 后端错误体结构为 `{ error: { message, code, error_code, ... } }`，见
> `docs/developer/09-api-guide.md` §8.2。

### 3.3 资源 API 封装示例（`console/src/api/projects.ts`）

```ts
import { api } from "./client";

export interface Project {
  id: string;
  name: string;
  description?: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export async function listProjects(): Promise<Project[]> {
  const res = await api.get<ListProjectsResponse>("/server/projects");
  return res.data.projects ?? [];
}

export async function createProject(input: { name: string; description?: string }): Promise<Project> {
  const res = await api.post<Project>("/server/projects", input);
  return res.data;
}
```

惯例：

- 每个文件导出接口类型 + 纯 async 函数；列表函数内部已解包 `res.data.xxx ?? []`；
- 路径与后端 `google.api.http` 注解一致（`/server/projects`、`/console/admins` 等）；
- JSON 字段名使用 proto 的 snake_case（后端 `CustomMarshaler` 用 `UseProtoNames: true`）。

---

## 4. 认证流程（Console admin 会话）

### 4.1 服务端（Go）

- 登录：`POST /v1/console/auth/sign-in`（`internal/api/consolegrpc/auth.go`）；
- 会话凭证：`TORCHWOOD_session_console`（Path `/`）+ `TORCHWOOD_console_refresh`
  （Path 限定 `/v1/console/auth`），两者均 **HttpOnly + SameSite=Lax**（`cookies.go`）；
  - HttpOnly：JS 不可读，天然免疫 XSS 窃取；
  - SameSite=Lax：跨站 POST 不携带 cookie，配合「变更类端点均为 POST」即构成 CSRF 防护，无需 CSRF token；
  - `Set-Cookie` 由 grpc-gateway 的 `authOutgoingHeaderMatcher` 透传（`internal/infra/server/grpc_gateway.go`）；
- 刷新：`POST /v1/console/auth/refresh`，空 body，从 cookie 取 refresh token。

### 4.2 前端（`hooks/useAuth.tsx`）

- 挂载时调用 `refreshSession()`（即上面的 single-flight refresh）探测会话：成功 → 已登录（顺带续期）；
  失败 → 匿名；`loading` 为 true 期间 `RequireAuth` 返回 null，避免闪现登录页；
- `login(email, password)` → 成功后置 `authenticated`，cookie 由服务端下发；
- `logout()` → 调 `/console/auth/sign-out`（`__skipAuthRetry` 标记跳过 401 刷新流，best-effort）+
  清空本地项目 ID + 立即清本地状态；
- `RequireAuth`（`App.tsx`）：未登录跳 `/console/login`；
- 路由守卫只做「登录与否」，角色细粒度控制（如仅 owner 可管理管理员）在页面内判断
  （如 `admins/pages.tsx` 的 `me?.role === "owner"`）。

---

## 5. 开发与构建流程

```bash
task console-install    # pnpm install（包管理器锁定 pnpm@11.20.0）
task console-dev        # pnpm run dev → vite dev server
task console-build      # pnpm run build → tsc -b && vite build，产出 dist/
task build              # Go 侧：console-build（依赖）+ go build cmd/server + cmd/worker
```

- **dev 代理**（`vite.config.ts`）：`base: '/console/'`；`server.proxy` 把 `/v1` 代理到
  `http://localhost:9099`（本地 Go server 的 `server.http` 端口），保证 dev 下 `/v1` 与页面同源，
  HttpOnly 会话 cookie 正常工作；
- **路径别名**：`@` → `./src`（tsconfig + vite alias 双配置）；
- **嵌入二进制**（`console/embed.go`）：

```go
//go:embed dist
var Dist embed.FS
```

  由 `internal/infra/server/console.go` 的 `NewConsoleHandler` 挂载：`/console/*` 命中 dist，
  SPA fallback（未知路由回 index.html）+ 安全头（`X-Frame-Options: DENY`、CSP 等）；
- **重要**：修改 Console 代码后必须 `task console-build` 再 `task build`，否则 Go embed 打包的是
  旧版本 dist。

---

## 6. 如何新增一个管理页面（以现有页面为模板）

以「admins」为最小模板（列表页 + 新建/编辑弹窗），按以下四步：

### 6.1 编写 API 封装（`src/api/<resource>.ts`）

```ts
// src/api/admins.ts（真实文件）
export async function listAdmins(): Promise<ConsoleAdmin[]> {
  const res = await api.get<ListAdminsResponse>("/console/admins");
  return res.data.admins ?? [];
}
export async function createAdmin(input: { email: string; password: string; role: string }): Promise<ConsoleAdmin> {
  const res = await api.post<ConsoleAdmin>("/console/admins", input);
  return res.data;
}
```

### 6.2 编写页面（`src/routes/<resource>/pages.tsx`）

列表页直接复用 `ResourceListPage`（搜索/分页/选择/空态/加载骨架全内置），只需提供列定义与行操作：

```tsx
// 参照 src/routes/admins/pages.tsx
const columns: ColumnDef<ConsoleAdmin>[] = [
  { key: "email", header: "邮箱", cell: (a) => a.email },
  { key: "role", header: "角色", cell: (a) => <Badge>{a.role}</Badge> },
];

export function AdminsListPage() {
  const queryClient = useQueryClient();
  const { data: admins = [], isLoading } = useQuery({
    queryKey: ["console-admins"],
    queryFn: listAdmins,
  });
  const remove = useMutation({
    mutationFn: deleteAdmin,
    onSuccess: () => {
      toast.success("管理员已删除");
      queryClient.invalidateQueries({ queryKey: ["console-admins"] });
    },
  });
  return (
    <ResourceListPage
      title="系统管理员"
      isLoading={isLoading}
      items={admins}
      columns={columns}
      getSearchText={(a) => `${a.email} ${a.role}`}
      toolbarActions={<CreateAdminDialog onCreated={() => queryClient.invalidateQueries({ queryKey: ["console-admins"] })} />}
      rowActions={(a) => <RowDeleteButton onConfirm={() => remove.mutate(a.id)} loading={remove.isPending} />}
    />
  );
}
```

增删改交互惯例：

- 弹窗表单用 `Dialog`（Radix）组合 `useMutation`；成功 → `toast.success` + 关闭弹窗 +
  `queryClient.invalidateQueries` 使列表刷新；
- 行内删除用 `RowDeleteButton`（`components/resource/shared.tsx`，内嵌确认弹窗）；
- 列表搜索/分页由 `useListParams`（URL query `q`/`page`/`pageSize`）+ `filterByQuery`/`paginate` 完成
  ——前端列表为「全量拉取 + 本地分页」模式（`ResourceListPage` 内部实现）。

### 6.3 注册路由（`src/App.tsx`）

```tsx
import { AdminsListPage } from "@/routes/admins/pages";
// ...
<Route path="admins" element={<AdminsListPage />} />
```

- 所有页面挂在受 `RequireAuth` 保护的 `/console` 布局路由下（`<Route element={<RequireAuth><Layout/></RequireAuth>}>`）；
- 嵌套详情页模式参照 databases：`databases/:dbId/collections/:collId` 用 `CollectionLayout` 包子路由。

### 6.4 接入菜单（`src/components/Layout.tsx`）

`navSections` 数组按分组声明菜单项，图标用 lucide-react：

```tsx
{
  title: "System",
  items: [
    { to: "/console/projects", label: "Projects", icon: FolderKanban },
    { to: "/console/admins", label: "Admins", icon: ShieldCheck },
    { to: "/console/settings", label: "Settings", icon: Settings },
  ],
}
```

### 6.5 验证

```bash
task console-build && task build   # 或先 task console-dev 手工走一遍流程
```

---

## 7. 常见坑

1. **改了前端忘了重构建**：`go:embed` 只打包构建时刻的 `dist/`，务必 `task console-build` 后再
   `task build`；
2. **dev 下 cookie 失效**：vite proxy 必须指向与后端 cookie Secure 配置匹配的地址（本地
   `http://localhost:9099`，同源才行）；
3. **列表接口空返回**：服务端 `ListProjectsResponse.projects` 为空数组时 `res.data.projects ?? []`
   兜底，页面拿到 `[]` 显示空态而不是 undefined 崩溃；
4. **错误消息**：从 `error.response.data.error.message` 读取，不要用 axios 默认 message；
5. **请求别绕过 `api` 实例**：刷新（refresh）故意用裸 axios，其余请求一律走 `api` 以获得
   `X-Torchwood-Project` 头与 401 自动刷新能力；
6. **角色/权限判断在页面做**：`useAuth()` 只给登录态，owner/admin/viewer 差异在页面内按
   `getCurrentAdmin()` 的 `role` 判断。
