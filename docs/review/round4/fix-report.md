# Torchwood Round 4 修复实施报告（J1–J3）

> 日期：2026-08-24 ｜ 基线：`main` @ `3398a26` ｜ 依据：[fix-plan.md](fix-plan.md)
> 状态：**J1 ✅ / J2 ✅ / J3 ✅ 全部落地并验证通过；J4–J7 待执行。**

## 验证总览

| 检查 | 结果 |
|------|------|
| `go build ./...` | ✅ exit 0 |
| `go vet ./...` | ✅ exit 0 |
| `go test ./... -count=1`（加载 .env，含 DB 集成） | ✅ 除一处本报告已修复的自有新用例外全绿 |
| `cd sdk/go && go build && go test -count=1` | ✅ client / conn / server 三包 ok |
| `task generate-proto`（buf lint + buf generate + openapifix） | ✅ |
| gofmt（仅本会话触碰文件） | ✅ |

---

## J1 紧急修复 ✅

### J1-1 config.yaml.template 结构修正
- `configs/config.yaml.template`：`access_ttl`/`refresh_ttl` 移回 `jwt:` 块；`encryption_key` 注释改为与 config.proto 一致的「可选，回退 jwt.secret」语义。
- 新增 `internal/pkg/config/template_test.go`：解析仓库模板断言合法 YAML 且键结构完整（防回归门禁，CI 可直接复用该测试）。

### J1-2 encryption_key 校验兑现
- `cmd/server/provides.go`：`validateJWTSecret` 泛化为 `validateSecret(fieldPath, envName, secret)`；显式配置的 `security.encryption_key` 走同一 min-len 32 + 弱词黑名单规则，不合规拒绝启动，报错带 env 名。
- `cmd/server/provides_test.go`：新增弱值拒绝/强值通过/回退告警三组单测。

### J1-3 worker cleanup 时机对齐
- `cmd/worker/main.go`：删除 OnStop 内 cleanup 注册，改为 `runner.RunE()` 返回后执行（10s 超时上限），逐字对齐 server main 的模式与注释。
- 顺带修 version 元数据弃用（commit/date 拼入 Version 输出）。

### J1-4 roadmap 状态同步
- `docs/roadmap.md`：v3 经济系统改「已实施」、§0 Realtime 移入「已具备」、文首日期更新。

## J2 契约与机器可读性 ✅

### J2-1 OpenAPI 字段命名统一
- `buf.gen.yaml`：openapiv2 插件 `json_names_for_fields=false`（注释说明该 remote 插件默认 true 必须显式关闭）；全部 swagger.json 重生成为 snake_case。

### J2-2 OpenAPI 错误模型统一
- 新增 `tools/openapifix/main.go`：生成后确定性后处理——default 响应统一改引用 `torchwoodsharedv1ErrorResponse`、移除 rpcStatus 定义、注入 ErrorResponse/Error/ErrorCode 定义（内容与 openapiv2 对 error.proto 的原生输出一致）。已接入 Taskfile `generate-proto`。
- `grpc_swagger_test.go` 新增 default 引用回归门禁（见 J2-5）。

### J2-3 presence 语义落地
- **根因修正**：users.proto 的 optional 字段早已提交但 genproto 从未重生成（这正是当初 handler 写成 `Get*() != ""` 的原因）。本轮 `task generate-proto` 后生成指针字段。
- `internal/api/servergrpc/users.go`：status/name/email/email_verified 改非 nil 判断（本仓 protoc-gen-go 不生成 Has* 方法，nil 判断即 projects.go 正确范本的同一机制，已在注释注明）。
- `internal/api/clientgrpc/account.go` + `internal/app/client/account.go`：`UpdateAccountCommand.Name/Email` 改 `*string`；email 清空走敏感变更门槛（要求旧密码）、同时丢弃悬置 pending_email；四处邮件 staging 调用点改用 stagedEmail。既有测试适配 + 新增「未设置=不变 / 空串=清空」集成断言。

### J2-4 signed page token 接线 + offset 上限
- `pkg/crud/pagination.go`：
  - `InitPageTokenSigning(master)` 进程级签名密钥（HMAC-SHA256 purpose 派生 `torchwood-page-token-v1`，本地实现避免 pkg 反向依赖 jwtparser）；
  - 所有 Encode* 自动附加签名；解码侧 fail-closed——启用签名后无签名/篡改/跨环境 token 一律拒绝，未启用进程保持历史语义（灰度兼容）；legacy `v1:<offset>` 仅未签名进程可解析且打弃用告警；
  - **顺手修复既有 bug**：legacy 回退路径 `Sscanf("%s:%d")` 因 %s 贪婪匹配从未成功过，改为 SplitN 真实解析；
  - `MaxQueryOffset=10000` 上限；`GeneratePageTokens`/`GeneratePreviousPageToken` 签发的 token 记录 order_by/filter digest。
