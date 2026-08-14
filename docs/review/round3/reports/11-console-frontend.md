# Round 3 全量审核报告：11 - Admin Console 前端（console/src）

> 审查基准：当前工作区代码（只读）。对照 `docs/review/prompts/11-console-frontend.md`、`docs/developer/10-console.md`、`docs/review/round2/reports/11-console-frontend.md`。通读 `console/src/`（`api/`、`routes/`、`components/`、`hooks/`、`lib/`、`App.tsx`、`main.tsx`），交叉阅读 `console/vite.config.ts`、`console/package.json` 以及 cookie/CSP 相关后端实现。未运行 dev server / 浏览器测试，未修改任何源代码。

## 摘要

Round 2 的核心缺口（路由级 `RequireRole`、`React.lazy` + 按路径 `ErrorBoundary`、登录/分享双 toast、API Key 删除 gating、分片上传 stale closure）均已落地。会话模型仍正确：HttpOnly + SameSite=Lax cookie，前端不存 token。未发现 XSS（无 `dangerouslySetInnerHTML`）或认证绕过（P0 为零）。

本轮新确认的主要问题：函数列表批量删除**没有确认框**（一键即删，数据丢失面）；文件预览 `<img src>` **无法携带 `X-Torchwood-Project`**，admin JWT 本身不含项目 ID，预览接口会返回 `missing project context`。viewer 写按钮整体隐藏正确，但 `canWrite(undefined)` 在角色查询完成前 fail-open，且「分享链接」未按角色隐藏。后端仍是权威。

**Verdict：不建议关闭本模块。** 需先处理 2 项 P1，并补齐 viewer gating 的 fail-closed 与基础测试后再验收。

## Round 2 遗留项复核

| 修复项 | 结论 | 证据 | 说明 |
|--------|------|------|------|
| F9-1 分片续传 key 碰撞 | ✅ 仍有效 | `chunked-uploader.tsx:35-37`、`:81-99` | key 含 `lastModified`；复用前校验 `part_count` |
| F9-2 登出失败兜底 + 清缓存 | ✅ 仍有效 | `useAuth.tsx:66-77` | await 登出；失败 toast；始终清 projectId / 状态 / Query 缓存 |
| F9-3-a 跨项目 Query Cache | ✅ 仍有效 | Dashboard / storage / functions / users 等列表 key 均含 `projectId` | `invalidateQueries` 也多用完整 key |
| F9-3-b 文档保存回填 | ✅ 仍有效 | `databases/pages.tsx:1681-1687` | `documentToValues` 用响应重建 |
| F9-3-c 批量上限预检 | ✅ 仍有效 | `databases/pages.tsx:102`、`:1290-1293`、`:1314-1317` | 删除/更新均预检 1000 |
| F9-3-d 无效路由白屏 | ✅ 基本闭环 | CollectionNewPage `:951-973` 查父库；详情页均有 `<NotFound />` | DatabaseNewPage 无父资源，无需校验 |
| F9-3-e 分片重试/取消 | ✅ 仍有效 | `chunked-uploader.tsx:23-24`、`:142-155`、`:184-201` | 3 次退避 + 取消 abort + 卸载中止 |
| F9-3-f 切片用服务端 chunk_size | ✅ 仍有效 | `storage.ts:98-108`；`chunked-uploader.tsx:141` | 前端常量仅预检 |
| F9-3-g 双 toast | ✅ 已闭环 | `auth.ts:13` / `:40` / `:62` `__skipToast`；`storage/pages.tsx:521-525` | 登录与分享不再与拦截器叠 toast |
| F9-3-h Promise.allSettled | ✅ 仍有效 | api-keys / users / teams / storage / functions / databases | 批量删除均 `__skipToast` + 汇总 |
| F9-3-i 角色 UI + 路由守卫 | ⚠️ 大部分已修 | `App.tsx:128-147` `RequireRole`；api-keys 删除已 `isPlatformAdmin` | 仍有 fail-open 与分享按钮漏网，见 P2-1 |
| F9-3-j lazy + ErrorBoundary | ✅ 已修复 | `App.tsx:10-105` lazy；`:150-155` 按 pathname 重置 | 单页崩溃不再拖垮整棵路由树 |
| 前端单元测试 | ❌ 仍无 | `console/src/**/*.test.*` 不存在；`package.json` 无 test 脚本 | 同 Round 2 P2 |

