# 复审任务（Round 2）：03 - Server API 传输层（servergrpc + serverhttp）

## 背景
- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色
你是资深 Go 后端代码审查专家，对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Server API 传输层」进行只读审查。同时你是修复验证者，需对照 `fix-plan.md` 逐条核实修复是否落地、是否完整、是否引入回归。

## 第一步：建立基线
- 读 `docs/review/prompts/03-server-api.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 F2-3、F2-4（httpAuth 抽取若已落地）、F4-4、F4-5、F6-2、F6-3（file_handler.go 相关子项）章节：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- internal/api/servergrpc/ internal/api/serverhttp/` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文
- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 架构分层：`internal/api/servergrpc` 是 Server API 的 gRPC handler（管理面），经 grpc-gateway 暴露为 `/v1/server/*` REST；`internal/api/serverhttp` 是自定义 HTTP handler（Storage 上传下载、Functions 代码包、OAuth 回调）。
- 关键约定：每个 gRPC 方法必须带 proto authz 注解（`proto/shared/v1/authz.proto` 的 `method_auth`）；列表查询复用 `pkg/crud`；鉴权由 `pkg/grpc/interceptor` 完成，handler 只接收 Principal；典型调用链：gRPC handler → app use-case → domain repo port → infra adapter。

## 复审重点 A：修复验证（逐条核实）

### F2-3：端用户可上传 Functions 部署代码包（P0）
- 文件锚点：`internal/api/serverhttp/functions_handler.go:173-193`（`authorize`）
- 修复项：`authorize()` 增加分支——`CredentialTypeToken/Session` 且 `ActorKind != Admin` 一律拒绝（PermissionDenied）；补端用户 JWT 上传 deployment 必须 403 的测试。
- 核实：1. 分支是否已落地；2. 是否覆盖了所有入口（create deployment / replace code / 其他 HTTP 上传路径），而非只改 `authorize` 一处；3. 合法 admin/API Key 调用是否仍被放行；4. 新增测试是否真实断言 403 且凭证类型覆盖完整。

### F2-4：纵深防御补强（P2）
1. `jwt.go` extractCredential 多凭证并存时拒绝
   - 文件锚点：`pkg/grpc/interceptor/jwt.go:150-165`
   - 核实：同时出现多个 credential header 时是否直接拒绝；拒绝状态码与错误体是否符合 `proto/shared/v1/error.proto` 约定。
2. HTTP 鉴权重复逻辑抽取公共 `httpAuth` 辅助（若已落地）
   - 文件锚点：`internal/api/serverhttp/file_handler.go:700-767`、`internal/api/serverhttp/functions_handler.go:173-232`
   - 核实：公共辅助是否**同时**被 `file_handler.go` 与 `functions_handler.go` 使用；提取后是否丢失了原上下文特有校验（如文件下载的 file token、上传的 bucket 权限）；是否存在多凭证并存时未拒绝的绕过。

### F4-4：UpdateUser 改邮箱不查重（P1）
- 文件锚点：`internal/app/server/users.go:141-153`
- 修复项：email 分支先按新邮箱查重（排除自身 userID），重复返回 AlreadyExists；并发兜底走 `MapDocumentDBError` 的 23505 → AlreadyExists。
- 核实：1. 查重逻辑是否落地；2. 是否排除自身 userID，避免用户改回原邮箱被误报；3. gRPC handler 是否把 `AlreadyExists` 映射为 `codes.AlreadyExists`；4. 并发路径的 23505 兜底是否真实生效。

### F4-5：GetProject 返回 nil,nil（P1）
- 文件锚点：`internal/api/servergrpc/projects.go:67-71`
- 修复项：`p == nil` 时返回 `codes.NotFound`（对齐 `users.go:87-89`）。
- 核实：1. nil 检查是否已加；2. 返回错误后 grpc-gateway 是否映射为 404；3. 同一文件内其他 `Get*` 方法是否存在同类 nil,nil 问题。

### F6-2：Preview 解码无像素级防线（P1）
- 文件锚点：`internal/api/serverhttp/file_handler.go:571,624-635`
- 修复项：解码前用 `image.DecodeConfig` 读取宽高，超过上限（如 8192 边长）直接拒绝；输出改流式编码。
- 核实：1. 是否在所有 preview 分支都加了 `DecodeConfig`；2. 像素上限是否配置化或硬编码合理；3. 流式编码是否正确设置 Content-Type/Content-Length/错误响应；4. 超大图是否不再全量解码到内存；5. 测试是否覆盖 8193×1 等畸形尺寸。

### F6-3（file_handler.go 相关补强项，P2）
1. 私有文件下载无 Cache-Control
   - 文件锚点：`internal/api/serverhttp/file_handler.go:497-507`
   - 修复项：响应头加 `private, no-store`。
   - 核实：是否仅对需要鉴权的私有文件加该头；公开 bucket 的下载是否未被误加。
2. 公开 bucket 匿名路径 bucketID 拼 DSL 未转义
   - 文件锚点：`internal/api/serverhttp/file_handler.go:538-541`
   - 修复项：bucketID 参数化或预校验 UUID。
   - 核实：是否不再把用户传入 bucketID 直接拼入 DSL 字符串；是否校验了 UUID 格式；是否存在其他拼接点（如 fileID）。

## 复审重点 B：回归与新问题排查
- 修复触动的文件及其上下游：重点检查 `file_handler.go` 同时被 F2-4（公共 auth 抽取）和 F6 改动，行为变化是否破坏上传、下载、preview 的功能完整性。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（注入、越权、路径穿越、信息泄露、输入校验）、正确性（错误处理、并发、事务边界）、一致性（与 AGENTS.md 约定、proto authz 注解、domain 端口签名）、测试质量。
- **本模块修复后特有风险点**：
  1. F2-3/F2-4 改动鉴权后，需重查合法 admin/API Key 调用 Functions 上传是否被误伤，以及 `file_handler.go` 改用公共 `httpAuth` 后是否丢失了 file token、public bucket 等特有分支的权限语义。
  2. F4-5 在 `projects.go` 加 nil 检查的同时，需排查 `servergrpc` 其他 Get 方法是否仍有 `nil,nil` 返回，避免局部修复造成不一致。
  3. F6-2 引入 `image.DecodeConfig` 和流式编码后，需确认错误路径仍返回合理 HTTP 状态码（如 415/400），而不是 200 后中断连接；同时确认 Content-Length 未知时不会导致 gateway 超时。
  4. F6-3 的公开 bucket 匿名路径若改为参数化 DSL，需验证 `bucketID` 校验与 `documentdb` 查询之间的权限边界：攻击者能否通过大小写/带空格 UUID 绕过校验。

## 输出要求
简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束
- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./internal/api/servergrpc/... ./internal/api/serverhttp/...` 与无外部依赖的纯单元测试辅助验证。
