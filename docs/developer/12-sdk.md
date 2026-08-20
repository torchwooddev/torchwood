# Torchwood TypeScript SDK 指南

> 本文档基于 `sdk/` 目录源码（`@torchwood/sdk` v0.1.0）编写，符号、方法与签名以
> `sdk/typescript/src/` 为准。目标读者：应用开发者、LLM Agent / 自动化脚本集成方。
> 关联：`docs/roadmap.md` §0（AI/Agent-Native 战略）、`sdk/README.md`。

---

## 1. SDK 定位

`@torchwood/sdk` 是 Torchwood 的官方 TypeScript 客户端，封装 **Client API**（终端用户，
JWT 鉴权）与 **Server API**（管理面，API Key + `X-Torchwood-Project` 鉴权），是
Torchwood **AI/Agent-Native** 能力的前端集成层 —— 便于 LLM Agent、自动化脚本与
MCP Tool Server 以类型安全的方式调用后端。

| 场景 | 推荐方式 | 说明 |
|------|----------|------|
| 管理面自动化（建用户、管文档、Storage） | **Server API** + API Key | 在 Console 或通过 `POST /v1/server/api-keys` 创建带 scope 的 Key |
| 终端用户身份流（注册/登录/会话） | **Client API** + JWT | SDK 自动持久化 access token（内存态，`setAccessToken` 可覆盖） |
| Agent 工具 schema 来源 | **OpenAPI** | `task generate-proto` 后在 `genproto/**/*.swagger.json` 获取 |
| 快速验证 | **Web 演示** | `task sdk-demo`，设置页填入首次部署引导展示的默认 API Key |

典型 Agent 工作流：用 scoped API Key 实例化 `Torchwood.withApiKey()` → 读取 OpenAPI
或 SDK 类型 → 调用 Server Databases/Users/Storage API → 将结构化响应回传给 LLM。

---

## 2. 包结构与构建

### 2.1 目录布局

| 路径 | 说明 |
|------|------|
| `sdk/typescript/` | SDK 包 `@torchwood/sdk` |
| `sdk/typescript/src/client/` | Client API 服务（Account / Databases / Groups） |
| `sdk/typescript/src/server/` | Server API 服务（Health / Projects / Users / Groups / Databases / APIKeys / OAuthProviders / Storage） |
| `sdk/typescript/src/graviton.ts` | `Torchwood` 门面类与静态工厂 |
| `sdk/typescript/src/http.ts` | `HttpTransport` 传输层与 `TorchwoodConfig` 配置类型 |
| `sdk/typescript/src/types.ts` | 手写 API 数据类型（Account、Document、User 等） |
| `sdk/typescript/src/errors.ts` | `TorchwoodError` 异常类型 |
| `sdk/demo/` | Web 演示站点（Vite，端口 5174） |

### 2.2 包元信息

- 名称：`@torchwood/sdk`，版本 `0.1.0`，license MIT，`engines.node >= 18`。
- 模块格式：ESM（`"type": "module"`），`main` / `types` 指向 `dist/index.js` /
  `dist/index.d.ts`，`exports` 仅暴露根入口 `.`。
- 构建工具：仅依赖 `typescript`（devDependency），`npm run build` 即 `tsc -p tsconfig.json`
  （ES2022 / NodeNext，输出到 `dist/`，含 `.d.ts` 声明与 sourcemap）。**SDK 不依赖
  运行时第三方包**，HTTP 走全局 `fetch`（可在 `TorchwoodConfig.fetch` 注入自定义实现）。

### 2.3 安装与构建

```bash
# 安装 SDK 与 demo 依赖并编译 SDK
task sdk-install          # sdk/typescript 与 sdk/demo 各执行 npm install
task sdk-build            # cd sdk/typescript && npm run build（tsc）

# 在应用项目中引入（本地路径或发布后从 npm 安装）
npm install @torchwood/sdk
```

若 `sdk/demo` 通过 `"@torchwood/sdk": "file:../typescript"` 本地引用，修改 SDK 源码后
需重新执行 `task sdk-build` 再启动 demo（`task sdk-demo` 自带该依赖）。

---

## 3. 入口：`Torchwood` 类