## 已核实健康

### 会话与 CSRF

- 启动时清理迁移残留 token：`console/src/api/client.ts:8-9`（`TORCHWOOD_console_token` / `torchwood_console_refresh_token`）。
- 凭证只走 cookie：`login` / `signUp` 注释与实现均不读写 token（`api/auth.ts:9-14`、`:58-64`）。项目 ID 才进 localStorage（`client.ts:50-59`），属上下文而非凭证。
- 401 单飞刷新 + 一次重试：`client.ts:36-47`、`:97-118`；登录请求不进刷新流；`missing project context` 不强制跳登录。
- 后端 cookie：`HttpOnly` + `SameSite=Lax`，refresh 限 `/v1/console/auth`（`internal/api/consolegrpc/cookies.go:23-28`、`:52-60`）。变更类接口均为 POST，跨站表单 POST 不带 cookie。
- 额外纵深：项目级 API 依赖自定义头 `X-Torchwood-Project`，跨站表单无法设置。SPA 下发 `X-Frame-Options: DENY` 与 CSP（`internal/infra/server/console.go:41-46`）。
- 登出：`__skipAuthRetry`（`auth.ts:25-28`）；失败 toast 并清本地态。

### `X-Torchwood-Project`

- 所有走 `api` 实例的请求在存在项目 ID 时统一带头（`client.ts:62-68`）。刷新故意用裸 axios，正确。
- `ProjectBootstrap`（`components/ProjectBootstrap.tsx:6-25`）在无项目或本地 ID 不在列表中时回落到第一个项目。
- 项目域查询普遍 `enabled: !!projectId`，避免头未就绪就打管理 API。
- 例外：`<img src={filePreviewUrl(...)}>` 与用户粘贴的分享 URL **不是** axios，带不上该头（见 P1-2）。

### 路由守卫与角色 UI

- `RequireAuth`（`App.tsx:116-126`）在 refresh 探测期间返回 null，未登录跳 `/console/login`。
- `RequireRole`（`App.tsx:128-147`）覆盖新建/编辑写路由：`projects/new`、`api-keys/new`、`users/new`、`users/:id/edit`、`teams/new`、`storage/new`、`databases/new`、`collections/new`、`documents/new`。角色加载中返回 null；`role === undefined` fail-closed。
- 列表/详情页：viewer 的创建/删除/批量按钮基本按 `canWrite` / `isPlatformAdmin` 隐藏；系统集合与 schema DDL 仅平台 admin。后端拒绝是最终权威。
- Admins 页仅 owner 可增删改（`admins/pages.tsx:72-113`）。

### XSS / 渲染转义

- `console/src` 无 `dangerouslySetInnerHTML` / `innerHTML` / `eval` / `javascript:` href。
- 用户数据、文档字段、prefs、执行 stdout/stderr 均经 React 文本节点或 `<pre>` 渲染，自动转义。
- 详情跳转使用 React Router `<Link to={detailPath(item)}>`（内部路径），不是自由拼接的外链。
- 下载走 blob + `createObjectURL`，用完 `revokeObjectURL`（`storage.ts:211-226`）。`a.download` 当文件名，不执行脚本。
- JSON 编辑（文档 / 团队 prefs / 批量更新）只 `JSON.parse` 后作为数据提交，不插入 DOM。

### 敏感字段

