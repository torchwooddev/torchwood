# Torchwood 分模块代码复审（Round 2：修复后复审）

本目录为 Round 2 复审 prompt 集合。Round 1 审查（12 份模块报告 → `docs/review/fix-plan.md`，
提交 1288705）及 F1–F11 修复均已完成，本轮目标是：

1. **修复验证**：对照 `fix-plan.md` 逐条核实修复是否落地、正确、完整，测试是否真实断言；
2. **回归排查**：修复引入的接口/行为变化是否同步到全部调用方、前端、SDK，有无功能完整性回归；
3. **新发现**：Round 1 未覆盖或未修项的重扫（安全、正确性、一致性、测试质量）。

## 使用方式

1. 将 `prompts/XX-*.md` 整份内容作为 prompt 交给一个 agent 执行（只读复审，不修改代码）。
2. 每个模块可独立并行执行；建议优先复审高优先模块（01 安全认证、02 文档层、03 Server API、07 Storage/Functions）。
3. 每份 prompt 自包含，审查 agent 需再读两个 repo 内文件建立基线：
   - `docs/review/prompts/XX-*.md`（Round 1 prompt：审查范围/重点/通用检查项基线）
   - `docs/review/fix-plan.md` 对应批次章节（Round 1 结论与修复方案）
4. 审查以**当前工作区代码**为准（fix-plan 中的行号锚点可能已漂移，用 `git blame`/搜索重新定位）。
5. 输出三节结构报告：修复验证结论表（✅/⚠️/❌/🔴）→ 新发现问题（P0–P3）→ 模块总体结论。

## 模块清单与修复批次对照

| 编号 | 模块 | 覆盖修复批次 | 优先级 |
|------|------|--------------|--------|
| 01 | 安全与认证（拦截器/会话/OTP/OAuth/MFA） | F1（infra/auth 部分）、F2-1/F2-4、F7-5、F7-6 | 高 |
| 02 | 动态文档层（Postgres adapter + 查询 DSL） | F3 全部（含并入的 F4-2）、F4-1 分页能力 | 高 |
| 03 | Server API 传输层（servergrpc + serverhttp） | F2-3/F2-4、F4-4/F4-5、F6-2、F6-3（file_handler） | 高 |
| 04 | Client/Console API 传输层（clientgrpc + consolegrpc） | F1-1、F2-1、F7-1、F8-2 | 中 |
| 05 | Account 用例层（internal/app/client） | F1 全部 | 高 |
| 06 | Server/Console 用例层（app/server、app/console） | F2-2、F4、F5-4、F7-1 | 中 |
| 07 | Storage / Functions / Worker | F5、F6、F10 | 高 |
| 08 | CRUD 抽象与领域端口（crud/domain/query/idgen） | F3-2、F5-1、F5-2 | 中 |
| 09 | 基础设施与服务器装配（config/clients/server/CI） | F7 全部、F10 | 中 |
| 10 | Proto 定义与代码生成 | F11 全部、F8-2 | 中 |
| 11 | Console 前端（console/src） | F9 全部 | 中 |
| 12 | SDK（Go/TS）与 CLI | F8 全部、F11-2 | 中 |

> 批次归属按 `fix-plan.md` §12 冲突矩阵调整后的口径（F4-2 并入 F3、F4-6 并入 F2）。

## 与 Round 1 的关系

- Round 1 prompt 保留在 `docs/review/prompts/`，本轮 prompt 引用而非复制其基线内容；
- Round 1 修复执行 prompt 在 `docs/review/prompts/fix/`，已全部执行完毕（见 git 历史）；
- 本轮报告建议存放于 `docs/review/round2/reports/`（执行时创建），全部完成后可汇总出第二轮修复计划。

## 修复阶段（Round-2）

- **`fix-plan.md`**：第二轮完整修复方案——12 份复审报告的全部发现（1 个 P0、约 20 个 P1 及 P2/P3），
  划分为 10 个批次 G1–G10（含依赖图、文件冲突矩阵、回归验证清单）。
- **`prompts/fix/orchestrator.md`**：接力修复总控 prompt。整份交给一个主 agent 执行，
  它会按依赖图自行分派子 agent 实施各批次并逐批验收，最终产出 `fix-report.md`。
- 执行顺序由总控保证：G1（CI）→ G2–G5/G7–G9（并行）→ G6（依赖 G2）→ G10（依赖 G3，generate-proto）。
- 修复全部完成后由上级（项目 owner 侧的 agent）对照 `fix-plan.md` 与 `fix-report.md` 做最终严格审核。

## 全量回归验证（所有模块复审完成后）

参照 `fix-plan.md` §13 回归清单，重点复跑其中的手工安全冒烟项（Magic URL 不含 secret、
并发双消费、viewer 写方法拒绝、恶意 function ID 拒绝、并发 upsert 不可改写、setup token 拦截），
以及 `task generate-all` / `task test` / `task lint` / `task build` / `task console-build`。
