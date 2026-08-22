# 2026-08 架构评审修复方案（双轮交叉验证）

> 依据：2026-08-22 两轮独立评审（第一轮 5 个评审代理 + 第二轮 4 个无锚定独立评审代理，
> 关键结论 100% 复现；分歧点经人工读码仲裁）。
> 行号以评审时代码为准，修复时以实际代码为准。
> 修复状态标记：✅ 本次已实施 / 📋 后续工作流（本文档含设计草案）。

## 0. 总览

| 编号 | 问题 | 严重度 | 状态 |
|------|------|--------|------|
| P0-1 | 队列重复执行：claimMinIdle=100ms + ProcessExecution 无状态闸门 | 高 | ✅ |
| P0-2 | buildDeployment 信号量满分支删除既有 deployment | 高 | ✅ |
| P0-3 | Functions HTTP 上传缺 owner/admin 角色门禁（viewer 可部署代码） | 高 | ✅ |
| P0-4 | Scoped 热路径每次调用执行迁移事务 + 项目级 advisory 锁 | 高 | ✅ |
| P1-1 | outbox 领取 FOR UPDATE SKIP LOCKED 不在事务内，互斥失效 | 中 | ✅ |
| P1-2 | outbox published 行永不清理，表无限增长 | 中 | ✅ |
| P1-3 | dev_log_otp/dev_log_sms 无生产门禁且模板默认开启 | 中 | ✅ |
| P1-4 | Redis 消费组以 0-0 起始，组重建重放全部历史 | 中 | ✅ |
| P1-5 | docker 超时路径不排空 ContainerWait 通道，泄漏 goroutine | 中 | ✅ |
| P1-6 | PruneOldExecutions 不看状态，积压时吞掉 queued 记录 | 中 | ✅ |
| P1-7 | MarkExecutionFailed 无条件覆盖，可把 completed 改写为 failed | 中 | ✅ |
| P1-8 | DB 连接超时全靠 pgdriver 隐式默认 | 中 | ✅ |
| P1-9 | fulltext 索引表达式与查询编译不一致 | 中 | 📋 W-E |
| P1-10 | ListDocuments N+1 权限查询 + 无条件精确 COUNT | 中 | 📋 W-D |
| P1-11 | cursor 分页 next token 编码 offset，深翻页语义漂移 | 中 | 📋 W-D |
| P1-12 | 加密密钥复用 JWT 主密钥（TOTP/OAuth secret） | 中低 | 📋 W-I |
| P1-13 | published_at 持续失败 >5min 时客户端可见事件重复 | 中 | 📋 W-J |
| P1-14 | 事件乱序（失败退避插队；经济事件无版本号） | 中 | 📋 W-J |
| P1-15 | Redis stream 无裁剪，内存单调增长 | 中 | 📋 W-F |
| P1-16 | run/build 信号量为进程级，server/worker 分进程各一份 | 中 | 📋 W-F |
| P2-1 | app 层 infra 渗透（*clients.Database、手写 SQL、自建适配器） | 高（架构） | 📋 W-A |
| P2-2 | gRPC status 渗透 app 层（111 文件 / 595 处；mapUserError 双份复制） | 高（架构） | 📋 W-A |
| P2-3 | pkg/grpc/interceptor 反向依赖 internal | 高（架构） | 📋 W-B |
| P2-4 | documentdb/postgres.go 2597 行 6 职责 | 中（架构） | 📋 W-C |
| P2-5 | realtime JWT AfterFunc 不 Stop | 低 | 📋 W-G |
| P2-6 | 手工 HTTP 入口不在审计仓库覆盖 | 中 | 📋 W-G |
| P2-7 | ListRequest.filter/order_by 大面积静默 no-op | 中（契约） | 📋 W-K |
| P2-8 | client/server proto 双份 message 字段号漂移 | 中（契约） | 📋 W-K |
| P2-9 | CI 缺 -race / vitest / golangci-lint / codegen 漂移门禁 | 中 | 📋 W-H |
| P2-10 | 其余低危（apikeys 注解失真、GetUploadSession 属主、时序均衡、iss/aud、file token in query 等） | 低 | 📋 W-L |

---

## 1. P0 修复（本次实施）

### P0-1 队列重复执行链路 ✅

