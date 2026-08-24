# Torchwood Round 4 审核汇总报告

> 日期：2026-08-24 ｜ 基线：`main` @ `3398a26`
> 标注约定：✅ = 主代理已亲自读源码复核确证；未标注 = 子代理报告原文（附置信度）。
> 级别定义：P0=安全漏洞/租户越权/数据丢失；P1=正确性缺陷/契约失真/可靠性隐患/发布断裂；P2=规范偏离/纵深缺失/可维护性；P3=改进建议。

---

## 总览

- **P0：0 项**。租户隔离、认证授权两条安全主线经攻击者视角审计未发现可利用漏洞。
- **P1：17 项**，全部列出如下；其余 P2/P3 按维度归档。
- Round 1–3 历史高危修复状态：**逐条代码级核验通过，无回退**（详见 §C 末尾对照表）。

### P1 清单

| # | 批次 | 一句话 | 位置 | 复核 |
|---|------|--------|------|------|
| 1 | J1 | config.yaml.template 为非法 YAML，照 README 快速开始复制即启动失败 | `configs/config.yaml.template:27-29` | ✅ |
| 2 | J1 | encryption_key「≥32 字符弱值拒绝启动」承诺未兑现，无任何强度校验 | `internal/pkg/config/crypto.go:14-16`、`cmd/server/provides.go:60-63` | ✅ |
| 3 | J1 | worker cleanup 注册进 OnStop，违反 server main 注释写明的自家不变量，威胁 outbox at-least-once | `cmd/worker/main.go:31-34` vs `cmd/server/main.go:51-54` | ✅ |
| 4 | J1/J7 | roadmap 状态落后代码一个身位（v3 经济系统已落地标"待实施"；Realtime 已交付标"规划中"） | `docs/roadmap.md:4,29,53` | 子代理 |
| 5 | J2 | OpenAPI 字段命名 camelCase 与运行时 snake_case 系统性不一致，Agent 按 schema 读响应字段全部落空 | `buf.gen.yaml:21` vs `internal/infra/server/errors.go:120` | ✅ |
| 6 | J2 | OpenAPI default 错误声明 rpcStatus，实际返回 shared.v1.ErrorResponse，错误通道机器可读不成立 | `genproto/**/*.swagger.json` vs `errors.go:46`、`grpc_gateway.go:45` | ✅机制 |
| 7 | J2 | proto3 optional 的 presence 在 handler→app 边界被丢弃，「设置空串=清空」静默 no-op 且返回 200 | `internal/api/servergrpc/users.go:103,112,115`、`internal/app/client/account.go:410,416` | ✅ |
| 8 | J2 | 分页 token 防护是死代码：HMAC 签名/绑定能力已建成零调用，文档声称存在防护；outbox 列表 offset 无上限 | `pkg/crud/pagination.go:87-123`（生产零调用）、`bunrepo/outbox_repo.go:34` | ✅ |
| 9 | J3 | sdk/go 对外分发链路断裂：replace 相对路径+伪版本，tag 已打但下游无法 go get；无 release job / CHANGELOG | `sdk/go/go.mod:5,9` | ✅ |
| 10 | J3 | SDK 无默认超时与重试，调用方忘传 deadline 即无限等待，Agent 长任务挂死风险 | `sdk/go/internal/conn/conn.go:17-22`、`sdk/go/server/client.go:89` | 子代理 |
| 11 | J3 | multipart 上传/分片/下载端点不在任何 swagger 中，roadmap §0 Agent-Native 验收标准第 1 条对大文件不成立 | `internal/api/serverhttp/file_handler.go:124-133` vs `storage.swagger.json` | 子代理 |
| 12 | J4 | 组装根寄生 infra 层：infra/server 反向 import 全部 5 个 api 子包，唯一系统性方向倒置 | `internal/infra/server/grpc.go:15-23`、`internal/infra/provides.go:85-87` | 子代理(高) |
| 13 | J4 | app/client/test_helpers.go 无 build tag，把 4 个 infra 包拖进生产编译图 | `internal/app/client/test_helpers.go:10-13` | 子代理(高) |
| 14 | J4 | OAuth 握手/微信 code 兑换/OTP 生成在用例层现场 new infra 实现，绕过 domain 端口 | `internal/app/client/oauth2.go:168,286`、`wechat.go:43`、`email_otp.go:69`、`phone_otp.go:62` | 子代理(高) |
| 15 | J5 | 限流 fail-closed 挂全量业务 RPC 且 healthz 豁免：Redis 抖动 = 全 API 面 500 而探活仍绿 | `internal/grpc/interceptor/ratelimit.go:106-110`、`infra/auth/ratelimit_redis.go:30-32` | 子代理(高) |
| 16 | J6 | CI 无覆盖率度量与阈值门禁 | `.github/workflows/ci.yml:94-99` | ✅ |
| 17 | J6 | 22 个 down 迁移全仓零验证（testutil 只 glob *.up.sql），生产回滚时才发现损坏 | `internal/testutil/db.go` runMigrations | 子代理(高) |

