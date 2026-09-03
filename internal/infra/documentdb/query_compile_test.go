package documentdb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/query"
	queryproto "github.com/torchwooddev/torchwood/pkg/query/proto"
)

func TestBuildAppwriteQuery_EqualMatchesProtoEq(t *testing.T) {
	parsed, err := query.Parse(`equal("a","b")`)
	require.NoError(t, err)
	fromDSL, argsDSL, _, err := buildAppwriteQuery(parsed)
	require.NoError(t, err)

	ast, err := queryproto.FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{
			Attribute: "a",
			Values:    []string{"b"},
		}}},
	})
	require.NoError(t, err)
	fromProto, argsProto, _, err := buildAppwriteQuery(ast)
	require.NoError(t, err)
	require.Equal(t, fromDSL, fromProto)
	require.Equal(t, argsDSL, argsProto)
	require.Equal(t, `d."a" = ?`, fromDSL)
	require.Equal(t, []any{"b"}, argsDSL)
}

func TestBuildAppwriteQuery_OrCompilesToSQLOr(t *testing.T) {
	ast, err := queryproto.FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Or{Or: &sharedv1.FilterList{
			Filters: []*sharedv1.Filter{
				{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{Attribute: "status", Values: []string{"a"}}}},
				{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{Attribute: "status", Values: []string{"b"}}}},
			},
		}}},
	})
	require.NoError(t, err)
	where, args, _, err := buildAppwriteQuery(ast)
	require.NoError(t, err)
	require.Equal(t, `(d."status" = ? OR d."status" = ?)`, where)
	require.Equal(t, []any{"a", "b"}, args)
	require.NotContains(t, where, " AND ")
}

// TestBuildAppwriteQuery_CustomOrderHasIDTiebreaker：自定义排序必须以
// _id 收尾且与 cursor 续页路径同构（重复排序键的全序确定性 + keyset 各页
// 同序；offset 翻页跨页不丢不重的机制保证）。
func TestBuildAppwriteQuery_CustomOrderHasIDTiebreaker(t *testing.T) {
	parsed, err := query.ParseMany([]string{`orderAsc("status")`})
	require.NoError(t, err)
	_, _, orderSQL, err := buildAppwriteQuery(parsed)
	require.NoError(t, err)
	require.Equal(t, `ORDER BY d."status" ASC, d._id ASC`, orderSQL)

	parsed, err = query.ParseMany([]string{`orderDesc("priority")`})
	require.NoError(t, err)
	_, _, orderSQL, err = buildAppwriteQuery(parsed)
	require.NoError(t, err)
	require.Equal(t, `ORDER BY d."priority" DESC, d._id DESC`, orderSQL)

	// 默认排序（无显式 orders）同样带 _id。
	parsed, err = query.ParseMany([]string{`equal("status","open")`})
	require.NoError(t, err)
	_, _, orderSQL, err = buildAppwriteQuery(parsed)
	require.NoError(t, err)
	require.Equal(t, `ORDER BY d._created_at DESC, d._id DESC`, orderSQL)
}

func TestBuildAppwriteQuery_CodecAndStillAnd(t *testing.T) {
	parsed, err := query.ParseMany([]string{`equal("a","1")`, `equal("b","2")`})
	require.NoError(t, err)
	where, args, _, err := buildAppwriteQuery(parsed)
	require.NoError(t, err)
	require.Equal(t, `(d."a" = ? AND d."b" = ?)`, where)
	require.Equal(t, []any{"1", "2"}, args)
}

func TestBuildAppwriteQuery_EmptyValuesInvalidArgument(t *testing.T) {
	_, _, _, err := buildAppwriteQuery(&query.Query{
		Filter: &query.Filter{Op: query.OpGreaterThan, Attribute: "n"},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, _, _, err = buildAppwriteQuery(&query.Query{
		Filter: &query.Filter{Op: query.OpEqual, Attribute: "a"},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAstFrom_BothASTAndQueries(t *testing.T) {
	_, err := astFrom(databases.Query{
		Queries: []string{`equal("a","1")`},
		AST:     &query.Query{Filter: &query.Filter{Op: query.OpEqual, Attribute: "a", Values: []string{"1"}}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAstFrom_EmptyGtEq(t *testing.T) {
	_, err := astFrom(databases.Query{AST: &query.Query{Filter: &query.Filter{Op: query.OpGreaterThan, Attribute: "n"}}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = astFrom(databases.Query{AST: &query.Query{Filter: &query.Filter{Op: query.OpEqual, Attribute: "a"}}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
