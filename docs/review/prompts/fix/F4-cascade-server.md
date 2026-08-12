# 修复任务 F4：级联删除与 Server 用例修复

## 角色

你是资深 Go 后端工程师，负责修复 Torchwood Server 用例层的级联删除与错误分类缺陷。
方案详见 `docs/review/fix-plan.md` §4（F4 批次）。**只修本任务列出的问题**。
注意：F4-2（DeleteDatabase/DeleteCollection 元数据清理）已并入 F3 批次，本任务**不要**处理。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`、`docs/review/fix-plan.md` §4
- 审查报告（背景）：`docs/review/` 下的 06 报告

## 修复清单

1. **用户/团队级联删除被 50 条截断**（P0）：
   - 位置：`internal/app/server/users.go:287-326`（deleteUserCascade：sessions/identities/
     memberships 三个集合的 ListDocuments 均未设 PageSize）、
     `internal/app/server/teams.go:455-463`（listMembershipDocs 同）、
     `internal/app/server/teams.go:165-182`（DeleteTeam 级联）。
   - 修复：ListDocuments 设 `PageSize: 1000` 并循环直至 `NextPageToken` 为空
     （每个集合循环删除）；DeleteTeam 的 memberships 同样分页循环。
   - 验证：补 >50 条会话/成员的删除集成测试（可标注需要 CI）。
2. **团队 last-owner 保护缺失**（P1）：
   - 位置：`internal/app/server/teams.go:280-300`（UpdateMembership 可降级任意成员）、
     `:357-368`（DeleteMembership 无 last-owner 保护）、
     `internal/app/client/teams.go:140-161`（client 路径 owner 自退）。
   - 修复：删除/降级 owner 角色 membership 前，统计该团队 accepted 且含 owner 角色的
     membership 数量，≤1 时拒绝（FailedPrecondition）；client 自退路径同样校验
     （可以只在 app/server 侧实现公共校验函数，client 路径调用同一逻辑或复制等价校验）。
3. **UpdateUser 改邮箱不查重**（P1）：
   - 位置：`internal/app/server/users.go:141-153`（email 分支仅 normalize + 置
     email_verified=false）。
   - 修复：改 email 前按新邮箱查重（排除自身 userID，复用 query.BuildEqual + ListDocuments），
     重复返回 AlreadyExists；并发唯一冲突（23505）经 `MapDocumentDBError` 映射为 AlreadyExists。
4. **GetProject 返回 nil,nil**（P1）：
   - 位置：`internal/api/servergrpc/projects.go:67-71`（use-case 返回 nil,nil 时 handler
     原样返回 → gRPC OK + 空响应）。
   - 修复：handler 在 `p == nil` 时返回 `status.Error(codes.NotFound, "project not found")`
     （对齐 users.go 的 GetUser 模式）。
5. **P2 补强**：
   - `users.go:272-282` DeleteUser 级联整体包入事务（可用 docDB 的 RunInTx 或
     `clients.InTx`；若无法简单实现则记录原因并保持现状——级联分页修复是主项）。
   - `users.go:249-270` CreateUserToken 增加审计标记（session provider 已为
     "server_token"，可再加日志记录调用者）与更短 token 生命周期说明（如注释
     明确为调试用途）；不改变 proto 契约。

## 约束

- **不要**改动 `internal/infra/documentdb/postgres.go` 的 DeleteDatabase/DeleteCollection
  （F3 批次负责）
- 不修改 proto；不修改 `internal/app/client/` 中除 teams.go last-owner 外的文件
- 保持现有代码风格；不引入新依赖；除必要外不新增注释
- 不运行需要本地 Postgres 的集成测试

## 验证

- `go vet ./internal/app/server/... ./internal/api/servergrpc/projects.go ./internal/app/client/teams.go`
- `go build ./...`
- 为 last-owner 与 UpdateUser 查重补单元/集成测试（标注需要 CI 的项）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；列出需要 CI 验证的项。
