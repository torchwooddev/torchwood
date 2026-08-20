# 验收 Prompt：Torchwood CLI 资源命令（databases/groups/storage/functions/oauth-providers）严格验收

> 将本文件整体作为验收任务分派给验收 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 任务定义：`docs/prompts/cli-resources.md`（**先通读再动手**，以代码现状为准）
> 验收对象：该任务声称已完成的实现（即当前工作区状态）；服务端代码与 proto 属冻结基线，不在验收范围内
> 实测环境：本地栈 `task up && task migrate && task dev-server` + 有效 API Key

---

## 任务目标

对「cmd/client 补齐 Server API 资源具名子命令：databases / groups / storage / functions / oauth-providers」实现做**严格验收**：
逐项核对代码与任务定义「覆盖范围/参数约定」的一致性，并实测全部验收项。

- **只读验收**：不得修改任何代码/文档文件（临时验证数据库除外）；发现偏差只报告，不修复。
- **证据要求**：每项结论必须附带可核查证据 —— 代码位置（`文件:行号`）、命令输出摘要、
  或可复现的请求与响应。禁止仅凭「读起来没问题」下结论。
- **严格程度**：任务定义要求但实现缺失/偏差即判「失败」；实现超出任务定义但行为等价的可判「通过」并注明。
- **方法覆盖以 `proto/server/v1` 实际方法为准**（任务定义表格中的「21 个」有误，DatabasesService 实际 **22** 个 RPC）。

## 验收前准备

```bash
task up            # Postgres / Redis / MinIO（先确认三个 torchwood-* 容器 healthy）
task migrate       # schema 就位（"no change" 也算就位）
task build         # 产出 bin/torchwood[.exe]（Windows 下必须是 torchwood.exe）
task dev-server    # 前台常驻正常（gRPC 127.0.0.1:9060 / HTTP :9080 / Metrics :9040）
```

**API Key（二选一，验收后删除）**：
- 方案 A：Console 引导产出的默认 API Key secret（scope=`all`）；
- 方案 B：插入已知 secret 的测试 key，哈希为 `sha256(secret)` 的 hex
  （`internal/infra/auth/validator.go`），project_id 取 `projects` 表现有行（如 `default`）：
  ```bash
  # secret=cli-verify-full（scope=*）与 secret=cli-verify-ro（scope=users.read）
  docker exec torchwood-postgres psql -U torchwood -d torchwood -c \
    "INSERT INTO api_keys (id, project_id, name, secret_hash, scopes, enabled) VALUES
     ('cli-verify-full','default','verify','<sha256(cli-verify-full)>', ARRAY['*'], true),
     ('cli-verify-ro','default','verify-ro','<sha256(cli-verify-ro)>', ARRAY['users.read'], true);"
  ```

## 一、代码层面核对

### C1. 命令树与覆盖（`cmd/client/main.go`、`cmd_databases.go`、`cmd_groups.go`、`cmd_storage.go`、`cmd_functions.go`、`cmd_oauth.go`）

| 服务 | 期望 CLI 命令（以 proto 实际方法为准） |
|----|----|
| DatabasesService（22 方法） | `databases` create/list/get/delete；`databases collections` create/list/get/update/delete；`databases attributes` create/delete；`databases indexes` create/delete；`databases documents` create/list/get/update/upsert/delete/count/bulk-update/bulk-delete |
| GroupsService（12 方法） | `groups` create/list/get/delete；`groups prefs` get/update；`groups memberships` create/list/get/update/update-status/delete |
| StorageService（12 方法中做 10） | `storage buckets` create/list/get/update/delete；`storage files` list/get/update/delete；`storage usage`。**不做** `files create`（bytes 上传）与 `files token`（CreateFileToken），代码与 help 中均不得出现 |
| FunctionsService（16 方法） | `functions` runtimes/specifications/create/list/get/update/delete；`functions deployments` **create**（gRPC 纯消息 bytes code，允许）/list/get/delete；`functions variables` set/get；`functions executions` create/list/get |
| OAuthProvidersService（3 方法） | `oauth-providers` list/upsert/delete（proto 无 get 方法，不得出现 `get`） |

