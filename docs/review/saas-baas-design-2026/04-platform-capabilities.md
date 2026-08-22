# 04 平台原语：Storage / Functions / Realtime / Events / Worker / Messaging

> 日期：2026-08-22  
> 切片：`internal/app|domain|infra/{storage,functions}`、`internal/api/serverhttp/{file,functions}_handler.go`、`internal/api/realtime`、`internal/infra/{realtime,events,queue,messaging}`、`internal/domain/{events,messaging}`、`cmd/worker`  
> 产品：给 SaaS 团队用的 BaaS 原语（文件、函数、实时、后台作业、邮件/短信）  
> 方法：代码即真相。不引用 roadmap / first-principles 已拍板决策。词汇：模块、接口、深度、缝、适配器。  
> 读者：独立设计评审。不改代码。

每条发现分三类判断：**已够深**（接口小、行为多、缝落对）、**设计问题**（缝/深度/局部性错了）、**能力缺失**（SaaS BaaS 该有的原语代码里没有）。

---

## 0. 四条路径（代码追踪）

### 0.1 文件上传：multipart → bun 行 + S3 对象

两条入口，同一用例：

1. HTTP `POST /v1/storage/buckets/{bucketId}/files`（`FileHandler.upload`，[`file_handler.go:110`](internal/api/serverhttp/file_handler.go)）→ `Storage.CreateFile`。
2. Server gRPC `StorageService.CreateFile`（`/v1/server/storage/...`，[`storage.proto:80`](proto/server/v1/storage.proto)）把 `bytes data` 送进同一用例。

`CreateFile`（[`storage.go:212`](internal/app/storage/storage.go)）：校验 bucket 存在 → `files.Insert`（项目 schema bun 表）→ `store.Put`（对象 key `{projectID}/{bucketID}/{fileID}`）。Put 失败回滚删元数据行。物理对象落在**一个** S3/MinIO bucket（默认 `torchwood-files`），租户隔离靠 key 前缀，不是对象存储 bucket。

分片路径更深：`CreateUploadSession`（Redis 会话，24h TTL）→ `UploadChunk` 写 `{objectKey}/chunks/{part}` → `CompleteUpload` 互斥锁 + `Compose` + bun Insert + 删分片。

下载/预览走同一 HTTP 前缀；读鉴权是凭证 **或** HMAC file token **或** public bucket + `?project=`（[`file_handler.go:519`](internal/api/serverhttp/file_handler.go)）。

### 0.2 CreateExecution → 队列 → worker → docker

`FunctionsService.CreateExecution`（[`functions.proto:109`](proto/server/v1/functions.proto)）→ `Functions.CreateExecution`（[`executions.go:67`](internal/app/functions/executions.go)）：写 `function_executions` 行为 `queued`。`async=false` 在请求内 `executor.Execute`；`async=true` 把 `{execution_id,function_id,project_id,data}` LPUSH 到 `torchwood:queue:functions-executions`。

`cmd/worker` 的 `Worker`（[`worker.go:108`](cmd/worker/worker.go)）BRPOP，4 并发，调 `ProcessExecutionPayload`：必要时补构建 → `running` → `dockerExecutor.Execute`（每次 `ContainerCreate` + Start + Wait + `defer Remove`）→ 写回 completed/failed。瞬时失败最多重抛 3 次；超限 `MarkExecutionFailed`。启动时把滞留 `building/running` >1h 的记录标 failed。

部署 zip 写本地 `{TempDir}/torchwood-functions/{project}/{function}/{deployment}.zip`（[`deployments.go:22`](internal/app/functions/deployments.go)），**不**走 `ObjectStore`。构建在 `CreateDeployment` 请求内同步完成。

### 0.3 文档写 → outbox → realtime WS

`postgresDocumentDB.publishDocumentEvent`（[`postgres.go:1086`](internal/infra/documentdb/postgres.go)）在同一 `RunInTx` 里 `EventPublisher.Publish`。系统集合 nop。信封含写后/写前 ACL 快照。