---

## A. 架构分层合规（7.5/10）

**亮点**：domain 层 16 子域零反向依赖（近乎教科书级纯净）；pkg/uow 19 行缝隙抽象 + 事务边界全落 app 层；fail-closed 启动断言文化（authz/scope/admin 角色/swagger 四重）；realtime handler 展示正确端口消费模式可作范本；三模块边界独立复测成立。

### P1

- **A-P1-1 用例层现场构造 infra 依赖**：见 P1 清单 #14。同文件中 `a.oauthState`/`a.mfa` 均走 `domainauth.*` 端口注入，唯 OAuth 握手链路例外。建议：domain/auth 定义 `OAuthAuthenticatorFactory`/`OTPGenerator` 端口，Wire 注入。置信度：高。
- **A-P1-2 test_helpers.go 污染生产编译图**：见 P1 清单 #13。设计文档 `saas-baas-design-2026/06-module-architecture.md:311` 已自认此债。建议：改名 `_test.go` 或加 build constraint。
- **A-P1-3 组装根错位**：见 P1 清单 #12。建议：`internal/infra/server` 迁出为 `cmd/server/runtime`（或 `internal/runtime`）。置信度：高。
- **A-P1-4 worker 关停漂移**：见 P1 清单 #3。查证 lynx v1.3.0 `lynx.go:404` 证实 OnStop 先于服务 Stop 执行。置信度：高。

### P2

- **A-P2-5 api 层直依赖具体 infra 类型**（11 处）：`serverhttp/auth.go:9,17` 收 `*auth.Validator`；`file_handler.go:26,62`；`functions_handler.go:15,42`；`health.go:7,13`；`users.go:12,246`。realtime 的窄接口模式是现成范本。
- **A-P2-6 SQLSTATE 映射错位**：`internal/app/shared/docdb_errors.go:12-34` 让 app 认识 pgdriver 结构。应下沉 documentdb adapter。
- **A-P2-7 app 层绑定 grpc/status**（56 文件）：务实取舍但需拍板——要么 AGENTS.md 显式合法化，要么引入 AppError 中间类型。
- **A-P2-8 Wire provider 构造适配器进 app**：`internal/app/functions/semaphores.go:21` new Redis 版实现。
- **A-P2-9 API Key scope 策略表寄居 interceptor 包**：`internal/grpc/interceptor/apikey_scope.go:25,157-172` 是全系统权限模型单一事实来源，却被 `app/server/apikeys.go:85`、`api/serverhttp/auth.go:40` 跨层消费（app→transport 倒挂）。建议迁 domain/auth 或 internal/pkg/authz。
- **A-P2-10 server/worker 装配样板复制粘贴已分叉**：`projectSchemaEnsureHook` 两处逐字重复（provides.go:143-161 ≈ worker/provides.go:129-147）；NewAppConfig server 校验 JWT 强度而 worker 不校验且无注释。
- **A-P2-11 上帝文件集中区**：`file_handler.go` ~900 行（含图像 preview 业务能力 :595-810）；`app/client/account.go` 26KB/21 依赖字段；`documentdb/postgres_collection_ddl.go` 32KB；`cmd/client/cmd/databases.go` 32KB。

### P3