根入口导出：

```typescript
import { Torchwood, TorchwoodError } from "@torchwood/sdk";
import type { TorchwoodConfig } from "@torchwood/sdk";
```

> 说明：`src/client/` 与 `src/server/` 各 Service 类（如 `AccountService`、
> `ServerDatabasesService`）在包内导出，但 npm `exports` 只暴露根入口 `.`，
> 常规使用一律通过 `Torchwood` 实例的属性访问。

### 3.1 配置类型

```typescript
interface TorchwoodConfig {
  endpoint: string;       // 例如 http://localhost:9080（grpc-gateway HTTP 地址）
  projectId: string;      // 例如 "default"
  apiKey?: string;        // Server API 使用（X-Api-Key）
  accessToken?: string;   // Client API 使用（Authorization: Bearer）
  fetch?: typeof fetch;   // 可选，注入自定义 fetch
}
```

### 3.2 构造与静态工厂

| 成员 | 签名 | 说明 |
|------|------|------|
| 构造器 | `new Torchwood(config)` | 直接构造 |
| 静态工厂 | `Torchwood.create(config)` | 等价于构造器 |
| 静态工厂 | `Torchwood.withApiKey(endpoint, projectId, apiKey)` | **Server API**（可同时用于 Client API），后续请求自动携带 `X-Api-Key` 与 `X-Torchwood-Project` 头 |
| 静态工厂 | `Torchwood.withAccessToken(endpoint, projectId, accessToken)` | 携带已有用户 access token 的 Client API |
| 方法 | `setAccessToken(token?)` | 设置/清除 access token |
| 方法 | `getAccessToken()` | 读取当前 access token |
| 方法 | `getProjectId()` | 读取 project id |

### 3.3 实例属性（服务分组）

```typescript
const tw = Torchwood.withApiKey("http://localhost:9080", "default", apiKey);

tw.account;              // Client: 注册/登录/会话/偏好（AccountService）
tw.databases;            // Client: 文档 CRUD + count（ClientDatabasesService）
tw.groups;                // Client: 我的用户组与成员（ClientGroupsService）

tw.server.health;        // Server: 健康检查
tw.server.projects;      // Server: 项目
tw.server.users;         // Server: 用户
tw.server.groups;         // Server: 用户组与成员
tw.server.databases;     // Server: 库/集合/属性/索引/文档/Bulk
tw.server.apiKeys;       // Server: API Key 管理
tw.server.oauthProviders;// Server: OAuth Provider 配置
tw.server.storage;       // Server: Bucket / File
```

### 3.4 基础用法示例

```typescript
import { Torchwood } from "@torchwood/sdk";

// Server API：健康检查（无需鉴权）
const admin = Torchwood.withApiKey("http://localhost:9080", "default", apiKey);
const { status } = await admin.server.health.check();   // { status: "ok" }

// Client API：注册（成功后自动保存 access token）
const client = Torchwood.create({ endpoint: "http://localhost:9080", projectId: "default" });
await client.account.signUp({ email: "u@example.com", password: "Pass@123", name: "User" });

// Client API：写文档
const doc = await client.databases.createDocument("app", "notes", {
  data: { title: "Hi" },
  permissions: ["read:any", "update:users"],
});

// 读取 access token / 换 token
client.setAccessToken(await client.account.refresh(refreshToken).then(t => t.access_token));
```

---

## 4. Client API（终端用户，JWT）

所有 Client 请求默认 `auth: "user"`：存在 access token 时携带
`Authorization: Bearer <token>`。注册/登录/刷新类方法成功后自动
`setAccessToken`。

### 4.1 Account（`tw.account`）

