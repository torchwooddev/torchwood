# 复审任务（Round 2）：11 - Console 前端（console/src）

## 背景
- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色
你是资深 React/TypeScript 前端代码审查专家。对 Torchwood Admin Console 前端做一次只读复审；同时你是修复验证者，需对照 fix-plan 逐条核实。

## 第一步：建立基线
- 读 `docs/review/prompts/11-console-frontend.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 F9 章节：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- console/src/` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文
- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（Console 会话约定）与 `docs/developer/10-console.md`
- 技术栈：React 19 + TypeScript + Vite + React Router 7 + TanStack Query 5 + Tailwind 3 + shadcn/ui + sonner + lucide-react
- 会话模型：**HttpOnly cookie** `TORCHWOOD_session_console`（SameSite=Lax，refresh cookie 限 `/v1/console/auth` 路径），前端不存 token 到 localStorage
- 嵌入方式：`console/embed.go` 将 `dist/` go:embed 进 Go 二进制；修改 Console 后需先 `task console-build` 再 `task build`
- 页面：Dashboard、Projects、API Keys、Users、Teams、Databases（含文档编辑器/Schema）、Storage、Functions、Admins、Settings

## 复审重点 A：修复验证（逐条核实）

对 fix-plan F9 中本模块的每一个修复项逐项核实：

- **F9-1 分片上传续传 key 碰撞 → 文件损坏**（`console/src/routes/storage/chunked-uploader.tsx:27-28`）
- **F9-2 登出失败无兜底**（`console/src/hooks/useAuth.tsx:63-70`）
- **F9-3-a 跨项目 Query Cache 污染**（`console/src/routes/functions/pages.tsx:106-109`、`console/src/routes/storage/pages.tsx:224-227`）
- **F9-3-b 保存后表单与服务端失同步**（`console/src/routes/databases/pages.tsx:1567-1580`）
- **F9-3-c 批量更新缺 MAX_BULK_OPERATIONS 预检**（`console/src/routes/databases/pages.tsx:1226-1244`）
- **F9-3-d 无效路由参数白屏**（`console/src/routes/databases/pages.tsx:997-998,1403-1404`）
- **F9-3-e 分片上传无重试/取消**（`console/src/routes/storage/chunked-uploader.tsx:75-108`）
- **F9-3-f 前端硬编码分片常量**（`console/src/api/storage.ts:98-99`）
- **F9-3-g 双 toast 重复提示**（`console/src/api/` 拦截器与页面级 `onError`）
- **F9-3-h Promise.all 批量删除无失败处理**（`console/src/routes/storage/pages.tsx:124-134`）
- **F9-3-i 角色权限 UI gating（viewer 隐藏写按钮）**（各列表/详情页）
- **F9-3-j 路由级 React.lazy + ErrorBoundary**（`console/src/` 路由配置）

每条按以下四点检查并给出结论：
1. 修复是否已落地（代码中能否找到对应改动）；
2. 修复是否正确完整——有无绕过路径、边界遗漏（例如只改了入口 A 没改入口 B、校验可加在错误层、并发场景仍可乘）；
3. 修复是否引入新问题（接口/行为变化是否同步到全部调用方与前端/SDK）；
4. 承诺的测试是否真实存在且断言的是真实行为（不是恰好通过的假断言）。

## 复审重点 B：回归与新问题排查
- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归）。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（XSS/CSRF/信息泄露/凭据处理）、正确性（错误处理/并发/竞态）、一致性（与 AGENTS.md 约定、后端 API 字段/错误码）、性能（不必要重渲染/大列表）、可维护性、测试覆盖。
- **本模块修复后特有风险点**：
  1. F9-1/F9-3-e 改动分片上传 key 与重试/取消逻辑后，要重查 localStorage 残留 key 是否在失败/取消/上传完成时清理，AbortController 是否被累积或泄漏；
  2. F9-3-a 在 query key 中追加 projectId 后，要重查所有 `invalidateQueries`/`setQueryData` 调用是否同步使用新 key，避免旧缓存无法失效导致跨项目数据残留；
  3. F9-3-g 统一 toast 处理后，要重查全局拦截器是否正确解析 `error.proto` 结构化错误体，以及页面级是否仍有重复 `onError` toast；
  4. F9-3-i 角色权限 UI gating 后，要重查「隐藏写按钮」是否只是 UI 层面，相关路由/操作是否仍可通过直接访问 URL 触发，前端路由守卫是否同步；
  5. F9-3-b 用响应重建表单 values 后，要重查 form dirty 状态、验证错误是否被正确重置，避免二次保存时提交旧数据。

## 输出要求
简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束
- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `npx tsc --noEmit`（`console/` 目录）与无外部依赖的纯单元测试辅助验证。