- 队列 payload 契约靠注释镜像（`cmd/worker/worker.go:180-186`）；`pkg/semaphore` 绑定 go-redis（pkg 层混入微型适配器）；domain 空 ProviderSet 引 wire（`internal/domain/provides.go`）。

---

## B. 多租户隔离与数据面安全（8.5/10）

**八项审计结论**：ident 校验 ✅ 通过（`^[a-z][a-z0-9]{0,27}$` 全锚定、一/两段式命名空间可证明不相交）；动态拼接 ✅ 全部经 quoteIdent/白名单；RejectExternalDatabaseID 覆盖 ✅ 无漏网入口；project_id 来源 ✅ 写路径全部取自 Principal；连接级隔离 ✅ 全仓零 `SET search_path`、schema 限定名彻底；行级权限 ✅ SQL 层 EXISTS 谓词非内存后过滤。

### P2

- **B-P2-1 DeleteProject 不清理 MinIO 对象** ✅机制确证：`internal/app/server/projects.go:133-168` 仅级联删 DB 三层，全程无对象存储清理；对照 `app/storage/storage.go:172-212` 对象仅随单 bucket 删除清尾。影响：删除项目后文件字节永久残留共享桶（GDPR 类合规风险）。建议：事务提交后异步按 `{projectID}/` 前缀 purge。置信度：高。
- **B-P2-2 Functions 容器共享 bridge 网络** ✅机制确证：`internal/infra/functions/docker.go:245-247` 配置为空才回落 `none`，模板默认 `network: "torchwood-functions"`（template:91）——跨项目函数容器内网互达。建议 per-project network `tw-func-<project.id>`。置信度：中。

### P3

- `internalIDCache sync.Map` 只有 Load/Store 无失效，删除重建项目后旧实例以陈旧 internal_id 打 `_tenant` 标签造成静默数据分裂（完整性问题，非泄露）（`postgres_catalog.go:225-239`）。
- realtime 集合频道订阅不校验 read 权限，存在性 oracle（内容有逐事件 ACL 过滤兜底）（`realtime/handler.go:575-582`）。
- 孤儿 `tw_<p>_<db>` 物理 schema 对账缺失（需异常前提触发）（`projects.go:137-149`）。
- adapter 层 sentinel `_` 映射分支仍在，最后防线位于 use-case 层而非 adapter（加固项）（`postgres_catalog.go:185-196`）。
- 已认证但 ProjectID 为空的主体降级 GuestPrincipal 而非显式拒绝（`clientgrpc/databases.go:176-189`）。

---

## C. 认证授权与会话安全（8.0/10）

**未发现问题的重点核查项**：拦截器顺序与跳过名单合理；authz 注解全仓仅登录类+Health 为 PUBLIC 无误标；API Key 双 UUID ≈244bit 熵、仅存 SHA-256、禁自铸提权、过期吊销不泄存在性；X-Torchwood-Project 仅 admin 生效且必过 ValidateAdminProjectAccess；trusted_proxies 空=零信任贯穿六处入口；爆破防护 email 10/IP 30 每 15min + dummy-hash 时序均衡 + TOTP 重放窗口；refresh Lua CAS+重用检测全家撤销；`.env` 未入库；webhook 裸 body 验签失败一律 401。

### P2

- **C-P2-1 config.yaml.template 结构损坏**：即 P1 清单 #1/#2 同源问题（与 H 维度交叉发现）。实测 yaml.v3 解析报 `did not find expected key`。若运维删行救急则 access_ttl 失配回落代码默认 24h。
- **C-P2-2 encryption_key 校验缺口** ✅确证：`crypto.go` 直接返回显式值无校验；`secretbox` KDF 为单轮 sha256 无盐；模板注释宣称必填≥32 字符与 config.proto「empty 回退 jwt.secret」矛盾。拖库场景下低熵密钥可离线爆破 TOTP seed。

### P3

