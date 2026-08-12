# 修复任务 F10：CI 修复（解锁测试）

## 角色

你是资深 DevOps/Go 工程师，负责修复 Torchwood GitHub Actions CI 的阻塞问题。
方案详见 `docs/review/fix-plan.md` §10（F10 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`docs/review/fix-plan.md` §10、`.github/workflows/ci.yml` 全文、
  `Taskfile.yml`（test 任务定义）
- 审查报告（背景）：`docs/review/` 下的 07 报告（P2-1）

## 背景

CI backend job 在 minio service 初始化阶段必然失败：`minio/minio:latest` 镜像
**不包含 curl**，而健康检查命令 `curl -f http://localhost:9000/minio/health/live`
必然失败 → service 容器被标记失败 → job 在测试前中止。后果：`go test ./...`、
docker 集成测试（`TestDockerExecutor_BuildAndRunNode`）、gofmt/vet 全部失去 CI 兜底，
放过了多个 P0/P1 缺陷（如 Docker 解压 0600 权限问题）。

## 修复清单

1. **修复 minio 健康检查**：`.github/workflows/ci.yml:38`（或实际行号）的
   `--health-cmd` 改为不依赖 curl 的探测方式，例如：
   - `bash -c 'exec 3<>/dev/tcp/127.0.0.1/9000'`（bash TCP 探测）；或
   - 改用 `minio/minio` 官方健康检查等价物（如 `mc ready local`，若 mc 镜像内可用）。
   同时为 postgres/redis service 的健康检查做同样的健壮性核对（如有 curl 依赖）。
2. **验证 docker 集成测试真实执行**：检查 `internal/infra/functions/docker_test.go`
   （或对应测试文件）在 CI 的 runner 上是否可运行（GitHub Actions ubuntu-latest
   支持 Docker；确认测试是否被 skip 逻辑跳过——`dockerAvailable` 检测）。如果
   Docker-in-Docker 不可用，将该测试改为明确跳过并记录原因，保证其余测试不被
   测试中断（`t.Skip` 而非失败）。
3. **验证 CI 全绿**：修复后 push 或触发 workflow，确认：
   - backend job：minio service 健康 → go test（含 documentdb/storage 集成测试）
   - lint job：gofmt/vet/eslint
   - build job：console-build + task build
   - 若仓库有 gh CLI 可本地查询 `gh run list` 确认最近一次 run 状态。

## 约束

- 只改 CI 相关文件（`.github/workflows/ci.yml`）与必要的测试 skip 逻辑
  （`internal/infra/functions/*_test.go` 的 skip 条件）
- **不要**改动业务代码
- 如果本地无法 push 验证，明确说明并给出预期行为

## 验证

- 本地：`task lint`、`task test` 的静态核对（测试需要本地基础设施，如 docker
  compose 可用则跑 `task up` + `task test` 验证文档类集成测试）
- 远端：push 后 `gh run watch` 或 `gh run list` 确认绿

## 输出

最终汇报：改动摘要 + CI 运行结果（截图/链接或状态）；列出仍然红的原因（如有）。
