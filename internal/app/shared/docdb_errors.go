package shared

import (
	"errors"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pgErrorFielder 与 docDBErrorSQLStates 已下沉至 internal/infra/documentdb/errors.go（J4-6）。
// app 层不再感知 pgdriver/SQLSTATE，仅做 domain→status 单向映射。

// UpdateDocumentVersionRequired 校验用户集合 Update/Delete 的 OCC 版本参数：
// 未设置或 ≤0 → FailedPrecondition version_required（Client/Server Databases
// 写路径只允许用户集合，系统集合在 ensureCollection 已拒）。
func UpdateDocumentVersionRequired(version *int64) error {
	if version == nil || *version <= 0 {
		return status.Error(codes.FailedPrecondition, databases.ErrVersionRequired.Error())
	}
	return nil
}

// MapDocumentDBError converts document database errors to gRPC status errors.
func MapDocumentDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ident.ErrInvalidSchemaResourceID) {
		return MapIdentError(err)
	}
	if errors.Is(err, databases.ErrPermissionDenied) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	if errors.Is(err, databases.ErrDuplicateKey) {
		return status.Error(codes.AlreadyExists, "duplicate key")
	}
	if errors.Is(err, databases.ErrDocumentNotFound) {
		return status.Error(codes.NotFound, "document not found")
	}
	if errors.Is(err, databases.ErrNoFieldsToUpdate) {
		return status.Error(codes.InvalidArgument, "no fields to update")
	}
	if errors.Is(err, databases.ErrVersionRequired) {
		return status.Error(codes.FailedPrecondition, databases.ErrVersionRequired.Error())
	}
	if errors.Is(err, databases.ErrVersionMismatch) {
		return status.Error(codes.FailedPrecondition, databases.ErrVersionMismatch.Error())
	}
	if errors.Is(err, databases.ErrVersionColumnConflict) {
		return status.Error(codes.FailedPrecondition, databases.ErrVersionColumnConflict.Error())
	}
	if errors.Is(err, databases.ErrVersionColumnUnavailable) {
		return status.Error(codes.InvalidArgument, databases.ErrVersionColumnUnavailable.Error())
	}
	return err
}

// Deprecated: MapDocumentDBErrorSQLState 仅为兼容旧测试保留，SQLSTATE 翻译已下沉至
// internal/infra/documentdb.MapError，app 层不再处理 pgdriver 错误。
// 新代码请直接在 infra 层调用 MapError，或在 app 层仅处理领域哨兵。
func MapDocumentDBErrorSQLState(err error) error {
	return MapDocumentDBError(err)
}
