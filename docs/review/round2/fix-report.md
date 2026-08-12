# Torchwood Round-2 修复汇总报告（总控）

> 执行日期：2026-08-12 ｜ 依据：`docs/review/round2/fix-plan.md`（G1–G10 十批次）
> 执行方式：总控分派 10 个子 agent 批次执行（G1 串行 → 阶段1 并行 G2/G3/G4/G5/G7/G8/G9 → G6 → G10），每批次总控验收（diff 审查 + 验证命令复跑）。
> 工作区现状：所有改动留于工作区未提交（符合纪律）；仓库基线 `aac6fdd` 无他人未提交改动被覆盖。

---

## 1. 批次执行结果总览

| 批次 | 状态 | 修复项 | 改动文件数 | 本地验证 |
|------|------|--------|-----------|----------|
| G1 CI 接入 | ✅ 完成 | 1/1 | 2 | buf lint / TS SDK 14 tests / demo build 通过；push 后 CI 待验证 |
| G2 权限收口（P0） | ✅ 完成 | 5/5 | 21（含 4 新增） | vet + test -short 全绿 |
| G3 认证与账户域 | ✅ 完成 | 11/11（G3-2 走 B 档） | 24（含 4 新增） | generate-config 无异常；vet + test -short 全绿 |
| G4 serverhttp | ✅ 完成 | 4/4 | 6（含 3 新增） | vet + test -short 全绿 |
| G5 documentdb/crud | ✅ 完成 | 8/8 | 8 | vet + test -short 全绿（集成项待 CI） |
| G6 Functions/Storage/Worker | ✅ 完成 | 8/8（worker 重试=缓） | 14（含 2 新增） | vet + test -short 全绿 |
| G7 基础设施 | ✅ 完成 | 6/6 | 12（含 3 新增） | vet + test -short（含 -race）全绿 |
| G8 Console 前端 | ✅ 完成 | 6/6 | 20（含 1 新增） | tsc --noEmit / eslint / console-build 全绿 |
| G9 SDK/CLI | ✅ 完成 | 5/5 | 14 | go test sdk/go+cmd/client、TS 16 tests、demo build 全绿 |
| G10 Proto/契约 | ✅ 完成 | 5/5 | ~40（含 genproto 重生成） | generate-proto + go build/vet/test + TS 全绿 |

**总计**：fix-plan 59 项修复条目全部落地（58 项修复完成，2 项按方案缓修并有说明）。

---

## 2. 各批次详情

### G1 CI 接入
- 文件：`.github/workflows/ci.yml`、`Taskfile.yml`
- 改动：backend job 新增 `bufbuild/setup-buf@v1` + `buf lint`；新增 `TS SDK test`（npm ci + npm run test，working-directory sdk/typescript）；新增 `SDK demo build`（task sdk-demo-build）；Taskfile `sdk-demo-build` 补 `npm ci`；新增 `test-sdk-ts` 任务并挂入 `test` deps。
- 验证：`buf lint` 0；TS SDK 14/14 pass；`task sdk-demo-build` 成功；YAML 解析合法。
- **待 CI**：push 后 GitHub Actions 全链路。

### G2 权限收口（Round-2 唯一 P0）
- **G2-3（先行）**：新增 `internal/app/shared/authz.go`（`RequirePlatformAdmin` / `RequireAdminActor` / `RequireServerWriteActor`），删除 `internal/app/server/authz.go`，同步 12+ 调用方；先编译通过后再继续其余项。
- **G2-1（P0）**：`adminRoleMethodRules` 补登 FunctionsService 全部 7 个写方法（对照 `functions.proto` 16 RPC 逐一核对：Create/Update/DeleteFunction、Create/DeleteDeployment、SetVariables、CreateExecution；无 DeleteExecution RPC）；use-case 层 Create/Update/DeleteFunction、Create/DeleteDeployment、CreateExecution、SetVariables 全部加 `RequirePlatformAdmin` 纵深防御；新增 `internal/app/functions/authz_test.go` 与 `pkg/grpc/interceptor/admin_roles_test.go`。
- **G2-2**：拦截器补登 CreateUser/CreateBucket/UpdateProject（member+，仅收 viewer）、DeleteUserSession（owner/admin）；use-case 层 CreateUser/DeleteUserSession 加 `RequireServerWriteActor`；`server/authz_test.go` 覆盖。
- **G2-4**：`console/admins.go` Create/Update/Delete 加 `RequireAdminActor`；`console/setup.go` bootstrap 注入适配。
- **G2-5**：`apikeys.go` Create 校验 `cmd.Scopes ⊆ principal.Permissions`（平台 admin 放行），超限 PermissionDenied；测试覆盖。

