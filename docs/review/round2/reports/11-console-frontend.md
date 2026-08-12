# Round 2 复审报告：11 - Console 前端（console/src）

> 审查基准：当前工作区代码（HEAD）。验证命令：`npx tsc --noEmit`（console/）通过；`pnpm lint` 通过。未运行浏览器/集成测试，未修改任何源代码。

## 1. 修复验证结论表

| 修复项 | 结论 | 证据（文件:行号） | 说明 |
|--------|------|-------------------|------|
| F9-1 分片上传续传 key 碰撞 | ✅ 已修复 | `console/src/routes/storage/chunked-uploader.tsx:35-37`（key 含 `lastModified`）、`:81-92`（size/part_count 一致性校验）、`:92-95`（不一致清旧 key） | 同 bucket+同名+同大小但不同内容或已过期文件不会再错用旧 session；碰撞场景仍有极小概率（name/size/lastModified 全相同），但已满足修复方案要求。 |
| F9-2 登出失败无兜底 | ✅ 已修复 | `console/src/hooks/useAuth.tsx:66-78` | `await apiLogout()`，失败 toast 提示，无论成败都清空 projectId、本地状态与全部 Query 缓存。 |
| F9-3-a 跨项目 Query Cache 污染 | ✅ 已修复 | `console/src/routes/functions/pages.tsx:112`（`["functions", projectId]`）、`console/src/routes/storage/pages.tsx:112`（`["buckets", projectId]`）、`console/src/routes/Dashboard.tsx:27-54` | 列表类查询 key 已追加 projectId；`invalidateQueries` 仍常使用前缀 key（如 `["buckets"]`），通过 TanStack Query 前缀匹配可失效当前项目缓存，但会跨项目过度失效。 |
| F9-3-b 保存后表单与服务端失同步 | ✅ 已修复 | `console/src/routes/databases/pages.tsx:1667-1671` | `updateDocument` onSuccess 用响应 doc 通过 `documentToValues` 重建表单 values；同时清空 increments。 |
| F9-3-c 批量更新缺 MAX_BULK_OPERATIONS 预检 | ✅ 已修复 | `console/src/routes/databases/pages.tsx:102`（常量 1000）、`:1276-1278`、`:1300-1302` | 批量删除与批量更新均在前端预检，超限立即 toast 阻断。 |
| F9-3-d 无效路由参数白屏 | ⚠️ 部分修复 | `console/src/routes/databases/pages.tsx:1590`、`console/src/routes/databases/CollectionLayout.tsx:55`、`console/src/routes/databases/pages.tsx:1688` | 详情页（Document/Collection/Database）已加 `<NotFound />` 兜底；但**新建页**（CollectionNewPage、DatabaseNewPage 等）仍未校验父资源存在性，仅在提交时由后端报错。 |
| F9-3-e 分片上传无重试/取消 | ✅ 已修复 | `console/src/routes/storage/chunked-uploader.tsx:23`（`MAX_CHUNK_ATTEMPTS=3`）、`:132-145`（退避重试）、`:173-191`（取消 + abortUpload + 清 key）、`:66-73`（卸载 abort） | 单分片失败自动重试，可手动取消；AbortController 在 cancel/卸载时中止，未观察到泄漏。 |
| F9-3-f 前端硬编码分片常量 | ✅ 已修复 | `console/src/api/storage.ts:100-101`（注释说明仅用于预检）、`console/src/routes/storage/chunked-uploader.tsx:98-108`（使用 session.chunk_size） | 实际切片大小以服务会话返回为准，前端常量仅用于 `shouldChunk/isTooLarge` 预检。 |
| F9-3-g 双 toast 重复提示 | ⚠️ 部分修复 | `console/src/api/client.ts:117-118`（全局拦截器 toast 业务错误）、`console/src/routes/storage/pages.tsx:519-520`（生成分享链接 catch 再 toast） | 全局拦截器已统一处理 API 错误，但 `generateShare` 仍 catch 并 toast，造成同一错误两次提示；Login 表单也会与拦截器 401 toast 重复通知。 |
| F9-3-h Promise.all 批量删除无失败处理 | ✅ 已修复 | `console/src/routes/storage/pages.tsx:130`、`console/src/routes/storage/pages.tsx:317`、`console/src/routes/functions/pages.tsx:157`、`console/src/routes/users/pages.tsx:101`、`console/src/routes/teams/pages.tsx:110`、`console/src/routes/databases/pages.tsx:658`、`console/src/routes/databases/pages.tsx:838`、`console/src/routes/api-keys/pages.tsx:85` | 全部改为 `Promise.allSettled`，失败数量汇总提示。 |
| F9-3-i 角色权限 UI gating | ⚠️ 部分修复 | `console/src/hooks/useAdminRole.ts:11-17`、`console/src/routes/storage/pages.tsx:109`、`console/src/routes/functions/pages.tsx:109`、`console/src/routes/databases/pages.tsx:769` 等 | viewer 写按钮已隐藏；member/platformAdmin 区分基本正确。**但缺少路由级角色守卫**，直接访问 URL 仍可进入表单页；`api-keys/pages.tsx:258` 删除按钮未按角色隐藏。 |
| F9-3-j 路由级 React.lazy + ErrorBoundary | ❌ 未修复 | `console/src/App.tsx:1-52`（静态 import）、`:134-147`（仅全局 ErrorBoundary） | 未使用 `React.lazy`/`Suspense` 做路由级拆分；ErrorBoundary 仅包裹整个路由树，非路由级。 |

