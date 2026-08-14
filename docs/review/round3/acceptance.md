# Round 3 修复严格验收

> 验收人：原审查方  
> 日期：2026-08-14  
> 对照：`docs/review/round3/fix-plan.md`、`plan-review.md`、`fix-report.md`  
> 方法：逐项读当前源码 + 亲自跑验证命令（不以实施方自报为准）

## 总评

**验收通过（Pass）。** H1–H6 全部落地，5 项方案偏差均在 plan-review 论证范围内且实现与论证一致。无假修复、无恒真断言、无越界重构、无 proto 变更、无 git commit。

阻断级 P1（viewer 越权、Functions HTTP 401、DDL 误伤 API Key、TS Functions 门面、Confirm 刷新、预览裂图、邀请不幂等）均已按方案关闭。

## 亲自验证

| 命令 | 结果 |
|------|------|
| `go vet ./...` + `go build ./...` + `go test -short ./...` | exit 0 |
| `go test ./sdk/go/client/... ./sdk/go/server/...` | exit 0 |
| `cd sdk/typescript && npm test` | 17/17 pass |
| `task console-build` | exit 0 |

## 逐项裁定

| ID | 裁定 | 核实要点 |
|----|------|----------|
| H1-1 | ✅ | `adminRoleMethodRules` 覆盖全部 write（与 `apiKeyScopeRules` 对称）；读方法未入表；角色不含 viewer |
| H1-2 | ✅ | `adminRoleWriteCoverageDiff` + `AssertAdminRoleWriteCoverage` 在 `NewGRPCServer` 启动期调用；单测构造 missing/extra |
| H1-3 | ✅ | `APIKeys.Delete` → `RequirePlatformAdmin`；`UpdateUser` → `RequireServerWriteActor`；Teams 未套守卫 |
| H1-4 | ✅ | viewer 拒 6 个补登写方法且 handler 不跑；member 拒接管面、放过业务写；use-case 测试断言真实错误码 |
| H2-1 | ✅ | `contexts.WithPrincipal` 后再 `CreateDeployment`；`RequireServerWriteActor` 未削弱 |
| H2-2 | ✅ | admin / `functions.write` 均 201，executor ctx 含对应 ActorKind |
| H3-1 | ✅ | 9 处 DDL 均为 `RequireServerWriteActor`；系统集合 / default 库保护仍在 |
| H3-2 | ✅ | `ddlCalls` 覆盖 9 方法；端用户/匿名拒；service 与各角色 admin 过守卫 |
| H4-1 | ✅ | `Torchwood.server.functions` 已挂；契约测试按 swagger 断言门面方法 |
| H4-2 | ✅ | Account 全部 `ACCESS_PUBLIC` + SignOut 已入 `noRefreshMethods`；TS `auth:"none"` |
| H4-3 | ✅ | Go Client `DeleteTeam` + 测试 |
| H5-1 | ✅ | `previewFile` blob + objectURL + revoke + 失败占位；`file_handler` cookie 优先未动 |
| H5-2 | ✅ | `canWrite` 白名单 fail-closed；分享按钮按 `writeable` |
| H5-3 | ✅ | 函数列表 `BulkDeleteButton` |
| H5-4 | ✅ | 入口 normalize email；`ensureMembershipUnique` 在 Create 前；测试覆盖同 user / 大小写 email / pending；total 不虚增 |
| H5-4 索引 | ⚠️ 接受 | 按方案 fallback：空串非 NULL，不加 unique。已写明 |
| H6-1 | ✅ | 点名的 6 处 handler 回传 token；`ListBuckets` 签名扩展（偏差 A）并同步 HTTP/测试 |
| H6-2 | ✅ | `GetBucket` 用 `query.BuildEqual` |
| H6-3 | ✅ | Lua 比对成功才 DEL；错 secret 计数；超 5 次锁定；改邮箱 `CheckSendRateLimit` |
| H6-4 | ✅ | 三处限流走 `incrWithTTL` Lua；首次计数带 TTL 有测试 |

## 偏差复核

| 偏差 | 裁定 |
|------|------|
| A ListBuckets 签名 | 接受。方案未写前置，改动最小且必要 |
| B token 用 JSON attempts 而非 HINCRBY | 接受。WRONGTYPE 判断正确；miniredis 测过 cjson |
| C noRefresh 纳入 client documents 公开读 | 接受。与「注释必须名副其实」一致，非范围膨胀 |
| D H2 用真实 use-case + recording executor | 接受。测到的是根因 |
| E 不加 unique 索引 | 接受。方案允许的 fallback |

## 残留（不阻断）

1. 文件详情在判定 MIME 前就会请求 preview（非图片会多一次失败请求，UI 不展示）。
2. `ensureMembershipUnique` 与 `CreateDocument` 之间仍有并发窗口（方案已接受应用层查重）。
3. account-token Lua 在 `PTTL<=0` 时会 `SET` 不带过期；创建路径总是带 TTL，实际打不到。
4. fix-report 写「39 项写方法」，表实际与 scope 对齐约为 48 项。以代码与覆盖测试为准。

## 结论

本轮可以关闭。工作区改动尚未提交，由仓库 owner 决定如何 commit。