`eventOutbox.Publish`（[`outbox.go:36`](internal/infra/events/outbox.go)）INSERT `public.document_events_outbox`。worker 的 `OutboxWorker` 每 200ms `FOR UPDATE SKIP LOCKED` 领取 → `RealtimeTransport.Enqueue`（Redis Stream `torchwood:realtime` XADD）→ 只标 `dispatched_at`。server 进程 `Subscriber` XREADGROUP → `Hub.Dispatch` → XACK → `published_at`。Hub 按集合频道 + 文档频道扇出，出站走 `ClientPayload()`（剥 acl），慢客户端丢帧。

经济事件共用同一信封（`Domain`/`Channel`/`Attrs`），只扇出 `accounts.{userId}`。

### 0.4 邮件/短信实际发什么

`Mailer.Send(to, subject, body)` / `SMSSender.Send(to, body)`。生产适配器：SMTP 明文、Twilio SMS。调用方**只有** `internal/app/client` 账户流：

| 场景 | 文件 | 内容 |
|---|---|---|
| 邮箱 OTP | `email_otp.go:77` | `"Your Torchwood sign-in code"` |
| 手机 OTP | `phone_otp.go:70` | `"Your Torchwood sign-in code is: …"` |
| Magic URL | `magic_url.go:79` | `"Sign in to Torchwood"` |
| 邮箱验证 | `verification.go:80` | `"Verify your Torchwood email"` |
| 密码恢复 | `recovery.go:76` | `"Reset your Torchwood password"` |
| 改邮箱确认/通知 | `account.go:492` / `510` | `"Confirm your Torchwood email change"` |

没有租户事务邮件 API、没有模板、没有 HTML、没有按项目 From。配置里的 `dev_log_otp` / `dev_log_sms` 把验证码打到进程日志。

---

## 1. 总判

这六块里，**文档 realtime 脊柱**和**分片上传**是真的深模块：小接口后面有事务、重试、ACL 快照、互斥、孤儿清理。其余平台原语是 **Appwrite 资源表面 + 单适配器**，不是 SaaS 团队能当积木用的 BaaS。

最大的错位：

1. **Storage 对端用户既不是干净的 Server-only，也不是完整的 Client 原语。** Client proto/SDK 没有 Storage；HTTP `/v1/storage/...` 已经接受端用户 JWT 直传。SaaS 标准路径（浏览器/App 直传、预签名、CDN）缺缝。
2. **Functions 的执行器缝落对了，产品动词只有「手动 CreateExecution」。** 没有 HTTP/cron/事件触发，冷启动每次新建容器，密钥是 PG 明文 env。对 SaaS 这是远程 `docker run`，不是 Functions 平台。
3. **异步有三条互不相认的脊柱**（Redis List 函数队列、PG outbox、Redis Stream 扇出），worker 进程是运维切分，不是模块缝。
4. **Messaging 不是产品原语**，是 Account 的 OTP 投递适配器；品牌写死 Torchwood。

一句话：Torchwood 能把文件存进 MinIO、把 zip 跑进 Docker、把文档变更推到 WS——这些路径实现充分。作为 **SaaS 的 BaaS**，缺的是端用户直传、函数触发器、租户配额、品牌邮件、可水平扩展的 realtime 扇出。这些不是「还没抄完」，是模块边界没按消费方切开。

---

## 2. 发现

### PC-1 Storage 的对象缝够深；权限接口是死的

**现状。** 三道真缝，调用方只认端口：

- `ObjectStore`（[`object.go:52`](internal/domain/storage/object.go)）：EnsureBucket / Put / Get / Delete / Compose / List / Ping。唯一生产适配器 MinIO（[`minio.go:16`](internal/infra/storage/minio.go)）。
- `BucketRepository` / `FileRepository`（[`repository.go:14`](internal/domain/storage/repository.go)）：项目 schema bun 静态表，文件行无 `_id`/`_perms`（[`files.go:11`](internal/infra/bun/model/files.go)）。
- `UploadSessionStore`（[`upload_session.go:28`](internal/domain/storage/upload_session.go)）：Redis TTL + complete 互斥锁。

对象 key `{project}/{bucket}/{file}`（[`storage.go:492`](internal/app/storage/storage.go)），全租户共用一个物理 bucket。

