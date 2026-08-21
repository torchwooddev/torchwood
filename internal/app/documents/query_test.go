package documents

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestResolveQuery_LegacyCodec(t *testing.T) {
	got, err := ResolveQuery(databases.Query{
		Queries:  []string{`equal("status","active")`},
		PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, query.OpEqual, got.Filter.Op)
	require.Equal(t, "status", got.Filter.Attribute)
	require.Equal(t, []string{"active"}, got.Filter.Values)
	require.Equal(t, int32(10), got.PageSize)
}

func TestResolveQuery_ProtoAST(t *testing.T) {
	got, err := ResolveQuery(databases.Query{
		PageSize: 25,
		AST: &query.Query{
			Filter: &query.Filter{Op: query.OpEqual, Attribute: "a", Values: []string{"b"}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, query.OpEqual, got.Filter.Op)
	require.Equal(t, int32(25), got.PageSize)
}

func TestResolveQuery_ConflictQueriesAndFilter(t *testing.T) {
	_, err := ResolveQuery(databases.Query{
		Queries: []string{`equal("a","1")`},
		AST: &query.Query{
			Filter: &query.Filter{Op: query.OpEqual, Attribute: "a", Values: []string{"2"}},
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestResolveQuery_ConflictPageSize(t *testing.T) {
	_, err := ResolveQuery(databases.Query{
		PageSize: 10,
		AST: &query.Query{
			Filter:   &query.Filter{Op: query.OpEqual, Attribute: "a", Values: []string{"1"}},
			PageSize: 20,
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestResolveQuery_SamePageSizeNotConflict(t *testing.T) {
	got, err := ResolveQuery(databases.Query{
		PageSize: 10,
		AST: &query.Query{
			Filter:   &query.Filter{Op: query.OpEqual, Attribute: "a", Values: []string{"1"}},
			PageSize: 10,
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(10), got.PageSize)
}
