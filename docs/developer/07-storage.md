# Torchwood 存储子系统（Storage）

> 本文描述 Torchwood 的对象存储适配、multipart 上传下载、**分片上传（upload session）**、预览缩略图、公开 bucket 匿名读、HMAC File Token、元数据更新与 Usage 统计。
> 相关代码：`internal/domain/storage/`、`internal/infra/storage/`、`internal/app/storage/`、`internal/api/serverhttp/file_handler.go`、`proto/server/v1/storage.proto`。

---

## 1. 架构总览

```
HTTP multipart (serverhttp.FileHandler) ──→ app/storage.Storage
                                              ├─ 元数据：bun 表 tw_<project>.buckets / tw_<project>.files（projectschema）
                                              └─ 对象数据：storage.ObjectStore（S3/MinIO，minio-go）
gRPC StorageService (proto/server/v1/storage.proto) ──→ app/storage.Storage
```

- **元数据与对象分离**：bucket / file 元数据是项目数据面 bun 静态表 `tw_<project>.buckets` / `tw_<project>.files`（`internal/infra/projectschema/`，仓储 `BucketRepository` / `FileRepository`），**不是** default 库文档集合，也**没有**文档 `_perms`。文件二进制存入对象存储。
- **对象键**：`<projectID>/<bucketID>/<fileID>`（`objectKey`），全部文件落在同一个 S3 bucket（`storage.s3.bucket`，未配置时回退 `torchwood-files`）。

### 1.1 端口与适配器

`internal/domain/storage/object.go` 定义 `ObjectStore` 端口：

| 方法 | 说明 |
|------|------|
| `EnsureBucket` | 底层 S3 bucket 不存在则创建（`MakeBucket`，region us-east-1） |
| `Put` | 上传对象（缺省 `application/octet-stream`） |
| `Get` | 下载对象（Stat 校验存在，NoSuchKey → not found 错误） |
| `Delete` | 删除对象 |
| `Compose` | 按序服务端合并 srcKeys 为 dstKey（映射 minio-go `ComposeObject`；除末片外每片 ≥5MiB、源数 ≤10000；目标对象 Content-Type 无法设置，mime 以文件元数据为准） |
| `Ping` | 健康探测 |

`internal/infra/storage/minio.go` 是唯一实现：`NewMinioObjectStore` 从配置 `storage.s3.*` 构造 `minio.Client`（静态 V4 凭据；endpoint 带 scheme 时自动解析 https 与主机名；`use_ssl` 可被 https scheme 覆盖）。

---

## 2. 配置项

| 配置路径 | 环境变量 | 默认值 | 说明 |
|----------|----------|--------|------|
| `storage.provider` | — | `s3` | 对象存储提供商 |
| `storage.s3.endpoint` | `TORCHWOOD_STORAGE_S3_ENDPOINT` | 空 | MinIO/S3 地址 |
| `storage.s3.region` | `TORCHWOOD_STORAGE_S3_REGION` | `us-east-1` | |
| `storage.s3.bucket` | `TORCHWOOD_STORAGE_S3_BUCKET` | 模板 `torchwood-storage`；未配置时代码回退 `torchwood-files`（`internal/domain/storage/object.go` 的 `DefaultBucketName`） | 承载全部对象的 bucket（S3/MinIO 要求全小写） |
| `storage.s3.access_key_id` | `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` | 空 | |
| `storage.s3.secret_access_key` | `TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY` | 空 | |
| `storage.s3.use_ssl` | `TORCHWOOD_STORAGE_S3_USE_SSL` | false | |
| `storage.local.path` | — | `./data/files` | local 提供商占位（未实现） |

> 配置 schema 见 `internal/pkg/config/config.proto`（`Storage.S3`），环境变量映射见 `internal/pkg/config/bind.go`（`storage.s3.*` → `TORCHWOOD_STORAGE_S3_*`）。

---

## 3. Bucket 与 File 元数据（bun 静态表）