`CreateFileCommand.Permissions` 与 `UploadSession.Permissions` 被接收，但 `File` 结构体没有权限字段，`CreateFile` 不写入（[`storage.go:235`](internal/app/storage/storage.go)）。`CompleteUpload` 建行时同样丢弃会话上的 `Permissions`（[`uploads.go:219`](internal/app/storage/uploads.go)）。`bucketPermissions` / `filePermissions`（[`storage.go:496`](internal/app/storage/storage.go)）无调用方。`GetFile` / `DeleteFile` / `ListFiles` 的 `principal` 参数不参与判定（[`storage.go:260`](internal/app/storage/storage.go)）。读路径真正的门是：API key/admin/JWT、file token、或 `bucket.Public`（[`file_handler.go:519`](internal/api/serverhttp/file_handler.go)）。bucket 行上的 `permissions` JSONB 是死列。

**判断（设计问题）。** 元数据/字节分离是对的深模块。迁出文档引擎后，ACL 没接到静态表，只留下文档时代的函数和 bucket 列。对 SaaS，「这个文件谁能读」要么是 bucket.public 布尔，要么是持有 token 的人——没有文档级 `_perms` 那种接口。接口看起来深（带 principal），行为是「忽略 principal」。

---

### PC-2 图像管线 / CDN / 租户配额不是模块，是 handler 旁路

**现状。** `GET .../preview?width=&height=` 在 HTTP handler 里当场 `imaging.Decode` + `Fit`（[`file_handler.go:617`](internal/api/serverhttp/file_handler.go)）。源 ≤50MiB、边长 ≤8192，输出 ≤4096。无持久化变体、无缓存键、无 AVIF 预览（白名单有 `image/avif` 但不能 preview）、webp 输出降级 JPEG（[`file_handler.go:759`](internal/api/serverhttp/file_handler.go)）。下载 `io.Copy` 整对象，无 `Range`。`ObjectStore` 无 Presign / GetRange。

配额：HTTP 全局 `maxUploadBytes = 101MiB`（[`file_handler.go:124`](internal/api/serverhttp/file_handler.go)），分片上限约 156GB。`GetStorageUsage` 是 SUM。billing worker `sampleStorage` 把 `storage_bytes` **快照**进 Redis 小时桶（[`billing.go:90`](internal/app/billing/billing.go)），`CreateFile` / `UploadChunk` **不读**该指标，不拒绝超限。无 per-project / per-bucket 硬配额。

**判断（能力缺失）。** SaaS 文件原语的深度在「上传一次，读路径由平台变形、缓存、计费、限流」。现在变形钉在 origin 请求上，CDN/预签名不存在，计量与写入切断。要加这些，正确缝是 `ObjectStore` 扩展 Presign/Range，以及写入路径上的 Quota 端口——不是再往 `file_handler.go` 堆。

---

### PC-3 Client 无 Storage SDK，HTTP 却已接受端用户直传

**现状。** `proto/client/v1/` 无 storage。`sdk/go/client` 的 `Client` 只有 Account / Groups / Databases / Payments / Assets / Subscriptions（[`client.go:72`](sdk/go/client/client.go)）。Server SDK 有完整 Storage（[`sdk/go/server/storage.go`](sdk/go/server/storage.go)）。gRPC `StorageService` `default_access: ACCESS_API_KEY`（[`storage.proto:58`](proto/server/v1/storage.proto)）。

并行的自定义 HTTP 挂在 **`/v1/storage/...`**（不是 `/v1/server/...`）：

```109:119:internal/api/serverhttp/file_handler.go
func (h *FileHandler) Register(mux *runtime.ServeMux) {
	_ = mux.HandlePath("POST", "/v1/storage/buckets/{bucketId}/files", h.upload)
	_ = mux.HandlePath("GET", "/v1/storage/buckets/{bucketId}/files/{fileId}/download", h.download)
	// ... view / preview / uploads ...
}
```