- API Key secret、初始化默认 Key 仅展示一次，不写 storage（`api-keys/pages.tsx:164-200`；`Login.tsx:34-35`、`:97-98`）。
- 函数环境变量与后端 `******` 掩码对齐；未编辑项原样提交以免覆盖（`functions.ts:62-65`；`functions/pages.tsx:541-544`、`:667-670`）。
- OAuth `client_secret` 用 password 输入，已配置时留空保留（`settings/pages.tsx:285-300`）；列表只展示 `client_id` + `has_client_secret`。
- 模拟登录只展示 `access_token`，不展示 `refresh_token`（`users/pages.tsx:351-353`、`:457-459`）。
- 文件分享 token 出现在一次性对话框与剪贴板，不持久化。

### 批量 / Schema / 分片上传

- 文档批量删除/更新走单次 RPC + `MAX_BULK_OPERATIONS=1000` 预检 + `BulkDeleteButton` 确认。
- 集合/库/属性/索引删除均有确认框；系统集合不可选、不可删。
- 分片：预检大小、服务端 `chunk_size`、续传一致性、重试、取消、卸载 abort。

### 错误与空态

- 全局拦截器解析 `error.response.data.error.message` 并 toast（可用 `__skipToast`）。
- 详情 404 用 `<NotFound />`；列表用 `EmptyState` + 骨架 `LoadingTable` / `DetailSkeleton`。
- 登录/初始化错误内联展示，并跳过全局 toast。

---

## 🔴 P0 严重

无。未发现 token 泄露到 `localStorage`/`sessionStorage`、XSS 注入点或可绕过 `RequireAuth` 的路径。

---

## 🟠 P1 高

### 1. 函数列表批量删除无确认，一点即删

- 位置：`console/src/routes/functions/pages.tsx:195-207`
- 问题：其它资源的批量删除一律走 `BulkDeleteButton`（内嵌确认）。Functions 列表直接把 `handleBulkDelete` 绑在红色按钮 `onClick` 上，无二次确认。
- 影响/风险：误触或误选后立即并发删除多个函数（含部署与变量），不可撤销。这是明确的数据丢失面。
- 建议：复用 `BulkDeleteButton`，与 storage / databases / users 对齐。

### 2. 文件预览 `<img>` 带不上项目头，admin 预览必然失败

- 位置：
  - `console/src/api/storage.ts:272-279`（`fileViewUrl` / `filePreviewUrl` 只拼路径）
  - `console/src/routes/storage/pages.tsx:595-599`（`<img src={filePreviewUrl(...)}>`）
- 问题：预览是浏览器直接 GET，**不能**附带 axios 拦截器里的 `X-Torchwood-Project`。Console admin JWT **不含** `ProjectID`（`internal/app/console/auth.go:247-255` 签发；`internal/infra/auth/validator.go:150-159` 组装 Principal 时也不填项目）。HTTP 读路径在 cookie 鉴权成功后若项目 ID 为空，直接 `missing project context`（`internal/api/serverhttp/file_handler.go:529-534`），**不会**回落到 `?token=`。
- 影响/风险：文件详情页图片预览对已登录管理员恒失败（裂图）。同一浏览器打开刚生成的分享链接也会因 cookie 抢先于 token 而失败，运维无法自测分享。下载走 axios 不受影响。
- 建议：预览改为 `api.get(..., { responseType: "blob" })` 再 `createObjectURL`（与 `downloadFile` 同路，自动带头）；或 URL 增加 `project` 查询参数。分享链接的「cookie 优先于 token」需后端 `resolveReadContext` 在缺项目上下文时继续尝试 file token（跨模块，前端可先用 blob 规避预览）。

---

## 🟡 P2 中

### 1. viewer 写操作 gating 仍有 fail-open 与漏网按钮