表在 `tw_<project>`（`internal/infra/bun/model/{buckets,files}.go`），无 `_id` / `_perms` / `_version`。

`buckets` 列：`id`（PK）、`name`、`permissions`（JSONB，调用方传入的字符串数组，**不是**文档 `_perms`）、`public`、`created_at`、`updated_at`。
`files` 列：`id`（PK）、`bucket_id`、`name`、`mime_type`、`size`、`metadata`（JSONB）、`owner_user_id`、`created_at`、`updated_at`。文件没有文档级 ACE；归属用 `owner_user_id`。

CreateBucket / CreateFile **不**再合成默认 `read:any` 等文档权限；`permissions` 仅 bucket 列按请求原样写入。鉴权在 Server API scope / HTTP `authorize`（凭证、File Token、公开 bucket 的 `public` 标志）。

MIME 归一化（`normalizeMimeType`）：空值及危险类型（`text/html`、`application/xhtml+xml`、`application/javascript`、`text/javascript`、`application/xml`、`text/xml`，取分号前 base 判断）一律改判 `application/octet-stream`，防止存储型 XSS 经 `/view` 端点内联执行。

---

## 4. API

### 4.1 gRPC `StorageService`（Server API，`/v1/server/storage/...`）

| 方法 | 路径 |
|------|------|
| `CreateBucket` | `POST /v1/server/storage/buckets` |
| `ListBuckets` | `GET /v1/server/storage/buckets` |
| `GetBucket` | `GET /v1/server/storage/buckets/{id}` |
| `UpdateBucket` | `PATCH /v1/server/storage/buckets/{id}`（name / public） |
| `DeleteBucket` | `DELETE /v1/server/storage/buckets/{id}`（先分页删除该 bucket 全部文件对象，再删元数据） |
| `CreateFile` | `POST /v1/server/storage/buckets/{bucket_id}/files`（gRPC body `data` bytes） |
| `ListFiles` | `GET /v1/server/storage/buckets/{bucket_id}/files`（queries / page_size / page_token） |
| `GetFile` | `GET /v1/server/storage/buckets/{bucket_id}/files/{file_id}` |
| `UpdateFile` | `PATCH /v1/server/storage/buckets/{bucket_id}/files/{file_id}`（name / mime_type / metadata 整体替换） |
| `DeleteFile` | `DELETE /v1/server/storage/buckets/{bucket_id}/files/{file_id}` |
| `CreateFileToken` | `POST /v1/server/storage/buckets/{bucket_id}/files/{file_id}/tokens` |
| `GetStorageUsage` | `GET /v1/server/storage/usage` |

### 4.2 HTTP multipart / 文件流（`internal/api/serverhttp/file_handler.go`）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/storage/buckets/{bucketId}/files` | multipart 上传，字段名 `file`；请求体上限 **100 MiB + 1MiB 缓冲**（`maxUploadBytes`），`ParseMultipartForm(32MB)` 后读取 |
| `GET` | `/v1/storage/buckets/{bucketId}/files/{fileId}/download` | 下载（`Content-Disposition: attachment`） |
| `GET` | `/v1/storage/buckets/{bucketId}/files/{fileId}/view` | 浏览器查看（安全 MIME 内联，见 §5） |
| `GET` | `/v1/storage/buckets/{bucketId}/files/{fileId}/preview` | 图片缩略图（见 §5） |

### 4.3 分片上传（upload session）