`authorize` 不拒绝 `ActorKindEndUser`。集成测试用端用户 JWT 对项目 A 直传成功（[`file_handler_integration_test.go:335`](internal/api/serverhttp/file_handler_integration_test.go)）。`CreateFile` 用例也**没有** `RequireServerWriteActor`（只有 `CreateBucket` 有，[`storage.go:69`](internal/app/storage/storage.go)）。下载另有 file token（HMAC，1h/7d，[`storage.go:389`](internal/app/storage/storage.go)）和 public bucket。

对比 Functions HTTP：明确拒绝端用户（[`functions_handler.go:187`](internal/api/serverhttp/functions_handler.go)），路径在 `/v1/server/functions/...`。

**判断（设计问题 + 产品洞）。** 「密钥留在 SaaS 服务端」成立的是 **Functions 和 bucket 管理**。文件直传是端用户 App 的标准动作（头像、附件、录课）；强迫 SaaS 后端反代字节，是把平台税转嫁给租户。代码已经开了一条无 proto、无 SDK、无预签名的 JWT 直传——半开的 Client 表面。正确切法：Client 只拿到短寿命上传/下载凭证（presign 或现有 file token 的上传对偶），元数据 CRUD 留 Server；而不是「proto 说 Server-only、handler 说谁 JWT 都能 POST」。

---

### PC-4 Functions `Executor` 缝落对了；每次执行都是冷容器

**现状。** 端口只有三个动词（[`executor.go:28`](internal/domain/functions/executor.go)）：

```go
Build(ctx, functionID, deploymentID, zipPath) error
Execute(ctx, Execution) (*ExecutionResult, error)
RemoveImage(ctx, functionID, deploymentID) error
```

唯一适配器 `dockerExecutor`。`Execute`：拼 `TW_DATA` + env → `ContainerCreate`（CapDrop ALL、no-new-privileges、ReadonlyRootfs、tmpfs、pids/mem/cpu）→ Start → Wait → `defer ContainerRemove`（[`docker.go:215`](internal/infra/functions/docker.go)）。无容器池、无镜像预热、无 keep-warm。进程级信号量：构建 4、运行 16（[`executions.go:36`](internal/app/functions/executions.go)）。运行时白名单 node-18 / python-3.11，Dockerfile 由平台生成（[`docker.go:450`](internal/infra/functions/docker.go)），入口文件名探测，`Function.Entrypoint` 字段占位。

`Build` 的入参是**本地 zip 路径**，不是 `io.Reader` / object key。zip 落在共享本地盘（[`deployments.go:22`](internal/app/functions/deployments.go) 注释「单机部署假设」）。server 与 worker 必须看见同一 `TempDir`。`ObjectStore` 这条已有的字节缝，函数代码包不用。

**判断（已够深的执行器 + 设计问题）。** 安全基线与「zip→镜像→跑一次」收在适配器内，调用方只认 `Executor`——这是真缝。浅的是：`zipPath` 把文件系统泄漏进端口；无 warm 实例，冷启动 = 每次 docker create。SaaS 函数平台的杠杆在「请求来了就有实例」，不在「能跑通 alpine」。

---

### PC-5 函数产品只有一个动词：CreateExecution。触发器整层缺失

**现状。** `FunctionsService` 是函数/部署/变量/执行的 CRUD + `CreateExecution`（[`functions.proto:61`](proto/server/v1/functions.proto)）。没有：

- 公开 HTTP 触发 URL（无 `POST /v1/functions/{id}` 给端用户或 webhook）
- cron / schedule 表或 worker 扫描
- 文档/存储/支付事件触发（`ProcessExecution` 不订阅 outbox）
- 多部署流量切分以外的「最新 ready」选择

事件模块产出 `databases.documents.*` 与经济事件；functions 不是订阅方。worker 只 BRPOP 那一条 Redis List。

**判断（能力缺失）。** 对 SaaS 团队，Functions 的深度是「文档写入 / 定时 / HTTP 进来，平台负责调用」。现在 SaaS 后端必须自己持 API key 调 `CreateExecution`——等于租了 Docker。Server-only 调用来保护密钥是对的；缺的不是 Client `CreateExecution`，是**平台触发器模块**（接口：`Trigger{HTTP, Cron, Event}` → 同一 `CreateExecution` 内部路径）。