**问题**（两轮一致复现）：
- `internal/infra/queue/redis_queue.go:23` `claimMinIdle = 100ms`——worker 4 个
  goroutine 共用 `hostname:pid` 同一 consumer 名，每轮 `Dequeue` 先 XAUTOCLAIM；
  任何执行超过 ~1.1s 的消息会被兄弟 goroutine 从 PEL 认领并**并发重复执行**（最多 4 份）。
- `internal/app/functions/executions.go` ProcessExecution 对 `rec.Status` 零检查：
  queued/running/**completed** 一律重跑；`UpdateExecution` 是无版本全字段覆盖
  （`function_repo.go:286`），并发写 last-write-wins，stdout/response 张冠李戴，
  `meterDuration` 重复计费。

**修复**（双保险）：
1. `claimMinIdle` 默认提升为 **15 分钟**（> 最坏在途时长：补构建 5min + 执行超时），
   改为包级 var 供测试覆写。PEL 认领回归"崩溃恢复"语义，不再参与常规重投。
2. ProcessExecution 加 **CAS 领取闸门**：
   - 新增 repo 方法 `TransitionExecutionStatus(from, to) (bool, error)`——
     `UPDATE ... SET status=to WHERE ... AND status=from`，按影响行数判定；
   - 消费入口先 `queued→building` CAS，0 行生效即判定为重复投递，静默跳过；
   - 可重试失败（补构建失败等）先 CAS `building→queued` 归还再返回错误，
     由 worker requeue 计数（maxProcessAttempts=3）兜底，超限走 failPayload；
   - 终态（completed/failed）永不被重复投递覆盖。
3. `MarkExecutionFailed` 改为 `FailExecutionIfActive`：仅当 status ∈
   {queued, building, running} 时置 failed，不覆盖终态。

**验收**：
- 单测：mock repo 上 ProcessExecution 对 completed/running 记录零执行；
  CAS 失败静默返回 nil；补构建失败后状态归回 queued。
- `internal/infra/queue` 测试显式覆写 claimMinIdle 后仍通过（重投语义保留）。

### P0-2 buildDeployment 信号量满分支删除既有 deployment ✅

**问题**：`internal/app/functions/deployments.go:87-91` 信号量满的 `default` 分支
执行 `DeleteDeployment` + `removeZip`。该清理只对 CreateDeployment API 路径
（行是本请求刚建）正确；worker 补构建路径（`executions.go:280-281`）复用同一函数，
对**既有** deployment 补构建时若恰逢 4 个构建槽满 → 永久删除 deployment 行与
代码包，瞬时限流造成持久数据丢失。

**修复**：从 `buildDeployment` 信号量满分支移除删除逻辑（只返回
ResourceExhausted）；删除决策上移给调用方——CreateDeployment 的错误分支已有
等价清理（幂等，行为不变）；worker 路径靠 P0-1 的"归回 queued + requeue 重试"
在信号量释放后重试，不再丢数据。

**验收**：单测——信号量占满时 CreateDeployment 仍清理自己的 pending 行；
worker 补构建路径 deployment 行与 zip 存活。

### P0-3 Functions HTTP 上传缺 owner/admin 角色门禁 ✅

**问题**：gRPC `CreateDeployment` 要求 owner/admin
（`pkg/grpc/interceptor/admin_roles.go:49`），HTTP
`POST /v1/server/functions/{id}/deployments/code` 的 `authorize`
（`functions_handler.go:180`）只拒 end-user；use-case 层
`RequireServerWriteActor` 不区分角色——三层防线无一复现角色要求。
viewer（只读角色）可上传 zip 触发 Docker 构建 = 项目内任意代码执行 +
环境变量读取。

**修复**：`authorize` 中对 `ActorKindAdmin` 补角色检查
`HasAnyRole([]string{"owner","admin"})`（照抄 `file_handler.go:790-794`
为 storage 写补检查的既有模式），viewer/member 一律 PermissionDenied。
API key 通道不变（scope `functions.write` 已校验）。

**验收**：与 gRPC 侧 admin_roles.go 的 CreateDeployment 要求逐字对齐；
end-user / API key / admin×(viewer,member,owner) 通道行为矩阵单测或集成断言。

### P0-4 Scoped 热路径迁移税 ✅

**问题**：`internal/infra/bun/bunrepo/project_table.go:56` 每次 repo 调用执行
完整 `projectschema.Apply`：1 事务 + advisory xact lock + 2 条 DDL + 2 条 catalog
查询 + `listMigrations()` 重读 embed FS——每次数据面读写固定 7 条语句开销；
写事务场景 advisory 锁持有到业务 COMMIT，同项目所有流量在 DB 层串行化
（估算写吞吐上限 30-200 tx/s）。session cookie 认证路径每请求一次。

**修复**：projectschema 增加进程内就绪缓存：
- `sync.Map`，键 = `{*clients.Database 指针, projectID}`——指针入键使测试进程内
  多套 testutil 库同 projectID 不互相污染；生产每进程一个 Database 实例；
- `Apply`（全量语义）在缓存命中时直接返回；applyUpTo 部分版本（拷贝测试）不缓存；
- 仅在**独立事务**成功提交后写缓存（`db.InTx(ctx)` 为真时跳过缓存写入，
  防外层事务回滚后缓存说谎）；
- 新增 `Invalidate(db, projectID)`，项目删除（DROP SCHEMA，`app/server/projects.go:162`）
  后调用，防"删除项目 → 缓存仍就绪 → 重建同 ID 项目时 schema 缺失"；
- 缓存失效天然正确性：迁移集是编译期 embed，新增迁移 = 新二进制 = 新进程。

稳态效果：GetSession 等热路径从 8 条语句回到 1 条，advisory 锁只在真正
需要迁移时出现。EnsureAll 启动路径负责预热缓存。

**验收**：migrator 集成测试——Apply 两次第二次直通（可用 hook/计数断言或
对比二次调用后 schema_migrations 无重复写入）；Invalidate 后 Apply 重建 schema。

---

## 2. P1 修复（本次实施）

### P1-1 outbox 领取事务化 ✅

**问题**：`outbox_worker.go:97-105` `For("UPDATE SKIP LOCKED")` 在自动提交的
单条 SELECT 上，行锁语句结束即释放——多副本会领到同一批行重复 PUBLISH
（当前被 Hub 5min event_id 去重巧合掩盖）。

**修复**：领取改为两段：
1. `RunInTx` 内 SELECT ... FOR UPDATE SKIP LOCKED + 同事务
   `UPDATE dispatched_at = NOW()`（领取即标记），COMMIT；
2. 事务外逐行 XADD（不持锁做 IO）。
`failRow` 重试分支增加 `dispatched_at = NULL` 归还，保持"失败后按
available_at 退避快速重试"的原语义（预标记不再挡住重试路径）。
崩溃语义不变：claim 后崩溃由 2 分钟 redispatch 窗口兜底。

### P1-2 outbox 已发布行清理 ✅

**修复**：OutboxWorker 增加低频清理 ticker（每 10 分钟）：
- `DELETE FROM document_events_outbox WHERE published_at < NOW() - 24h`
  （保留窗口供排障/重放核对）；
- 死信表 `document_events_outbox_dead` 保留 30 天后清理，删除量记日志。
单次 DELETE 不设 LIMIT（低频全量删，行数受保留窗口约束）。

### P1-3 dev_log 生产门禁 ✅

**问题**：`mailer.go:46` / `sms.go:45` 在 provider 未配置且开关开启时把含
验证码的完整 body `fmt.Printf` 到 stdout；`config.yaml.template:139-140`
默认 `true`；`TORCHWOOD_ENV` 缺省按 production 处理——生产误配即日志泄漏 OTP。

**修复**：
- Send 的 dev 分支前置检查 `config.CurrentRuntimeEnv() == EnvProduction`
  时返回错误（fail-closed：宁可 OTP 发送失败也不泄漏验证码）；
- 模板默认改为 `false`，注释说明仅 development 可用。
（不选择改构造函数签名——Send 时拦截已完整阻断泄漏路径，且避免 wire 重生成。）

### P1-4 消费组起始位 ✅（方案调整）

**原方案**：XGROUP CREATE 起始位 `0-0` → `$`（防组误删重建后重放全量历史）。
**实施后推翻**：`$` 会导致"server 先入队、worker 后首次启动"的部署顺序下
消息被静默跳过（组创建时 last-delivered-id 直接跳到队尾）——丢消息比重放
更糟。实施时回归测试捕获了该行为。

**定案**：保留 `0-0`；组重建重放的重复投递由 P0-1 的 CAS 领取闸门收敛为
幂等跳过（重复无害），重放风暴治理归属 W-F 的 stream 裁剪。
redis_queue.go ensureGroup 注释记录了该取舍。

### P1-5 docker 超时路径排空 wait 通道 ✅

**问题**：`docker.go` Execute 超时分支只 ContainerStop + `<-done`，不排空
`ContainerWait` 的无缓冲结果通道（docker client v28 内部 goroutine 阻塞发送），
每次超时执行泄漏一个 goroutine（含 HTTP resp）。

**修复**：Done 分支异步排空 `waitCh`/`errCh` 后再返回。

### P1-6 PruneOldExecutions 排除未终态 ✅

**修复**：DELETE 增加 `AND status IN ('completed','failed')`——queued/building/
running 记录永不被保留策略物理删除；积压时队列消息消费不再静默丢任务
（rec==nil → Ack 的吞消息路径被关闭）。

### P1-7 MarkExecutionFailed 防覆盖终态 ✅

随 P0-1 实施：`FailExecutionIfActive` 仅作用于未终态记录。

### P1-8 DB 连接超时显式化 ✅

**问题**：`database.go:80-83` 仅 WithDSN + WithBufferSize；pgdriver 隐式默认
ReadTimeout=10s 会在客户端杀掉长查询（大集合 CREATE INDEX）而服务端继续执行。

**修复**：显式设置 `WithDialTimeout(5s)`、`WithReadTimeout(60s)`（容纳索引
构建/迁移）、`WithWriteTimeout(10s)`、`WithApplicationName("torchwood")`。
不设全局 statement_timeout（会误杀控制面迁移与拷贝任务，风险大于收益，
W-H 中以 per-语句 ctx deadline 收敛）。

---

## 3. 后续工作流（📋 本次不做，含设计草案）

### W-A app→infra 依赖收口（架构 P2-1/P2-2）
分四步，每步独立可合入：
1. ✅ 错误映射收编（2026-08-22 完成）：`app/shared.MapUserError` 成为
   users 域错误映射唯一事实来源——删除 app/client 与 app/server 两份
   逐行复制的 mapUserError（9 处调用点改引 shared）；统一
   UpdateAccount/ConfirmEmailChange 两处 `errors.Is` 硬编码映射；
   占用检查的消息绑定 `users.ErrEmailAlreadyRegistered.Error()` 防字符串
   漂移；新增 table-driven 测试（含 wrapped sentinel 与未知错误透传）。
2. ✅ `*clients.Database` 构造参数改端口注入（2026-08-22 完成）：
   `server.NewUsers` / `assets.NewAssets` / `payments.NewPayments` 改收
   `uow.Runner`；`subscriptions` 的本地 `txRunner` 上收为 `pkg/uow.Isolator`
   （Runner + RunInNewTx，订单两段式语义保留），`NewSubscriptions` 改收
   `uow.Isolator`；4 个 app 文件删除 `infra/clients` import；
   `infra.ProviderSet` 与 `cmd/worker` 各加两个 `wire.Bind`（worker 不整包
   引入 infra.ProviderSet，按既有 bind 复制先例）。wire_gen 无 diff
   （具体类型变量直传接口参数，无需胶水）。users.go 的 `RunInTx` 调用改
   `Run`（tx.go 中 Run 直接委托 RunInTx，零行为变化）；
3. ✅ `app/server/projects.go` 的 DDL/清表 SQL 下沉（2026-08-22 完成）：
   `domain/projects` 新增 `SchemaManager` 端口（Ensure/DropCascade/Invalidate，
   infra/projectschema/manager.go 适配，事务感知）与 `Repository.DeleteProjectControlPlaneRows`
   （bunrepo 实现）；Projects 用例改持 `uow.Runner + SchemaManager` 端口，
   删除 `*clients.Database`/`infra/bun/model`/`infra/projectschema` 三个
   import 与本地 quoteIdent——app/server 包零 infra 依赖。
4. ✅ app 层 import 守卫测试（2026-08-22 完成）：`internal/app/import_guard_test.go`
   （AST 解析，复用 cmd/client 守卫模式）禁止非测试源码 import internal/infra；
   现存 6 个 client 文件以棘轮白名单锁定（只许缩减不许新增，条目带 W-A 待办说明）。

### W-B pkg/grpc/interceptor 迁 internal（P2-3）✅（2026-08-22）
`git mv` 至 `internal/grpc/interceptor`（保留文件历史）；9 个引用方机械
替换（api/realtime、serverhttp×4、app/server/apikeys、infra/server、
testutil、acceptance）；AGENTS.md 与 developer/tech-decision/roadmap
在世文档同步（implementation-*/archived 历史文档保留原文）；顺带修
`IsAPIKeysServiceMethod` 裸子串匹配 → 全限定服务段后缀精确匹配
（首版相等比较被既有测试当场纠正：服务段含包路径）。

