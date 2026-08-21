# E-4 Query AST（一页规格）

> Wave 2。对应 `docs/review/first-principles-plan.md` E-4。  
> 日期：2026-08-21。**可施工。**

## 锁定

1. **单一查询模型**在 `pkg/query`：`AST`（Filter 树 + Orders + Page）。`Parse` / `ParseMany` 把 Appwrite 字符串编进 AST（codec）。SQL 编译仍在 documentdb，但输入是 AST，不再在 adapter 里直接解析字符串。
2. **proto** 新增 `proto/shared/v1/query.proto`：`Filter`（eq/ne/lt/lte/gt/gte/in/contains/starts_with/ends_with/search + `and`/`or` 递归）、`Order`、`Query`（filter + orders + page_size + page_token）。`and`/`or` 必须能表达；Appwrite 字符串 codec 仍是隐式 AND（与今日一致）。
3. **双栈**：`ListDocumentsRequest` 增加 `optional shared.v1.Query query = N`（新字段号，不复用）。若 `query` 有 filter/orders/page，用它；否则 `queries[]string` + `page_size`/`page_token` 走 codec。两者同时提供且冲突 → `InvalidArgument`。Client 与 Server 两侧同样加字段。
4. **分页**：权威是 `page_token`（K-20）。codec 把 Appwrite `limit`/`offset`/`cursorAfter`/`cursorBefore` 映射进 AST 的 page；adapter 继续用现有 token 实现，不新开第三套分页。
5. **不做**：ListProjects 的 AIP `filter`/`order_by` 落地（handler 接线 ≠ 实现）。Count 可继续吃 `queries[]string` 或同一 AST；有 `query` 时 Count 也走 AST。
6. **domain `databases.Query`**：保留 `Queries []string` 作为传输入口；app 层解析成 `pkg/query.Query` 再交给 documentdb。不要让每个 handler 手写 Parse。

## 验收

- `equal("a","b")` 与 proto `eq{attr:a values:[b]}` 生成同一 SQL 谓词。
- proto `and`/`or` 至少有一条集成测试（两条件 OR）。
- 旧 SDK 只填 `queries` 仍绿。
- 不改默认 `read:any`、不改 `_perms` SQL 下推。
