package documents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
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

func TestResolveQuery_ConflictQueriesAndOrders(t *testing.T) {
	_, err := ResolveQuery(databases.Query{
		Queries: []string{`equal("a","1")`},
		AST: &query.Query{
			Orders: []query.Order{{Attribute: "a", Desc: true}},
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestResolveQuery_ConflictQueriesAndPageToken(t *testing.T) {
	_, err := ResolveQuery(databases.Query{
		Queries: []string{`equal("a","1")`},
		AST:     &query.Query{PageToken: "tok"},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestResolveQuery_ConflictPageToken(t *testing.T) {
	_, err := ResolveQuery(databases.Query{
		PageToken: "top",
		AST: &query.Query{
			Filter:    &query.Filter{Op: query.OpEqual, Attribute: "a", Values: []string{"1"}},
			PageToken: "ast",
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestResolveQuery_CodecLimitMapsPageSize(t *testing.T) {
	got, err := ResolveQuery(databases.Query{
		Queries: []string{`limit(7)`},
	})
	require.NoError(t, err)
	require.Equal(t, 7, got.Limit)
	require.Equal(t, int32(7), got.PageSize)
}

func TestBindListQuery_EmptyGtEq(t *testing.T) {
	_, err := BindListQuery(nil, 0, "", &sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Gt{Gt: &sharedv1.Comparison{Attribute: "$id"}}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = BindListQuery(nil, 0, "", &sharedv1.Query{
		Filter: &sharedv1.Filter{Expr: &sharedv1.Filter_Eq{Eq: &sharedv1.Comparison{Attribute: "title"}}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListCountDocuments_EmptyComparisonValues(t *testing.T) {
	core := New(newMemDocDB(), nil)
	ctx := context.Background()
	emptyGt := databases.Query{AST: &query.Query{Filter: &query.Filter{Op: query.OpGreaterThan, Attribute: "n"}}}
	_, _, _, err := core.ListDocuments(ctx, "p", "app", "notes", emptyGt, databases.Principal{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = core.CountDocuments(ctx, "p", "app", "notes", emptyGt, databases.Principal{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	emptyEq := databases.Query{AST: &query.Query{Filter: &query.Filter{Op: query.OpEqual, Attribute: "title"}}}
	_, _, _, err = core.ListDocuments(ctx, "p", "app", "notes", emptyEq, databases.Principal{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = core.CountDocuments(ctx, "p", "app", "notes", emptyEq, databases.Principal{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