### W-C documentdb/postgres.go 拆分（P2-4）
按端口能力拆：collection_ddl.go / document_crud.go / query_compile.go /
permissions.go / catalog.go（2597 行 → 5 个内聚文件，同包内拆分零调用方影响）。

### W-D ListDocuments 查询效率（P1-10/P1-11）
- N+1：`attachDocumentPermissions` 改单条
  `WHERE (collection, document) IN (...)` 批量查询；
- COUNT：ListResponse 增加按需计数（请求带 `count_total` 或
  `total_count` 仅在 offset 模式返回），或 `pg_class.reltuples` 近似；
- cursor：next token 编码 keyset 游标（sort 值 + _id），
  与首页 ORDER BY 同构，废除 cursor 模式下的 offset 混用。

### W-E fulltext 索引表达式对齐（P1-9）✅（2026-08-22）
- 建索引表达式改 `to_tsvector('simple', "col"::text)`，与查询编译
  （compilePredicate）逐字对齐——此前索引建 `to_tsvector('simple', col)`、
  查询编译 `col::text`，非 TEXT 列与多列索引永不命中，search 退化为
  全表逐行 to_tsvector；
- `validateIndexDefinition`：fulltext 索引限制单属性（多列拼接表达式与
  任何单字段查询都不匹配，索引形同虚设），CreateIndex 与
  CreateCollection 两个入口生效；存量 catalog 行不受影响（保留旧拼接
  分支兼容重建路径）；