## 2. 新发现问题

### 🟠 P1

1. **路由级权限守卫缺失，URL 可直接绕过 UI 隐藏**
   - 位置：`console/src/App.tsx:75-131`
   - 问题：`RequireAuth` 仅检查登录态，未按 `owner/admin/member/viewer` 限制写路由。viewer/member 直接访问 `/console/users/new`、`/console/databases/new` 等 URL 仍会渲染页面（仅按钮被 disabled）。
   - 影响：前端权限 gating 仅停留在 UI 层，依赖后端兜底；体验与安全性不一致。
   - 建议：增加 `RequireRole` 组件或在页面级对敏感路由做 `useEffect` redirect；与 `useAdminRole` 的 `canWrite`/`isPlatformAdmin` 对齐。

2. **API Key 详情页删除按钮未按角色隐藏**
   - 位置：`console/src/routes/api-keys/pages.tsx:258`
   - 问题：`ApiKeyDetailPage` 的 `actions` 中 `<DeleteButton />` 未使用 `isPlatformAdmin(role)` 包裹，与列表页（`platformAdmin` 才显示删除）不一致。
   - 影响：任何登录 admin 都能看到并点击删除，后端会拒绝但交互错误。
   - 建议：用 `isPlatformAdmin(role)` 条件渲染，与列表页保持统一。

3. **生成分享链接失败时出现双 toast**
   - 位置：`console/src/routes/storage/pages.tsx:519-520`、`console/src/api/client.ts:117-118`
   - 问题：`createFileToken` 失败已由全局拦截器 toast 一次；`generateShare` 的 catch 又 toast 一次。
   - 影响：同一错误提示两次，干扰用户。
   - 建议：`generateShare` 不再 toast，仅做状态回滚；或给该请求加标记跳过拦截器 toast。

4. **登录失败时表单内错误与全局 401 toast 重复通知**
   - 位置：`console/src/routes/Login.tsx:67-70`、`console/src/api/client.ts:96-98`
   - 问题：Login 将错误显示在表单内；拦截器对登录 401 又触发一次 `toast.error`。
   - 影响：用户同时看到 inline error 和全局 toast。
   - 建议：登录请求跳过全局 toast，仅由页面展示错误。

5. **路由级 React.lazy + ErrorBoundary 未实现**
   - 位置：`console/src/App.tsx:1-147`
   - 问题：所有页面静态 import，无代码分割；ErrorBoundary 仅全局包裹。
   - 影响：首屏 bundle 大；单个页面渲染错误会波及整个 Console。
   - 建议：按资源路由使用 `React.lazy` + `<Suspense fallback={...}>`，并在每个 lazy 组件外或路由出口处加 ErrorBoundary。

