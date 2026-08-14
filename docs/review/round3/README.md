# Torchwood Round 3 全量审核

> 日期：2026-08-13  
> 基线：`main` @ `031ce90`（工作区干净）  
> 方式：12 个模块子代理并行只读审查当前源码，主代理交叉核实 P0/P1 后汇总

Round 1（F1–F11）与 Round 2（G1–G12 + B1/B2/B3）已闭环。本轮不是对照旧 diff，而是对当前树做独立深审。

## 模块报告

| 编号 | 模块 | 报告 | 主代理复核 verdict |
|------|------|------|-------------------|
| 01 | 安全与认证 | [reports/01-security-auth.md](reports/01-security-auth.md) | 主路径健康；P1 viewer 越权属实 |
| 02 | 动态文档层 | [reports/02-documentdb.md](reports/02-documentdb.md) | 无 P0/P1，可关闭安全主线 |
| 03 | Server API 传输层 | [reports/03-server-api.md](reports/03-server-api.md) | P1 Functions HTTP 未注入 Principal，属实 |
| 04 | Client/Console API | [reports/04-client-console-api.md](reports/04-client-console-api.md) | 安全主路径可关闭 |
| 05 | Account 用例层 | [reports/05-account-use-cases.md](reports/05-account-use-cases.md) | B1 验收通过，无 P0/P1 |
| 06 | Server/Console 用例层 | [reports/06-server-console-use-cases.md](reports/06-server-console-use-cases.md) | DDL 守卫过严 + 邀请不幂等 |
| 07 | Storage / Functions / Worker | [reports/07-storage-functions-worker.md](reports/07-storage-functions-worker.md) | 与 03 同一 P1；B2 已落地 |
| 08 | CRUD 与领域端口 | [reports/08-crud-domain.md](reports/08-crud-domain.md) | 无 P0/P1 |
| 09 | 基础设施与装配 | [reports/09-infra-assembly.md](reports/09-infra-assembly.md) | 无 P0/P1 |
| 10 | Proto 与代码生成 | [reports/10-proto-codegen.md](reports/10-proto-codegen.md) | 通过，可关闭 |
| 11 | Console 前端 | [reports/11-console-frontend.md](reports/11-console-frontend.md) | 预览裂图属实；批量删确认降为 P2 |
| 12 | SDK 与 CLI | [reports/12-sdk-cli.md](reports/12-sdk-cli.md) | Functions 门面缺失属实 |

汇总结论见 [audit-report.md](audit-report.md)。

## 修复

| 文件 | 用途 |
|------|------|
| [fix-plan.md](fix-plan.md) | Round 3 完整修复方案（H1–H6，实施的唯一事实来源） |
| [prompts/implement.md](prompts/implement.md) | 交给第三方 agent 的实施 prompt（先审方案再实现） |
| [plan-review.md](plan-review.md) | 实施方对方案的审核 |
| [fix-report.md](fix-report.md) | 实施方逐项对照 |
| [acceptance.md](acceptance.md) | 原审查方严格验收（通过） |