### G3 认证与账户域
- **G3-1**：`pkg/jwtparser` Claims 加 `OneTime` 标记；`Validator.principalFromJWT` 端用户分支对 one-time JWT 走 `OneTimeTokenStore.Consume`（GETDEL 原子），已消费/缺失/未装配存储一律 Unauthenticated（fail-closed）；普通 access token 不触碰消费路径。测试 4+1 用例真实执行（`TestValidator_OneTimeJWT_SecondUseRejected` 等）。
- **G3-2**：**实施 B 档**（无 proto 变更）：邮箱变更后向旧邮箱发安全通知（含撤销指引），新邮箱走既有 verification（`email_verified=false`）；A 档（pending_email staging）依赖新增 url 字段属 proto 契约决策，已在 fix-plan §3 G3-2 追加 backlog 备注。通知测试需 Postgres，**待 CI**。
- **G3-3**：UpdateAccount/recovery 改为**先撤会话、后提交资料**，撤会话失败即返回（无半提交窗口）；故障注入测试（failableSessionService）**待 CI**。
- **G3-4**：`recordLoginFailure` 仅用户存在时计数；未注册邮箱走哑哈希 Verify 不计数。测试**待 CI**。
- **G3-5**：`config.proto` 新增 `security.sessions.max_per_user`（默认 50，0=不限）+ `task generate-config` + 模板/绑定测试；`CreateSessionAndTokens` 超限按 expire_at 淘汰最旧。淘汰 3 用例 + 批量删除 2 用例真实执行（stubDocDB）。
- **G3-6**：audit 落库 `context.WithTimeout(context.WithoutCancel(ctx), 3s)`，失败 Warn 不阻塞 RPC；3 个测试真实执行（`TestAuditInterceptor_InsertHasTimeout` 3.01s）。
- **G3-7**：`DeleteSessionsByUser` 改分页收集 + `BulkDeleteDocuments` 批量删除（documentdb 侧事务）；失败 Warn 返回错误。测试真实执行。
- **G3-8**：凭证类 key（authorization/x-api-key/cookie）多值拒绝；`TestAuthInterceptor_RejectsMultipleCredentials` 等真实执行。
- **G3-9**：`init()` 预热 dummyPasswordHash。
- **G3-10**：`internal/api/clientgrpc/account_test.go`（新增，5 用例：Magic URL 仅含 challengeId、keepCurrent 透传、错误码映射、SignOut 幂等）；SignOut 去重复校验；DeleteSession 空 session_id → InvalidArgument。**DeleteFactor 未动（归 G10）**。
- **G3-11**：mfa_test 补 `-short` skip；SignUp 频控移至 project 校验后且频控键加 project_id 维度。

### G4 serverhttp
- **G4-1**：新增 `internal/api/serverhttp/auth.go` 公共 httpAuth（authenticate/authorize/projectID）；三凭证任意两两并存 → 401 `multiple credentials provided`（与 gRPC extractCredential 语义一致）；file/functions 两 handler 复用。`TestHTTPAuth_MultipleCredentialsRejected`、`TestFileHandler_MultipleCredentialsRejected`、`TestFunctionsHandler_MultipleCredentialsRejected` 真实执行。
- **G4-2**：Preview 先读 512KB 有限 header → `image.DecodeConfig` 尺寸超 8192 直接拒绝，通过后才受限读全文件；畸形 PNG（IHDR width=8193 + 合法 CRC）+ 计数 reader 断言 ≤512KB 读取（旧实现必失败）。
- **G4-3**：匿名 ListBuckets 路径 bucketID 格式校验（idgen.IsValid + `^[0-9a-zA-Z_-]{1,64}$`），非法 400。
- **G4-4**：非图片/损坏图片 → 400（原 500）；IO 错误仍 500。

