# 修复任务 G11：Round-2 终审 P2 缺口补齐

## 角色

你是资深 Go 工程师，负责补齐 Torchwood Round-2 终审（`docs/review/round2/audit-report.md`）
发现的 5 个 P2 缺口。**只修本任务列出的问题**，不做清单外的任何改动。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，bash 用正斜杠路径）
- 必读：`docs/review/round2/audit-report.md`「发现的问题」节、`AGENTS.md`
- 当前工作区有 G1–G10 的全部未提交改动，在此基础上增量修改；**不做任何 git 操作**，不回滚他人改动

## 修复清单

1. **dbhook 脱敏正则覆盖多行/批量 INSERT**（P2）
   - 位置：`internal/infra/clients/dbhook.go:84-102`（`sensitiveInsertPattern` 仅匹配单个 VALUES 元组）。
   - 修复：`INSERT INTO t (password_hash) VALUES ('a'), ('b'), ('c')` 形式的**每一个** VALUES 元组
     都要脱敏（正则全局匹配所有元组，或先定位 VALUES 段再逐元组替换）；保持单行场景行为不变。
   - 验证：`dbhook_test.go` 补多行/批量 INSERT 用例（3+ 元组），断言所有元组的敏感值均被
     `[REDACTED]` 替换且非敏感列不受影响。

2. **HTTP 侧同 key 多值凭证头拒绝**（P2）
   - 位置：`internal/api/serverhttp/auth.go:30-52`（多类型并存已拒，但同 key 多值取首值）。
   - 修复：对凭证类 header（`X-Api-Key`、`Authorization`、session cookie）检查
     `len(r.Header.Values(key)) > 1` 时返回 401 `multiple credentials provided`，
     与 gRPC `pkg/grpc/interceptor/jwt.go:207-217` 语义对齐。
   - 验证：补同 key 双值（双 `X-Api-Key`、双 `Authorization`）返回 401 的测试。

3. **zip 总预算超限时完整清理**（P2）
   - 位置：`internal/infra/functions/docker.go:408-421`（仅 `os.Remove` 当前条目）。
   - 修复：总预算（或单条目预算）超限报错时，清理整个解压目标目录（含已解压的前序条目），
     不留半成品；注意不要误删目录外内容（沿用既有 assertZipDir 防护）。
   - 验证：构造前序条目已解压、后续条目触发超限的 zip，断言目标目录被完整清理。

4. **Go SDK 补 `DeleteFactor`**（P2）
   - 位置：`sdk/go/client/account.go`（G9 补了 33 个方法但漏此项；proto 见
     `proto/client/v1/account.proto:693-699` 的 `DeleteFactorRequest.code`，TS SDK 参照
     `sdk/typescript/src/client/account.ts` 的 `deleteFactor(factorId, code?)`）。
   - 修复：补 `DeleteFactor(ctx, factorID, code string)`（code 可选传空串）；对照
     `genproto/client/v1` 的 `AccountServiceClient.DeleteFactor` 签名。
   - 验证：补 bufconn 测试（正常透传 code + 一个错误路径用例）。

5. **`max_per_user` 默认值语义对齐**（P2）
   - 位置：`internal/infra/auth/session_service.go:200-205`、`internal/pkg/config/config.proto:55-56`、
     `configs/config.yaml.template:35`。
   - 修复：统一语义为「未配置/0 = 默认 50；-1 = 不限」：代码读取处 0 值回退 50、-1 表示不限；
     同步修正 `config.proto` 注释与模板注释（注意：改 config.proto 注释需跑
     `task generate-config` 确认产物一致；若仅注释变化导致生成 diff 异常，可只改注释文本不动字段）。
   - 验证：补未配置（0）、显式 -1、显式 10 三种场景的淘汰行为测试。

## 约束

- **不要**改 `apiKeyScopeRules`、functions 写方法的 `RequirePlatformAdmin` 守卫
  （API key 可否调 Functions 写方法是待定产品决策，不在本批次）
- 不引入新依赖；遵循 AGENTS.md 约定
- 不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；`go test -short` + 纯单元测试为主
- 不做 git 提交

## 验证

- `go vet ./internal/infra/clients/... ./internal/api/serverhttp/... ./internal/infra/functions/... ./internal/infra/auth/... ./sdk/go/...`
- `go test -short ./internal/infra/clients/... ./internal/api/serverhttp/... ./internal/infra/functions/... ./internal/infra/auth/...`
- `cd sdk/go && go test ./...`
- 新增测试必须断言真实行为（含上面逐条要求的用例），禁止恒真断言

## 输出

逐项汇报「改动文件:位置 + 改动摘要 + 验证结果」；若有项因故未修，明确说明原因。
