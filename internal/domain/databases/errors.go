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
	ErrCodeExhausted                = "DOCUMENT.EXHAUSTED"
)

// ErrorCodeRetryable 是域码静态可重试表：OCC 冲突可重读合并重试、资源耗尽
// 可退避重试；参数/权限/存在性类错误重试无意义。
func ErrorCodeRetryable(code string) bool {
	switch code {
	case ErrCodeVersionConflict, ErrCodeExhausted:
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

// ErrVersionColumnConflict 是存量用户表已有非 bigint 的 _version 列（用户属性抢占）
// 时返回的错误，OCC fail-closed；映射为 FailedPrecondition / version_column_conflict。
var ErrVersionColumnConflict = errors.New("version_column_conflict")

// ErrVersionColumnUnavailable 是尚未 reconcile（缺 _version 列）的用户表上
// 写文档或使用 $version 查询时返回的错误；映射为 InvalidArgument / version_column_unavailable。
var ErrVersionColumnUnavailable = errors.New("version_column_unavailable")

// SimpleDocumentUpdate builds a DocumentUpdate for data and optional permission changes.
func SimpleDocumentUpdate(doc Document, perms []Permission) DocumentUpdate {
	return DocumentUpdate{Document: doc, Permissions: perms}
}