- root 命令树挂载 5 个资源命令；每资源一个 `cmd_*.go` 文件；子命令分组（collections/attributes/indexes/documents、prefs/memberships、buckets/files、deployments/variables/executions）与上表一致；层级最深 3 级（如 `databases documents bulk-update`），help 中有说明不判失败。
- 全仓不得出现 `api-keys` 命令、不得出现 `projects create/update` 命令（沿用 Prompt B 边界）；不得出现文件上传/下载/分片上传会话相关命令，**help 文本也不得出现分片字样**（`functions deployments create` 的 `--code` 除外——它是 gRPC 纯消息）。
- `deployments create --code <zip>`：读取本地文件为 bytes 构造 `CreateDeploymentRequest`；文件不存在/为空 → CLI 侧报错；>50MiB → CLI 侧拒绝并提示走 multipart 路径（服务端上限 `internal/app/functions/deployments.go:18`）。

### C2. flag 与参数约定

- 标量参数用具名 flag；列表类命令统一 `--page-size`/`--page-token`（以各 proto 实际字段为准：`ListCollections/ListDocuments/ListMemberships/ListFiles` 有，`ListDeployments/ListExecutions` 无分页字段**不得提供**）。
- 复杂结构一律 JSON 字符串 flag：`--queries`（Appwrite 风格查询字符串数组）、`--permissions`/`--roles`/`--scopes`/`--attributes`/`--orders`/`--document-ids`/`--conflict-columns`（字符串数组）、`--metadata`/`--vars`（string→string 对象）、`--increment`（string→int64 对象）。非法 JSON → CLI 侧报错（提示期望格式）、退出码 1。
- `--data` 语义必须区分（验收重点）：
  - `databases documents create/update/upsert/bulk-update` 与 `groups prefs update`：`--data` 为**字段本体**（document data / prefs 的 Struct 对象），如 `--data '{"title":"hi"}'`；
  - 其余命令（users 等 Prompt B 命令）沿用请求级 `protojson` 合并（`mergeData`），不得被本次改动破坏。
- proto3 optional 字段用 presence 语义（未显式传 flag 不得设置字段）：`--document-security`/`--disabled`/`--public`/`--enabled`/`--async`（bool，`changedBoolPtr`）、`--timeout-seconds`（int32，`changedInt32Ptr`）、`--spec`/`--deployment-id`（string，`changedStringPtr`）；字符串 optional 字段沿用「非空即设置」也可接受，但需与任务定义一致。
- 必填校验与服务端一致（对照 `internal/app/server/*.go` 的 InvalidArgument 校验）：databases 的 id/name/key/type/attributes/data/conflict-columns/document-ids、groups 的 name/user-id+email 二选一/roles/status、storage 的 name/至少一个更新字段、functions 的 id/name/runtime/input、oauth 的 provider/client-id。
- 输出、错误处理、退出码沿用 `cmd/client/output.go`：成功 stdout 缩进 JSON（protojson camelCase）；错误 `code + message` 到 stderr、退出码 1；`PermissionDenied` 附带 scope 提示。

### C3. 注册表（`cmd/client/registry.go`）

- 本次**不应改动**注册表（Prompt B 已全量覆盖 80 条）；`TestRPCRegistryCoverage` 必须保持通过（遍历描述符 + 请求类型一致性 + 无多余条目）。
- 各资源测试文件含注册表类型一致性测试（具名命令构造的请求类型与注册表条目一致）。

### C4. 测试存在性（`cmd/client/cmd_*_test.go`）

- 每个资源的「flag → request message」构造函数 table-driven 单测：**至少一条成功路径 + 一条参数校验失败路径**（缺必填、非法 JSON、presence 未设置等）；
- 覆盖所有构造器：databases 的 buildCreateDatabaseReq / buildCreateCollectionReq / buildUpdateCollectionReq / buildCreateAttributeReq / buildCreateIndexReq / buildCreateDocumentReq / buildListDocumentsReq / buildUpdateDocumentReq / buildUpsertDocumentReq / buildBulkUpdate+DeleteDocumentsReq；groups 的 5 个；storage 的 3 个；functions 的 5 个（含 `--code` 文件读取）；oauth 的 1 个；
- `go test ./cmd/client/...` 全部通过。

### C5. 文档

- `docs/developer/02-quickstart.md` §7：含完整命令树与新增资源示例；
- `docs/developer/09-api-guide.md` §12：含命令树、示例，且「具名命令覆盖范围」表述已更新（不再写「只覆盖部分资源」）；
- `AGENTS.md` 无需改动（已含 cmd/client 与注册表维护说明）。

### C6. 已知服务端缺陷（冻结基线，只核对 CLI 透传正确，**不得判为 CLI 失败**）

以下为服务端既有缺陷（本任务不修改服务端），验收时用有效 `*` scope key 复现并确认 CLI 错误提示清晰、退出码 1：

