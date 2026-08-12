# Round-2 修复最终审核报告（上级严格审核）

> 审核日期：2026-08-12 ｜ 审核对象：`fix-report.md` 声称的 G1–G10 全部修复（143 文件改动）
> 审核方式：亲自复跑全量验证 + 三路独立只读复核（G2/G3、G4/G5/G6、G7/G8/G9/G10/G1）+ 关键 P2 发现点亲自验证
> 基线：`aac6fdd`；工作区 143 文件改动、22 新增，无 git 提交（符合纪律）

## 总体结论

**审核通过（有条件）**：fix-report 的完成度声称属实——59 项修复条目经逐项独立复核基本全部真实落地，
无假修复、无恒真断言测试、无越界重构。**但发现 5 个 P2 缺口**，建议以一个小批次补齐后关闭本轮；
另有 2 项产品决策待确认（非缺陷）。

## 亲自验证结果

| 项 | 结果 |
|----|------|
| `go vet ./...` / `go build ./...` | ✅ exit 0 |
| `go test -short ./...`（56 包输出） | ✅ 全绿无 FAIL |
| 用户原有 3 个未提交改动 | ✅ 已确认由用户在 `26dad39` 自行提交，未丢失 |
| G7-2 多行 INSERT 脱敏缺陷 | ✅ 复核属实（正则仅匹配单个 VALUES 元组） |
| G9-1 Go SDK 缺 DeleteFactor | ✅ 复核属实（`grep -rn DeleteFactor sdk/go/` 无结果） |
| G3-5 max_per_user 默认值语义 | ✅ 复核属实（代码读配置零值=不限，模板写 50，proto 注释称默认 50） |

## 逐项复核结论（三路独立复核汇总）

- **G2（P0 权限收口）**：✅ 全部真实。Functions 7 个写方法与 proto RPC 逐一核对无遗漏、无误伤读方法；
  use-case 纵深防御全覆盖；`RequireServerWriteActor` 设计（admin+service 放行、匿名/端用户拒绝）与拦截器规则一致。
- **G3**：✅ 10/11 真实；G3-2 按方案 B 档执行（A 档留 backlog，符合 fix-plan 预设）。
- **G4**：✅ 3/4 真实；G4-1 见 P2-2。
- **G5**：✅ 全部真实（8/8）。
- **G6**：✅ 7/8 真实；G6-1 见 P2-3；worker 重试持久化按方案缓修（有注释标注）。
- **G7**：✅ 5/6 真实；G7-2 见 P2-4。
- **G8**：✅ 全部真实；掩码碰撞见 P3-1。
- **G9**：✅ 4/5 真实；G9-1 见 P2-5。
- **G10**：✅ 全部真实。`AssertAPIKeyScopeCoverage` 启动期 fail-closed 断言已验证不会因
  functions「死登记」误触发 panic（断言只校验 proto↔规则表）。
- **G1**：✅ CI 步骤语法与 Taskfile 依赖正确（真实执行待 push）。

## 发现的问题

### 🟠 P2（建议合入前补齐，均为小改动）

1. **dbhook 脱敏正则无法覆盖多行/批量 INSERT**
   - 位置：`internal/infra/clients/dbhook.go:89`（`sensitiveInsertPattern`）
   - 问题：`VALUES ('a'), ('b')` 仅脱敏第一个元组，后续元组敏感值明文残留。
   - 建议：正则改全局循环替换每个 VALUES 元组，或按列位置解析；补多行 INSERT 测试。

2. **HTTP 侧同 key 多值凭证头未拒绝（与 gRPC 不一致残留）**
   - 位置：`internal/api/serverhttp/auth.go:30-52`
   - 问题：`r.Header.Get` 只取首值；gRPC 侧 G3-8 已拒同 key 多值（`jwt.go:207-217`），HTTP 侧未同步。
   - 建议：对凭证类 header 检查 `len(r.Header.Values(key)) > 1` 返回 401。

3. **zip 总预算超限时部分清理**
   - 位置：`internal/infra/functions/docker.go:408-421`
   - 问题：总预算超限仅删除当前条目文件，已解压的前序条目与目录残留。
   - 建议：超限时清理整个解压目标目录。

