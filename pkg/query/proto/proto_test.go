package queryproto

import (
	"testing"

	"github.com/stretchr/testify/require"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/pkg/query"
)

func TestFromProto_EqMatchesParseEqual(t *testing.T) {
	parsed, err := query.Parse(`equal("a","b")`)
	require.NoError(t, err)

	ast, err := FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{
			Attribute: "a",
			Values:    []string{"b"},
		}}},
	})
	require.NoError(t, err)
	require.NotNil(t, ast.Filter)
	require.Equal(t, parsed.Filter.Op, ast.Filter.Op)
	require.Equal(t, parsed.Filter.Attribute, ast.Filter.Attribute)
	require.Equal(t, parsed.Filter.Values, ast.Filter.Values)
}

func TestFromProto_OrTree(t *testing.T) {
	ast, err := FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Or{Or: &sharedv1.FilterList{
			Filters: []*sharedv1.Filter{
				{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{Attribute: "status", Values: []string{"a"}}}},
				{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{Attribute: "status", Values: []string{"b"}}}},
			},
		}}},
	})
	require.NoError(t, err)
	require.NotNil(t, ast.Filter)
	require.Equal(t, query.OpOr, ast.Filter.Op)
	require.Len(t, ast.Filter.Children, 2)
	require.Equal(t, query.OpEqual, ast.Filter.Children[0].Op)
	require.Equal(t, "status", ast.Filter.Children[0].Attribute)
}

func TestFromProto_EmptyComparisonValues(t *testing.T) {
	_, err := FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Gt{Gt: &sharedv1.Comparison{
			Attribute: "$id",
		}}},
	})
	require.Error(t, err)

	_, err = FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{
			Attribute: "title",
			Values:    nil,
		}}},
	})
	require.Error(t, err)
}

func TestFromProto_LeafCountCap(t *testing.T) {
	filters := make([]*sharedv1.Filter, query.MaxQueries+1)
	for i := range filters {
		filters[i] = &sharedv1.Filter{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{
			Attribute: "a",
			Values:    []string{"1"},
		}}}
	}
	_, err := FromProto(&sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Or{Or: &sharedv1.FilterList{Filters: filters}}},
	})
	require.Error(t, err)
}
