# 复审报告（Round 2）：03 - Server API 传输层（servergrpc + serverhttp）

> 审查范围：当前工作区 `internal/api/servergrpc/`、`internal/api/serverhttp/` 及交叉引用的用例层代码。
> 验证命令：`go vet ./internal/api/servergrpc/... ./internal/api/serverhttp/...` 通过；`go test ./internal/api/servergrpc/... ./internal/api/serverhttp/... -short` 全部通过（集成测试因无本地基础设施被 `-short` 跳过）。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 证据（文件:行号） | 说明 |
|--------|------|-------------------|------|
| **F2-3** 端用户可上传 Functions 部署代码包 | ✅ 已修复 | `internal/api/serverhttp/functions_handler.go:186-189` | `authorize()` 已增加分支：`CredentialTypeToken/Session` 且 `ActorKind != Admin` 时返回 `PermissionDenied`； |
| | | `internal/api/serverhttp/functions_handler_test.go:179-206`、`210-237` | 新增测试分别断言端用户 Bearer JWT 与 session cookie 上传返回 403； |
| | | `internal/api/serverhttp/functions_handler_test.go:241-300` | 对照测试覆盖 admin 放行、带 `functions.write` scope 的 API Key 放行、无 scope API Key 拒绝。 |
| **F2-4** `jwt.go` extractCredential 多凭证并存时拒绝 | ✅ 已修复 | `pkg/grpc/interceptor/jwt.go:176-178`、`183-185` | 同时出现 `authorization` + `cookie`、`authorization` + `x-api-key` 或 `cookie` + `x-api-key` 时返回 `errors.New("multiple credentials provided")`； |
| | | `pkg/grpc/interceptor/jwt.go:94-98` | 错误被映射为 `codes.Unauthenticated`，对应 `proto/shared/v1/error.proto:15` 的 `ERROR_CODE_INVALID_CREDENTIALS`。 |
| **F2-4** HTTP 鉴权重复逻辑抽取公共 `httpAuth` 辅助 | ❌ 未修复 | `internal/api/serverhttp/file_handler.go:738-764`、`766-786`、`788-805` | `FileHandler` 仍保有独立的 `authorize`/`authenticate`/`projectID`； |
| | | `internal/api/serverhttp/functions_handler.go:173-199`、`202-220`、`222-239` | `FunctionsHandler` 同样重复实现上述三函数，未复用公共辅助。 |
| **F4-4** UpdateUser 改邮箱不查重 | ✅ 已修复 | `internal/app/server/users.go:146-171` | email 分支先按新邮箱 `ListDocuments` 查重，循环中通过 `dup.ID != userID` 排除自身（`users.go:164`），避免改回原邮箱误报； |
| | | `internal/app/shared/docdb_errors.go:28` | `23505` 映射为 `codes.AlreadyExists`； |
| | | `internal/app/server/users.go:191` | 并发路径经 `fmt.Errorf("update user: %w", appshared.MapDocumentDBError(err))` 兜底映射为 `AlreadyExists`。 |
| **F4-5** GetProject 返回 nil,nil | ✅ 已修复 | `internal/api/servergrpc/projects.go:69-73` | `p == nil` 时返回 `codes.NotFound`； |
| | | `internal/api/servergrpc/projects_test.go` | `TestProjectsService_GetProject_Missing` 通过。 |
| **F6-2** Preview 解码无像素级防线 | ⚠️ 部分修复 | `internal/api/serverhttp/file_handler.go:641-662` | 缩放分支已用 `image.DecodeConfig` 读取宽高，超过 `maxPreviewSourceDimension = 8192` 直接拒绝； |
| | | `internal/api/serverhttp/file_handler.go:677-686` | 输出改为 `imaging.Encode(w, dst, ...)` 流式编码； |
| | | `internal/api/serverhttp/file_handler.go:645-653` | **不足**：源文件仍整体读入内存（受 `maxPreviewSourceBytes = 50 MiB` 限制），并非从 reader 直接流式解析；且当前测试未覆盖 `8193×1` 等畸形超大尺寸。 |
| **F6-3** 私有文件下载无 Cache-Control | ✅ 已修复 | `internal/api/serverhttp/file_handler.go:509-513` | `public` 为 false 时设置 `Cache-Control: private, no-store`；公开 bucket 匿名路径设置 `public, max-age=86400`，未误伤公开流量。 |
| **F6-3** 公开 bucket 匿名路径 bucketID 拼 DSL 未转义 | ⚠️ 部分修复 | `internal/api/serverhttp/file_handler.go:548` | 已改用 `query.BuildEqual("$id", bucketID)` 构造 DSL，不再直接字符串拼接； |
| | | `internal/api/serverhttp/file_handler_dsl_test.go:13-28` | 新增单测验证含引号/反斜杠/换行的恶意 bucketID 不会逃逸出 `equal` 参数； |
| | | `internal/api/serverhttp/file_handler.go:545-553` | **不足**：未对 `bucketID` 做 UUID 格式预校验，仍依赖 DSL 解析器转义。 |

---

## 2. 新发现问题

### 🟠 P1（高）