---

### PC-6 「密钥」是 `function_variables` 明文列

**现状。** `SetVariables` 全量替换，`******` 掩码回显（[`variables.go:12`](internal/app/functions/variables.go)）。仓储把 `value` 明文写入 `function_variables`（[`function.go:37`](internal/infra/bun/model/function.go)，[`function_repo.go:193`](internal/infra/bun/bunrepo/function_repo.go)）。执行时 `sanitizeEnv` 去掉带换行的 key，注入容器 `Env`（[`docker.go:249`](internal/infra/functions/docker.go)）。总量 32KiB，与 `TW_DATA` 争 execve 预算。无 KMS、无版本、无 rotation、无 secret vs config 区分。

**判断（能力缺失）。** 掩码是 API 卫生，不是密钥模块。SaaS 函数几乎总会放第三方 API key；明文 PG + docker env 是单租户脚本的接口，不是平台原语。缝应在 `Executor.Execute` 之前：`SecretResolver` 返回注入值，仓储只存引用。

---

### PC-7 Realtime 频道只覆盖文档和经济账户；Hub 不能水平扩

**现状。** 频道派发表只有两族（[`channels.go:32`](internal/api/realtime/channels.go)）：

- `databases.{db}.collections.{coll}[.documents.{doc}]`
- `accounts.{userId}`（订阅侧：本人或 platform admin）

握手：Client JWT 或 console cookie，禁 API key（[`handler.go:343`](internal/api/realtime/handler.go)）。配额：4 连/用户、32 订/连接。无 presence、无自定义频道、无 `files.*` / `functions.*`、无历史回放（Hub 满载丢帧，[`hub.go:128`](internal/infra/realtime/hub.go)）。

`Hub` 是**单进程内存** `map[channel]map[connID]*Conn`（[`hub.go:27`](internal/infra/realtime/hub.go)）。server `Subscriber` 用 Redis consumer group `torchwood-realtime`（[`subscriber.go:20`](internal/infra/realtime/subscriber.go)）XREADGROUP。组语义：每条消息只给组内**一个**消费者。多 server 副本时，WS 连在 A、事件被 B 的 Subscriber 领走，A 的 Hub 静默。正确扇出应是 pub/sub（每个 server 都收到），不是工作队列。

**判断（能力缺失 + 设计问题）。** 派发表是预留的真缝（加一族登记一行）。当前深度只够「订阅某集合/某文档/本人钱包」。SaaS 还要：在线状态、用户收件箱、房间、文件处理完成。更硬的是：realtime 的运输缝选错了适配器模式——outbox→Stream 作为**投递队列**合理，Hub 前一跳必须是广播。现在把 fan-out 做成了 competing consumer。

---

### PC-8 事件不是一个模块：信封超载，三条异步脊柱互不相认

**现状。** 「事件」散落：

| 位置 | 职责 |
|---|---|
| `domain/events.Envelope` | 文档事件 + 经济事件（`Domain`/`Channel`/`Attrs` 零值兼容，[`envelope.go:48`](internal/domain/events/envelope.go)） |
| `shared.EventPublisher` | PG outbox INSERT |
| `shared.RealtimeTransport` | Stream XADD |
| `shared.RealtimeFanout` / `RealtimeHub` | server 侧消费与订阅表 |
| `shared.Queue` | 仅函数执行 Redis List |
| `cmd/worker` OutboxWorker | 领 outbox |
| `cmd/server` Subscriber | 领 Stream |
| `cmd/worker` Worker | 领函数队列 |

文档写与支付/资产/订阅都 `Publish` 同一 outbox。Storage 写入、Functions 执行**不**发事件。`Queue` 端口通用，生产只有 `QueueFunctionsExecutions` 一个名字（[`ports.go:11`](internal/domain/shared/ports.go)）。函数失败重试靠 payload 内 `attempt`；outbox 失败靠 `attempts` + 死信表。两套至少一次语义，互不复用。

