# Torchwood Round 4 修复实施报告（J4–J7）

> 日期：2026-08-24 ｜ 基线：`main` @ `3398a26` ｜ 依据：[fix-plan.md](fix-plan.md) §4–§7
> 本报告与 [fix-report.md](fix-report.md)（J1–J3）共同构成 Round 4 完整实施记录。

## J4 架构收口

### J4-1 装配去重 ✅ `defbc6f`
- 抽 `internal/pkg/bootkit` 承载 `projectSchemaEnsureHook` 与 AppConfig 公共校验，`cmd/server` 与 `cmd/worker` 共享；`cmd/server/provides_test.go` 随迁至 `bootkit/config_test.go`。
- `NewAppConfig` 差异点显式化（worker 补齐 JWT 校验或注释说明）。

### J4-2 test_helpers 隔离 ✅ `defbc6f`
- `internal/app/client/test_helpers.go`（生产文件）→ `test_helpers_test.go`，`go list -deps ./internal/app/client` 的 `infra/{bun,clients,messaging}` 依赖清零。
- 外部包（`serverhttp/file_handler`、`tests/acceptance`）改为就地装配，不再依赖 `client` 包内 helper。

### J4-3 OAuth/OTP 端口化 ✅ `defbc6f`（本会话直接落地）
- `domain/auth` 新增 `OAuthAuthenticatorFactory` / `WeChatMiniProgramExchanger` / `OTPGenerator` 三端口；
- `infra/auth/ports.go` 提供实现并 Wire 注入；`Account` 追加三字段并移除对 `infra/auth` 的直接依赖（`oauth2.go`/`wechat.go`/`email_otp.go`/`phone_otp.go` 现经端口调用，`mfa.go` 的 `ParseSessionTime` 本地化）；
- `go list -deps ./internal/app/client` 的 `internal/infra/auth` 清零；`task wire-all` 重生成。

### J4-4 组装根迁出 ✅ `38d24c8`
- `git mv internal/infra/server → internal/runtime`，包名 `server→runtime` 统一；新建 `runtime/provides.go`。
- `infra/provides.go` 摘除 `server.New*` 三项，`infra→api` 依赖清零（`go list -deps ./internal/infra | rg internal/api` 为空）；`cmd/server` 接入 `runtime.ProviderSet`，`task wire-all` 重生成；`grpc_swagger_test.go` 相对路径 3→2 层修复。

### J4-5 策略表迁出 + 窄接口化 ✅ `e4b6969`
- `internal/domain/auth/scope.go` 成为 API Key scope 策略单一事实来源；`interceptor/apikey_scope.go` 薄转发兼容。
- `app/server/apikeys.go` 与 `serverhttp/auth.go` 改引 `domain/auth` 版本，`app→interceptor` 依赖清零。
- `serverhttp` 11 处直依赖具体类型改为窄接口（`AuthValidator`/`HealthCheckers`），仿 `realtime/handler.go` 的 `CredentialValidator` 模式；组合根显式 `wire.Bind`。

### J4-6 SQLSTATE 下沉 ✅ `e4b6969`
- `internal/app/shared/docdb_errors.go` 的 `pgErrorFielder` 与 13 项 `SQLSTATE→code` 表完整迁至 `infra/documentdb/errors.go`，adapter 层统一翻译后上抛；`app` 层仅保留哨兵→status 映射。
- `go list -deps ./internal/app/shared | rg pgdriver` 为空；`postgres_test.go` 期望同步更新。

## J5 可靠性与租户生命周期 ✅ `2051fe9`

- **J5-1** 限流熔断：连续失败 5 次/10s 进入 30s 短窗放行，`torchwood_ratelimit_infra_error_total` 与 rejected 分离；登录/MFA 维持 fail-closed。
- **J5-2** DeleteProject 事务后异步 purge `projectID/` 前缀对象（`domain/storage.Purger` + `infra/storage.Purger` 实现，60s 超时重试一次）。
- **J5-3** `internalIDCache` 失效接线：`InvalidateInternalIDCache` 经组合根桥接 `documentdb` 与 `projectschema`；修复 DDL 把陈旧 `internal_id` 烤进 `_tenant` 列默认值的深层缺陷（`resolveInternalIDFresh` 绕过缓存）。
- **J5-4** Functions 默认 per-project network `tw-func-<project.id>`，全局 `torchwood-functions` 改为 opt-in 并加跨租户警告注释。
- **J5-5** 小件：`redis.NewClient` addr 校验带 env 名 + DialTimeout/PoolSize；连接池 duration 解析失败 Warn；`NoopLoginThrottle/NoopMFAChallengeStore` 显式类型；`realtime` published_at 攒批；`semaphore` release 2s 超时；集合频道订阅补 read ACL；删除前孤儿 schema 对账。

## J6 测试与门禁 ✅ `2051fe9`（含真实缺陷修复）

- **J6-1** CI coverage 门禁（实测 51.1%→阈值 48%）+ `Taskfile test` 补 `-race`。
- **J6-2** down 迁移循环测试（`migrations_cycle_test.go`）并**修复真实缺陷**：`000003_document_catalog_composite_keys.down.sql` 从未被执行且必然失败（先挂外键后拆主键等），已重写。
- **J6-3** `.golangci.yml` 启用 `gosec/bodyclose/sqlclosecheck/noctx` 全量 0 issues（生产存量 24 条逐条注释圈定），移除 `--new-from-rev` 棘轮。
- **J6-4** `Eventually` + clock 注入消除 `sleep 1.1s`/`sleep 200ms` flaky。
- **J6-5** `import_guard_test` 递归化 + docker e2e executed 断言 + `CreateTestProjectT`。

## J7 卫生批 ✅ `b38a5e4` + `a3458a0`（构建产物清理）

18 项全部落地：TTL 1h、IP 频控、错误统一文案、X-Project 多值拒绝、CORS Vary、KDF 收敛、proto `reserved` 补齐、`total_count` 注释、CountDocuments 独立 Request / ListLogs & ListAdmins 分页、`console.go` 缓存头、`functions` queryKey 修复、`databases/pages.tsx` 拆四文件、`dist` 占位、`i18n` 统一、SDK `DocumentsPager` 与 storage HTTP helper、`Tool` InputSchema、`rpc_count.go`（187）、AGENTS 取舍说明。`buf lint` + `task generate-proto` 均通过。

## 整体验证

- `go build ./...` ✅ `go vet ./...` ✅
- `go test ./... -count=1`（加载 `.env`，PG 5432 可达）全绿
- `go list -deps ./internal/app/client` 不含 `internal/infra` ✅ `go list -deps ./internal/infra` 不含 `internal/api` ✅ `go list -deps ./internal/app/shared` 不含 `pgdriver` ✅
- `a3458a0` 清理 `console/dist` 构建产物，仅保留占位 `index.html`（`git add -f` 强制跟踪）

## 遗留与后续建议

- `release.yml` 的下游 `go get` 端到端验收需合并后真实触发一次 workflow。
- `CountDocuments` 等新增分页字段为 additive 变更，旧 SDK 兼容；`secretbox` 旧 KDF 解密保留一版回退。
- 控制台 `dist` 构建仍需 `task console-build`，占位仅保证 `go build` 不因 `embed` 失败。