- 修复 orders=["desc"] 时 fulltext DDL 拼出 `"col" DESC || ' '`
  语法错误的分支（GIN 忽略 order，用无序列）；
- search 查询校验保持宽进（字段 ∈ 任一 fulltext 索引属性集）：存量
  多列 fulltext 上的 search 仍可用（结果正确、seq scan），随存量索引
  重建自然消亡；
- 集成测试 TestCreateIndex_FulltextAlignment：search 往返 +
  `pg_indexes.indexdef` 断言表达式含 `(col)::text` + orders DESC 回归 +
  双入口多列拒绝。

### W-F 函数运行时并发与队列治理（P1-15/P1-16）
- Redis stream 周期 `XTRIM MAXLEN ~100000 APPROX`（worker 低频 ticker，
  与 P1-2 清理任务同框架）；
- run/build 信号量改全局配额：Redis 计数信号量（SETNX+TTL 租约）或
  环境变量配置的 Docker daemon 层限流，消除"每进程各 16"的乘法。

### W-G 实时与审计卫生（P2-5/P2-6）
- JWT AfterFunc 存 `*time.Timer`，cleanup 时 `Stop()`；
- serverhttp 的 logOp 升级为 audit.Repository.Insert（复用 gRPC 拦截器的
  Entry 结构），特权写操作获得持久审计轨迹。