| 方法 | 请求 | 说明 |
|------|------|------|
| `signUp({email, password, name})` | `POST /v1/account/sign-up` | 注册（`auth: "none"`），自动保存 token |
| `signIn({email, password})` | `POST /v1/account/sign-in` | 登录，自动保存 token |
| `signOut()` | `POST /v1/account/sign-out` | 登出，清除 token |
| `refresh(refreshToken)` | `POST /v1/account/refresh` | 刷新 access token，自动保存 |
| `me()` | `GET /v1/account/me` | 当前账号信息 |
| `updateAccount({name?, email?, password?, old_password?})` | `PATCH /v1/account` | 更新资料 |
| `listSessions()` / `deleteSession(id)` / `deleteSessions(keepCurrent?)` | `GET/DELETE /v1/account/sessions` | 会话管理 |
| `getPrefs()` / `updatePrefs(prefs)` | `GET/PUT /v1/account/prefs` | 用户偏好 JSON |
| `createOAuth2Session({provider, success, failure})` | `GET /v1/account/sessions/oauth2/{provider}` | OAuth2 跳转 URL（`{redirect_url}`） |
| `createOAuth2TokenSession({provider, code, state, ...})` | `POST .../oauth2/{provider}/token` | OAuth2 回调换 token，自动保存 |
| `createEmailOTP({email})` / `createEmailOTPSession({email, challenge_id, otp})` | `POST /v1/account/sessions/email-otp(+/verify)` | 邮箱验证码登录，后者自动保存 token |
| `createPhoneOTP({phone})` / `createPhoneOTPSession({phone, challenge_id, otp})` | `POST /v1/account/sessions/phone-otp(+/verify)` | 短信验证码登录，后者自动保存 token |
| `createWeChatMiniProgramSession({code})` | `POST /v1/account/sessions/wechat/miniprogram` | 微信小程序登录，自动保存 token |

### 4.2 Databases 文档（`tw.databases`）

签名统一为 `(databaseId, collectionId, ...)`：

| 方法 | 说明 |
|------|------|
| `createDocument(databaseId, collectionId, {document_id?, data, permissions?})` | 创建文档 |
| `listDocuments(databaseId, collectionId, params?)` | 列表，返回 `{documents, meta?}` |
| `getDocument(databaseId, collectionId, documentId)` | 获取单个文档 |
| `updateDocument(databaseId, collectionId, documentId, {data?, permissions?, increment?})` | 更新（支持字段增量 `increment`） |
| `deleteDocument(databaseId, collectionId, documentId)` | 删除文档 |
| `countDocuments(databaseId, collectionId, params?)` | 计数，返回 `number` |

### 4.3 Groups（`tw.groups`）

| 方法 | 说明 |
|------|------|
| `createGroup(name)` / `listGroups(params?)` / `getGroup(id)` / `deleteGroup(id)` | 用户组 CRUD |
| `createMembership(groupId, {email, name?, roles?})` | 创建成员/邀请 |
| `listMemberships(groupId)` | 成员列表 |
| `updateMembershipStatus(groupId, membershipId, "accepted" \| "rejected")` | 接受/拒绝邀请 |
| `deleteMembership(groupId, membershipId)` | 删除成员 |

---

## 5. Server API（管理面，API Key）

Server 请求统一 `auth: "apiKey"`，携带 `X-Api-Key: <key>` 与
`X-Torchwood-Project: <projectId>` 头；未配置 apiKey 时抛出
`TorchwoodError`（status 0，提示 "API key is required"）。

### 5.1 Health / Projects / Users / APIKeys / OAuthProviders

```typescript
await tw.server.health.check();                       // { status: "ok" }

tw.server.projects.list(params?)                      // Project[]
tw.server.projects.get(id)
tw.server.projects.create({ id, name })

tw.server.users.create({ email, password, name?, status?, labels?, prefs? })
tw.server.users.list(params?)                         // User[]
tw.server.users.get(id)
tw.server.users.update(id, { name?, email?, status?, email_verified?, labels?, prefs? })
tw.server.users.updatePassword(id, password)
tw.server.users.delete(id)
tw.server.users.listSessions(id)                      // Session[]
tw.server.users.deleteSession(id, sessionId)
tw.server.users.createToken(id)                       // 模拟登录，返回 TokenBundle

tw.server.apiKeys.list()                              // APIKey[]
tw.server.apiKeys.get(id)
tw.server.apiKeys.create({ name, scopes? })           // { api_key, secret }（secret 仅返回一次）
tw.server.apiKeys.delete(id)

tw.server.oauthProviders.list()                       // OAuthProvider[]
tw.server.oauthProviders.upsert({ provider, enabled, client_id, client_secret?, scopes? })
tw.server.oauthProviders.delete(provider)
```