- 位置：
  - `console/src/hooks/useAdminRole.ts:15-16`（`canWrite`：`role !== "viewer"`，`undefined` 视为可写）
  - `console/src/routes/storage/pages.tsx:559-562`（「分享链接」未包 `writeable`）
  - 各列表页 `const writeable = canWrite(role)`（如 `users/pages.tsx:76`、`storage/pages.tsx:109`、`functions/pages.tsx:110`）
- 问题：
  1. 角色查询完成前 `role === undefined`，`canWrite` 为 true，viewer 首屏会闪现删除/新建/批量按钮，并可能点出写请求（后端拒绝）。对比 `RequireRole` 与 `isPlatformAdmin(undefined)` 已是 fail-closed。
  2. 签发文件分享 token 是写操作，viewer 始终可见并可点击。
- 影响/风险：非提权（后端权威），但与「viewer UI 隐藏写操作」约定不一致；误点会产生错误 toast。分享 token 若后端未按角色拒绝，则是真实越权面（需后端模块确认）。
- 建议：`canWrite` 改为显式白名单（`owner|admin|member`），加载中隐藏写控件；分享按钮加 `writeable`。

### 2. 「模拟登录」跳过确认对话框

- 位置：`console/src/routes/users/pages.tsx:367-370`（按钮直接 `issueToken.mutate`）；`:437-445`（`tokenOpen` 确认框从未被打开）
- 问题：重置密码走 `ConfirmDialog`，模拟登录本意也有确认框，但主按钮绕过它，一点即签发用户 access token 并创建会话。
- 影响/风险：平台 admin 误触会以该用户身份开会话。token 出现在弹层，不是静默泄露，但属于敏感操作缺少确认。
- 建议：按钮改为 `setTokenOpen(true)`，确认后再 `mutate`。

### 3. array 属性在文档表单中被 `String()` 打平，回写会损坏数据

- 位置：`console/src/routes/databases/pages.tsx:114-117`（`documentToValues`）；`:1504-1517`（`parseFieldValue` 无视 `array`）；`:1558-1575`（`buildDocumentData`）
- 问题：`["a","b"]` 变成 `"a,b"`；integer 数组 `Number.parseInt("1,2") === 1`。保存会把数组写成标量。Schema 明确支持 Array 勾选（`:376-378`）。
- 影响/风险：在文档详情点一次保存即可破坏数组字段。
- 建议：array / json 一律用 `JSON.stringify` / `JSON.parse`；或对 array 属性禁用简易输入、只走 JSON 模式。

### 4. 文档表单客户端校验失败时静默

- 位置：`console/src/routes/databases/pages.tsx:1558-1575`、`:1619-1621`、`:1665-1674`、`:1728-1730`
- 问题：`JSON.parse` 失败、必填为空、增量非整数都在 `mutationFn` 里 `throw`。无 `onError`，拦截器也看不到客户端异常。团队 prefs / 批量更新对 JSON 有 try/catch + toast，此处没有。
- 影响/风险：用户点保存后按钮恢复，无任何提示，以为没改动或以为已保存。
- 建议：提交前校验并 toast；`useMutation.onError` 兜底。

### 5. 分片上传重试会触发多次全局 toast

- 位置：`console/src/routes/storage/chunked-uploader.tsx:142-155`；`console/src/api/client.ts:122-123`
- 问题：`uploadChunk` 走 `api` 且未 `__skipToast`。单片最多失败 3 次，每次 ≥400 都 toast，再叠加最终失败。
- 影响/风险：弱网下 toast 刷屏；用户难以判断是否仍在重试。
- 建议：分片请求加 `__skipToast`，由 Uploader 汇总一次。

### 6. Console 前端仍无任何自动化测试

- 位置：`console/package.json:6-11`（无 `test`）；`console/src` 下无 `*.test.ts(x)`
- 问题：分片状态机、`canWrite`/`RequireRole`、`documentToValues`/`buildDocumentData`、批量 `allSettled` 汇总均无回归网。
- 影响/风险：Round 2/3 修过的逻辑再次回归时只能靠手工。
- 建议：先给纯函数（`canWrite`、`documentToValues`、`uploadKey`、批量结果汇总）补 Vitest。

