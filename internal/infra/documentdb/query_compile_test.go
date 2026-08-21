package documentdb

import (
	"testing"

	"github.com/stretchr/testify/require"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/pkg/query"
)

func TestBuildAppwriteQuery_EqualMatchesProtoEq(t *testing.T) {
	parsed, err := query.Parse(`equal("a","b")`)
	require.NoError(t, err)
	fromDSL, argsDSL, _, err := buildAppwriteQuery(parsed)
	require.NoError(t, err)

	ast, err := query.FromProto(&sharedv1.Query{
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
	ast, err := query.FromProto(&sharedv1.Query{
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
