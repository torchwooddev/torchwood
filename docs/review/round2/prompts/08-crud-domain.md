# 复审任务（Round 2）：08 - CRUD 抽象与领域端口（pkg/crud、internal/domain、pkg/query、pkg/idgen）

## 背景

- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色

你是资深 Go 代码审查专家（通用抽象库与 DDD 端口设计），对 Torchwood 的「CRUD 抽象库、查询 DSL、ID 校验与领域端口」做只读审查。**同时你是修复验证者，需对照 fix-plan 逐条核实。**

## 第一步：建立基线

- 读 `docs/review/prompts/08-crud-domain.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 **F3-2、F5-1、F5-2** 章节，并浏览 §0 总览表与 §12 交叉依赖矩阵：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- pkg/crud/ internal/domain/ pkg/query/ pkg/idgen/` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`——约定：列表查询复用 `pkg/crud`（AIP-132/158/160 抽象），动态文档查询优先使用 `pkg/query`；ID 统一由 `pkg/idgen` 生成/校验；`internal/domain` 只定义接口与模型、不依赖 infra
- `pkg/crud` 是全仓库列表/分页/排序共享抽象：`filter.go`、`list.go`、`order.go`、`pagination.go`、`repository.go`（泛型 repo + FieldMappings）
- `pkg/query`：Appwrite 风格查询 DSL 解析器，`ParseMany` 决定 limit 默认值注入行为
- `pkg/idgen`：ID 生成与校验工具，`IsValid` 在修复前仅判非空
- `internal/domain/*`：领域层聚合/实体与仓库端口；F5-2 将改变 `functions` repo 端口签名
- 典型使用方：`internal/infra/bun/bunrepo/*`、`internal/infra/documentdb/*`、`internal/app/*`；端口签名变化必须同步到全部实现、调用方与 `wire_gen.go`

## 复审重点 A：修复验证（逐条核实）

### F3-2 ListDocuments `page_size` 参数失效（P1）

- 文件锚点：`pkg/query/query.go:205-207`（`ParseMany` 恒注入默认 50）、`internal/infra/documentdb/postgres.go:729-735`
- 修复方案：
  1. `ParseMany` 不再注入默认 limit，默认值交由 adapter 决定；
  2. `ListDocuments` 在 DSL 未显式指定 limit 时使用 `q.PageSize` 并保留上限 clamp；
  3. 重写被掩盖的 `TestListDocuments_PaginationGuards`，断言真实行为。
- 请逐条核实：
  1. `ParseMany` 中是否已删除固定 `limit=50` 的注入逻辑；
  2. `ListDocuments` 是否实现「DSL 无 limit 时取 `PageSize` + 上限 clamp」；
  3. 测试是否真实覆盖「page_size 小于/大于默认值、DSL 显式 limit、上限越界」等场景，而非因恒 50 而恰好通过的假断言。

### F5-1 Function ID 路径穿越 → 任意文件写入（P0）

- 文件锚点：`internal/app/functions/management.go:47-49`、`pkg/idgen/id.go:20-22`（`IsValid` 仅判非空）、`internal/app/functions/deployments.go:144-157`
- 修复方案：
  1. `CreateFunction` 对 ID 做字符集+长度校验（如 `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`）；
  2. 纵深防御：`zipPath` 中 functionID 用 `filepath.Base` 或哈希后再拼接；
  3. `writeZip`/`removeZip` 落盘前断言 `filepath.Dir(path)` 仍在 `os.TempDir()/torchwood-functions` 前缀内。
- 请逐条核实：
  1. `pkg/idgen` 的 `IsValid` 或 `CreateFunction` 入口是否已增加字符集/长度校验，能拒绝 `../../x` 等恶意 ID；
  2. 函数部署路径是否不再直接使用用户输入的 functionID，或已做 `filepath.Base`/哈希/前缀校验；
  3. 是否存在绕过路径（如重命名、复制、worker 侧直接构造路径）；
  4. 是否补了恶意 ID 拒绝测试，且断言的是返回 `InvalidArgument` 而非仅空值。

### F5-2 GetDeployment/DeleteDeployment 跨项目 IDOR（P1）

- 文件锚点：`internal/domain/functions/repo.go:17,20`、`internal/infra/bun/bunrepo/function_repo.go:80-93,118-124`、`internal/app/functions/management.go:114-141`
- 修复方案：
  1. 前置 `GetFunction(projectID, functionID)` 校验；
  2. repo 端口签名加 `projectID`，SQL 加 `fd.project_id = ?`；
  3. 更新接口后同步所有调用方与测试（Wire 通常无需改动，但需编译通过）。
- 请逐条核实：
  1. `internal/domain/functions/repo.go` 的接口方法是否已加入 `projectID` 参数；
  2. 所有实现（bunrepo、可能的 mock/stub）SQL 是否都增加了 `project_id` 过滤；
  3. 所有调用方（management.go、grpc handler、use-case）是否都传递了正确的 `projectID`，无零值或错误传参；
  4. `wire_gen.go` 是否通过 `go vet`/`go build`；相关测试是否已更新并通过。

## 复审重点 B：回归与新问题排查

- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归）。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：
  - 安全：注入（filter/order 白名单）、越权信息泄露（cursor 内容）、ID 路径穿越
  - 正确性：错误处理/并发/事务边界、ParseMany 默认值变更后的调用方契约
  - 一致性：与 AGENTS.md 约定、proto 注解、domain 端口签名、Wire 生成产物
  - 测试质量：边界测试、假断言、mock 同步
- **本模块修复后特有风险点**：
  1. **ParseMany 移除默认 limit 后，动态文档查询可能变得无界**：若 adapter 忘记 clamp 或某些调用方绕开 adapter 直接拿 DSL，可能出现 `LIMIT 0` 或全表扫描；需重点核对 `postgres.go` 与所有 `ListDocuments` 调用点。
  2. **idgen.IsValid 收紧可能破坏既有数据/测试 fixture**：若仓库中已存在不符合新字符集的 function ID，旧数据读取或测试会失败；需确认校验只用于写入口，还是也用于读入口，是否存在误伤。
  3. **domain repo 加 projectID 容易产生半同步更新**：某个实现加了参数但 SQL 没加 `project_id` 过滤，或某个调用方仍传旧签名，会重新引入 IDOR；需遍历接口所有实现与调用，并确认 `go build ./...` 无编译错误。
  4. **F3-2 与排序/cursor 的交互**：移除默认 limit 后，若排序字段缺失，`postgres.go` 的默认排序是否仍稳定，是否与 cursor 编码一致，防止结果重复或丢失。

## 输出要求

简体中文复审报告，三节结构：

1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束

- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./pkg/crud/... ./pkg/query/... ./pkg/idgen/... ./internal/domain/...` 与无外部依赖的纯单元测试辅助验证，如 `go test ./pkg/crud/... ./pkg/query/... ./pkg/idgen/...`。
