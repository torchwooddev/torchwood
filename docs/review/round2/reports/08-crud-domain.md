# Round-2 复审报告：08 - CRUD 抽象与领域端口

> 审查范围：`pkg/crud`、`internal/domain`、`pkg/query`、`pkg/idgen` 及其修复验证。
> 审查基准：当前工作区代码（HEAD = `c640d9b`，1288705 为祖先提交）。
> 执行方式：只读审查；辅助验证 `go vet`/`go test -short`/`go build`。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 证据与说明 |
|--------|------|------------|
| **F3-2** ListDocuments `page_size` 参数失效 | ✅ 已修复 | `pkg/query/query.go:182-183` 注释与 `ParseMany` 实现（`:194-195`）均未注入默认 limit；`internal/infra/documentdb/postgres.go:875-886` 明确「DSL 未显式指定 limit 时用 `q.PageSize` 回退，仍 ≤0 则回退 50，并保留上限 clamp（`maxQueryLimit=100`）」。回归测试 `postgres_test.go:681-773` 覆盖 page_size 生效、负数/零回退、DSL limit 优先、解析期负 limit/offset 报错、offset 超上限、非法 PageToken；`query_test.go:82-96` 单独断言 `ParseMany` 无默认 limit。 |
| **F5-1** Function ID 路径穿越 → 任意文件写入 | ✅ 已修复 | `internal/app/functions/management.go:17` 定义 `functionIDPattern = ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`，`:60-65` 对 `cmd.ID` 做非空+字符集+保留字校验，拒绝 `../../x` 等恶意 ID 并返回 `InvalidArgument`；`deployments.go:171-173` 的 `zipPath` 对 `projectID/functionID/deploymentID` 均做 `filepath.Base` 消毒；`:176-183` 的 `assertZipDir` 在 `writeZip`/`removeZip` 落盘前校验父目录仍在 `os.TempDir()/torchwood-functions` 前缀内；`:185-200` 写入/删除均调用该校验。测试 `security_test.go:16-48` 明确断言恶意 ID 返回 `codes.InvalidArgument`，`:51-88` 验证路径消毒与逃逸拒绝。 |
| **F5-2** GetDeployment/DeleteDeployment 跨项目 IDOR | ✅ 已修复 | `internal/domain/functions/repo.go:17-20` 端口签名已加入 `projectID`；`internal/infra/bun/bunrepo/function_repo.go:80-93` 与 `:118-124` 的 `GetDeployment`/`DeleteDeployment` SQL 均带 `fd.project_id = ?` 与 `fd.function_id = ?` 过滤；`internal/app/functions/deployments.go:121-137` 与 `:141-162` 在 Get/Delete 前均前置 `f.repo.GetFunction(projectID, functionID)` 校验，函数不存在直接返回 `NotFound`；`internal/api/servergrpc/functions.go:196-217` handler 正确透传 `projectID`。测试 `security_test.go:90-132` 覆盖跨项目 Get/Delete 被拒绝；`function_repo_test.go:73-77, 115` 覆盖 SQL 级跨项目不可见。`go vet ./...` 与 `go build ./...` 通过，无签名不一致编译错误。 |

---

## 2. 新发现问题

### 🔴 P0

无。

### 🟠 P1

1. **function ID 字符集允许大写字母，导致 Docker 镜像名非法、部署构建失败**
   - 位置：`internal/app/functions/management.go:17`
   - 代码：`var functionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)`
   - 影响：Docker 镜像 repository 名要求全小写（`[a-z0-9]+([._-][a-z0-9]+)*`）。当前 pattern 允许 `Fn-1` 等大写 ID，创建函数成功，但 `dockerExecutor.imageName`（`internal/infra/functions/docker.go:90-95`）直接使用该 ID 拼镜像 tag，后续 `ImageBuild` 将因 `invalid reference format` 失败。测试 `security_test.go:42` 甚至把 `Fn-1` 列为合法 ID，形成回归缺口。
   - 修复建议：pattern 收紧为 `^[a-z0-9][a-z0-9_-]{0,63}$`；同步调整 `security_test.go` 合法/非法用例；如要兼容历史数据，可在 `imageName` 内对 functionID 做 `strings.ToLower` 兜底。

### 🟡 P2

2. **`pkg/crud` 的 `contains`/`notcontains` 未转义 LIKE 通配符**
   - 位置：`pkg/crud/filter.go:306-312`
   - 代码：`return fmt.Sprintf("%s LIKE ?", fieldSQL)` / `*args = append(*args, "%"+expr.Value+"%")`
   - 影响：用户输入含 `%`、`_` 时会改变 SQL LIKE 语义，导致查询结果偏离预期（非注入，但属于正确性缺陷）。该问题在 `pkg/query`/`documentdb` 层已修复（`postgres.go:655-659, 1885-1893`），但共享抽象库 `pkg/crud` 仍存在。
   - 修复建议：增加 `escapeLikePattern` 并配合 `ESCAPE '\'` 子句，与 `postgres.go` 保持一致。