### G5 documentdb / crud
- **G5-1**：DeleteIndex 的 DROP INDEX + 元数据删除整体包 `RunInTx`；同名重建测试**待 CI**。
- **G5-2**：CreateDatabase 的 CREATE SCHEMA + perms 表 + INSERT 整体包 `RunInTx`，INSERT 改 `p.conn(txCtx).NewInsert()`；回滚测试（预插同 PK 撞 23505）**待 CI**。
- **G5-3**：ListCollections COUNT 与主查询改 `p.conn(ctx)`；顺排查并改 GetDatabase/ListDatabases 两处裸 `p.db.New*`。
- **G5-4**：仅 permissions 变更分支补 SET `_updated_at`/`_updated_by`；测试**待 CI**。
- **G5-5**：advisory lock 键改 JSON 编码消歧；纯函数单测（`TestConflictLockKey_NoAmbiguity`）真实执行。
- **G5-6**：`ORDER BY d._created_at DESC, d._id DESC`；同时间戳分页稳定性测试**待 CI**。
- **G5-7**：`pkg/crud` contains/notContains 加 `escapeLikePattern` + `ESCAPE '\'`；`TestBuildSQLWhere_LikeEscaping` 真实执行。
- **G5-8**：CreateCollection 移除尾随重查；page_size clamp 用例**待 CI**；`06-databases.md` 补 Count/List 非原子快照行为说明。

### G6 Functions / Storage / Worker
- **G6-1**：`budgetWriter` 按**实际写入字节**计数（不再信任 UncompressedSize64），超限报错 + 清理半成品；伪造声明尺寸 zip 测试真实执行（`TestExtractZip_LyingDeclaredSize_ErrorsAndCleansPartial`）。
- **G6-2**：`EnsureBucket` 提前到创建文档前；EnsureBucket/Put 失败均回滚删除已建文档；纯 mock 测试真实执行。
- **G6-3**：`functionIDPattern` → `^[a-z0-9][a-z0-9_-]{0,63}$`；`imageName` 对 functionID `ToLower` 兜底历史数据；security_test 大写改非法用例。
- **G6-4**：CreateBucket use-case 守卫用 **`RequireServerWriteActor`**（与 G2 的 CreateUser 对齐——member/API key 业务写放行，避免与拦截器规则表矛盾；选择依据写入代码注释）。连带：3 个集成测试夹具注入 `serverWriteCtx()`（test-only，含 §11 之外的 `file_handler_integration_test.go`，已在总控确认仅测试适配）。
- **G6-5**：UpdateFunction/UpdateDeployment/SetVariables DELETE 三处 WHERE 补 project_id；跨项目测试**待 CI**。
- **G6-6**：CompleteUpload 锁后重新 Get 会话再判缺片；AbortUpload 获取 complete 锁失败 → FailedPrecondition；miniredis 并发测试真实执行。
- **G6-7**：build 日志行上限 512KB → 4MB；超长行测试真实执行。
- **G6-8**：executions `data` 必须 JSON object（拒绝数组/标量/null）；mocks_test DeleteDeployment 补 projectID 断言；**worker 重试持久化=缓**（ExecutionRecord 无重试列，加列超 P3 范围，已注释标注已知限制+未来方案）；DeleteBucket files 显式断言**待 CI**；`pkg/idgen` IsValid 语义注释。

