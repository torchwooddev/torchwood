# 修复任务 F5：Functions 安全修复

## 角色

你是资深 Go 安全工程师（容器执行领域），负责修复 Torchwood Functions 模块的安全缺陷。
方案详见 `docs/review/fix-plan.md` §5（F5 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`、`docs/review/fix-plan.md` §5、`docs/implementation-functions-executor.md`
- 审查报告（背景）：`docs/review/` 下的 07 报告

## 修复清单

1. **Function ID 路径穿越 → 任意文件写入**（P0）：
   - 位置：`internal/app/functions/management.go:47-49`（CreateFunction 仅校验
     `idgen.ID(cmd.ID).IsValid()`，而 `pkg/idgen/id.go:20-22` IsValid 仅判非空）、
     `internal/app/functions/deployments.go:144-157`（zipPath 将 functionID 拼入
     `filepath.Join(os.TempDir(), zipDir, projectID, functionID, ...)`，`../../` 可逃逸）。
   - 修复：
     a. `CreateFunction` 对 ID 做字符集+长度校验（如 `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`），
        非法返回 InvalidArgument（**不要**改 `idgen.ID.IsValid` 的全局语义，避免影响其他
        使用方——在 use-case 内校验）；
     b. 纵深防御：`zipPath` 中 functionID 先 `filepath.Base` 或哈希后再拼接；
     c. `writeZip`/`removeZip` 落盘前断言 `filepath.Dir(path)` 在
        `os.TempDir()/torchwood-functions` 前缀内，否则报错。
   - 验证：补 `../../x`、`a/b`、超长等恶意 ID 的拒绝测试。
2. **GetDeployment/DeleteDeployment 跨项目 IDOR**（P1）：
   - 位置：`internal/app/functions/management.go:114-141`（GetDeployment/DeleteDeployment
     不校验 projectID）、`internal/infra/bun/bunrepo/function_repo.go:80-93,118-124`
     （repo 查询仅按 function_id/id）、`internal/domain/functions/repo.go:17,20`（端口签名）。
   - 修复：use-case 前置 `GetFunction(projectID, functionID)` 校验（对齐 ListDeployments）；
     repo 端口签名加 projectID，SQL 加 `fd.project_id = ?` 条件（同步更新接口与全部调用方）。
3. **Docker 解压 0600 + USER 非 root → EACCES**（P1）：
   - 位置：`internal/infra/functions/docker.go:328`（0o600 写入）、`:365-375`（USER node/1000）。
   - 修复：解压文件写入改 0o644（或写入后 `os.Chmod(target, 0o644)`）。
   - 说明：无法本地验证 Docker 执行时，用代码走查确认，并注明需 CI 验证
     （CI 修复在 F10 批次）。
4. **GetVariables 明文返回全部 secret**（P1）：
   - 位置：`internal/app/functions/variables.go:30-35`、`internal/api/servergrpc/functions.go:237-247`。
   - 修复：`GetVariables` 对值脱敏返回（空串或掩码 `******`）；值仅在 SetVariables
     请求/响应中可见；**不要**本轮做存储加密（可备注后续）。
5. **docker build 失败被吞 + 构建日志丢弃**（P1）：
   - 位置：`internal/infra/functions/docker.go:149-157`（`io.Copy(io.Discard, resp.Body)`）。
   - 修复：`LimitReader(64KB+1)` 读取并保存构建输出；扫描流内 `{"error":...}` JSON 记录
     （BuildKit 模式失败可能不返回 Go error）；存在 error 或非零 exit 时返回带日志的错误；
     `buildDeployment`（`internal/app/functions/deployments.go:88-104`）截断写入 dep.Error
     （与设计文档 §5.3 一致）。
6. **TW_DATA 64KB 超 execve 32KiB 硬限制**（P1）：
   - 位置：`internal/app/functions/executions.go:20`（maxExecutionDataBytes=64KB）、
     `internal/infra/functions/docker.go:185`（TW_DATA 注入环境变量）。
   - 修复：`maxExecutionDataBytes` 收紧至 32KB，并与 env 合并预算校验
     （`len(data)+总 env 大小 ≤ 32KB`，再留 argv 余量）；超限返回 InvalidArgument。
7. **P2 补强**：
   - `variables.go:12-28` SetVariables 校验 function 存在（GetFunction nil → NotFound）
   - `executions.go:254` worker 补构建加 `context.WithTimeout(ctx, 5*time.Minute)`
   - `deployments.go:61-80` 信号量满/中途失败时删除 deployment 行与 zip（或记录 pending 对账）
   - `docker.go:96-112` ensureNetwork 失败不缓存（sync.Once 仅缓存成功）
   - `cmd/worker/worker.go:120-125` 消费瞬时失败重抛回队（LPUSH）或标 failed，避免静默丢弃
   - `internal/infra/bun/bunrepo/function_repo.go:217-234` 孤儿对账仅处理 building/running
     （queued 且仍在队列的任务不被误标 failed）

## 约束

- **不要**改 `internal/api/serverhttp/functions_handler.go` 的鉴权（F2 批次负责）
- 不修改 proto；不引入新依赖；除必要外不新增注释
- 不运行需要 Docker/DB 的测试（静态走查 + 标注需 CI 验证项）

## 验证

- `go vet ./internal/app/functions/... ./internal/infra/functions/... ./cmd/worker/... ./internal/domain/functions/...`
- `go build ./...`
- 为路径穿越、IDOR、变量脱敏补单元测试（不依赖 Docker 的部分）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；明确标注需 CI/Docker
验证的项（F10 修复后）。
