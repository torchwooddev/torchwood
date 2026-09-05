package databases

import "errors"

// ---------------------------------------------------------------------------
// 域错误码体系（redesign §4.1）：稳定 snake_case 域码静态映射 gRPC code，
// 消息格式 "CODE: message"，供 Agent 机器判定恢复路径；retryable 为静态表。
// ---------------------------------------------------------------------------

const (
	ErrCodePermissionDenied         = "DOCUMENT.PERMISSION_DENIED"
	ErrCodeAlreadyExists            = "DOCUMENT.ALREADY_EXISTS"
	ErrCodeNotFound                 = "DOCUMENT.NOT_FOUND"
	ErrCodeNoFieldsToUpdate         = "DOCUMENT.NO_FIELDS_TO_UPDATE"
	ErrCodeVersionRequired          = "DOCUMENT.VERSION_REQUIRED"
	ErrCodeVersionInvalid           = "DOCUMENT.VERSION_INVALID"
	ErrCodeVersionConflict          = "DOCUMENT.VERSION_CONFLICT"
	ErrCodeVersionColumnConflict    = "DOCUMENT.VERSION_COLUMN_CONFLICT"
	ErrCodeVersionColumnUnavailable = "DOCUMENT.VERSION_COLUMN_UNAVAILABLE"
	ErrCodeInvalidArgument          = "DOCUMENT.INVALID_ARGUMENT"
	ErrCodeTooLarge                 = "DOCUMENT.TOO_LARGE"
	// ACL_TOO_LARGE：文档 _acl ACE 数超上限（redesign §11-J H2：≤64）。
	ErrCodeACLTooLarge = "DOCUMENT.ACL_TOO_LARGE"
	// ATTRIBUTE_UNSERIALIZABLE：载荷属性值不可 JSON 序列化（如通道/函数值）。
	ErrCodeAttributeUnserializable = "DOCUMENT.ATTRIBUTE_UNSERIALIZABLE"
	ErrCodeExhausted               = "DOCUMENT.EXHAUSTED"
	// 写幂等（redesign §4.1/§10.1）：KEY_CONFLICT = 同 key 复用给不同请求
	//（InvalidArgument）；IN_PROGRESS = 同 key 请求仍在执行、等待超时
	//（Aborted，可重试）。
	ErrCodeIdempotencyKeyConflict = "IDEMPOTENCY.KEY_CONFLICT"
	ErrCodeIdempotencyInProgress  = "IDEMPOTENCY.IN_PROGRESS"
	// 聚合（单 AST 会话·预决策 5）：integer 聚合超出 int64 范围
	//（SUM(bigint)::int8 溢出，PG 22003）。
	ErrCodeAggregateOverflow = "AGGREGATE.OVERFLOW"
	// catalog DDL 乐观锁（阶段②包 B，redesign §4.4）：ddl_seq CAS 递增时
	// 0 行受影响——并发 schema 变更先行提交，调用方应重读 catalog 后重试。
	ErrCodeDDLConflict = "CATALOG.DDL_CONFLICT"
	// COLUMN_LIMIT_EXCEEDED：集合属性列数超软预算（redesign §11-J H2：≤200，
	// PG 1600 列硬限留余量）。
	ErrCodeColumnLimitExceeded = "CATALOG.COLUMN_LIMIT_EXCEEDED"
	// 事件重放窗口过期（阶段④ §4.5）：since_seq/last_seq 早于该集合最老
	// 可用事件（24h published 保留 >> 1h 重放承诺），无法保证增量完整——
	// 指引客户端全量重拉后重新续传。
	ErrCodeResumeExpired = "EVENTS.RESUME_EXPIRED"
)

// ErrorCodeRetryable 是域码静态可重试表：OCC 冲突可重读合并重试、资源耗尽
// 可退避重试、幂等执行中稍后重试；参数/权限/存在性类错误重试无意义。
func ErrorCodeRetryable(code string) bool {
	switch code {
	case ErrCodeVersionConflict, ErrCodeExhausted, ErrCodeIdempotencyInProgress,
		ErrCodeDDLConflict:
		return true
	}
	return false
}

// ErrorDomainCode 返回领域哨兵对应的域码；非哨兵错误返回空串。
func ErrorDomainCode(err error) string {
	switch {
	case errors.Is(err, ErrPermissionDenied):
		return ErrCodePermissionDenied
	case errors.Is(err, ErrDuplicateKey):
		return ErrCodeAlreadyExists
	case errors.Is(err, ErrDocumentNotFound):
		return ErrCodeNotFound
	case errors.Is(err, ErrNoFieldsToUpdate):
		return ErrCodeNoFieldsToUpdate
	case errors.Is(err, ErrVersionRequired):
		return ErrCodeVersionRequired
	case errors.Is(err, ErrVersionInvalid):
		return ErrCodeVersionInvalid
	case errors.Is(err, ErrVersionMismatch):
		return ErrCodeVersionConflict
	case errors.Is(err, ErrVersionColumnConflict):
		return ErrCodeVersionColumnConflict
	case errors.Is(err, ErrVersionColumnUnavailable):
		return ErrCodeVersionColumnUnavailable
	case errors.Is(err, ErrIdempotencyKeyConflict):
		return ErrCodeIdempotencyKeyConflict
	case errors.Is(err, ErrAggregateOverflow):
		return ErrCodeAggregateOverflow
	case errors.Is(err, ErrDDLConflict):
		return ErrCodeDDLConflict
	case errors.Is(err, ErrResumeExpired):
		return ErrCodeResumeExpired
	}
	return ""
}