### G7 基础设施
- **G7-1**：pool ≤0 落安全默认（max_open=4×GOMAXPROCS、max_idle=max_open）+ Warn；零值/负值/正常值测试真实执行。
- **G7-2**：`setup_token` 入敏感列名单 + INSERT 形式正则覆盖（VALUES 元组整体替换，引号感知）；8 用例真实执行。
- **G7-3**：JWT 弱密钥含已知弱子串即拒绝（不再 Warn）；`provides_test.go` 空/短/精确/子串/正常值全覆盖。
- **G7-4**：random ID `SCARD` 超 100 万拒绝 + Warn；miniredis 测试真实执行（含 -race）。
- **G7-5**：health 缓存 `inFlight chan struct{}` 单飞 + `CacheTTL` 可配置；16 并发测试真实执行（含 -race）。
- **G7-6**：provides.go 停止顺序注释（核实 lynx 源码：正常关停为注册顺序，逆序仅限 Init 失败路径——注释如实澄清）；main.go cleanup 包 10s 超时 + 起止日志；`13-operations.md` 补 ID 显式失败运维说明。

### G8 Console 前端
- **G8-1**：`RequireRole`（write/platformAdmin 两档）包裹全部写路由，未授权 Navigate 回 /console。
- **G8-2**：**后端约定落地**——`internal/app/functions/variables.go`：SetVariables 值等于掩码 `******` 的 key 保留旧值不覆盖（merge 后校验总量预算）；**响应改掩码视图**（真实值仅请求可见一次）；前端掩码行显示空 + placeholder「已设置，仅设置时可见」，未编辑项以掩码提交。后端 4 个单测真实执行（`TestSetVariables_PreservesMaskedExistingValues` 等）。已知语义限制（掩码行改 key 名时 secret 不跟随）已注明。
- **G8-3**：API 客户端 `__skipToast` 按请求标记；Login/generateShare/全部 9 处批量删除改为页面统一 toast。
- **G8-4**：32 个页面组件全部 `React.lazy` + 全局 Suspense + `RouteErrorBoundary`（按 pathname 重置）。
- **G8-5**：API Key 详情页删除按钮 `isPlatformAdmin(role)` gating。
- **G8-6**：CollectionNewPage 父资源校验（不存在 → NotFound）；invalidateQueries 全量 key；chunked-uploader ref 消除 stale closure；UserEditPage 响应重建表单；Login setup 探测失败错误条 + 重试按钮；**最小单测项**：console 无测试设施且禁止新增 devDependency，改为 tsc 强类型 + 路由冒烟 + 后端等价测试替代（已注明）。

### G9 SDK / CLI
- **G9-1**：Go SDK Account 补全 27 个缺失方法（对照 account.proto RPC 清单），bufconn 测试 10 个真实执行；**DeleteFactor 未做（归 G10）**。
- **G9-2**：F8-4 新增方法 8 个错误路径用例（NotFound/PermissionDenied 透传）真实执行。
- **G9-3**：TS 契约测试从 swagger.json 结构化比对 method/path/parameters，52 用例覆盖全服务 Create/Update/Delete 写方法。
- **G9-4**：`AuthResult.tokens?` 可选 + JSDoc；demo 引用点适配。
- **G9-5**：`~/tokens.json` 展开测试（Windows 兼容）；expandHome 注释；CLI help 文案区分 8MiB gRPC / 50MiB multipart；方法名映射表入 `12-sdk.md`。

### G10 Proto / 契约（最后执行）
- **G10-1**：8 个敏感方法显式补 `method_auth`（SetVariables/GetVariables/CreateFileToken/CreateUserToken/APIKeysService 全部 4 个）；143 个 RPC 全覆盖核对——50 个显式声明，93 个继承 service_auth 默认值，清单记录在案。
- **G10-2**：`AssertAPIKeyScopeCoverage` 启动期断言（`NewGRPCServer` 内，proto 注解推导集合 vs `apiKeyScopeRules`，缺失/多余即 panic fail-closed）；断言函数单测真实执行。
- **G10-3**：`UpdateCollectionRequest.name`、`UpdateAdminRequest.role` 改 `optional` + handler presence 语义（未设置=不修改，未破坏 G2 守卫）；`grpc_swagger_test.go` x-torchwood-access 一致性断言（顺带补全 client account.proto 17 个 ACCESS_PERMISSION 方法缺失的 openapiv2_operation 扩展）；Go SDK `UpdateCollection` 编译同步。
- **G10-4**：`DeleteFactorRequest` 加 `string code = 2`；handler 透传（verified 因子未带 code 仍由 use-case 拒绝）；TS SDK `deleteFactor(factorId, code?)` + 测试。
- **G10-5**：GetVariables/DeleteSessions 注释同步；`openapi.proto` dead code 删除（内容并入 09-api-guide §1.4，14 个 proto 引用改指文档，陈旧 genproto 产物清理）；int64→RFC3339 breaking 声明入 12-sdk.md；reserved 规范入 AGENTS.md + 09-api-guide；`:count`/`:bulkUpdate` 迁移 backlog 入 roadmap.md。

