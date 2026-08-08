package shared

import (
	"errors"
	"fmt"
	"testing"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/stretchr/testify/require"
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

// TestMapDocumentDBError_SQLState (A6): 满足 Field(byte) string 接口的 PG 错误
// 按 SQLSTATE 映射（客户端类 → InvalidArgument，资源类 → ResourceExhausted）；
// 未收录的 SQLSTATE 原样透传。
func TestMapDocumentDBError_SQLState(t *testing.T) {
	cases := []struct {
		state string
		code  codes.Code
	}{
		{"22P02", codes.InvalidArgument},
		{"22001", codes.InvalidArgument},
		{"23502", codes.InvalidArgument},
		{"42703", codes.InvalidArgument},
		{"42601", codes.InvalidArgument},
		{"23503", codes.InvalidArgument},
		{"42883", codes.InvalidArgument},
		{"53100", codes.ResourceExhausted},
		{"53200", codes.ResourceExhausted},
		{"54000", codes.ResourceExhausted},
		{"53400", codes.ResourceExhausted},
	}
	for _, tc := range cases {
		require.Equal(t, tc.code, status.Code(MapDocumentDBError(sqlstateStub{state: tc.state})), "state %s", tc.state)
	}

	// %w 包装链可穿透（errors.As）。
	wrapped := fmt.Errorf("pg error: %w", sqlstateStub{state: "42703"})
	require.Equal(t, codes.InvalidArgument, status.Code(MapDocumentDBError(wrapped)))

	// 未收录 SQLSTATE → 原样透传（不降级为其它错误码）。
	unknown := sqlstateStub{state: "99999"}
	require.Equal(t, unknown, MapDocumentDBError(unknown))

	// 普通错误（无 Field 方法）→ 原样透传（既有断言 :19-21 保持）。
	plain := errors.New("SQLSTATE 23505 unique constraint")
	require.Equal(t, plain, MapDocumentDBError(plain))
}