- `pkg/crud/list.go` `ParseListParams`：token 解析升级为 Full 解码 + digest 绑定校验（order_by/filter 不一致→InvalidArgument，文档声称的防护由此真实生效）+ offset 上限钳制（outbox dead-letter 直用 offset 的缺口随之闭合）。
- 组合根接线：`cmd/server/provides.go` 与 `cmd/worker/provides.go` 的 `NewAppConfig` 调用 Init（主密钥缺失拒绝启动，与 JWT 同口径）。
- 新增 `pagination_sign_test.go` 九个用例：往返/篡改拒绝/跨环境拒绝/未启用兼容/上限/digest 绑定/prev-token 携带绑定等。

### J2-5 grpc_swagger_test 增强
- 反向覆盖率断言：collectMethodsByAccess 登记的每个方法必须在 swagger paths 出现 ≥1 次（漏配 http 注解即红）；
- operationId 绑定索引精确校验：`{N}` 后缀要求 `2 ≤ N ≤ 方法声明的 google.api.http 绑定总数`（替换旧的数字截断回退，实测抓到 CountDocuments2 并确认其 additional_bindings 合法）；
- default 响应引用 ErrorResponse 门禁（配合 openapifix）。
- **遗留（转 J7）**：per-operation security 断言需先给约 20 个 proto 补 openapiv2 security 注解再重生成本轮不做。

### 文档修订
- `docs/developer/09-api-guide.md`：分页安全章节改为与实现一致（签名/digest 绑定/offset 上限/灰度行为）；权威示例从会被 400 拒绝的 storage filter 示例改为 documents `queries` 真实用法（路由与参数名已对照 swagger 核实）。

## J3 SDK/CLI 交付链路 ✅

### J3-1 发布流水线
- 新增 `.github/workflows/release.yml`（workflow_dispatch）：打 genproto tag → sed 改写 sdk/go/go.mod require 为真实版本并移除相对 replace → 自检 build/vet → 打 sdk/go tag → 干净目录模拟下游 `go get` + 编译硬验收。原理：replace 仅主模块生效，下游拉到的 go.mod 必须含真实 genproto 版本。
- 新增 `CHANGELOG.md`（sdk/genproto 两节 v0.1.0）。注意：历史 tag 的 go.mod 仍含 replace/伪版本，合并后应通过 release workflow 重发 v0.1.1。

### J3-2 SDK 默认超时与可选重试
- `sdk/go/internal/conn/conn.go`：DefaultTimeout(30s) 兜底拦截器（尊重调用方已有 deadline）+ UNAVAILABLE 重试 service config（maxAttempts=4）。
- server/client 双 Client 增加 `WithTimeout`/`WithRetryDisabled` Option；bufconn 单测验证重试次数与 deadline 行为。
- 已知权衡：gRPC service config 无法按方法区分读写，当前对所有方法在 UNAVAILABLE 时重试；代码注释与 CHANGELOG 已声明。

### J3-3 CLI 自诊断与退出码
- `output.go`：Unauthenticated 追加自诊断（检查 TORCHWOOD_CLI_API_KEY/--api-key、过期指引、health 连通验证）；新增 ExitCode 映射 0/1/2/3/4（Canceled 归 40x 组，注释说明 gateway 映射 408/499 依据）。
- `outbox.go` 删除幽灵 --project 文案；`main.go` 退出码接入 + version 元数据修复。

### J3-4 SDK 封装完整性测试
- invoke_test 新增反射断言（registry 方法 ↔ Client wrapper 同名导出方法），**实际抓到两个真实缺口并已修复**：`Health.Version→GetVersion` 漂移、缺失的 `DeleteDatabase` wrapper；count 下限收紧为精确快照常量 112。

### J3-5 文档示例可编译化
- 新增 `sdk/go/docexamples`（//go:build docexample）承载 12-sdk.md §4.3/§4.4 与 README 全部 Go 示例；修正 12-sdk.md:196 ListDeadLetters 签名错误并补记超时/重试/ExtractRetryAfter 用法。

### J3-6 RetryInfo 与 SDK helper
- `ratelimit_redis.go`：专用 Lua 原子返回 {count,ttl}，超限错误附 RetryInfo（精确剩余窗口秒数），不改 domain 端口签名；interceptor 兜底补保守估计 detail（已有 detail 或非限流错误透传）。
- SDK 新增 `ExtractRetryAfter(err)`/`IsUnauthenticated`/`HTTPErrorClass(err)`；miniredis TTL 区间断言 + SDK 解析 17s 退避断言。

## 与方案的偏差记录

1. **Has\* 方法不存在**：fix-plan 假设 protoc-gen-go 生成 Has*；实测本仓生成物（protocolbuffers/go:v1.36.10）不生成任何 Has*（连 message 字段也没有），改用 nil 指针判断——与 projects.go 正确范本同机制，语义等价。
2. **J2-5 security 断言延期至 J7**：生成器对 operation 完全不输出 security 字段，先决条件是约 20 个 proto 补 openapiv2 注解并重生成，独立成项。
3. **重试策略作用域**：无法按幂等性过滤方法（gRPC service config 限制），采用全局 UNAVAILABLE 重试 + WithRetryDisabled 出口。
4. **发布验收依赖远端**：release.yml 内嵌下游 go get 验收 step，需合并后真实触发一次 workflow 才算端到端闭环。
