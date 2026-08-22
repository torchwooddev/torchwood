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
