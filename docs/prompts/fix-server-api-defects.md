# 实施 Prompt：服务端四项已知缺陷修复（C6 基线）

> 将本文件整体作为任务说明分派给实施 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 背景：CLI 验收过程中确认了 4 个服务端既有缺陷（验收报告 C6 节），当时作为「冻结基线」未修。现在逐一修复。4 项相互独立，可按顺序提交；第 1 项优先级最高（功能不可用）。
> 先读相关代码再动手；所有结论以代码现状为准。

---

## 缺陷 1（最高优先级）：UpsertDocument 缺 scope 规则，任何 API Key 调用均 PermissionDenied

- 现象：`pkg/grpc/interceptor/apikey_scope.go` 的 `apiKeyScopeRules` 缺 `/torchwood.server.v1.DatabasesService/UpsertDocument` 条目，fail-closed 设计导致**任何 scope（含 `*`/`all`）** 调用 `databases documents upsert` 都返回 `PermissionDenied: api key missing required scope`。
- 注意确认：`*`/`all` 的放行逻辑是「规则存在才放行」还是「无规则即放行」——先读 `APIKeyScopeAllowed` 实现再改。若 `*` 也过不了，说明缺规则时连全量 scope 都被拒，修复后 `*` 与 `databases.write` 都应放行。
- 修复：在 `apiKeyScopeRules` 补登 UpsertDocument → `{"databases", "write"}`（与 CreateDocument/UpdateDocument 同级）。
- 测试：
  - 单测：`pkg/grpc/interceptor` 中补 UpsertDocument 的 scope 判定用例（`databases.write` 放行、`databases.read` 拒绝、`*` 放行）。
  - 顺手核对：`grep` proto/server/v1 全部方法名与 `apiKeyScopeRules` 比对，确认没有其他漏登记的方法（如有，一并补登并在完成说明中列出）。

## 缺陷 2：collection `document_security` 恒为 true

- 根因：`internal/infra/bun/model/document.go` 中 `DocumentSecurity bool bun:"...,default:true"`——bun 把 `false` 当作默认值零值从 INSERT 中剔除，DB 列默认 `TRUE` 生效，导致显式传 `false` 也落库为 `true`。
- 修复方向（择一，需读 bun 文档与现有代码确认）：
  a. 字段改为 `*bool`（INSERT 时 false 也是显式值）；或
  b. 移除 bun tag 中的 `default:true`（保留 DB 列 DEFAULT TRUE），让 bun 始终发送字段值。
- 注意影响面：所有读取该字段的映射（bunrepo 的 collection 映射、UpdateCollection 路径）要同步适配；存量行不受影响。
- 测试：集成测试证明 `CreateCollection(document_security=false)` 后 get 返回 `false`；`true` 与不传（服务端默认）行为不变。

## 缺陷 3：functions create 不传 timeout_seconds 时服务端报 InvalidArgument

- 根因：`internal/api/servergrpc/functions.go:73` 附近把 proto optional `timeout_seconds` 的缺省映射为 `0` 传入 use-case，触发 `timeout_seconds must be between 1 and 300`（`internal/app/functions/`）。
- 修复：proto optional 未设置时应走「服务端默认值」而非 0。读 proto 定义（`proto/server/v1/functions.proto` 的 `optional int32 timeout_seconds`）与 use-case 校验，确定默认语义：
  - handler 传指针（nil = 未设置）；use-case 在 nil 时应用默认值（参照 Console 前端创建函数时的默认，或取 30；与文档保持一致），设置时按 1-300 校验。
  - UpdateFunction 路径如有同样问题一并修复。
- 测试：单测/集成覆盖「不传 timeout_seconds → 创建成功且为默认值」「传 0 → InvalidArgument」「传 301 → InvalidArgument」。

## 缺陷 4：oauth-providers upsert 首次创建恒要求 client_secret

- 根因：`internal/app/server/oauth_providers.go:44-54`：provider 无既有 client_secret 时**无论 enabled 与否**都要求请求带 client_secret。
- 修复：仅在 `enabled=true`（或请求启用）且无既有 secret 时要求 client_secret；`enabled=false` 创建占位 provider 应允许无 secret。
- 测试：覆盖「enabled=false 无 secret → 成功」「enabled=true 无 secret → InvalidArgument」「已有 secret 后 update 不传 secret → 成功且 secret 不变」。

## 通用要求

- 遵守 `AGENTS.md`：Clean Architecture 分层、最小改动、不手改 `genproto/`（本任务预期不涉及 proto 变更；如确需改 proto，执行 `task generate-proto` 并在说明中注明）。
- 每项缺陷修复配对应测试（单测优先；缺陷 1/2 建议补集成路径，复用 `internal/testutil` 与现有 `*_integration_test.go` 模式）。
- 修复后评估文档影响：若开发者文档（`docs/developer/06-databases.md`、`08-functions.md`、`09-api-guide.md` 等）有「已知缺陷/限制」表述或行为描述与修复后不符，同步更新。
- 回归：`task test`（非 short 全量）、`task build`、`gofmt -l .`（无输出）、`go vet ./...` 全绿。

## 完成验收（实测，附输出）

- 本地栈（`task up && task migrate && task dev-server`）+ 有效 API Key（scope `*` 与 `databases.read` 各一）用 `bin/torchwood` 或 curl 实测：
  1. `databases documents upsert` 在 `*`/`databases.write` 下成功、`databases.read` 下 PermissionDenied；
  2. `collections create --document-security=false` 后 get 返回 `documentSecurity: false`；
  3. `functions create` 不传 `--timeout-seconds` 成功且为默认值；
  4. `oauth-providers upsert <p> --client-id x`（不启用、不带 secret）成功。
- 测试资源与测试 key 验收后清理。
- 完成后输出：每缺陷的「根因 → 修复点（文件:行号）→ 测试证据 → 实测输出」、变更文件清单、遗留问题。