### 5.2 Server Groups（`tw.server.groups`）

| 方法 | 说明 |
|------|------|
| `create({name, permissions?})` / `list(params?)` / `get(id)` / `delete(id)` | 用户组 CRUD |
| `createMembership(groupId, {email? \| user_id?, name?, roles?, status?})` | 创建成员（支持直接按 user_id 添加） |
| `listMemberships(groupId, params?)` / `getMembership(groupId, membershipId)` | 成员查询 |
| `updateMembership(groupId, membershipId, roles)` | 更新角色 |
| `updateMembershipStatus(groupId, membershipId, status)` | 更新状态（invited/accepted/rejected） |
| `deleteMembership(groupId, membershipId)` | 删除成员 |

### 5.3 Server Databases（`tw.server.databases`）

| 分组 | 方法 |
|------|------|
| 数据库 | `createDatabase({id, name})`、`listDatabases(params?)`、`getDatabase(id)`、`deleteDatabase(id)` |
| 集合 | `createCollection(databaseId, {id, name, permissions?, document_security?})`、`listCollections`、`getCollection`、`updateCollection(databaseId, collectionId, {name?, permissions?, document_security?, disabled?})`、`deleteCollection` |
| 属性 | `createAttribute(databaseId, collectionId, {key, type, size?, required?, array?, default_value?})`、`deleteAttribute(databaseId, collectionId, key)` |
| 索引 | `createIndex(databaseId, collectionId, {id, type, attributes, orders?})`、`deleteIndex(databaseId, collectionId, indexId)` |
| 文档 | `createDocument` / `listDocuments` / `getDocument` / `updateDocument`（支持 `increment`）/ `deleteDocument` / `countDocuments`（与 Client 版同名同签名，均带 `auth: "apiKey"`） |
| 批量 | `bulkUpdateDocuments(databaseId, collectionId, {document_ids, data?, permissions?})`、`bulkDeleteDocuments(databaseId, collectionId, documentIds)`，返回 `{affected}` |

> **⚠️ Breaking change（REST 自定义动词迁移，R10-P1-3/B3）**：TS SDK Server API 的
> `countDocuments`/`bulkUpdateDocuments`/`bulkDeleteDocuments` 与 `listRuntimes`/
> `listSpecifications` 内部路径已从字面量段切换为自定义动词（`.../documents:count`、
> `.../documents:bulkUpdate`、`.../documents:bulkDelete`、`.../functions:runtimes`、
> `.../functions:specifications`）。方法名与签名不变，但旧版本 SDK 或手写旧路径的
> 请求在新服务端将返回 404，升级服务端时需同步升级 SDK。
> **Client API 同步迁移**：Client `countDocuments` 路径已由
> `.../documents/count` 切换为 `.../documents:count`（方法名与签名不变，旧路径
> 在新服务端返回 404）。

### 5.4 Server Storage（`tw.server.storage`）

| 方法 | 说明 |
|------|------|
| `createBucket({name, permissions?})` / `listBuckets(params?)` / `getBucket(id)` / `deleteBucket(id)` | Bucket 管理 |
| `listFiles(bucketId, params?)` / `getFile(bucketId, fileId)` / `deleteFile(bucketId, fileId)` | 文件管理 |
| `uploadFile(bucketId, file: Blob, filename)` | 上传（FormData multipart，`POST /v1/storage/buckets/{id}/files`） |

```typescript
// Agent 上传文件示例
const file = new Blob([jsonText], { type: "application/json" });
const uploaded = await tw.server.storage.uploadFile("bucket-id", file, "export.json");
```

---

## 6. 鉴权与传输机制

`HttpTransport.request(method, path, {auth?, query?, body?})` 支持三种鉴权模式：

| auth | 行为 |
|------|------|
| `"apiKey"` | 必须配置 `apiKey`；发送 `X-Api-Key` + `X-Torchwood-Project` |
| `"user"`（默认） | 有 access token 时发送 `Authorization: Bearer <token>`；未配置则匿名发送 |
| `"none"` | 不携带任何鉴权头（如 sign-up、health） |