// ErrPermissionDenied is returned when the caller lacks document-level permission.
var ErrPermissionDenied = errors.New("permission denied")

// ErrDuplicateKey is returned when a unique constraint violation occurs.
var ErrDuplicateKey = errors.New("duplicate key")

// ErrNoFieldsToUpdate is returned when an update request carries no effective
// field changes (e.g. an increment map containing only zero deltas).
var ErrNoFieldsToUpdate = errors.New("no fields to update")

// ErrDocumentNotFound is returned when an update targets a document that does
// not exist; mapped to codes.NotFound by the app layer.
var ErrDocumentNotFound = errors.New("document not found")

// ErrVersionRequired 是用户集合 Update/Delete 未携带 ExpectedVersion（缺省）
// 时返回的错误；映射为 FailedPrecondition / DOCUMENT.VERSION_REQUIRED。
var ErrVersionRequired = errors.New("version_required")

// ErrVersionInvalid 是 ExpectedVersion 显式设为 ≤0（非法值，与缺省语义不同）
// 时返回的错误；映射为 InvalidArgument / DOCUMENT.VERSION_INVALID。
// Phase 1 裁决②：拆分"缺省"与"显式 0"，消灭错误码错位（C4 本意）。
var ErrVersionInvalid = errors.New("version_invalid")

// ErrVersionMismatch 是 ExpectedVersion 与当前行 _version 不一致时返回的错误；
// 映射为 FailedPrecondition / version_mismatch。
var ErrVersionMismatch = errors.New("version_mismatch")

// VersionConflictError 是 ErrVersionMismatch 的载荷形态：OCC 冲突时附带探测
// SELECT 读到的当前 _version（redesign §10.1：冲突错误体带 current_version——
// Agent 直接取该值合并重试，无需额外读回）。经 errors.As 从包装链提取后由
// app 层 MapDocumentDBError 塞进 ErrorInfo metadata；errors.Is 对
// ErrVersionMismatch 哨兵保持等价，既有判定路径零改动。
type VersionConflictError struct {
	CurrentVersion int64
}

func (e *VersionConflictError) Error() string { return ErrVersionMismatch.Error() }

// Is 使 *VersionConflictError 与 ErrVersionMismatch 哨兵双向可判定。
func (e *VersionConflictError) Is(target error) bool { return target == ErrVersionMismatch }

// ErrVersionColumnConflict 是存量用户表已有非 bigint 的 _version 列（用户属性抢占）
// 时返回的错误，OCC fail-closed；映射为 FailedPrecondition / version_column_conflict。
var ErrVersionColumnConflict = errors.New("version_column_conflict")

// ErrVersionColumnUnavailable 是尚未 reconcile（缺 _version 列）的用户表上
// 写文档或使用 $version 查询时返回的错误；映射为 InvalidArgument / version_column_unavailable。
var ErrVersionColumnUnavailable = errors.New("version_column_unavailable")

// ErrAggregateOverflow 是 integer 属性聚合结果超出 int64 范围时返回的错误
//（SUM(bigint)::int8 溢出）；映射为 InvalidArgument / AGGREGATE.OVERFLOW。
var ErrAggregateOverflow = errors.New("aggregate_overflow")

// ErrDDLConflict 是 catalog 元数据写路径的 ddl_seq CAS 失败（并发 schema 变更
// 先行提交，0 行受影响）；映射为 Aborted / CATALOG.DDL_CONFLICT（R12 裁决：
// CAS 冲突非参数错误，对齐 IDEMPOTENCY.IN_PROGRESS 的 Aborted+retryable
// 先例），retryable=true（调用方重读 catalog 后重试），redesign §4.4 / §11-G3。
var ErrDDLConflict = errors.New("ddl_conflict")

// ErrResumeExpired 是事件重放游标（since_seq / last_seq）早于该集合最老可用
// 事件时返回的错误；映射为 FailedPrecondition / EVENTS.RESUME_EXPIRED，
// 消息指引客户端放弃增量、全量重拉后重新续传（阶段④ §4.5）。
var ErrResumeExpired = errors.New("resume_expired")

// SimpleDocumentUpdate builds a DocumentUpdate for data and optional permission changes.
func SimpleDocumentUpdate(doc Document, perms []Permission) DocumentUpdate {
	return DocumentUpdate{Document: doc, Permissions: perms}
}
