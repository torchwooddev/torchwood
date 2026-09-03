package documentdb

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
)

// pgErrorFielder 是 pgdriver.Error 的最小接口面（Field('C') = SQLSTATE）；
// 仅做类型匹配，不做 SQLSTATE 字符串回退，普通错误保持原样透传。
// 从 internal/app/shared/docdb_errors.go 下沉至 infra 层（J4-6），app 层不再感知 pgdriver。
type pgErrorFielder interface {
	Field(byte) string
}

// docDBErrorSQLStates 将常见 PG 客户端错误映射为 InvalidArgument，
// 资源类错误映射为 ResourceExhausted（A6）。与原 app 层定义保持一致。
var docDBErrorSQLStates = map[string]codes.Code{
	"22P02": codes.InvalidArgument,   // invalid_text_representation
	"22001": codes.InvalidArgument,   // string_data_right_truncation
	"23502": codes.InvalidArgument,   // not_null_violation
	"42703": codes.InvalidArgument,   // undefined_column
	"42601": codes.InvalidArgument,   // syntax_error
	"42804": codes.InvalidArgument,   // datatype_mismatch（值与列类型不符，如字符串进 integer 列）
	"23503": codes.InvalidArgument,   // foreign_key_violation
	"42883": codes.InvalidArgument,   // undefined_function
	"42P10": codes.InvalidArgument,   // invalid_column_reference (ON CONFLICT 无匹配唯一索引)
	"23505": codes.AlreadyExists,     // unique_violation（元数据重复键兜底映射）
	"53100": codes.ResourceExhausted, // disk_full
	"53200": codes.ResourceExhausted, // out_of_memory
	"54000": codes.ResourceExhausted, // program_limit_exceeded
	"53400": codes.ResourceExhausted, // configuration_limit_exceeded
}

// mapPGError 将 pgdriver 错误按 SQLSTATE 翻译为 gRPC status 或领域哨兵。
// 仅做类型匹配（errors.As 到 pgErrorFielder），不做字符串回退，保持与原
// MapDocumentDBError 的 SQLSTATE 处理语义一致。
// 23505 特殊处理：返回领域哨兵 ErrDuplicateKey（与 isUniqueViolation 路径一致），
// 其余按 docDBErrorSQLStates 映射为 status.Error(code, "document database error")。
func mapPGError(err error) error {
	if err == nil {
		return nil
	}
	var fielder pgErrorFielder
	if errors.As(err, &fielder) {
		if code, ok := docDBErrorSQLStates[fielder.Field('C')]; ok {
			if code == codes.AlreadyExists {
				return databases.ErrDuplicateKey
			}
			return status.Error(code, "document database error")
		}
	}
	return err
}

// MapError 供测试或外部调用，显式触发 SQLSTATE 翻译（薄包装）。
func MapError(err error) error {
	return mapPGError(err)
}

// mapError 是 adapter 内部统一出口：先尝试 SQLSTATE 翻译，失败则原样返回。
// 供各 DocumentDB 方法在返回前调用，确保裸 pgdriver.Error 不上抛至 app 层。
func (p *postgresDocumentDB) mapError(err error) error {
	return mapPGError(err)
}
