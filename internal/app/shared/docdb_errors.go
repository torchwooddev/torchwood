package shared

import (
	"errors"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pgErrorFielder 是 pgdriver.Error 的最小接口面（Field('C') = SQLSTATE）；
// 仅做类型匹配，不做 SQLSTATE 字符串回退，普通错误保持原样透传。
type pgErrorFielder interface {
	Field(byte) string
}

// docDBErrorSQLStates 将常见 PG 客户端错误映射为 InvalidArgument，
// 资源类错误映射为 ResourceExhausted（A6）。
var docDBErrorSQLStates = map[string]codes.Code{
	"22P02": codes.InvalidArgument,   // invalid_text_representation
	"22001": codes.InvalidArgument,   // string_data_right_truncation
	"23502": codes.InvalidArgument,   // not_null_violation
	"42703": codes.InvalidArgument,   // undefined_column
	"42601": codes.InvalidArgument,   // syntax_error
	"23503": codes.InvalidArgument,   // foreign_key_violation
	"42883": codes.InvalidArgument,   // undefined_function
	"42P10": codes.InvalidArgument,   // invalid_column_reference (ON CONFLICT 无匹配唯一索引)
	"23505": codes.AlreadyExists,     // unique_violation（元数据重复键兜底映射）
	"53100": codes.ResourceExhausted, // disk_full
	"53200": codes.ResourceExhausted, // out_of_memory
	"54000": codes.ResourceExhausted, // program_limit_exceeded
	"53400": codes.ResourceExhausted, // configuration_limit_exceeded
}

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
	var fielder pgErrorFielder
	if errors.As(err, &fielder) {
		if code, ok := docDBErrorSQLStates[fielder.Field('C')]; ok {
			return status.Error(code, "document database error")
		}
	}
	return err
}