- console admin access JWT 默认 TTL 24h 且同值作 cookie Max-Age（`app/console/auth.go:264-267`）；对比端用户 15m。建议收紧 ≤1h。
- 公开消费口（改邮/找回/MagicURL）无独立频控，知道 project_id+user_id 可烧受害者待点击链接（`account_token_redis.go:134-138`）。
- 非 status 错误全文回显 err.Error() 至 HTTP 500 body（`errors.go:19-21`）。
- admin JWT 未绑定 admin 域密钥验签（`validator.go:109-115`，利用需 master 泄露，低危）。
- X-Torchwood-Project 多值取首值未拒（凭证头多值已拒，纯一致性缺口）（`interceptor/jwt.go:135`）。
- CORS 反射 origin 时 Vary 条件不足（`cors.go:32-46`）。
- KDF 风格三种并存（DeriveKey HMAC / secretbox sha256 / otp 前缀拼接），增加未来误用面。

### 历史评审回归核验（Round 1–3 → 本轮现状）

| 历史问题 | 现状 |
|---|---|
| R3-P1 viewer denylist 不完整 | ✅ 已修复（admin_roles.go 39 项 + AssertAdminRoleWriteCoverage 启动断言） |
| R3-P2 GETDEL 先烧后比 | ✅ 已修复（Lua 比对成功才 DEL，错码计数 5 次） |
| R2/R3 HTTP-gRPC 凭证策略漂移、弱密钥子串绕过 | ✅ 已修复（ParseAuthnRequest 统一） |
| R3-P3-4 会话 expire_at fail-open | ✅ 已修复 |
| R3-P3-8 session secret_hash 存明文 | ✅ 已修复 |
| R3-P3-5/P3-6/P3-7 低危项 | ❌ 未修（本轮 C-P3 归档，均低危） |

---

## D. API 契约与 Proto 规范（7.0/10）

**验证手段**：buf lint 通过；buf breaking --against origin/main 通过；proto 全量重生成与 genproto MD5 比对 byte-identical（生成物零漂移）。

**亮点**：时间字段 100% Timestamp；更新请求全面 proto3 optional；authz 注解体系闭环 fail-closed；错误模型单一事实来源 + error_id(uuid) 可追踪；异构分页中 keyset/复合游标做得扎实。

### P1

- **D-P1-1 presence 边界断裂**：见 P1 清单 #7。proto 注释承诺「设置（含空串）=更新/清空」，handler `Get*() != ""` 丢弃 presence；对照 `servergrpc/projects.go:87-92` 用指针是正确实现。二选一：改 Has*()/指针传递，或诚实化注释。置信度：高。
- **D-P1-2 OpenAPI 错误模型失真**：见 P1 清单 #6。建议 openapiv2 responses 统一覆盖引用 ErrorResponse 并入 grpc_swagger_test 断言。置信度：高。
- **D-P1-3 字段命名系统性不一致**：见 P1 清单 #5。推荐零成本方向：buf.gen.yaml 去 `json_names_for_fields=true` 让文档也用 snake_case（请求方向 protojson 双名兼容故现状无感，响应侧每字段都错）。置信度：高。
- **D-P1-4 page_token 防护死代码 + 文档谎言**：见 P1 清单 #8。`docs/developer/09-api-guide.md:120,131` 断言的 FilterDigest 校验不存在于任何生产路径。短期接线（能力已存在），长期静态表迁 keyset。置信度：高。

### P2

- 09-api-guide.md 权威分页示例会被服务端 400 拒绝（storage ListBuckets rejectListFilterOrderBy，guide:136-138 vs storage.go:54）。
- grpc_swagger_test 四盲区：security/securityDefinitions 不校验；无「accessOf 方法必须出现在 swagger」反向覆盖率断言（RPC 缺 http 注解会静默消失）；findMethodByOperationID 数字截断回退可能误配；path/method/body 一致性不校验（grpc_swagger_test.go:142-194）。
- ListLogs 分页契约离群（int32 limit 无 page_token/meta，account.proto:753-755）。
- total_count keyset 下 0=未知与空集合不可区分，proto 字段零注释（common.proto:19 vs postgres_document_query.go:154-157）。

### P3

- storage.proto permissions 用 deprecated 而非 reserved（规范字面偏离，效果等同甚至更兼容）；entities.proto Subscription 跳号 11/12/15 无 reserved 声明；状态字段自由 string 无 enum 约束（ENUM_VALUE_PREFIX 全局豁免放大）；CountDocuments 复用 ListDocumentsRequest 使 :count 在 OpenAPI 暴露无效分页参数；console ListAdmins 无分页。