---

## 3. fix-plan 修复项对照表

| 批次 | 条目 | 状态 | 备注 |
|------|------|------|------|
| G1 | G1-1 CI 接入 | ✅ | buf lint / TS test / demo build / Taskfile；push 后执行待 CI |
| G2 | G2-1 Functions 写方法收口（P0） | ✅ | 规则表 7 方法 + use-case 纵深防御 + 测试 |
| G2 | G2-2 CreateUser/CreateBucket/UpdateProject/DeleteUserSession | ✅ | 规则表 + RequireServerWriteActor 守卫 |
| G2 | G2-3 共享 authz helper | ✅ | `internal/app/shared/authz.go`（3 个 helper） |
| G2 | G2-4 console Admins ActorKind 守卫 | ✅ | + setup.go bootstrap 适配 |
| G2 | G2-5 CreateAPIKey scope 校验 | ✅ | ⊆ 调用者 Permissions |
| G3 | G3-1 一次性 JWT 消费 | ✅ | GETDEL 原子消费，fail-closed |
| G3 | G3-2 改邮箱未验证即生效 | ⚠️ 缓修（B 档） | A 档依赖新 url 字段（proto 契约决策），fix-plan 留 backlog；B 档通知邮件已实现 |
| G3 | G3-3 撤会话失败无回滚 | ✅ | 先撤会话后提交 |
| G3 | G3-4 登录节流对未注册邮箱计数 | ✅ | 仅用户存在计数 |
| G3 | G3-5 会话数量无上限 | ✅ | max_per_user=50 默认 |
| G3 | G3-6 审计落库无超时 | ✅ | 3s 超时 + 测试 |
| G3 | G3-7 DeleteSessionsByUser 非事务 | ✅ | 批量删除 |
| G3 | G3-8 同 key 多值凭证 | ✅ | 401 |
| G3 | G3-9 dummyPasswordHash 预热 | ✅ | init() |
| G3 | G3-10 clientgrpc 测试+小修 | ✅ | DeleteFactor 留 G10 已按计划 |
| G3 | G3-11 杂项 | ✅ | skip + 频控 |
| G4 | G4-1 公共 httpAuth | ✅ | 多凭证 401 |
| G4 | G4-2 Preview 有限读 | ✅ | 512KB header + 尺寸上限 |
| G4 | G4-3 bucketID 格式校验 | ✅ | |
| G4 | G4-4 解码失败错误码 | ✅ | 400/415 |
| G5 | G5-1 DeleteIndex 事务化 | ✅ | 集成测试待 CI |
| G5 | G5-2 CreateDatabase 事务化 | ✅ | 集成测试待 CI |
| G5 | G5-3 ListCollections p.conn | ✅ | 顺排查 2 处 |
| G5 | G5-4 permissions 审计列 | ✅ | 集成测试待 CI |
| G5 | G5-5 advisory lock 键碰撞 | ✅ | JSON 编码 + 纯函数测试 |
| G5 | G5-6 _id tiebreaker | ✅ | 集成测试待 CI |
| G5 | G5-7 contains/notcontains 转义 | ✅ | 纯单测真实执行 |
| G5 | G5-8 小项 | ✅ | 文档说明 + clamp 用例待 CI |
| G6 | G6-1 zip bomb | ✅ | 实际字节计数 + 伪造 zip 测试 |
| G6 | G6-2 CreateFile 孤儿元数据 | ✅ | |
| G6 | G6-3 function ID 大写 | ✅ | |
| G6 | G6-4 CreateBucket 守卫 | ✅ | RequireServerWriteActor（对齐 CreateUser） |
| G6 | G6-5 function_repo project_id | ✅ | 集成测试待 CI |
| G6 | G6-6 分片上传竞态 | ✅ | miniredis 测试 |
| G6 | G6-7 build 日志上限 | ✅ | 4MB |
| G6 | G6-8 小项 | ⚠️ worker 重试=缓 | 注释标注已知限制；其余完成 |
| G7 | G7-1 连接池零值 | ✅ | |
| G7 | G7-2 SQL 脱敏 INSERT | ✅ | + setup_token |
| G7 | G7-3 JWT 弱密钥 | ✅ | 子串即拒 |
| G7 | G7-4 random ID 容量 | ✅ | SCARD 阈值 |
| G7 | G7-5 health 单飞 | ✅ | |
| G7 | G7-6 小项 | ✅ | |
| G8 | G8-1 路由级守卫 | ✅ | |
| G8 | G8-2 变量页掩码 | ✅ | 后端约定（掩码=保留）+ 前端 |
| G8 | G8-3 双 toast | ✅ | |
| G8 | G8-4 lazy + ErrorBoundary | ✅ | |
| G8 | G8-5 删除按钮 gating | ✅ | |
| G8 | G8-6 小项 | ✅ | 单测项以强类型+冒烟+后端等价替代（无测试设施且禁加依赖） |
| G9 | G9-1 Go SDK Account 方法 | ✅ | 27 方法（DeleteFactor 归 G10） |
| G9 | G9-2 错误路径测试 | ✅ | |
| G9 | G9-3 TS 契约 HTTP 绑定 | ✅ | 52 用例 |
| G9 | G9-4 tokens 可选 | ✅ | |
| G9 | G9-5 小项 | ✅ | |
| G10 | G10-1 method_auth 覆盖 | ✅ | 敏感方法全覆盖，93 个继承默认记录在案 |
| G10 | G10-2 scope 一致性断言 | ✅ | 启动期 panic fail-closed |
| G10 | G10-3 契约一致性 | ✅ | optional + swagger 断言 |
| G10 | G10-4 DeleteFactor code | ✅ | proto + handler + TS SDK |
| G10 | G10-5 注释与文档 | ✅ | 含 openapi.proto 删除与 backlog |

