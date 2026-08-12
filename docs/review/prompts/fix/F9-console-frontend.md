# 修复任务 F9：Console 前端修复

## 角色

你是资深 React/TypeScript 工程师，负责修复 Torchwood Admin Console 前端的审查发现。
方案详见 `docs/review/fix-plan.md` §9（F9 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`（Console 会话约定：HttpOnly cookie、不用 localStorage 存 token）、
  `docs/review/fix-plan.md` §9
- 审查报告（背景）：`docs/review/` 下的 11 报告

## 修复清单

1. **分片上传续传 key 碰撞 → 文件损坏**（P1）：
   - 位置：`console/src/routes/storage/chunked-uploader.tsx:27-28`
     （`uploadKey = torchwood:upload:${bucketId}:${file.name}:${file.size}` 不含内容特征）。
   - 修复：key 加入 `file.lastModified`（或内容哈希前缀）；`start()` 复用 session 前
     校验 session 与当前文件 `size` 一致，不一致则重建会话并删除旧 key。
2. **登出失败无兜底 + 缓存未清理**（P1）：
   - 位置：`console/src/hooks/useAuth.tsx:63-70`（logout 对 apiLogout fire-and-forget）、
     `console/src/App.tsx:53-60`。
   - 修复：logout await 登出请求，失败时 toast 警告「登出失败，会话可能仍有效」并引导
     重试；成功（或本地完成）时 `queryClient.clear()` 清空所有查询缓存
     （在 AuthProvider 内用 useQueryClient 实现）。
3. **P2 补强**：
   - 跨项目 Query Cache 污染：`routes/functions/pages.tsx:106-109`（queryKey 无 projectId）、
     `routes/storage/pages.tsx:224-227` 同理 → key 统一为 `[resource, projectId]`
     （其余页面已遵守此约定，可参考）。
   - 保存后表单与服务端失同步：`routes/databases/pages.tsx:1567-1580`（DocumentDetailPage
     的 initialized 守卫 + invalidate 后不回填）→ 保存成功后用响应文档重建 values。
   - 批量更新缺 MAX_BULK_OPERATIONS 预检：`routes/databases/pages.tsx:1226-1244`
     → 与 `handleBulkDelete`（:1206-1210）一致预检 ids.length 并 toast。
   - 无效路由参数白屏：`routes/databases/pages.tsx:997-998,1403-1404`
     → 返回 NotFound 组件（参考其他页面的 NotFound 用法）。
   - 分片上传无重试/取消：`routes/storage/chunked-uploader.tsx:75-108`
     → 单分片有限重试（2 次退避）+ 「取消上传」（abortUpload + 清 localStorage key）+
     unmount 时中止。
   - 前端硬编码分片常量：`api/storage.ts:98-99` + `chunked-uploader.tsx:81`
     → 切片一律使用服务端 `session.chunk_size`，前端常量仅用于预检。
   - 双 toast：`storage/pages.tsx:459`、`databases/pages.tsx:1512,1605`、
     `functions/pages.tsx:488`、`chunked-uploader.tsx:92-96` 的 onError 再 toast
     （`api/client.ts:117-119` 拦截器已统一 toast）→ 删除页面级 onError toast
     （保留状态重置逻辑）。
   - Promise.all 批量删除无失败处理：`storage/pages.tsx:124-134`（api-keys/teams/databases
     同模式）→ 改 `Promise.allSettled` 汇总成功/失败数分别提示。
   - 角色权限 UI gating：`routes/admins/pages.tsx:72-115` 已有 owner 判断模式 →
     为 projects/users/teams/storage/databases/functions/settings 的写操作按钮
     按当前 admin 角色隐藏/禁用（viewer 只读、member 部分写）——若获取当前 admin
     角色的 hook 不存在则新增（参考现有 getCurrentAdmin 用法）。
   - 路由级 `React.lazy` + 全局 ErrorBoundary：`App.tsx`（可选，若改动大可只做
     ErrorBoundary）。

## 约束

- **不要**改后端代码；不修改 `api/client.ts` 的拦截器主体逻辑（双 toast 只在页面侧修）
- 不引入新依赖；保持 Tailwind + shadcn/ui 风格
- 完成后必须 `npx tsc --noEmit`（console/ 目录）通过；如 `npm run build` 可运行则运行

## 验证

- `npx tsc --noEmit`（console/ 目录）
- 如可行：`npm run build`（console/ 目录）
- 手动走查改动路径（静态确认）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；列出需要人工浏览器
验证的项（如续传、登出失败场景）。