---

## E. 可靠性与运维（8.0/10）

**E1 正面结论（研究报告式核实）**：

- Outbox 写入与业务同事务 ✅（tx-aware `Conn(ctx)`：events/outbox.go:67-72，文档 CRUD/支付回调/退款/订阅四类生产者均在 uow 内 Publish）；
- Relay 用 `FOR UPDATE SKIP LOCKED` 领取 ✅（outbox_worker.go:140-150，多副本不重复领取），指数退避上限 60s、attempts≥10 死信迁移、清理 24h/30d、replay 幂等（CONFLICT DO NOTHING）均有集成测试；
- projectschema 迁移有 `pg_advisory_xact_lock(hashtext)` 按项目键控防多副本竞争 ✅（migrator.go:79-87）+ dirty 拒绝；
- 删除项目四步（业务库→public 行→tw_p→projects 行）同一事务提交，部分失败整体回滚天然幂等 ✅（projects.go:137-160，有测试锁死行为）；
- spec 中「按项目批量 replay」明确 Out of Scope，一致。

### P1

- **E-P1-1 限流 fail-closed 全局化**：见 P1 清单 #15。Redis 故障时全 API Internal，healthz/reflection 豁免掩盖故障。建议熔断降级或本地令牌桶兜底，并把基础设施错误与 ResourceExhausted 分开观测。置信度：高。

### P2

- Redis 是登录/MFA/OTP/refresh/admin 会话的同步强依赖（五处 err→Internal 上抛），fail-closed 取向安全正确但无降级预案与运维契约（login_throttle_redis.go:63-64 等五处）。
- legacy offset token 无上限钳制：documentdb 有 maxQueryOffset=10000 兜底，但 `bunrepo/outbox_repo.go:34` 直用客户端 offset 可伪造深翻页打爆连接池（池仅 4×GOMAXPROCS）。
- 连接池参数 time.ParseDuration 解析失败静默跳过（clients/database.go:97-101），配错单位即静默用无限寿命。

### P3

- redis.NewClient 无 DialTimeout/PoolSize，addr 缺失静默回退 localhost:6379 报错不带 env 提示（对比 database source 的范本报错）；nil loginThrottle/mfaChallenges 即旁路（当前组合根恒注入，属埋雷写法）；semaphore release Eval 无超时；broadcast 模式每事件每副本单行 UPDATE 写放大（subscriber.go:117-123）。

**资源泄漏三项扫描均干净**：time.After 仅出现在退避 select 且与 ctx 并联；7 处出站 HTTP 全部 defer Body.Close；realtime timer/cleanup/stopOnce 生命周期严谨。上传 MaxBytesReader + gRPC MaxRecvMsgSize 8MiB + limit clamp 100 + keyset 游标多层设防。

---

## F. 测试质量与 CI 门禁（7.5/10）

**分布**：≈1213 个测试函数（app 363 / infra 372 / api 98 / domain 88 / sdk 78 / grpc 54 / cmd 52 / pkg 91）。断言普遍精确（gRPC code + 字段级 require）。集成测试无 docker 快速失败而非假绿；CI 有 race/buf/codegen drift/console embed/TS SDK 独立 job。

### P1

- CI 无 coverage 度量与阈值（ci.yml:95,99 仅 `-race`；Taskfile 本地有 -cover 无阈值且缺 -race 与 CI 不一致）。
- 22 个 down 迁移零验证（testutil runMigrations 只 glob *.up.sql）。
- golangci 仅 standard 五件套，gosec/bodyclose/sqlclosecheck/noctx 缺失；`--new-from-rev` 棘轮存量豁免现已清零（43→0）应收紧为全量。

### P2

- token 过期测试真实 sleep 1.1s×2 处（TTL=1s 余量偏小，负载下 flaky）；realtime/health 固定 sleep 做 goroutine 同步（60-200ms×多处）。
- functions docker e2e 本机默认 skip 显示 ok（局部虚假绿灯；CI 有开关真跑）。
- domain 五子域（audit/billing/functions/messaging/idgen）零测试。
- import_guard_test 非递归两层扫描，新增子包即绕过（当前平铺结构下有效）。
- testutil fixture 失败 panic 而非 t.Fatal、cleanup 吞错。