1. `UpsertDocument` 未登记 scope 规则（`pkg/grpc/interceptor/apikey_scope.go` 的 `apiKeyScopeRules` 缺该条目）→ **任何 scope（含 `*`）** 调用 `databases documents upsert` 都返回 `PermissionDenied: api key missing required scope`（fail-closed 设计）。
2. `document_security` 恒为 true：bun 模型 `DocumentSecurity bool bun:"...,default:true"`（`internal/infra/bun/model/document.go`）把 `false` 当默认值剔除出 INSERT，DB 列默认 TRUE 生效；CLI 传 `--document-security=false` 服务端仍返回 true。
3. `functions create` 不传 `--timeout-seconds` 时服务端 handler 映射为 0 → `InvalidArgument: timeout_seconds must be between 1 and 300`（`internal/api/servergrpc/functions.go:73`）；CLI 须显式传 `--timeout-seconds` 才能成功。
4. `oauth-providers upsert` 首次创建（provider 无既有 client_secret）时服务端**恒要求** client_secret（`internal/app/server/oauth_providers.go:44-54`）；CLI 侧仅在 `--enabled=true` 时要求，属校验宽松差异，透传服务端错误即可。

## 二、行为验证（实测）

前置：dev server 运行中；`bin/torchwood[.exe]` 已构建；以下 `torchwood` 指该二进制；`FULL=cli-verify-full`、`RO=cli-verify-ro`（方案 B）或等价 key。**每项记录退出码与 stdout/stderr 摘要。**

1. **命令树**：`torchwood --help` 含 databases/groups/storage/functions/oauth-providers/rpc 等全部命令；`torchwood databases --help` 含 8 个子命令；`torchwood databases documents --help` 含 9 个子命令；storage/groups/functions/oauth-providers 各层 help 与 C1 表一致，**help 中不得出现 files create / token / 分片 / multipart 上传命令**。
2. **databases 全链路**（全用 `FULL`）：
   - `databases create --id clivdb --name <任意>` → 成功；`databases list` / `get clivdb` → 成功；
   - `collections create clivdb --id col1 --name <任意> --permissions '["read(\"users\")"]'` → 成功且 permissions 回显；`collections list clivdb` / `get clivdb col1` → 成功；
   - `attributes create clivdb col1 --key title --type string --size 128 --required` → 成功；
   - `indexes create clivdb col1 --id ix1 --type key --attributes '["title"]'` → 成功；
   - `documents create clivdb col1 --document-id d1 --data '{"title":"hi"}'` → 成功；`documents get clivdb col1 d1` → 成功；
   - `documents list clivdb col1 --queries '["equal(\"title\",\"hi\")"]' --page-size 10` → 成功且 `totalCount=1`；`documents count clivdb col1 --queries '["equal(\"title\",\"hi\")"]'` → `count=1`；
   - `documents update clivdb col1 d1 --data '{"title":"hi2"}'` → 成功；`documents bulk-update clivdb col1 --document-ids '["d1"]' --data '{"title":"bulk"}'` → 成功；`documents bulk-delete clivdb col1 --document-ids '["d1"]'` → 成功；`documents delete clivdb col1 d1` → 成功；
   - 清理 `databases delete clivdb`。
   - **对照项**：`--queries` 引用集合不存在的字段（如 `"equal(\"bogus\",1)"`）→ 服务端 InvalidArgument 属正常（非 CLI 缺陷，注明即可）；`documents upsert`（C6.1）与 `--document-security=false`（C6.2）按 C6 结论核对。
