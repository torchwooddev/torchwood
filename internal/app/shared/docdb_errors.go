package shared

import (
	"errors"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
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
	"53100": codes.ResourceExhausted, // disk_full
	"53200": codes.ResourceExhausted, // out_of_memory
	"54000": codes.ResourceExhausted, // program_limit_exceeded
	"53400": codes.ResourceExhausted, // configuration_limit_exceeded
}

// MapDocumentDBError converts document database errors to gRPC status errors.
func MapDocumentDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, databases.ErrPermissionDenied) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	if errors.Is(err, databases.ErrDuplicateKey) {
		return status.Error(codes.AlreadyExists, "duplicate key")
	}
	if errors.Is(err, databases.ErrNoFieldsToUpdate) {
		return status.Error(codes.InvalidArgument, "no fields to update")
	}
	var fielder pgErrorFielder
	if errors.As(err, &fielder) {
		if code, ok := docDBErrorSQLStates[fielder.Field('C')]; ok {
			return status.Error(code, "document database error")
		}
	}
	return err
}