**判断（设计问题）。** 文档 outbox→WS 这一条够深（见 PC-13）。模块边界错在：用字段袋让经济事件搭便车，而不是 `Envelope` 作为「可扇出的事实」+ 各域自己的 payload。Queue 与 Outbox 是两个作业系统。SaaS 要「文件就绪触发函数、函数结果进 realtime」，今天必须在应用层手写，因为 Storage/Functions 根本不是事件生产者。

---

### PC-9 Messaging 是 Account 的投递适配器，不是品牌邮件原语

**现状。** 领域接口各一行：

```5:7:internal/domain/messaging/mailer.go
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}
```

适配器：SMTP（`Content-Type: text/plain`，From 默认 `noreply@torchwood.local`，[`mailer.go:106`](internal/infra/messaging/mailer.go)）与 Twilio。`proto/client` / `proto/server` 都没有「代表本项目发一封邮件」的 RPC。文案品牌写死 Torchwood（§0.4）。无 HTML、无模板 ID、无按项目 SMTP/From、无附件、无投递状态。SMS 同构，只服务手机 OTP。

**判断（能力缺失）。** 缝的位置对（`Mailer`/`SMSSender` 可换厂商），接口浅到只够验证码。SaaS BaaS 几乎总需要：欢迎信、账单、运营通知、租户自己的 From 域。把平台 OTP 和租户事务邮件挤在同一个 `Send(to,subject,body)` 上，租户无法白标，也无法审计。缺的是 Messaging **产品模块**（模板、身份域、投递日志），Account 只做其中一个调用方。

---

### PC-10 worker 是运维进程，不是模块缝；函数队列还是 at-most-once

**现状。** `cmd/worker` 一个二进制、七个 lynx Service（[`provides.go:109`](cmd/worker/provides.go)）：functions 队列、孤儿分片清理、outbox 领取、支付关单、资产过期、订阅扣款、用量 rollup。`import_guard_test.go` 禁止桶包和 Account/gRPC/documentdb，但仍显式装配 Functions + Storage + Payments + Assets + Subscriptions + Billing + Docker + MinIO + Stream 写入端。

函数队列：Redis List LPUSH/BRPOP（[`redis_queue.go:21`](internal/infra/queue/redis_queue.go)）。BRPOP 取出即离开 List；worker 在 `ProcessExecution` 中途崩溃 → 消息丢失，靠启动时 1h 孤儿扫描兜底标 failed（[`worker.go:59`](cmd/worker/worker.go)），**不会重放执行**。对比 outbox 用 SKIP LOCKED + Stream PEL + XAUTOCLAIM，至少一次。同一进程两种投递哲学。

**判断（设计问题）。** 把阻塞/轮询作业从 serving 进程拿开，是运维缝，该留。它没有变成模块缝：没有 `Job` 端口，没有统一 outbox，Functions 与经济作业只是共享进程。Queue 接口看起来可换，生产语义却是「最多一次 + 1h 后放弃」。SaaS 后台作业原语需要的是 outbox 那种至少一次；函数执行偏偏走了更弱的那条。

---

### PC-11 分片上传是本切片里最深的 Storage 子模块

**现状。** 会话预生成 `file_id`；非末片强制 16MiB；complete 前 `LockComplete`（SETNX 1h）再重读缺片；Compose 失败保留分片可重试；Insert 失败用 `IsLockOwner` 决定是否删最终对象（[`uploads.go:151`](internal/app/storage/uploads.go)）；abort 与 complete 抢同一把锁。worker 每小时扫 `LastModified > 48h` 且 key 含 `/chunks/` 的对象（[`cleanup.go:16`](internal/app/storage/cleanup.go)）。

**判断（已够深）。** 上传协议的复杂度收在 `Storage` 用例 + 两个端口（`ObjectStore.Compose`、`UploadSessionStore`）后面，HTTP handler 只做 multipart 与 JSON。这是该有的深度。它不能补 PC-2/PC-3：直传/CDN/配额仍缺。

---

### PC-12 文档写 → outbox → WS 是够深的 realtime 脊柱（仅限文档/钱包）