- 传输层基于全局 `fetch`，JSON 序列化/反序列化；query 参数支持数组重复展开
  （`queries[]` 透传 Appwrite 风格查询 DSL，如 `equal("tag","hot")`）。
- 204 / 空响应体返回 `undefined`；非 2xx 一律抛 `TorchwoodError`。

---

## 7. 错误处理与类型

### 7.1 `TorchwoodError`

```typescript
class TorchwoodError extends Error {
  readonly status: number;   // HTTP 状态码；未配置 API Key 等客户端错误为 0
  readonly code?: string;    // 服务端 error.code（可选）
  readonly body?: unknown;   // 完整响应体（可选）
}
```

错误体按 `{ error: { message, code } }` 信封解析，`message` 兜底为 `res.statusText`。
SDK 无内置重试/刷新逻辑——Agent 集成方可自行 catch 后刷新 token 或换 Key 重试。

```typescript
import { TorchwoodError } from "@torchwood/sdk";
try {
  await tw.server.users.list();
} catch (err) {
  if (err instanceof TorchwoodError) {
    console.log(err.status, err.code, err.message);   // 401 PermissionDenied ...
  }
}
```

### 7.2 类型说明（如实说明）

> **⚠️ Breaking change（时间戳 wire 格式）**：自 `c4d0bcb`（proto 时间戳统一）起，
> 所有时间字段（`created_at`/`updated_at`/`expires_at`/`expire_at`/`invited_at`/
> `joined_at` 等）的 HTTP JSON 表示从 **int64 unix 毫秒** 变更为 **RFC3339 字符串**
> （`google.protobuf.Timestamp` 的标准 JSON 映射，如 `"2026-08-13T00:00:00Z"`）。
> 旧客户端若仍按数字解析会失败；新集成一律按 RFC3339 解析（`new Date(ts)` 可直接消费），
> 与 swagger 中 `"type": "string", "format": "date-time"` 一致。

SDK 类型为**手写维护**（`src/types.ts`），并非由 proto 自动生成：`Account`、`Document`、
`User`、`Group`、`Membership`、`Project`、`APIKey`、`Database`、`Collection`、
`Attribute`、`Index`、`Bucket`、`FileItem`、`TokenBundle`、`Session`、`ListMeta`、
`ListParams`、`BulkDocumentsResponse`、`UpdateDocumentInput` 等接口与 HTTP JSON
响应一一对应（snake_case 字段，与 proto JSON 映射一致）。若服务端字段演进，需同步
更新 `types.ts`；Agent 集成建议以 `genproto/**/*.swagger.json` 为 schema 权威来源，
SDK 类型仅作便捷参考。

### 7.3 RPC ↔ SDK 方法名映射表

proto RPC（swagger `operationId` 为 `{Service}_{RPC}`）与 TS SDK 方法并非一一同名，
SDK 采用简短命名。下表为全量映射（权威来源：`sdk/typescript/src/__tests__/contract.test.ts`
的 `RPC_TO_METHOD`，新增 RPC 后契约测试会强制要求补登映射与方法实现）：

**Server API**

