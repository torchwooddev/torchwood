# 审查任务：11 - Console 前端（console/src）

## 角色

你是资深 React/TypeScript 前端代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Admin Console 前端」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定，特别是 Console 会话约定）与 `docs/developer/10-console.md`
- 技术栈：React 19 + TypeScript + Vite + React Router 7 + TanStack Query 5 + Tailwind 3 + shadcn/ui 风格组件 + sonner（toast）+ lucide-react
- 会话模型：**HttpOnly cookie** `TORCHWOOD_session_console`（SameSite=Lax，refresh cookie 限 `/v1/console/auth` 路径），**前端不用 localStorage 存 token**
- 嵌入方式：`console/embed.go` 将 `dist/` go:embed 进 Go 二进制，`/console/` 路径 serve；修改 Console 代码需先 `task console-build` 再 `task build`
- 页面：Dashboard、Projects、API Keys、Users、Teams、Databases（含文档编辑器/Schema）、Storage、Functions、Admins、Settings

## 审查范围

- `console/src/`（全部 `*.ts`/`*.tsx`，含 `api/`、`components/`、`hooks/`、`lib/`、`routes/`）
- 交叉引用（只读）：`console/src/main.tsx`、`console/src/App.tsx`（如存在）、`console/vite.config.ts`、`console/package.json`（依赖与脚本）

## 审查重点

1. **会话与认证**：登录/刷新/登出流程（cookie 使用）、401 处理（是否全局跳登录、是否静默刷新）、CSRF 防护现状（cookie SameSite=Lax 下 GET 安全但跨站 POST 的表单攻击面——API 是否要求自定义 header 或 token 校验）；是否有 token 写入 localStorage/sessionStorage 的残留。
2. **数据获取**（TanStack Query）：query key 设计（跨页面缓存污染）、staleTime/invalidation 正确性（写入后是否 invalidate 相关 key）、错误处理（toast 与重试）、乐观更新与回滚、分页/无限查询的 cursor 处理。
3. **路由与权限**：受保护路由的守卫逻辑（未认证跳转）、admin/owner 权限的 UI 一致性（无权限页面/按钮是否隐藏或禁用）、路由参数校验（无效 ID 的处理）。
4. **XSS 风险**：`dangerouslySetInnerHTML`、URL 拼接（下载链接、预览链接是否 encodeURIComponent）、用户数据（文档字段、文件名、prefs）的渲染转义、`<a href>` 的 `javascript:` 防护。
5. **表单与校验**：表单校验与后端校验一致性、上传组件（Storage 分片上传：进度、断点续传 localStorage 的 key 隔离与清理）、文件大小前端预检。
6. **状态管理**：非 Query 状态的 Context/本地状态使用是否合理、避免 prop drilling 反模式；全局 loading/error 状态。
7. **可访问性与体验**：shadcn/ui 组件使用是否规范（Dialog/Dropdown 的可访问性）、键盘操作、加载骨架屏。
8. **构建与打包**：bundle 体积（大依赖拆分）、`vite.config.ts` 的代理与构建配置、与 embed 约定一致。
9. **API 客户端封装**（`api/`）：请求封装是否统一处理错误体（结构化错误 `error.proto` 解析）、header 设置（`X-Torchwood-Project` 的来源——是否可被用户篡改）。

## 通用检查项

1. 安全：XSS、CSRF、敏感数据泄露（日志/console/存储）
2. 正确性：数据一致性（缓存陈旧）、竞态（请求乱序响应覆盖）
3. 性能：不必要的重渲染（useMemo/useCallback 滥用或缺失）、大列表渲染
4. 一致性：与后端 API 契约一致（字段名、错误码）、与 AGENTS.md 约定一致
5. 可维护性：组件拆分合理性、重复代码、命名
6. 测试：现有测试覆盖情况（如无测试文件，评估可测性并指出关键逻辑缺少测试）

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：XSS、token 泄露、认证绕过
- 🟠 **P1 高**：功能缺陷、数据丢失风险（缓存错误覆盖）、竞态
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（安全水平、代码质量、最需优先修复的 3 项）。

## 验证方式

- 可运行 `npx tsc --noEmit`（`console/` 目录）辅助检查类型错误
- 不要运行 dev server 或浏览器测试；纯静态审查
