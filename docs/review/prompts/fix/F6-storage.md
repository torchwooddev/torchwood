# 修复任务 F6：Storage 修复

## 角色

你是资深 Go 后端工程师（对象存储领域），负责修复 Torchwood Storage 模块的审查发现。
方案详见 `docs/review/fix-plan.md` §6（F6 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`、`docs/review/fix-plan.md` §6、`docs/implementation-storage-chunked-upload.md`
- 审查报告（背景）：`docs/review/` 下的 07 报告

## 修复清单

1. **complete 互斥锁 TTL 短于长 Compose**（P1）：
   - 位置：`internal/infra/storage/redis_upload_session.go:24`（completeLockTTL=5min）、
     `internal/app/storage/uploads.go:166-199`（Lock/Unlock/Compose）、
     `:213-220`（建文档失败回滚删最终对象）。
   - 修复：锁 TTL 提至 1h（宕机自愈成本可接受）；回滚删对象前确认
     「自己仍是锁持有者 + 会话仍存在」双重条件（目前仅查会话存在，锁过期后
     第二个 complete 会先建文档成功，第一个 complete 回滚会误删对象 → 数据损坏）。
2. **Preview 解码无像素级防线**（P1）：
   - 位置：`internal/api/serverhttp/file_handler.go:571,624-635`（maxPreviewSourceBytes
     只限压缩大小，imaging.Decode 整图解压，50MiB 图可解码出 ~600MB 位图，公开桶匿名
     可反复触发 → OOM DoS）。
   - 修复：解码前用 `image.DecodeConfig` 读取宽高，任一维度超过上限（如 8192）直接拒绝；
     输出改流式编码（w 直接作为 Encoder 目标，避免整图 bytes.Buffer）。
3. **P2 补强**：
   - DeleteBucket 不删 files 元数据（`internal/app/storage/storage.go:150-184`）→
     按 bucket_id 过滤删除文件文档（分页循环 DeleteDocument）；`store.Delete` 失败
     记录日志（目前 `_ =` 吞掉）。
   - UploadChunk 缺 EnsureBucket（`uploads.go:138-141`）→ CreateUploadSession 或
     UploadChunk 补一次 EnsureBucket（对齐 `storage.go:228-230`）。
   - 默认 bucket 名大小写不一致（`storage.go:484-490` 大写 vs `internal/infra/storage/minio.go:45-49`
     小写 vs `minio.go:126-131` 大写）→ 统一小写 `torchwood-files` 单一常量。
   - upload session 无 owner 绑定（`uploads.go:111-146` UploadChunk 的 principal 未使用、
     `:150` CompleteUpload 用调用方作 owner）→ 会话增加 OwnerUserID 字段，
     UploadChunk/Complete/Abort 校验调用方身份（admin/keys 豁免按权限模型处理）。
   - file token 与 JWT 共用密钥（`storage.go:414,427`）→ 独立 purpose key
     （参考 `pkg/jwtparser.DeriveKey` 模式，如 "file-token:" 前缀）。
   - 私有文件下载/预览无 Cache-Control（`file_handler.go:497-507`）→ 鉴权路径设
     `Cache-Control: private, no-store`（public bucket 匿名路径保持 public）。
   - 公开桶匿名读的 bucketID DSL 字符串拼接（`file_handler.go:538-541`
     `"equal(\"$id\",\"" + bucketID + "\")"`）→ 用 `query.BuildEqual("$id", bucketID)`
     或先校验 bucketID 格式。

## 约束

- **不要**改 `internal/api/serverhttp/functions_handler.go` 的鉴权（F2 批次负责）；
  file_handler.go 本批次可以改（preview/缓存头/DSL 拼接三处）
- 不修改 proto（upload session owner 字段若需落库进 files 文档，用文档字段而非 proto）
- 保持现有代码风格；不引入新依赖；除必要外不新增注释
- 不运行需要本地 MinIO/Redis 的集成测试

## 验证

- `go vet ./internal/app/storage/... ./internal/infra/storage/... ./internal/api/serverhttp/...`
- `go build ./...`
- 为 file token 密钥分离、DSL 拼接、owner 校验补单元测试（不依赖 MinIO 的部分）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；明确标注需 MinIO/CI 验证的项。