| 服务 | RPC → 方法 |
|------|------------|
| UsersService | `CreateUser→create`、`ListUsers→list`、`GetUser→get`、`UpdateUser→update`、`UpdateUserPassword→updatePassword`、`DeleteUser→delete`、`ListUserSessions→listSessions`、`DeleteUserSession→deleteSession`、`CreateUserToken→createToken` |
| GroupsService | `CreateGroup→create`、`ListGroups→list`、`GetGroup→get`、`DeleteGroup→delete`、`GetGroupPrefs→getPrefs`、`UpdateGroupPrefs→updatePrefs`、`CreateMembership→createMembership`、`ListMemberships→listMemberships`、`GetMembership→getMembership`、`UpdateMembership→updateMembership`、`UpdateMembershipStatus→updateMembershipStatus`、`DeleteMembership→deleteMembership` |
| DatabasesService | `CreateDatabase→createDatabase`、`ListDatabases→listDatabases`、`GetDatabase→getDatabase`、`DeleteDatabase→deleteDatabase`、`CreateCollection→createCollection`、`ListCollections→listCollections`、`GetCollection→getCollection`、`DeleteCollection→deleteCollection`、`UpdateCollection→updateCollection`、`CreateAttribute→createAttribute`、`DeleteAttribute→deleteAttribute`、`CreateIndex→createIndex`、`DeleteIndex→deleteIndex`、`CreateDocument→createDocument`、`ListDocuments→listDocuments`、`GetDocument→getDocument`、`UpdateDocument→updateDocument`、`UpsertDocument→upsertDocument`、`DeleteDocument→deleteDocument`、`CountDocuments→countDocuments`、`BulkUpdateDocuments→bulkUpdateDocuments`、`BulkDeleteDocuments→bulkDeleteDocuments` |
| FunctionsService | `ListRuntimes→listRuntimes`、`ListSpecifications→listSpecifications`、`CreateFunction→create`、`ListFunctions→list`、`GetFunction→get`、`UpdateFunction→update`、`DeleteFunction→delete`、`CreateDeployment→createDeployment`、`ListDeployments→listDeployments`、`GetDeployment→getDeployment`、`DeleteDeployment→deleteDeployment`、`SetVariables→setVariables`、`GetVariables→getVariables`、`CreateExecution→createExecution`、`ListExecutions→listExecutions`、`GetExecution→getExecution` |
| StorageService | `CreateBucket→createBucket`、`ListBuckets→listBuckets`、`GetBucket→getBucket`、`DeleteBucket→deleteBucket`、`UpdateBucket→updateBucket`、`CreateFile→uploadFile`、`ListFiles→listFiles`、`GetFile→getFile`、`DeleteFile→deleteFile`、`UpdateFile→updateFile`、`CreateFileToken→createFileToken`、`GetStorageUsage→getStorageUsage` |
| APIKeysService | `CreateAPIKey→create`、`ListAPIKeys→list`、`GetAPIKey→get`、`DeleteAPIKey→delete` |
| OAuthProvidersService | `ListOAuthProviders→list`、`UpsertOAuthProvider→upsert`、`DeleteOAuthProvider→delete` |
| ProjectsService | `CreateProject→create`、`ListProjects→list`、`GetProject→get`、`UpdateProject→update` |
| HealthService | `Check→check`（含 additional_bindings 的 `Check2`）、`GetVersion→getVersion` |

**Client API**（AccountService；`tw.account.*`）

| RPC → 方法 |
|------------|
| `SignUp→signUp`、`SignIn→signIn`、`SignOut→signOut`、`RefreshToken→refresh`、`Me→me`、`UpdateAccount→updateAccount`、`ListSessions→listSessions`、`DeleteSession→deleteSession`、`DeleteSessions→deleteSessions`、`GetPrefs→getPrefs`、`UpdatePrefs→updatePrefs`、`CreateEmailOTP→createEmailOTP`、`CreateEmailOTPSession→createEmailOTPSession`、`CreateOAuth2Session→createOAuth2Session`、`CreateOAuth2TokenSession→createOAuth2TokenSession`、`CreatePhoneOTP→createPhoneOTP`、`CreatePhoneOTPSession→createPhoneOTPSession`、`CreateWeChatMiniProgramSession→createWeChatMiniProgramSession`、`CreateAnonymousSession→createAnonymousSession`、`CreateOAuth2LinkSession→createOAuth2LinkSession`、`CreateOAuth2LinkTokenSession→createOAuth2LinkTokenSession`、`CreateVerification→createVerification`、`UpdateVerification→updateVerification`、`CreateRecovery→createRecovery`、`UpdateRecovery→updateRecovery`、`ListFactors→listFactors`、`CreateTOTPFactor→createTOTPFactor`、`VerifyTOTPFactor→verifyTOTPFactor`、`DeleteFactor→deleteFactor`、`CreateMFASession→createMFASession`、`CreateJWT→createJWT`、`CreateMagicURLSession→createMagicURLSession`、`UpdateMagicURLSession→updateMagicURLSession`、`ListLogs→listLogs` |