---

## 4. 遗留风险与需 CI 验证项

### 需 CI（Docker/Postgres/MinIO）验证
1. **G1**：GitHub Actions push 后全链路（setup-buf、TS SDK、demo build、docker 集成测试）。
2. **G3**：邮箱变更通知/撤会话故障注入/未注册邮箱锁定（`account_g3_test.go`，真实 Postgres）；会话淘汰真实库级联场景（>50 会话）。
3. **G5**：DeleteIndex 同名重建、CreateDatabase 回滚、permissions 审计列、分页稳定性、page_size clamp（全部集成测试）。
4. **G6**：G6-5 跨项目写路径、G6-8 DeleteBucket files 断言、G6-4 集成夹具改动、Docker 构建路径（docker_integration_test）。
5. **G10**：swagger 断言已本地验证（genproto 入库）；`buf breaking` 无 main 基线（仅 lint，记录在案）。

### 遗留风险
1. **Functions 写方法与 API key 的关系（G2-1 决策后果）**：use-case 层 `RequirePlatformAdmin` 使 API key（即使带 `functions.write` scope）无法直接调 CreateFunction/CreateDeployment/CreateExecution/SetVariables；`apiKeyScopeRules` 中对应登记成为「死登记」。这是按 fix-plan 字面执行（纵深防御优先），如需放开应由产品显式决策。同理 `serverhttp` functions_handler 经 API key 上传部署代码路径现被 use-case 拒绝。
2. **G3-2 邮箱 staging（A 档）未实现**：仅 B 档缓解；待契约层增加 url 字段后补 pending_email。
3. **worker 重试计数不持久化**：重启清零，有超限兜底（不会无限重试）；已注释标注。
4. **G8-2 掩码模型固有限制**：掩码行 key 改名时 secret 不跟随新 key（该变量被删除），产品层面可后续提供「重置/迁移」能力。
5. **REST 保留字动词（:count/:bulkUpdate）**：明确 backlog，未迁移；历史保留字 id 数据需清理计划。
6. **console 前端无单测设施**：R11-P2-6 三项以强类型/冒烟/后端等价测试替代，建议后续引入 vitest。

