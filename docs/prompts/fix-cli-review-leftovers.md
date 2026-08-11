# 实施 Prompt：CLI 验收遗留修复（help 措辞 / presence 语义 / 测试补齐）

> 将本文件整体作为任务说明分派给实施 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 背景：cmd/client（Torchwood CLI）已完成并通过验收，验收报告留下 3 个非阻断修复项 + 1 个注释瑕疵。本任务全部为 cmd/client 内的小改动，预期半天内完成。

---

## 任务清单

### 1. `storage files` help 文本含「分片」字样（验收 C1.3 失败项）

- 位置：`cmd/client/cmd_storage.go:145`，当前为：
  `文件元数据管理（上传/下载/分片会话走 HTTP，CLI 不提供）`
- 要求：help 全文（含 Long/Short/Example）不得出现「分片」「chunk」「multipart」字样（`functions deployments create --code` 相关说明除外）。
- 修复：改为不含上述字样的等价表述，如 `文件元数据管理（上传/下载走独立 HTTP handler，CLI 不提供）`。
- 自查：`grep -rn -E '分片|chunk|multipart' cmd/client/`，确认 help 可见文本中无残留（代码注释中的说明性提及可保留，但 help 字符串不行）。

### 2. `databases collections create --document-security` 缺 presence 语义（验收 C2.4 失败项）

- 位置：`cmd/client/cmd_databases.go:121-144`（`newCollectionsCreateCmd` 或其 build 函数）。
- 现状：`cmd.Flags().BoolVar(&documentSecurity, ...)` 后直接把 `&documentSecurity` 放进 `CreateCollectionRequest`，未传 flag 时也显式发送 `false`。
- 要求：与同文件 UpdateCollection 的写法一致，改用 `changedBoolPtr(cmd, "document-security", documentSecurity)`（`cmd/client/output.go:66-71`），未显式传 flag 时字段为 nil（proto3 optional presence）。
- 同步核对 `collections create` 的 `--disabled`（如存在）是否也有同样问题，一并修复。
- 补/改单测：未传 flag → `DocumentSecurity == nil`；显式 `--document-security=false` → 非 nil 且为 false。
- 注意：服务端当前存在「document_security 恒 true」缺陷（另有专门 prompt 修复），本任务只修 CLI presence 语义，不要动服务端。

### 3. `buildListDocumentsReq` 缺独立单测（验收 C4 警告项）

- 位置：构造器在 `cmd/client/cmd_databases.go:727` 附近（被 `documents list` 与 `documents count` 复用）。
- 要求：在 `cmd/client/cmd_databases_test.go` 新增 `TestBuildListDocumentsReq`，table-driven，至少覆盖：
  - 成功路径：`--queries '["equal(\"title\",\"hi\")"]' --page-size 10` 解析正确（queries 数组、分页字段）；
  - 失败路径：`--queries 'oops'` 非法 JSON 报「解析失败」。

### 4. 注释瑕疵（顺手）

- `cmd/client/cmd_databases.go:11`：注释「全部 21 个方法」改为「全部 22 个方法」（DatabasesService 实际 22 个 RPC）。

## 约束

- 只改 `cmd/client/` 内文件；不改服务端、不改 proto、不引入新依赖。
- 遵守现有代码风格（中文注释、cobra 模式、`cmd_helpers.go` 的 JSON 解析 helper）。
- 改完运行：`gofmt -l cmd/client`（无输出）、`go vet ./cmd/client/...`、`go test ./cmd/client/... -count=1`（全绿，含 `TestRPCRegistryCoverage`）。

## 完成验收

- 上述 4 项逐条完成；`go test ./cmd/client/...` 全部通过；`task build` 产出 `bin/torchwood.exe` 且 `torchwood storage files --help` 无「分片」字样；`torchwood databases collections create --help` 行为不变（presence 语义由单测证明）。
- 完成后输出：变更文件清单、每项修复的证据（diff 摘要 + 测试输出）、遗留问题。
