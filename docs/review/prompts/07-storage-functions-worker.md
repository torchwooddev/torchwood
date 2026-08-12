# 审查任务：07 - Storage / Functions / Worker

## 角色

你是资深 Go 后端代码审查专家（对象存储、容器执行、消息队列领域）。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Storage / Functions / Worker」模块做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）、`docs/roadmap.md` §2.5/§2.6、`docs/implementation-storage-chunked-upload.md`、`docs/implementation-functions-executor.md`（分片上传与 Docker 执行器的设计偏离说明）
- 架构：`internal/app/storage` + `internal/app/functions` 为用例层；`internal/infra/storage`（MinIO/S3）、`internal/infra/functions`（Docker build/run）、`internal/infra/messaging`、`internal/infra/queue`（Redis List）为适配器；`cmd/worker` 消费执行队列（BRPOP、N=4 并发、孤儿对账）
- 能力：S3 上传/下载/预览缩略图、公开 bucket、HMAC file token（1h 默认/7d 上限）、分片上传（≤16MiB/part、≤10000 parts、24h TTL、ComposeObject 合并）、Docker 构建与同步/异步执行、execution 历史

## 审查范围

- `internal/app/storage/`、`internal/app/functions/`（全部 `*.go`，含测试）
- `internal/infra/storage/`、`internal/infra/functions/`、`internal/infra/messaging/`、`internal/infra/queue/`（全部 `*.go`，含测试）
- `cmd/worker/`：`worker.go`、`cleaner.go`、`main.go`、`provides.go`（含 `cleaner_test.go`）
- 交叉引用（只读）：`internal/domain/storage/`、`internal/domain/functions/`、`internal/domain/shared/`（端口）、`proto/storage/v1/*.proto`、`proto/functions/v1/*.proto`（如存在对应文件）

## 审查重点

1. **文件路径安全**：对象 key 的构造（bucket/file ID 是否允许任意字符、`..`、分隔符）、下载/预览时的 key 校验（防路径穿越读任意对象）；上传文件名是否影响存储路径。
2. **上传限制**：文件大小上限、multipart 总大小、Content-Type/魔数校验、预览缩放（50MiB 上限、webp 解码）、缩略图生成的内存/CPU 上限。
3. **分片上传**（对照 `docs/implementation-storage-chunked-upload.md`）：partNumber 校验（1..10000）、part 大小上限、upload session 的 TTL 与过期清理、complete 时 part 集合完整性（缺失 part 是否报错）、complete 互斥（防双 complete 重复合并）、abort 的资源清理（孤儿分片/临时对象）、断点续传的 ETag/校验和核对。
4. **File token**：HMAC 签名内容与密钥管理、过期与篡改检测（401）、token 的 bucket/file 绑定、预览与下载的 token 权限一致性、公开 bucket + `?project=` 参数匿名访问的正确性（`read:any` 兜底逻辑）。
5. **Docker executor**（`docker.go`，对照 `docs/implementation-functions-executor.md`）：zip 解压安全（zip bomb 总大小限制、slip 路径归一化、symlink）、Dockerfile 注入（运行时镜像白名单）、build 超时、run 的资源限制（CPU/mem/网络隔离）、容器清理（defer remove、超时 kill）、镜像缓存膨胀、stdout/stderr 大小截断、环境变量注入（secret 不打印）。
6. **执行队列与 Worker**：BRPOP 阻塞与并发消费（N=4）的正确性、任务丢失（Redis 崩溃/worker 崩溃）、execution 状态机（queued → running → success/failed）、孤儿对账（`cleaner.go`）逻辑、幂等处理、失败重试策略、死信处理。
7. **Messaging 适配器**：占位/真实实现的边界、发送失败的错误处理（对照 roadmap §3.3 属 P2 规划，评估现有代码质量即可）。
8. **用例层**：bucket/files 元数据 CRUD 的项目隔离、usage 统计聚合正确性、execution 历史的分页、变量（environment variables）的存储与读取脱敏。

## 通用检查项

1. 安全：路径穿越、zip bomb/slip、容器逃逸风险、信息泄露（日志打印 secret/代码内容）、上传文件作为可执行内容的风险
2. 错误处理：错误吞掉、部分失败的处理（合并/清理失败）、panic
3. 并发：队列消费竞态、complete 互斥、cleaner 与正常流程竞争
4. 性能：大文件内存加载、ComposeObject 拷贝、worker 并发度
5. 一致性：与端口签名、与设计文档一致；生成代码未手动修改
6. 测试：分片上传、token 过期、zip 异常、队列失败路径是否有测试

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：任意文件读写、容器逃逸、任务丢失、数据损坏
- 🟠 **P1 高**：功能缺陷、资源泄漏（容器/分片）、边界条件错误
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（安全边界、资源管理、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./internal/app/storage/... ./internal/app/functions/... ./internal/infra/storage/... ./internal/infra/functions/... ./internal/infra/messaging/... ./internal/infra/queue/... ./cmd/worker/...` 辅助检查
- 集成测试需要本地 MinIO/Postgres/Redis/Docker，**不要运行**；Docker 相关代码只做静态审查