### W-H 交付工程门禁（P2-9 + P1-8 收尾）✅ 主体完成（2026-08-22）
- CI：`go test -race ./...`（本地全量验证零数据竞争）、console vitest
  job（本地 4 文件 7 用例通过）、golangci-lint v2.12 棘轮门禁
  （`--new-from-rev=origin/main` 只拦新增，存量 78 项遗留债渐进烧；
  本地全量 `golangci-lint run` 可见全部）、codegen 漂移门禁
  （buf generate + config.proto + wire-all + `git diff --exit-code`，
  本地验证零漂移）、minio 服务镜像钉版（对齐 docker-compose）；
- Taskfile：`test` 依赖 `lint-go`（质量门不再是可选项）、新增
  `lint-golangci` 并入 `lint`、install-tools 补 golangci-lint 并将 buf
  对齐 CI 版本（v1.65.0）；
- 顺手修两个 lint 真发现：`outboxRedispatchAfter` 常量与 SQL 硬编码
  统一（fmt.Sprintf 引用常量）、functions_handler 死赋值 ctx；
- **残留**：覆盖率收集上报、outbox/subscriber/cleaner 后台循环的
  per-语句 ctx deadline、78 项存量 lint 债的渐进清理。

### W-I 独立加密密钥（P1-12）
config 增加 `security.encryption_key`（可选，缺省回退 jwt.secret 派生并
Warn）；TOTP/OAuth secret 加密改用独立 key 域派生（沿用
`jwtparser.DeriveKey` 前缀域分离模式，过渡期读旧值重加密）。