---

## 🟢 P3 低

1. **OAuth 配置删除无确认**（`settings/pages.tsx:380-387`）：垃圾桶直接 `remove.mutate`。建议复用 `ConfirmDialog`。
2. **集合/属性删除文案过于笼统**（`shared.tsx:229-231`；`databases/pages.tsx:1051-1057`）：删 attribute 可能丢列数据，未单独警告。
3. **`document_security` 无 UI**（API 在 `databases.ts:108`、`:134`；`CollectionNewPage` / 设置弹窗未暴露）：文档级安全开关无法从 Console 配置。
4. **路径 ID 编码不一致**：`admins.ts` / `oauthProviders.ts` 用了 `encodeURIComponent`，多数 `/server/.../${id}` 未编码。自定义 collection/document ID 含 `/` 时可能切错路径。
5. **函数 zip 无前端体积预检**（`functions/pages.tsx:727-733`）：文案写 ≤50MiB，选择后直接上传。
6. **函数删除成功用 `window.history.back()`**（`functions/pages.tsx:528`）：可能回到站外或非列表页。应 `navigate("/console/functions")`。
7. **环境变量增删行未按角色禁用**（`functions/pages.tsx:660-696`）：仅「保存变量」看 `platformAdmin`。viewer 可改本地行，造成可写错觉。
8. **`forceReLogin` 文案英文**（`client.ts:77`）与全站中文不一致。
9. **复制 secret 未处理 clipboard 失败**（`Login.tsx:121-126`；`api-keys/pages.tsx:175-180`）。storage 分享复制已有 try/catch。
10. **列表全量拉取 + 前端分页**（`ResourceListPage.tsx:54-62`）：文档/用户量大时首屏与搜索都在浏览器完成。架构已知，中长期需服务端 filter/page。
11. **Dashboard 项目域卡片加载中显示 0**（`Dashboard.tsx:71-77`）：仅 Projects 有 skeleton。
12. **`RequireAuth` / `RequireRole` 加载返回 `null`**：整页空白一瞬。可复用「加载中…」骨架。
13. **用户会话「删除」无确认**（`users/pages.tsx:294-301`）：立即吊销令牌。
14. **增量 UI `step="any"` 但校验要求整数**（`databases/pages.tsx:1671-1672`、`:1758-1759`）。
15. **显式 `Content-Type: multipart/form-data`**（`storage.ts:92`、`:167`；`functions.ts:136`）：可能干扰浏览器自动 boundary。建议删掉该头，只传 `FormData`。

---

## 模块结论

| 维度 | 评价 |
|------|------|
| 安全水平 | 中上。会话/CSRF/XSS/密钥展示整体扎实；P0 无。预览与分享链接的项目上下文是本轮最大功能/安全交界问题。 |
| 代码质量 | 中上。API 封装统一，列表页模式一致，Round 2 的缓存/上传/守卫修复保持完好。文档 array 与静默校验是正确性短板。 |
| 权限模型 | UI 隐藏 + 后端权威的方向正确，且已有路由级守卫。剩余是 `canWrite` fail-open 与分享按钮。 |
| 测试 | 无。关键状态机不可回归。 |

**最需优先修复的 3 项**

1. 函数批量删除补确认框（P1-1，改动小、数据丢失面大）。
2. 文件预览改为携带项目上下文的 blob/`project` 参数（P1-2）。
3. `canWrite` fail-closed + 分享按钮 gating，并给 `documentToValues`/`buildDocumentData` 补 array/JSON 测试（P2-1 / P2-3 / P2-6）。

**是否建议关闭本模块审查：否。** Round 2 清单已基本闭环，但本轮仍有 2 个 P1 与 viewer gating 残留；补齐并回归分片上传 / 文档保存 / 预览后再做最终验收。