Client 的 DatabasesService / GroupsService 使用同名映射（`createDocument`、`listDocuments`、
`getDocument`、`updateDocument`、`upsertDocument`、`deleteDocument`、`countDocuments`；
`createGroup`、`listGroups`、`getGroup`、`deleteGroup`、`createMembership`、`listMemberships`、
`updateMembershipStatus`、`deleteMembership`）。

---

## 8. Demo 应用（`sdk/demo/`）

Vite + React 19 + react-router-dom 7 + Tailwind 的演示站点，**默认端口 5174**：

```bash
task sdk-demo        # 自动先跑 sdk-build，然后 vite dev（http://localhost:5174）
```

启动前确认本地后端已就绪（`task up` + `task migrate` + `task dev-server`），
并在全新数据库上先完成首次部署引导（打开 `/console/` 注册第一个管理员——需先
配置 `TORCHWOOD_SECURITY_SETUP_TOKEN`；注册响应展示的默认 API Key secret 用于
Server API），复制 `sdk/demo/.env.example` 为 `.env` 可覆盖默认值：

```dotenv
VITE_TORCHWOOD_ENDPOINT=http://localhost:9080
VITE_TORCHWOOD_PROJECT_ID=default
```

| 页面 | 演示能力 |
|------|----------|
| `/register` `/login` | Client Account：注册/登录（`signUp` / `signIn`） |
| `/login/oauth/callback` | OAuth2 回调处理（`createOAuth2TokenSession`） |
| `/app/account` | `me` / prefs / sessions / refresh |
| `/app/databases` | Server + Client Databases 全功能验证：一键初始化演示环境（建库/集合/属性/索引/种子文档）、单按钮逐项调用、**全量验证**（30 余步端到端：CRUD、increment、Bulk、清理） |
| `/app/groups` | 创建用户组、刷新 Token、邀请成员 |
| `/app/server` | `health.check` / `projects.list` / `users.list` / `groups.create` / `databases.listDatabases` |
| `/app/settings` | Endpoint、Project ID、API Key 配置（本地持久化） |

Server API 页面需要先在设置页填入首次部署引导展示的默认 API Key；设置与登录态
保存在 localStorage（`Torchwood-demo-settings` / `Torchwood-demo-auth`）。

---

## 9. Go SDK（`sdk/go/`）

> 独立 Go module：`github.com/torchwooddev/torchwood/sdk/go`（require + replace
> 本地开发）。拆分为两个子包：`sdk/go/client`（end-user 认证，自动刷新 token）
> 与 `sdk/go/server`（API Key 认证，含 InvokeJSON 动态分发）。gRPC 直连，
> 默认明文（insecure），生产用 `WithDialOptions` 配置 TLS。

### 9.1 包结构与客户端类型

| 包 | 类型 | 认证 | 服务 |
|----|------|------|------|
| `sdk/go/client` | `client.New(target, opts...)` | `Authorization: Bearer <JWT>`（自动刷新） | `Account`（SignUp/SignIn/RefreshToken/Me/SignOut）/ `Groups` / `Databases`（文档 CRUD） |
| `sdk/go/server` | `server.New(target, opts...)` | `x-api-key`（+ 可选 `x-torchwood-project`） | `Health` / `Users` / `Groups` / `Databases` / `Projects` / `Storage` / `Functions` / `OAuthProviders` + `InvokeJSON` |

server 包 Options：`WithAPIKey` / `WithProjectID` / `WithDatabaseID` / `WithDialOptions`；
client 包 Options：`WithProjectID` / `WithDatabaseID` / `WithTokenStore` /
`WithOnTokensChanged` / `WithInitialTokens` / `WithDialOptions`。
`UseDatabase(id)` 返回绑定指定库的文档服务副本（两包均有）。

### 9.2 Token 管理（client 包）

- `TokenStore` 接口：`Load() (*clientv1.TokenBundle, error)` / `Save` / `Clear`；
  内置 `MemoryTokenStore`（进程内）与 `FileTokenStore`（JSON 文件、0600、
  临时文件 + rename 原子写、内置 mutex 可并发）。