1. **Preview 源文件仍整体载入内存，且缺少超大尺寸单元测试**
   - 位置：`internal/api/serverhttp/file_handler.go:645-653`
   - 问题描述：虽然修复了像素级防线，但实现先把文件整体读入 `[]byte`（上限 50 MiB）再交给 `image.DecodeConfig`。攻击者仍可发送接近 50 MiB 的压缩图片消耗服务端内存；且仓库中无针对 `8193×1` 或 `1×8193` 等畸形尺寸的纯单元测试，维度防线仅由集成测试覆盖，而集成测试在无本地 MinIO/Postgres 时无法运行。
   - 影响：内存占用与 OOM 风险未被完全消除；回归防护不足。
   - 修复建议：使用 `io.TeeReader` 或先读取足够 header 字节再决定是否继续读取全量；补一个纯单元测试，构造合法 PNG header 但 IHDR 中 width/height 为 8193 的字节流，断言返回 400。

### 🟡 P2（中）

2. **HTTP 自定义 handler 未抽取公共 `httpAuth`，重复逻辑未消除**
   - 位置：`internal/api/serverhttp/file_handler.go:738-805`、`internal/api/serverhttp/functions_handler.go:173-239`
   - 问题描述：`FileHandler` 与 `FunctionsHandler` 各自实现了几乎一致的 `authorize`/`authenticate`/`projectID` 三件套，与 fix-plan F2-4 方案不符。重复代码增加了后续鉴权策略变更（如多凭证拒绝、scope 调整）时漏改一处的一致性问题。
   - 影响：可维护性下降；file_handler 后续若再改，functions_handler 可能不同步。
   - 修复建议：在 `internal/api/serverhttp` 下新增 `auth.go`，暴露 `httpAuth(r *http.Request, scopeForMethod string)` 等辅助函数，两个 handler 复用。

3. **HTTP 侧未拒绝多凭证并存请求（与 gRPC 行为不一致）**
   - 位置：`internal/api/serverhttp/file_handler.go:766-786`、`internal/api/serverhttp/functions_handler.go:202-220`
   - 问题描述：`authenticate` 按 `X-Api-Key` → `Authorization` → `TORCHWOOD_session_*` cookie 的固定顺序取第一个有效凭证，不会拒绝同时携带多种凭证的请求。gRPC 拦截器已在 `extractCredential` 中明确拒绝多凭证并存。
   - 影响：凭证选择策略可能被客户端利用产生混淆（例如 session 与 API Key 归属不同主体时，实际使用顺序靠前的凭证）。
   - 修复建议：在公共 `httpAuth` 辅助函数中实现与 gRPC 一致的“多 credential header 并存则拒绝”逻辑。

4. **公开 bucket 匿名路径未校验 `bucketID` UUID 格式**
   - 位置：`internal/api/serverhttp/file_handler.go:545-553`
   - 问题描述：`resolveReadContext` 对公开 bucket 匿名读使用 `query.BuildEqual` 转义 bucketID，但未校验 UUID。虽然 DSL 解析器能防注入，但若 bucketID 带空格、大小写异常或含特殊字符，仍可能进入 documentDB 产生非预期查询或日志污染。
   - 影响：输入校验不彻底；存在通过构造性 bucketID 触发非预期查询的微小风险。
   - 修复建议：在 resolve 前增加 `idgen.ID(bucketID).IsValid()` 或 UUID 正则校验，非法则直接返回 400。

### 🟢 P3（低）

5. **Functions proto 注释与实现不一致（GetVariables 已脱敏但注释称“回显明文”）**
   - 位置：`proto/server/v1/functions.proto:158-160`
   - 问题描述：proto 注释声明环境变量值在 `GetVariables` 响应中回显，但实际 `internal/app/functions/variables.go:49-53` 已用 `secretMask = "******"` 脱敏。
   - 影响：文档/契约注释误导消费者与前端。
   - 修复建议：更新 proto 注释，明确 `value` 字段在 `SetVariables` 请求/响应中可见一次，`GetVariables` 返回掩码值。

6. **Preview 解码失败返回 `codes.Internal` 而非更准确的 415/400**
   - 位置：`internal/api/serverhttp/file_handler.go:655-657`、`665-667`
   - 问题描述：`image.DecodeConfig` / `imaging.Decode` 失败时统一返回 `codes.Internal`（HTTP 500），而非 `InvalidArgument`（400）或 `Unimplemented`（415 Unsupported Media Type）。
   - 影响：客户端收到 500 可能误判为服务端故障。
   - 修复建议：区分“文件根本不是图片”与“图片数据损坏”，分别返回 400/415。

---

## 3. 模块总体结论

- **修复完成度**：约 **75%**。F2-3、F4-4、F4-5、F6-3（Cache-Control）已完整落地；F2-4 的 gRPC 多凭证拒绝已落地但 HTTP 公共辅助未抽取；F6-2、F6-3（bucketID DSL）已解决核心风险但存在边界不足。
- **剩余风险 Top 3**：
  1. Preview 仍整体读源文件入内存且缺少畸形尺寸单测，OOM 防线不完整；
  2. HTTP 侧多凭证并存不拒绝，与 gRPC 纵深防御不一致；
  3. `httpAuth` 未公共化，file_handler 与 functions_handler 鉴权逻辑重复，后续变更易遗漏。
- **是否建议关闭本模块审查**：**不建议关闭**。建议在完成 HTTP 公共 `httpAuth` 抽取、补全 preview 畸形尺寸单元测试、统一多凭证拒绝后，再进行一次聚焦审查。当前代码已无 P0 越权/注入类漏洞，可进入收尾阶段。