3. **`bunrepo.UpdateFunction` / `UpdateDeployment` 未按 `project_id` 过滤**
   - 位置：`internal/infra/bun/bunrepo/function_repo.go:60-64, 113-117`
   - 代码：`_, err := r.db.NewUpdate().Model(m).WherePK().Exec(ctx)`
   - 影响：当前用例层均先 `GetFunction`/`GetDeployment` 再更新，直接调用安全；但 repo 层缺少 `project_id` 纵深防御，若未来绕过用例直接调用接口，仍存在跨项目更新风险（F5-2 同一类 IDOR 的剩余面）。
   - 修复建议：`UpdateFunction` 加 `.Where("project_id = ?", fn.ProjectID).Where("id = ?", fn.ID)`；`UpdateDeployment` 加 `project_id`/`function_id` 过滤。

4. **`ListDocuments` 默认排序缺少 `_id` 稳定键**
   - 位置：`internal/infra/documentdb/postgres.go:1912`
   - 代码：`orderSQL := "ORDER BY d._created_at DESC"`
   - 影响：默认按 `_created_at DESC` 单字段排序，当多行 `_created_at` 相同时，offset 分页可能出现结果重复或遗漏；cursor 模式已补充 `_id` 谓词与排序（`:945`），但 offset 默认路径未补充。
   - 修复建议：默认排序改为 `ORDER BY d._created_at DESC, d._id DESC`。

### 🟢 P3

5. **测试 mock `DeleteDeployment` 未校验 `projectID`**
   - 位置：`internal/app/functions/mocks_test.go:110-119`
   - 代码：仅判断 `d.FunctionID != functionID` 后返回，随后无条件 `delete(r.deployments, deploymentID)`，未检查 `d.ProjectID == projectID`。
   - 影响：mock 未严格执行 `FunctionRepo` 接口契约，若未来写直接调用 repo 的测试会给出错误预期。
   - 修复建议：删除前断言 `d.ProjectID == projectID` 且 `d.FunctionID == functionID`。

6. **`idgen.IsValid` 仍只判断非空**
   - 位置：`pkg/idgen/id.go:20-22`
   - 代码：`return id.String() != ""`
   - 影响：通用 ID 校验工具未复用字符集规则，function ID 的合法性依赖 `management.go` 的局部 pattern，其他写入口若忘记校验仍可引入非法 ID。
   - 修复建议：为 function ID 等场景提供 `idgen.IsValidFunctionID()` 或把字符集校验下沉到 idgen；`IsValid` 保持非空语义也可接受，但需在 AGENTS.md 中明确。

7. **`TestListDocuments_PaginationGuards` 未覆盖 `page_size > maxQueryLimit` 的 clamp 场景**
   - 位置：`internal/infra/documentdb/postgres_test.go:681-773`
   - 影响：上限 clamp 逻辑（`postgres.go:884-885`）缺乏回归测试保护，未来误改可能导致越界大查询。
   - 修复建议：增加 `PageSize: 200` + 7 条数据场景，断言返回 100 条（受 `maxQueryLimit` 限制）。

---

## 3. 模块总体结论

- **修复完成度**：约 90%。F3-2 / F5-1 / F5-2 三个核心修复项均已实现，代码、测试、端口签名、实现、handler 调用链保持一致，`go vet` 与 `go test -short` 均通过。
- **剩余风险 Top 3**：
  1. 大写 function ID 导致 Docker 镜像构建失败（P1，核心功能受损）。
  2. `pkg/crud` 共享 `contains` 未转义 LIKE 通配符（P2，与 documentdb 层修复不同步）。
  3. 默认排序无 `_id` tiebreaker（P2，分页稳定性隐患）。
- **是否建议关闭本模块审查**：**不建议立即关闭**。待 P1（大写 ID）修复、P2（crud LIKE 转义、默认排序 tiebreaker、Update* project_id 过滤）补齐并补充对应测试后，方可关闭。当前状态可满足阶段性合入，但需在下一轮或相关批次（F5 后续清理）中收尾。

---

## 4. 验证命令与结果

```bash
# go vet 目标模块（无输出，通过）
go vet ./pkg/crud/... ./pkg/query/... ./pkg/idgen/... ./internal/domain/... \
       ./internal/app/functions/... ./internal/infra/bun/bunrepo/... \
       ./internal/infra/documentdb/...

# 纯单元测试（无外部依赖，通过）
go test ./pkg/crud/... ./pkg/query/... ./pkg/idgen/... ./internal/domain/... \
        ./internal/app/functions/... -short

# 目标模块编译（通过）
go build ./pkg/crud/... ./pkg/query/... ./pkg/idgen/... ./internal/domain/... \
         ./internal/app/functions/... ./internal/infra/bun/bunrepo/... \
         ./internal/infra/documentdb/... ./internal/infra/functions/... ./cmd/worker/...
```

所有命令均成功退出；需要 Postgres/Redis/MinIO/Docker 的集成测试未运行，按任务书约束由 CI 兜底。