### 🟡 P2

6. **Console 前端无任何单元测试**
   - 位置：`console/src/**/*.test.{ts,tsx}` 不存在
   - 问题：分片上传重试/取消、表单同步、权限 gating 等关键逻辑无自动化测试。
   - 影响：回归风险高，重构困难。
   - 建议：为 `useAdminRole`、分片上传状态机、批量删除结果汇总等补充纯函数/Hook 测试。

7. **分片上传 `run` callback 依赖不稳定状态导致频繁重建与 stale closure**
   - 位置：`console/src/routes/storage/chunked-uploader.tsx:115-165`（`useCallback` deps 含 `uploaded`、`total`）
   - 问题：`uploaded`/`total` 在运行过程中变化，使 `run` 每次上传进度变化都重新创建；错误提示中引用的 `uploaded`/`total` 可能为旧值。
   - 影响：性能开销与错误信息不准确。
   - 建议：使用 ref 或 reducer 保存运行中状态，避免把高频变化状态放入 callback deps。

8. **批量删除 summary toast 可能与全局拦截器重复提示**
   - 位置：各 `handleBulkDelete`（如 `storage/pages.tsx:127-143`、`functions/pages.tsx:154-170`）
   - 问题：`Promise.allSettled` 不会吞掉 rejected promise 的拦截器 toast，随后 summary 又 toast 一次。
   - 影响：失败条目多时会弹出多个 toast。
   - 建议：批量请求统一使用一个 toast 汇总，或在 API 层对批量调用标记跳过单条 toast。

9. **新建页未校验父资源存在性**
   - 位置：`console/src/routes/databases/pages.tsx:938-980`（CollectionNewPage）、`:726-761`（DatabaseNewPage）等
   - 问题：直接进入 `/console/databases/invalid/collections/new` 会渲染表单，提交后才报错。
   - 影响：无效路由参数体验差，接近原 F9-3-d 的遗漏场景。
   - 建议：新建页也查询父资源，不存在时返回 `<NotFound />`。

10. **`invalidateQueries` 使用前缀 key 导致跨项目过度失效**
    - 位置：`console/src/routes/storage/pages.tsx:121`、`console/src/routes/functions/pages.tsx:131` 等
    - 问题：`queryClient.invalidateQueries({ queryKey: ["buckets"] })` 会匹配所有 projectId 的 bucket 列表。
    - 影响：切换项目时可能触发不必要的重新请求。
    - 建议：列表失效使用完整 key `["buckets", projectId]`。

### 🟢 P3

11. **部分表单字段缺少服务端响应后的 dirty 状态重置**
    - 位置：`console/src/routes/users/pages.tsx:481-508`（UserEditPage 编辑后未用响应重建表单）等
    - 问题：虽然后端返回最新数据，但页面依赖后续 `invalidateQueries` 刷新，表单状态与查询缓存存在短暂不一致。
    - 影响：极低，仅可维护性。
    - 建议：统一仿照 DocumentDetailPage 模式，在 mutation onSuccess 用响应重建表单值。

## 3. 模块总体结论

- **修复完成度估计**：约 **70%**（11 项中 8 项 ✅、3 项 ⚠️、1 项 ❌）。核心数据一致性与上传稳定性修复已落地，但权限路由守卫、ErrorBoundary/懒加载、残留重复 toast 仍未闭环。
- **剩余风险 Top 3**：
  1. 前端权限仅 UI 层隐藏，缺乏路由级守卫，依赖后端兜底（P1）。
  2. 无路由级懒加载与 ErrorBoundary，bundle 与稳定性风险仍在（P1）。
  3. 关键交互（分片上传、批量操作、权限分支）缺少自动化测试，后续回归无保障（P2）。
- **是否建议关闭本模块审查**：**不建议关闭**。需先解决 F9-3-j（❌）与 F9-3-i 的路由守卫、F9-3-g 的残留双 toast，并补充基础单元测试后再做最终验收。