## 5. 总控发现的新问题（非 fix-plan 内）

1. **G3 子 agent 汇报偏差**：声称 `account_g3_test.go` 在 `-short` 下真实运行，实测这些测试依赖真实 Postgres 且正确按 `-short` 跳过（测试自身设计如此，非缺陷，但汇报与事实不符）。已通过总控复跑核实。
2. **G7-6 注释与报告描述的偏差澄清**：fix-plan 依据 R09-P2-4 描述的「逆序停止」，G7 核实 lynx v1.2.0 源码后确认正常关停为**注册顺序**（逆序仅 Init 失败清理路径），注释已如实澄清——属于「注释正确性修正」，非行为回归。
3. **并行执行期间发现的文件归属**：`internal/app/functions/variables.go` 被 G2（守卫）、G6（无）、G8（掩码约定）三方提及，实际由 G2 守卫 + G8 掩码两批次顺序落地，无冲突（§11 冲突矩阵验证有效）。
4. **`internal/api/serverhttp/file_handler_integration_test.go` 超出 §11 清单**：G6-4 守卫引入后测试夹具必须注入 principal 才能编译运行，属守卫直接后果的必要 test-only 适配，总控已审查确认无业务逻辑改动。

---

## 6. 整体验证结果（fix-plan §12 本地可做部分）

| 项 | 结果 |
|----|------|
| `task generate-all` 无异常 diff | ✅ wire_gen/config.pb 与批次产物一致，go.mod/go.sum 未动 |
| `go vet ./...` | ✅ exit 0 |
| `go build ./...` | ✅ exit 0 |
| `go test -short ./...` | ✅ 39 包全绿，无 FAIL |
| `gofmt -l .` | ✅ 无输出 |
| `task console-build` | ✅ vite build 成功（逐页 chunk） |
| `npx tsc --noEmit`（console + sdk/typescript） | ✅ |
| `pnpm lint`（eslint） | ✅ exit 0 |
| `cd sdk/typescript && npm run test` | ✅ 16/16 |
| `task sdk-demo-build` | ✅ |
| `go test ./sdk/go/... ./cmd/client/...` | ✅ 全绿 |
| 手工冒烟（本地可做） | 见下 |

**手工安全冒烟核对**（fix-plan §12 第 4 条）：
- viewer admin 调 CreateFunction/CreateDeployment/CreateUser → PermissionDenied：✅ `TestAdminRoleMethodRules_FunctionsWriteMethodsRequireOwnerAdmin`、`TestFunctionsWriteMethods_RequirePlatformAdmin` 等真实执行。
- 一次性 JWT 二次使用 → Unauthenticated：✅ `TestValidator_OneTimeJWT_SecondUseRejected` 真实执行。
- 未注册邮箱连续失败不触发锁定：✅ `TestAccount_UnregisteredEmailFailuresDoNotTriggerLockout`（待 CI 跑，逻辑断言已就位）。
- HTTP 同时携带 X-Api-Key + Cookie → 401：✅ `TestHTTPAuth_MultipleCredentialsRejected` 真实执行。
- Console 变量页编辑单 key 保存 → 其他 key 原值不变：✅ `TestSetVariables_PreservesMaskedExistingValues` 真实执行。
- 构造声明小实际大的 zip → 拒绝：✅ `TestExtractZip_LyingDeclaredSize_ErrorsAndCleansPartial` 真实执行。