### P3

- 每测试建库+重放 22 迁移偏重（隔离性极强但分钟级成本，可评估 template database）；表驱动占比低；sdk invoke_test `count > 60` 下限过松（实际 ~112，可悄悄删 19 个方法仍绿）。

---

## G. SDK / CLI / Agent-Native（7.0/10）

**亮点**：InvokeJSON 基于 dynamicpb+protoregistry 动态分发，新 RPC 零登记自动可用（全仓最聪明的设计决策）；APIKeysService 被 findServerMethod 与 Tools catalog 双测锁定排除（威胁建模到位）；Client SDK token 生命周期工程完整（skew 主动刷新+被动重试+并发去重+FileTokenStore 0600 原子写）。

### P1

- 发布链路断裂：见 P1 清单 #9。CI 无 release job、无 CHANGELOG。建议发布流水线改写 require→真实 tag 再打 module tag。
- SDK 无默认超时/重试：见 P1 清单 #10。CLI 自己包了 30s（output.go:29）SDK 层没有。
- OpenAPI 与存储传输面脱节：见 P1 清单 #11。roadmap.md:40 明确验收「仅凭 API Key+OpenAPI 完成上传文件」。建议 HandlePath OpenAPI overlay 或手写片段合并。

### P2

- 方法覆盖完整性测试保证范围有限：invoke_test 遍历 registry 断言 findServerMethod 可解析（两者同源几乎恒真），真正拦的是白名单回归与 APIKeysService 泄漏；类型化封装（c.Users.CreateUser 等）无测试强制；下限 >60 过松。
- 文档示例编译失败级不符：12-sdk.md:196 `ListDeadLetters(ctx,"default",20,"")` vs 实际 `(ctx, req)`。
- CLI 401 无自诊断提示（仅 PermissionDenied 有 scope 提示）；outbox help 引用不存在的 `--project` flag。
- 退出码无区分度（统一 exit 1）、输出仅 json 无 table。
- 限流无 RetryInfo detail（全仓无 WithDetails 使用）；SDK 无结构化错误包装。
- 类型化封装风格分裂：9/13 服务透传 request 对象迫使用户 import genproto。
- 缺分页迭代器/storage 传输 helper/泛型 marshal；tool catalog 无 JSON Schema、无 MCP（roadmap 诚实标 P3）。

### P3

- RPC 计数漂移（文档 185 vs 实测 rpc 语句 187/swagger 操作 192）；import_guard 非递归（同 F）；protojson detrand 输出冒号后空格随机；version/commit/date 弃用（cmd/client/main.go:13-18）。

---

## H. Console 前端与文档一致性（8.0/10）

**亮点**：认证流四方逐字吻合（cookies.go/client.ts/AGENTS.md/docs 05§7）；XSS 面干净（零 dangerouslySetInnerHTML、CSP script-src 'self'、blob 预览 revoke、WS URL 仅由 location 构造）；TanStack Query 纪律好（queryKey 带 projectId 作用域、角色 fail-closed 双重 gating）；06-databases.md 与物理 schema 000001~000009 精确一致。

### P1

- config.yaml.template YAML 损坏：见 P1 清单 #1（C/H 两维度交叉独立发现并实测解析失败）。
- roadmap v3 经济系统状态滞后：见 P1 清单 #4。§0 Realtime 同样滞后（roadmap.md:29 vs internal/api/realtime/ 已交付）。

### P2

- Console 静态服务无 Cache-Control 策略 + SPA fallback 把缺失资源当 HTML 200（部署后白屏风险）（infra/server/console.go:18-31,41-47）。
- databases/pages.tsx 单文件 1704 行承载 9 个页面组件（前端最明显维护热点）。

### P3

- CI lint 棘轮注释过期（称存量 78 项，实际已清零）；functions queryKey 列表/详情共用形状语义碰撞；UI 中英混排无 i18n；console/dist 不入库且 embed 无占位（新克隆直连 go build 必失败，CI 靠 touch 打桩自证）。