### W-J 事件重复/乱序收敛（P1-13/P1-14）
- Hub dedup 命中时刷新时间戳（消除 5min 窗口过期后的可见重复）；
- 经济事件信封补 `version` 字段（对齐文档事件），客户端可判序；
- 死信表增加重放 CLI（`torchwood admin outbox replay`）与 dead 计数告警。

### W-K API 契约治理（P2-7/P2-8）
- ListRequest.filter/order_by：12 个未实现端点要么实现（复用 pkg/crud）
  要么 proto 显式删除字段（reserved），消灭静默 no-op；
- client/server 重复 message 抽 shared 基底（Subscription/Group/Session/
  TokenBundle），字段号漂移用 buf breaking 检测兜底；
- apikeys 注解与执行对齐：proto 增加 console-session-only 语义的
  AccessLevel 或注释修正 + `assertRegisteredMethodsHaveAuthz` 扩展覆盖
  HTTP 手工路由。

### W-L 低危清单（P2-10）
sessionAsDocument 移除 secret_hash；IsSensitiveSystemCollectionID 接线或
删除；console 登录哑哈希时序均衡；GetUploadSession 校验属主；API key
校验检查项目 status；JWT iss/aud；file token 改 header/短 TTL；
audit IP 回退走 trusted-proxy；`databases.go:311` 双重 WithAuditResource；
console TS 类型 buf 生成；`.env` 中弱默认清理；仓库根二进制清理。

---

## 4. 验证与回归（2026-08-22 实施结果）

- **本次改动涉及的全部包**：queue / app-functions / messaging / worker /
  serverhttp / servergrpc / events / projectschema / bunrepo / acceptance —— 全绿；
- **全量 `go test ./...`**（2026-08-22 终态）：**65 个包全部通过，零失败**。
  此前记录的 5 个预存失败已全部处置（detached HEAD 定位到两个根因）：
  - **Databases 家族 3 个**（NextPageToken / Document_Increment /
    ReservedIDDocumentCRUD）+ EmptyPermissions 断言：`017fc4d` C1 改动的
    `read:__private__` 占位 ACE 不匹配任何常规角色且关闭集合回落，keys
    创建的文档自己读不回，Server CRUD 往返整体断裂。修订：占位 ACE 绑定
    创建者凭证角色（读写删，与 user: owner 分支同构），guest/any 仍剔除、
    C1 的防 guest 目标不变；系统推导 ACE 不受授予者校验约束。
  - **TestStorage_FileToken**：`1289922` A8/A9 有意移除文件文档权限回落
    （CreateFile 的 Permissions 不再落地，canAccessFile 只认 public/属主/
    特权）——陈旧测试前提，断言更新为"非属主非特权 users 角色签发受限
    文件 token 被拒"。
  - **TestPayments_WeChatULIDCallbackClosesOrder**：A2 校验要求
    `purpose.amount` 与订单金额相等——测试建单命令补 Purpose 载荷。
- **实施中的两个设计定案**（与原方案的偏差，均有代码注释）：
  1. P1-4 消费组起始位保留 `0-0`（`$` 会静默跳过 worker 首启前已入队的
     消息，重放危害已被 P0-1 闸门中和——详见 §2 P1-4）；
  2. 就绪缓存的契约边界：迁移器之外的**带外 schema 状态改动必须先
     Invalidate**（项目删除路径已接入；migrator 脏标记测试同步补了
     Invalidate 调用）。
- 不涉及 proto/wire 变更（无生成步骤）；未跑 Docker 门控测试
  （`TORCHWOOD_RUN_DOCKER_TESTS=1`）。