3. **groups 全链路**：`groups create --name <任意>` → 记录 group id；`groups list` / `get <id>` → 成功；`groups prefs update <id> --data '{"theme":"dark"}'` → 成功且 `prefs.theme=dark`；`groups prefs get <id>` → 回显一致；`memberships create <id> --email <任意> --name <任意> --roles '["admin"]'` → 成功；`memberships list <id>` → 成功；`memberships update <id> <mid> --roles '["member"]'` → 成功；`memberships update-status <id> <mid> --status accepted` → 成功（合法取值：roles ∈ owner/admin/member，status ∈ pending/accepted/rejected）；`memberships delete <id> <mid>` → 成功；清理 `groups delete <id>`。
4. **storage 全链路**：`storage buckets create --name clivbkt --public` → 成功且 `public=true`；`storage buckets list` / `get <id>` → 成功；`storage buckets update <id> --name clivbkt2 --public=false` → 成功且 `public=false`（验证 optional bool presence）；`storage usage` → 成功（buckets/files/totalSize）；`storage files list <id>` → 成功；清理 `storage buckets delete <id>`。
5. **functions 全链路**：`functions runtimes` / `functions specifications` → 成功（记录 runtime id 与 spec id）；`functions create --id clivfn --name <任意> --runtime <记录值> --timeout-seconds 30 --enabled` → 成功（**必须显式传 --timeout-seconds，否则按 C6.3 失败，不判 CLI 失败**）；`functions get clivfn` / `functions list` → 成功；`functions update clivfn --name <新名> --timeout-seconds 60` → 成功；`functions variables set clivfn --vars '{"FOO":"bar"}'` → 成功；`functions variables get clivfn` → 回显一致；制作真实 zip（如 `index.js` 内容 `module.exports = async function () { return {}; }`，Node 运行时为 CommonJS 入口），`functions deployments create clivfn --code <zip>` → 成功且 `status=ready`（构建需数秒，必要时加大 `--timeout`）；`functions deployments list clivfn` / `get clivfn <dep-id>` → 成功；`functions executions create clivfn --input '{"a":1}' --async` → 成功（status=queued）；`functions executions list clivfn` / `get clivfn <ex-id>` → 成功；`functions deployments delete clivfn <dep-id>` 与 `functions delete clivfn` → 成功。
   - `deployments create --code` 指向不存在文件 → CLI 报「读取 --code 失败」、退出码 1（服务端未触达）。
6. **oauth-providers**：`oauth-providers upsert google --client-id <任意> --client-secret <任意> --enabled --scopes '["email"]'` → 成功（必须带 secret，否则按 C6.4 服务端拒绝，不判 CLI 失败）；`oauth-providers list` → 回显一致；`oauth-providers delete google` → 成功。
7. **错误路径（不触达服务端）**：缺必填 flag（`databases create` 缺 `--id`、`databases collections create <db>` 缺 `--name`、`storage files update <bid> <fid>` 无任何更新字段、`groups memberships update <tid> <mid>` 缺 `--roles`）→ 退出码 1、stderr 含必填提示、**stdout 为空**；非法 JSON（`--queries 'oops'`、`--permissions '[1]'`、`--increment '{"v":"x"}'`）→ 退出码 1、stderr 含「解析失败」。
8. **scope 不足**：用 `RO`（users.read）调写命令（如 `databases create --id x --name x`、`storage buckets create --name x`）→ 退出码 1、stderr 含 `PermissionDenied` **且** 含 scope 提示（`users.read/users.write` 或 `* / all` 字样）；用 `RO` 调 `users list` → 成功（对照）。
9. **rpc 与具名一致**：`torchwood rpc /torchwood.server.v1.DatabasesService/ListDatabases --data '{}' --api-key <FULL>` 与 `torchwood databases list --api-key <FULL>` 的 stdout **逐字节一致**；`rpc /torchwood.server.v1.GroupsService/ListGroups` 与 `groups list` 同样对照。
10. **gateway REST 语义一致**：对同一资源（如 `databases get clivdb`）对比 `torchwood` 输出与
    `curl -H "x-api-key: <FULL>" http://127.0.0.1:9080/v1/server/databases/clivdb`：
    字段名约定差异（CLI 为 protojson camelCase，如 `createdAt`；gateway 为 snake_case，如 `created_at`）属设计约定，**字段名与取值集合须语义一致**；二者不一致判失败。
11. **清理**：删除本次验收创建的全部资源（databases/groups/buckets/functions/oauth providers）与测试 key，确认无残留。

## 三、工程化

- `task test` 全部通过（含 cmd/client 与全仓，非 `-short`）；
- `task build` 通过且产出 `bin/torchwood[.exe]`；
- `gofmt -l cmd/client` 无输出；`go vet ./cmd/client/...` 通过。

## 四、输出要求

验收报告（markdown），包含：

1. **逐项结论表**：`# | 验收项 | 结论（通过/失败/警告） | 证据（文件:行号 或 命令输出摘要）`，覆盖 C1–C6、行为验证 1–11、工程化 3 项。
2. **失败项清单**：按严重程度排序（阻断/非阻断），每条附：期望 vs 实际、复现步骤、最小修复建议（建议即可，不修改代码）。
3. **与任务定义的偏差清单**：逐条标注「设计接受」（如 22 vs 21 个方法、CLI camelCase 输出）或「需修复」。
4. **已知服务端缺陷核对表**：C6 的 4 项逐一给出 CLI 透传行为证据，确认**未**被误判为 CLI 失败。
5. **最终结论**：整体是否通过验收；若不通过，列出必须修复项（按优先级）。
