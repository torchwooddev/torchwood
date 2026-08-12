# 复审任务（Round 2）：10 - Proto 定义与代码生成（proto / buf / genproto 一致性）

## 背景

- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色

你是资深 Protobuf/API 设计审查专家。对 Torchwood 项目的「Proto 定义与代码生成」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。同时你是修复验证者，需对照 fix-plan 逐条核实。

## 第一步：建立基线

- 读 `docs/review/prompts/10-proto-codegen.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 F11 全部与 F8-2 章节：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- proto/ genproto/ buf.* sdk/typescript/src/` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 生成管线：`buf.yaml`、`buf.gen.yaml` 驱动 `buf generate` → `genproto/`（gRPC 桩 + grpc-gateway handler + OpenAPI spec）；**不要手工编辑 `*.pb.go`**
- 关键约定：每个 gRPC 方法必须带 authz 注解（`proto/shared/v1/authz.proto` 的 `method_auth`，缺失会导致 `collectMethodsByAccess` 报错）；REST 映射经 `google.api.http` 注解
- 目录：`proto/client/v1/`（终端用户 API）、`proto/server/v1/`（Agent/自动化 API）、`proto/console/v1/`（Admin Console API）、`proto/shared/v1/`（common/authz/error）

## 复审重点 A：修复验证（逐条核实）

### F11 Proto/OpenAPI 契约修复

1. **F11-1 OpenAPI 产物无认证元数据**（`buf.gen.yaml`、OpenAPI JSON 输出）
   - `buf.gen.yaml` openapiv2 插件是否输出稳定 JSON；
   - proto 是否引入 `openapiv2_swagger` options 声明 securityDefinitions（apiKey `X-API-Key` / Bearer / cookie）；
   - `method_auth` 是否透传到 operation 级（自定义 extension 或文档映射）。

2. **F11-2 TS SDK 与 proto 脱节**（`sdk/typescript/src/server/functions.ts`、account 方法、`sdk/typescript` 契约测试）
   - `functions.ts` 是否补齐 16 个 RPC；
   - account 相关文件是否补齐缺失 16 个方法；
   - 是否建立 CI 契约测试（proto RPC 集合 vs SDK 方法集合比对）并通过。

3. **F11-3 REST 保留字路径段遮蔽资源 id**（`proto/server/v1/databases.proto:93-102`、`proto/server/v1/functions.proto:20-23`）
   - `count`、`bulkUpdate` 等保留字是否改用 `:count`、`:bulkUpdate` 自定义方法风格，或 Create 时校验保留字 id；
   - 重新生成后 gateway 路由是否无冲突。

4. **F11-4-1 101/143 方法补方法级 method_auth**（敏感方法：SetVariables/GetVariables/CreateFileToken/CreateUserToken/APIKeys 等）
   - 统计 RPC 总数 vs 带 `method_auth` 数量，确认覆盖率；
   - 敏感方法 access level 与 permission 是否合理。

5. **F11-4-2 API key scope 映射从 Go 硬编码改为由注解推导**（或启动期一致性断言）
   - 代码中是否移除硬编码映射表并以 proto 注解为源；
   - 若无推导，启动期是否对 method_auth 与硬编码表做一致性断言。

6. **F11-4-3 error.proto 映射补齐**（`proto/shared/v1/error.proto`）
   - `Aborted`→`CONCURRENT_MODIFICATION`、`ResourceExhausted`→`QUOTA_EXCEEDED`、`DeadlineExceeded`→`TIMEOUT` 是否落地。

7. **F11-4-4 时间戳统一 Timestamp；更新类请求补 optional**（清空语义）
   - 涉及时间字段是否由 int64/string 统一为 `google.protobuf.Timestamp`；
   - `Update*` 请求中可清空字段是否标 `optional`。

8. **F11-4-5 buf lint/breaking 接入 CI**（`.github/workflows/ci.yml`、`buf.yaml`）
   - CI 是否运行 `buf lint`；
   - `buf.yaml` 是否启用 breaking 检查；
   - 删除字段是否一律 `reserved`。

9. **F11-4-6 敏感字段注释补「仅一次返回」**（secret/token/client_secret 等）
   - 相关字段 proto 注释是否明确标注不回显。

### F8-2 DeleteSessions body 修复

10. **F8-2 TS deleteSessions keepCurrent 无法传递**（`proto/client/v1/account.proto:56-57`、`sdk/typescript/src/client/account.ts:92-96`）
    - `DeleteSessions` 的 `google.api.http` 是否已加 `body: "*"`（或改为 query 绑定）；
    - 重新生成后 genproto/gateway 是否允许 body 传递 `keepCurrent`；
    - TS SDK 调用处是否同步更新并可通过类型检查。

## 复审重点 B：回归与新问题排查

- 修复触动的文件及其上下游：`genproto/` 生成物、grpc-gateway 路由、`sdk/typescript` 类型、拦截器 `collectMethodsByAccess`、OpenAPI JSON；确认行为变化未破坏既有 handler 与 SDK 调用。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：authz 注解完整性、REST 映射正确性、字段编号稳定性、版本与 reserved、生成配置一致性、实现与定义同步、字段语义安全。
- 本模块修复后特有风险点：
  - **F11 重生成 genproto 后 gateway 路由变化**：`count`/`bulkUpdate` 等路径改动可能导致旧客户端 URL 404，需核对兼容性与 SDK 调用点；
  - **OpenAPI securityDefinitions 与拦截器实际鉴权语义不一致**：若注解声明了 Bearer/API Key 但拦截器未同步，会导致文档与运行时脱节；
  - **method_auth 批量补齐可能误标 access level**：public/private 选择错误会直接把内部接口暴露或把公开接口锁死；
  - **F8-2 给 DELETE 加 body 后缓存/代理行为变化**：部分网关/缓存默认忽略 DELETE body，需确认 grpc-gateway 与前端实际生效。

## 输出要求

简体中文复审报告，三节结构：

1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束

- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `buf lint`、`go vet ./proto/... ./genproto/...`（若适用）与无外部依赖的纯单元测试辅助验证；
- 如需验证生成，先确认 git 状态干净，避免覆盖未提交的生成文件。
