package shared

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sqlstateStub 是 pgdriver.Error 的本地替身（pgdriver.Error 无导出构造函数，
// 单测不得直接构造），实现最小接口 Field(byte) string（A6）。
type sqlstateStub struct{ state string }

func (s sqlstateStub) Error() string { return "sqlstate " + s.state }
func (s sqlstateStub) Field(b byte) string {
	if b == 'C' {
		return s.state
	}
	return ""
}

func TestMapDocumentDBError_DuplicateKey(t *testing.T) {
	// The infra layer re-exports the domain error as an alias; errors.Is must
	// still match so the mapping to AlreadyExists holds for both instances.
	err := errors.New("duplicate key")
	require.Error(t, err)

	plain := errors.New("SQLSTATE 23505 unique constraint")
	mapped := MapDocumentDBError(plain)
	require.Equal(t, plain, mapped)

	dup := databases.ErrDuplicateKey
	require.Equal(t, codes.AlreadyExists, status.Code(MapDocumentDBError(dup)))

	wrapped := errors.Join(dup, errors.New("context"))
	require.Equal(t, codes.AlreadyExists, status.Code(MapDocumentDBError(wrapped)))

	denied := databases.ErrPermissionDenied
	require.Equal(t, codes.PermissionDenied, status.Code(MapDocumentDBError(denied)))
	require.Nil(t, MapDocumentDBError(nil))
}

// TestMapDocumentDBError_SQLState (A6 → J4-6): SQLSTATE 翻译已下沉至
// internal/infra/documentdb.MapError，app 层不再感知 pgdriver。
// 此处断言 app 层的 MapDocumentDBError 对满足 Field 接口的 PG 错误一律透传，
// 映射由 infra 层负责；普通错误与未收录 SQLSTATE 同样透传。
func TestMapDocumentDBError_SQLState(t *testing.T) {
	cases := []string{"22P02", "22001", "23502", "42703", "42601", "23503", "42883", "53100", "53200", "54000", "53400", "99999"}
	for _, state := range cases {
		stub := sqlstateStub{state: state}
		require.Equal(t, stub, MapDocumentDBError(stub), "state %s should passthrough in app layer", state)
		wrapped := fmt.Errorf("pg error: %w", stub)
		// errors.Is/As 不应触发映射，返回原包装错误
		require.Equal(t, wrapped.Error(), MapDocumentDBError(wrapped).Error(), "state %s wrapped should passthrough", state)
	}

	// 普通错误（无 Field 方法）→ 原样透传（既有断言保持）。
	plain := errors.New("SQLSTATE 23505 unique constraint")
	require.Equal(t, plain, MapDocumentDBError(plain))
}

// TestMapDocumentDBError_OCCVersionErrors (PR1): OCC 版本相关领域错误按稳定
// 消息映射（SDK/Console 分支依赖消息文本）：
//
//	version_required / version_mismatch / version_column_conflict → FailedPrecondition
//	version_column_unavailable → InvalidArgument
func TestMapDocumentDBError_OCCVersionErrors(t *testing.T) {
	cases := []struct {
		err  error
		code codes.Code
		msg  string
	}{
		{databases.ErrVersionRequired, codes.FailedPrecondition, "version_required"},
		{databases.ErrVersionMismatch, codes.FailedPrecondition, "version_mismatch"},
		{databases.ErrVersionColumnConflict, codes.FailedPrecondition, "version_column_conflict"},
		{databases.ErrVersionColumnUnavailable, codes.InvalidArgument, "version_column_unavailable"},
	}
	for _, tc := range cases {
		mapped := MapDocumentDBError(fmt.Errorf("update document: %w", tc.err))
		require.Equal(t, tc.code, status.Code(mapped), "err %v", tc.err)
		require.Equal(t, tc.msg, status.Convert(mapped).Message(), "err %v", tc.err)
	}

	// UpdateDocumentVersionRequired：未设置 / ≤0 → version_required。
	require.Equal(t, codes.FailedPrecondition, status.Code(UpdateDocumentVersionRequired(nil)))
	zero := int64(0)
	require.Equal(t, codes.FailedPrecondition, status.Code(UpdateDocumentVersionRequired(&zero)))
	one := int64(1)
	require.NoError(t, UpdateDocumentVersionRequired(&one))
}

func TestMapDocumentDBError_Ident(t *testing.T) {
	mapped := MapDocumentDBError(ident.ErrInvalidSchemaResourceID)
	require.Equal(t, codes.InvalidArgument, status.Code(mapped))
	require.Equal(t, ident.ErrInvalidSchemaResourceID.Error(), status.Convert(mapped).Message())

	wrapped := fmt.Errorf("schema: %w", ident.ErrInvalidSchemaResourceID)
	require.Equal(t, codes.InvalidArgument, status.Code(MapDocumentDBError(wrapped)))
}