**现状。** 写路径与 outbox 同行事务（[`postgres.go:1082`](internal/infra/documentdb/postgres.go)）。256KiB 截断不回滚业务写。ACL 快照只在 outbox/Stream，不出站。`VisibleTo` 复用文档读语义（[`envelope.go:128`](internal/domain/events/envelope.go)）。Hub 按 event_id 去重 5 分钟。client SDK 实现了 hello/订阅/断线重订（[`sdk/go/client/realtime.go:1`](sdk/go/client/realtime.go)），明确不补历史。

**判断（已够深）。** 作为「集合变更流」模块，接口（频道名 + 信封）后面有事务、至少一次、ACL、截断、去重。深度到此为止：生产者只有 DocumentDB 与经济用例；运输层选错了组模式（PC-7）；Functions/Storage 不在这条脊柱上（PC-8）。

---

### PC-13 Server-only Functions 对密钥是对的；对 SaaS 仍是半成品原语

**现状。** 部署 HTTP 拒端用户（[`functions_handler.go:187`](internal/api/serverhttp/functions_handler.go)）。`CreateExecution` / `CreateFunction` / `SetVariables` 均 `RequireServerWriteActor`。Client proto/SDK 无 Functions。无公开 invoke URL。变量进容器 env（PC-6）。同步执行超时 capped 30s（[`management.go:26`](internal/app/functions/management.go)）。

**判断。** 端用户不能 `CreateExecution`、不能读 secret——这是对的（密钥与计费留在 SaaS 服务端）。产品洞不在「少一套 Client RPC」，在 PC-5（无触发器）+ PC-4（无 warm）+ zip 的单机磁盘假设。对比 Storage（PC-3）：Functions 的 Server-only 是干净的；Storage 的 Server-only 已经被 HTTP 戳穿且没有补上 presign。两者不要用同一句「v1 后端专用」概括。

---

## 3. 对照表（缝与深度）

| 模块 | 端口 | 生产适配器 | 深度 | SaaS 缺口 |
|---|---|---|---|---|
| Storage 字节 | `ObjectStore` | MinIO ×1 | 中（无 Presign/Range） | 直传、CDN、配额 |
| Storage 元数据 | bun repo | PG ×1 | 中（权限死列） | 文件 ACL |
| 分片上传 | `UploadSessionStore` | Redis ×1 | **深** | — |
| Functions 执行 | `Executor` | Docker ×1 | 中（zipPath、无 warm） | 触发器、密钥、HTTP |
| 函数队列 | `Queue` | Redis List ×1 | 浅（at-most-once） | 至少一次 |
| 文档事件 | `EventPublisher` | PG outbox ×1 | **深** | 让 Storage/Functions 成为生产者 |
| Realtime 运输 | `RealtimeTransport` | Stream ×1 | 中（组模式错） | 广播扇出、presence |
| Realtime Hub | `RealtimeHub` | 进程内存 ×1 | 浅（不可扩） | 多副本 |
| Messaging | `Mailer`/`SMSSender` | SMTP + Twilio | 浅（三字符串） | 模板、白标、产品 API |
| worker | 无 Job 端口 | 七个 ticker/循环 | 运维缝 | 统一 outbox |

---

## 4. 若按「给 SaaS 的 BaaS」收口，模块该怎么切

不在本文件施工。仅记录与代码对齐的切口：

1. **Storage：** 保住 `ObjectStore` + bun 元数据；给 `ObjectStore` 加 Presign/Range；写入路径接 Quota；Client 只发凭证不碰密钥；删掉死的 permissions 函数或接到真 ACL。
2. **Functions：** 保住 `Executor`；`Build` 改为吃 object key 而非本地路径；触发器作为独立模块喂 `CreateExecution`；Queue 换成与 outbox 同级的至少一次。
3. **Events：** 一个「可扇出事实」模块；文档/经济/文件/函数都是生产者；Hub 前一跳改广播。
4. **Messaging：** Account OTP 继续走浅 `Send`；另开租户事务邮件模块（模板 + From 域），不要把品牌写进 Account。
5. **worker：** 继续单进程多作业；不要假装它是模块边界。模块边界在端口（Executor / EventPublisher / ObjectStore / Mailer），不在 `cmd/worker`。