> 设计文档：`docs/implementation-storage-chunked-upload.md`（含与 roadmap 端点占位的显式偏离说明）。

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/storage/buckets/{bucketId}/uploads` | 创建会话，JSON body `{name, mime_type, size, metadata?, permissions?}`（1MiB 上限）；201 返回 `{upload_id, file_id, chunk_size, part_count, expires_at}` |
| `GET` | `/v1/storage/buckets/{bucketId}/uploads/{uploadId}` | 查询会话（断点续传）：`{upload_id, part_count, received: [1,3,...], chunk_size}`；**GET 分支要求 `storage.read` scope** |
| `POST` | `/v1/storage/buckets/{bucketId}/uploads/{uploadId}/chunks/{partNumber}` | 上传分片，multipart 字段名 `chunk`（16MiB 整片 + 1MiB 缓冲上限）；同号覆盖 = 幂等；成功不记 logOp |
| `POST` | `/v1/storage/buckets/{bucketId}/uploads/{uploadId}/complete` | 合并分片并写入 files 元数据（mime 已归一化）；缺片 400 |
| `DELETE` | `/v1/storage/buckets/{bucketId}/uploads/{uploadId}` | 取消上传：删会话 + 清理暂存分片对象；204 |

**约定**：

- 常量（`internal/domain/storage/upload_session.go`）：`DefaultChunkSize = MaxChunkSize = 16MiB`、`UploadSessionTTL = 24h`、`MinComposePartSize = 5MiB`、`MaxComposePartCount = 10000`、`MaxUploadSize ≈ 156.25GB`；
- 分片大小严格校验：非末片 **== chunkSize**（保证 sum(parts)==size），末片 `1..chunkSize`（可 < 5MiB，5MiB 约束仅对非末片，由 ComposeObject 兜底）；
- 会话存 Redis：`torchwood:upload:{id}` Hash（metadata/permissions JSON）+ `:parts` Set，Create/MarkChunk 刷新 24h TTL（MarkChunk 前 EXISTS 检查防孤儿 key）；
- complete 互斥：`SETNX torchwood:upload:{id}:lock EX 300`，重复 complete → FailedPrecondition（HTTP 400）；时序 Lock → 缺片校验 → Compose → 写 files 行 → 删分片 → 删会话 → Unlock；
- 分片对象 key：`{projectID}/{bucketID}/{fileID}/chunks/{part:03d}`；
- 鉴权：create/uploadChunk/complete/abort 复用 `authorize`（POST 分支要求 `storage.write`）；项目归属在 use-case 二次校验（`databases.Principal` 无 ProjectID，由 handler 传入）；
- Console：`ChunkedUploader`（`console/src/routes/storage/chunked-uploader.tsx`）对 >16MiB 文件自动分片：`File.slice` 顺序上传、进度条（`components/ui/progress.tsx`）、uploadId 存 localStorage（键含 bucketId+fileName+size）实现失败/刷新后续传（getUploadSession 跳过已收分片）、complete/abort 后清除。

### 4.4 读上下文解析（`resolveReadContext`，优先级从高到低）

认证（`authorize`）：`X-Api-Key` / `Authorization`（Bearer/Session/ApiKey scheme，与 gRPC 拦截器同一解析）/ `TORCHWOOD_session_*` cookie；API Key 按方法检查 scope（上传 → `StorageServiceCreateFile`，下载/预览 → `StorageServiceGetFile`，避免只读 key 越权上传）；admin 会话带 `X-Torchwood-Project` header 并校验项目访问权。上传/下载输出结构化访问日志（含解析后的客户端 IP，与 gRPC 同一 trusted-proxy 规则）。

1. **常规凭证**：API key / admin / end-user JWT / session cookie；
2. **File Token**：URL 携带 `?token=` 且解析后与路径 bucket/file 匹配 → 匿名下载；
3. **公开 bucket**：无凭证但 bucket 为 `public`，URL 携带 `?project=` 定位项目 → `GuestPrincipal`（看 `buckets.public`，不是文档 `_perms`）。

---

## 5. 安全输出与预览

- **响应加固**：`X-Content-Type-Options: nosniff` + `Content-Security-Policy: default-src 'none'; sandbox`；
- **内联白名单**（`inlineSafeMime`）：`image/png`、`image/jpeg`、`image/gif`、`image/webp`、`image/avif`、`image/svg+xml`、`text/plain`、`application/pdf` + `video/*`、`audio/*`；**SVG 强制降级为附件**（可内嵌脚本）；`/download` 路径恒为附件；文件名经 `safeFilename` 清洗（去控制字符，防 header 注入），响应头含 ASCII 回退 + `filename*=UTF-8''` 双编码；
- **preview 缩略图**（`disintegration/imaging`，`golang.org/x/image` 解码 bmp/tiff/webp）：
  - 仅 `image/png` / `image/jpeg` / `image/gif` / `image/webp`；源文件 > **50 MiB** 拒绝（防解压炸弹）；
  - `?width=&height=`（正整数，上限 **4096**）→ `imaging.Fit`（Lanczos）等比缩放，webp 源输出 JPEG 兜底；无参数时直接回源；
  - 输出响应带 `Cache-Control: public, max-age=86400`。

---

## 6. File Token（HMAC 临时访问令牌）

- 签发：`POST /v1/server/storage/buckets/{bucket_id}/files/{file_id}/tokens`，`expires_in` 缺省 **3600s（1h）**，上限 **7 天**（`maxFileTokenLifetime`）；
- 格式：`"{expiresAt}.{projectID}.{bucketID}.{fileID}.{hex(hmac)}"`，HMAC-SHA256 密钥取自 `security.jwt.secret`（`TORCHWOOD_SECURITY_JWT_SECRET`）；
- 校验：`ParseFileToken` 检查段数、过期时间、重算 HMAC 比对（`hmac.Equal` 防时序攻击）；任一不符 → `Unauthenticated`；下载 URL 形如 `/v1/storage/buckets/{b}/files/{f}/download?token=...`；
- 签发需先通过文件 read 权限校验。

---

## 7. Usage 统计

`GET /v1/server/storage/usage`（`GetStorageUsage`）返回 `{buckets, files, total_size}`：

- bucket 数量：`buckets.Count(ctx, projectID)`；
- file 数量：`files.Count(ctx, projectID)`；
- 总容量：`files.SumSize(ctx, projectID)`（项目 schema 内 `SUM(size)`）。

三项都不传入 principal，**不**按文档 `_perms` 过滤，也**不**走 `CountDocuments` / `SumDocumentField`。

---

## 8. 测试

- `internal/api/serverhttp/file_handler_integration_test.go` + `file_handler_uploads_test.go`：multipart 上传、下载、preview、File Token、公开 bucket 匿名读、分片上传全流程/scope/校验端到端测试。
- `internal/app/storage/storage_integration_test.go` + `uploads_integration_test.go`：bucket/file CRUD、元数据更新、usage 聚合、分片会话全流程/续传/校验/互斥等用例层测试。
- `internal/infra/storage/redis_upload_session_test.go`：miniredis 会话往返/TTL 刷新/过期/锁互斥。
- `internal/infra/storage/minio_integration_test.go`：真实 MinIO `ComposeObject`（`TORCHWOOD_TEST_MINIO_ENDPOINT` 未设跳过；CI backend job 提供 minio service）。
- 均使用 `testutil.SetupTestDB`（`TORCHWOOD_TEST_DATABASE_SOURCE` / `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE`，见 `docs/developer/06-databases.md` §7）。

---

## 9. 已知边界

- **分片上传**：单分片 ≤16MiB、part_count ≤10000（size ≤156.25GB）；会话 24h TTL；重复上传同号分片幂等覆盖；
- **孤儿分片清理（已实现）**：`cmd/worker` 的 `ChunkCleaner` 每小时扫描各项目对象，
  删除 key 含 `/chunks/` 且 LastModified 超过 48h 的分片对象（活跃会话分片必然
  <24h，48h 为 2 倍安全余量）；`DeleteBucket` 同步按前缀清尾（含孤儿分片）；
  与上传并发产生的极小概率残留由下轮扫描覆盖；
- `storage.provider: local` 仅配置占位，未实现本地磁盘适配器；
- 底层 bucket 无 per-bucket 映射：所有 project/bucket 共享同一 S3 bucket，以 `projectID/bucketID/fileID` 键隔离。
