# Torchwood 审查文档

本目录有两类审查，不要混单：

| 类型 | 文档 | 用途 |
|------|------|------|
| **第一性原理设计评审** | [first-principles-design.md](first-principles-design.md) | 不考虑既有预设，从最优设计评估模块切分、接口深度与缝。按 ID 逐项验证后再规划。**不是**缺陷修复清单。 |
| **Round 1 分模块代码审查** | 下文 prompt + [fix-plan.md](fix-plan.md) | 对照源码找缺陷并修复（F1–F11）。 |
| **Round 2** | [round2/](round2/) | 第二轮审计与修复（G 批次）。 |
| **Round 3** | [round3/](round3/) | 第三轮全量审核与验收。 |

---

# 分模块代码审查

本目录为 Torchwood 项目分模块代码审查的 prompt 集合，每个模块一份独立 prompt，
可直接整份复制交给其他 agent 执行（一次一个模块，可并行）。

## 使用方式

1. 将 `prompts/XX-*.md` 整份内容作为 prompt 交给一个 agent（`explore` 或 `general`，
   建议 `general`，因为涉及推理与交叉比对）。
2. 每个模块可独立并行执行；高优先模块（安全、文档层、传输层）建议先做。
3. agent 只读审查、不修改代码；输出分级问题清单（P0/P1/P2/P3）。
4. 汇总各模块报告后，把 P0/P1 项排期修复。

## 模块清单

| 编号 | 模块 | 范围（目录） | 优先级 |
|------|------|--------------|--------|
| 01 | 安全与认证（拦截器/会话/OTP/OAuth/MFA） | `pkg/grpc/interceptor`、`internal/infra/auth`、`pkg/jwtparser`、`pkg/secretbox`、`pkg/password` | 高 |
| 02 | 动态文档层（Postgres adapter + 查询 DSL） | `internal/infra/documentdb`、`pkg/query` | 高 |
| 03 | Server API 传输层 | `internal/api/servergrpc`、`internal/api/serverhttp` | 高 |
| 04 | Client/Console API 传输层 | `internal/api/clientgrpc`、`internal/api/consolegrpc` | 中 |
| 05 | Account 用例层 | `internal/app/client` | 高 |
| 06 | Server/Console 用例层 | `internal/app/server`、`internal/app/console`、`internal/app/shared` | 中 |
| 07 | Storage / Functions / Worker | `internal/app/storage`、`internal/app/functions`、`internal/infra/storage`、`internal/infra/functions`、`internal/infra/messaging`、`internal/infra/queue`、`cmd/worker` | 高 |
| 08 | CRUD 抽象与领域端口 | `pkg/crud`、`internal/domain` | 中 |
| 09 | 基础设施与服务器装配 | `internal/infra/bun`、`internal/infra/clients`、`internal/infra/idgen`、`internal/infra/health`、`internal/infra/server`、`internal/pkg`、`cmd/server`、`db/migrations`、`internal/testutil` | 中 |
| 10 | Proto 定义与代码生成 | `proto/`、`buf.yaml`、`buf.gen.yaml`、`genproto`（抽查）、`sdk/typescript/src`（类型对照） | 中 |
| 11 | Console 前端 | `console/src` | 中 |
| 12 | SDK（Go/TS）与 CLI | `sdk/go`、`sdk/typescript/src`、`cmd/client`、`sdk/demo` | 中 |

> 规模参考（2026-08 统计）：Go 服务端约 4.5 万行、Console 约 8600 行、SDK 约 4000 行、proto 约 1700 行。

## 公共上下文（每份 prompt 已内嵌，此处仅备查）

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 技术栈：Go 1.26 + gRPC/grpc-gateway + Wire + bun + PostgreSQL + Redis + MinIO；React 19 + TS + TanStack Query + shadcn/ui
- 架构：Clean Architecture：`internal/api`（传输）→ `internal/app`（用例）→ `internal/domain`（端口）→ `internal/infra`（适配器）；`pkg/` 为跨层工具
- 约定（AGENTS.md）：gRPC 方法必须带 proto authz 注解；列表查询复用 `pkg/crud`；动态文档查询用 `pkg/query`；JWT claims 与 `pkg/jwtparser` 映射兼容；对话与文档用简体中文
- 安全要点：API Key 以 `keys` 角色参与 `_perms`，不默认 bypass；admin 通过 `X-Torchwood-Project` 头指定项目；Console 会话用 HttpOnly cookie

## 修复阶段

- **`fix-plan.md`**：完整修复方案（11 个批次 F1-F11，含 P0/P1 全部问题、位置、方案、验收、文件冲突矩阵、回归清单）
- **`prompts/fix/F*.md`**：接力修复 prompt，每个批次一份，可直接整份交给一个 agent 执行
- 执行顺序建议：F10（解锁 CI）→ F1+F2（并行）→ F3+F4（并行）→ F5+F6（并行）→ F7+F8+F9（并行）→ F11
- 每个批次建议独立 git 分支，完成后合并并跑 `task test`/`task build`
