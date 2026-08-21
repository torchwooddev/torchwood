package databases

import "errors"

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

// ErrVersionRequired 是用户集合 Update/Delete/Increment 未携带（或 ≤0）ExpectedVersion
// 时返回的错误；映射为 FailedPrecondition / version_required。
var ErrVersionRequired = errors.New("version_required")

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