4. **Go SDK 缺 `DeleteFactor`（G10-4 同步遗漏）**
   - 位置：`sdk/go/client/account.go`
   - 问题：G9-1 补了 33 个方法但漏了 G10-4 新增的 code 参数版 DeleteFactor；TS SDK 已同步。
   - 建议：补 `DeleteFactor(factorId, code)` + bufconn 用例。

5. **`max_per_user` 代码默认值与文案承诺不一致**
   - 位置：`internal/infra/auth/session_service.go:204`、`internal/pkg/config/config.proto:55-56`
   - 问题：未显式配置时读得 0=不限，而注释/模板承诺默认 50。
   - 建议：代码中 0 值回退默认 50（并将「0=不限」改为显式 -1 或在注释中澄清），或修正注释与模板。

### 🟡 P3（可接受，记录在案）

1. **掩码串碰撞**：`internal/app/functions/variables.go` 约定值=`******` 即保留旧值，用户真实意图设为
   `******` 会被静默丢弃；前端亦无法区分。执行方已注明为已知语义限制，建议文档化。
2. **G3-3 新半状态窗口**：先撤会话后提交，若提交失败用户被登出但资料未改——fix-plan 预设方案，可接受。
3. **计划外改动（均为良性）**：`docker.go` extractZip 新增符号链接条目拒绝（安全增强）；
   `variables.go` 跨批次改动属冲突矩阵预期内。

## 产品决策待确认（非缺陷，需你拍板）

1. **API key 无法调用 Functions 写方法**：G2-1 纵深防御使 `RequirePlatformAdmin` 拒绝 API key
   （ActorKind=service），`apiKeyScopeRules` 中 `functions.write` 成死登记，agent-native 场景
   （SDK/CLI 以 API key 部署函数）被切断。若产品需要 Agent 自动化部署函数，应改为
   `RequireServerWriteActor`（对齐 CreateBucket 的处理）。
2. **G3-2 邮箱 staging（A 档）**：待 proto 契约增加 url 字段后实施（backlog 已记录）。

## 后续

- 上述 5 个 P2 建议合并为一个小批次（G11）修复后，本轮审查-修复闭环即可关闭。
- 待 CI 验证项见 `fix-report.md` §4（Postgres 集成测试、Docker 构建、Actions 全链路）——
  G1 已把这些纳入 CI，push 后关注首轮运行结果。

---

## 附：G11 复核结论（2026-08-12，本轮闭环）

G11（`prompts/fix/G11-audit-p2.md`）五项 P2 缺口已修复并经我逐项复核：

| 项 | 结论 | 亲自验证证据 |
|----|------|--------------|
| 1. 多行 INSERT 脱敏 | ✅ | `dbhook.go:89` 正则扩展为整段 VALUES 元组序列（引号感知）；`dbhook_test.go` 含批量/跨行/非敏感不受影响等真实断言 |
| 2. HTTP 同 key 多值拒绝 | ✅ | `auth.go:34,45,63` 三处 `multiple credentials provided`；`TestHTTPAuth_SameKeyMultipleValuesRejected` 3 子用例 |
| 3. zip 超限完整清理 | ✅ | `docker.go:378,383,417,424` 四处 `os.RemoveAll(destDir)`；总预算/单条目两个目录级清理测试 |
| 4. Go SDK DeleteFactor | ✅ | `sdk/go/client/account.go:309`；`account_test.go:357` bufconn 透传 code + 错误路径 |
| 5. max_per_user 默认值 | ✅ | `session_service.go:23` `defaultMaxSessionsPerUser=50`，0→50、-1→不限；4 场景测试；`task generate-config` 幂等 |

全量回归（我亲自执行）：`go vet ./...`、`go build ./...`、`go test -short ./...` EXIT=0 全绿。
未触碰 `apiKeyScopeRules` 与 Functions 写方法守卫（符合约束），无 git 操作。

**结论：本轮（Round-2）审查 → 修复 → 审核闭环正式关闭。**
遗留：API key 可否调 Functions 写方法的产品决策（挂在用户处）；CI 全链路首轮结果（push 后关注）；
G3-2 A 档邮箱 staging、worker 重试持久化、REST 保留字迁移三项记录在案的 backlog。
