# Torchwood Round 4 全量审核

> 日期：2026-08-24
> 基线：`main` @ `3398a26`
> 方式：8 个维度子代理并行只读深审（架构 / 租户隔离 / 认证授权 / API 契约 / 可靠性 / 测试 / SDK·CLI / Console·文档），主代理逐条复查全部 P1 结论后汇总。可靠性维度拆分为 E1（outbox·关停·迁移·级联删除，研究报告式）与 E2（韧性·泄漏）两部分。
> 前置：Round 1–3 高危项经本轮独立核验**均已真实落地且带启动期回归护栏，无回退**。

## 维度评分

| 维度 | 报告 | 评分 | P0 | P1 |
|------|------|------|----|----|
| A 架构分层合规 | [audit-report.md §A](audit-report.md#a-架构分层合规-7510) | 7.5 | 0 | 4 |
| B 多租户隔离与数据面安全 | [audit-report.md §B](audit-report.md#b-多租户隔离与数据面安全-8510) | 8.5 | 0 | 0 |
| C 认证授权与会话安全 | [audit-report.md §C](audit-report.md#c-认证授权与会话安全-8010) | 8.0 | 0 | 0 |
| D API 契约与 Proto 规范 | [audit-report.md §D](audit-report.md#d-api-契约与-proto-规范-7010) | 7.0 | 0 | 4 |
| E 可靠性与运维 | [audit-report.md §E](audit-report.md#e-可靠性与运维-8010) | 8.0 | 0 | 1 |
| F 测试质量与 CI 门禁 | [audit-report.md §F](audit-report.md#f-测试质量与-ci-门禁-7510) | 7.5 | 0 | 3 |
| G SDK / CLI / Agent-Native | [audit-report.md §G](audit-report.md#g-sdk--cli--agent-native-7010) | 7.0 | 0 | 3 |
| H Console 前端与文档一致性 | [audit-report.md §H](audit-report.md#h-console-前端与文档一致性-8010) | 8.0 | 0 | 2 |

**综合评分：7.7 / 10。**

核心判断：

1. **未发现任何 P0**（可利用的跨租户读写 / SQL 注入 / 认证绕过均不存在）。
2. 主要问题模式不是"能力缺失"，而是三类系统性缝隙：
   - **最后一公里未接线**：HMAC 分页 token 能力已建成未启用；encryption_key 校验承诺未兑现；SDK 类型化封装无测试强制；
   - **契约与实况脱节**：OpenAPI 字段命名 / 错误模型 / 存储传输端点与运行时不符；roadmap 状态落后代码一个身位；
   - **装配层漂移**：worker 关停清理时机违反 server 已写明的自家不变量。
3. 多租户隔离四层纵深（ident 校验 / schema 限定名 / 租户谓词 / 行级 ACL）达到一线 BaaS 水准，剩余缺口集中在**资源生命周期末端**（删除不清对象存储、缓存失效）与 Functions 网络边界。

## 修复

| 文件 | 用途 |
|------|------|
| [audit-report.md](audit-report.md) | 全部发现清单（P1/P2/P3 含位置、证据、建议、置信度与主代理复核标记） |
| [fix-plan.md](fix-plan.md) | Round 4 完整修复方案（批次 J1–J7，实施的**唯一事实来源**） |
| [fix-report.md](fix-report.md) | J1–J3 实施报告（已落地，2026-08-24） |

批次速览：

| 批次 | 名称 | 级别 | 状态 |
|------|------|------|------|
| J1 | 紧急修复（配置模板 / encryption_key / worker 关停 / roadmap） | P1 | ✅ 已落地 |
| J2 | 契约与机器可读性（OpenAPI 三件套 / presence / signed token） | P1 | ✅ 已落地 |
| J3 | SDK/CLI 交付链路（发布流水线 / 超时重试 / DX） | P1 | ✅ 已落地（发布流水线待真实触发验收） |
| J4 | 架构收口（组装根 / 端口化 / 跨层解耦） | P1+P2 | ⏳ 待执行（串行） |
| J5 | 可靠性与租户生命周期（限流降级 / 删除 purge / Functions 网络） | P1+P2 | ⏳ 待执行 |
| J6 | 测试与门禁加固（coverage / down 迁移 / linter / race） | P1+P2 | ⏳ 待执行 |
| J7 | P3 卫生批（低危打包，含 security 注解补齐） | P3 | ⏳ 最后 |