- `TokenBundle` 直接复用 proto 类型（access_token / refresh_token / expires_at）。
- 自动刷新（unary interceptor 对全部调用透明生效）：
  1. **主动刷新**：`expires_at` 距现在不足 30 秒且持有 refresh token 时先刷新；
  2. **401 重试**：返回 Unauthenticated 时刷新一次并重试原调用；
  3. **并发去重**：刷新用 mutex 串行 + double-check；
  4. 刷新失败仅当 RPC 返回 Unauthenticated 才清空本地 token，临时错误保留。
- `OnTokensChanged` 回调在登录/刷新/清空（nil）时触发；SignOut 成功或
  Unauthenticated 都清空本地 token；SignIn/SignUp 仅在非 MFA 且 access_token
  非空时落 token。

### 9.3 InvokeJSON（server 包）

```go
respJSON, err := c.InvokeJSON(ctx, "/torchwood.server.v1.UsersService/CreateUser", reqJSON)
```

- 动态分发：从 `protoregistry.GlobalFiles` 按 full method name 查找，限定
  `torchwood.server.v1.*` 且排除 `APIKeysService`；proto 新增方法自动获得支持。
- `reqJSON` 为 protojson（camelCase 键，未知字段报错）；响应为缩进 protojson。
- 错误形态：未知方法 `torchwood: unknown method "<method>"`；非法 JSON 为
  protojson 原始错误；RPC 错误为 gRPC status 错误（CLI 侧用
  `server.IsPermissionDenied` 判断 scope 提示）。

### 9.4 典型用法

```go
import (
    "context"
    "github.com/torchwooddev/torchwood/sdk/go/client"
    "github.com/torchwooddev/torchwood/sdk/go/server"
)

ctx := context.Background()

// Server API：API Key 管理面
srv, err := server.New("127.0.0.1:9060",
    server.WithAPIKey(os.Getenv("TORCHWOOD_API_KEY")),
    server.WithDatabaseID("app"),
)
user, err := srv.Users.CreateUser(ctx, "agent-1@agents.local", "pw", "Agent One", "active", nil, nil)
tok, err := srv.Users.CreateUserToken(ctx, user.Id) // 签发 Agent 登录凭证
doc, err := srv.Databases.UpsertDocument(ctx, "members", "m1",
    map[string]any{"channel_id": "ch1", "user_id": "u1", "last_read_seq": 42},
    []string{"channel_id", "user_id"}, nil) // ON CONFLICT DO UPDATE
count, err := srv.Databases.CountDocuments(ctx, "messages",
    []string{`equal("channel_id","ch1")`})
// 逃生舱：按方法名 + JSON 调用任意 Server API unary 方法
respJSON, err := srv.InvokeJSON(ctx, "/torchwood.server.v1.UsersService/ListUsers", []byte(`{"pageSize":10}`))

// Client API：注册/登录自动保存 token，后续调用自动刷新
store := client.NewFileTokenStore("~/.torchwood/tokens.json")
c, err := client.New("127.0.0.1:9060",
    client.WithProjectID("default"),
    client.WithTokenStore(store),
)
_, err = c.Account.SignIn(ctx, "u@example.com", "Pass@123")
me, err := c.Account.Me(ctx)
```

### 9.5 行为说明

- **错误**：全部调用返回 gRPC `status` 错误，用 `status.Code(err)` 判别
  （`codes.NotFound`、`codes.PermissionDenied` 等），与 TS SDK 的 `TorchwoodError.status` 对应。
- **文档数据**：`map[string]any` 入参内部转 `structpb`；数值字段读回为 `float64`。
- **查询**：List/Count 使用 Appwrite 风格 DSL 字符串（`equal`/`greaterThan`/`orderAsc` 等），
  与 `pkg/query` 一致；List 返回 `([]*Document, nextPageToken, error)`。
- **CLI 依赖**：`cmd/client` 只依赖 sdk/go（server 包 InvokeJSON），源码不直接
  import genproto/grpc（import_guard_test 兜底）；新增 RPC 无需在 CLI 登记。
- **测试**：bufconn 内存 gRPC fake 服务，无外部依赖；已纳入 `task test`（`test-sdk-go`）
  与 `task lint`（`lint-sdk-go`）。
- **发版**：`sdk/go` 为独立 module，发版时单独 tag（如 `sdk/go/v0.1.0`）。
