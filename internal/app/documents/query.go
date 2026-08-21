package documents

import (
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BindListQuery 把 ListDocuments 传输字段编进 domain Query（proto codec 在此完成，
// handler 不手写 Parse）。
func BindListQuery(queries []string, pageSize int32, pageToken string, protoQ *sharedv1.Query) (databases.Query, error) {
	ast, err := query.FromProto(protoQ)
	if err != nil {
		return databases.Query{}, status.Errorf(codes.InvalidArgument, "invalid query: %v", err)
	}
	return databases.Query{
		Queries:   queries,
		PageSize:  pageSize,
		PageToken: pageToken,
		AST:       ast,
	}, nil
}

// ResolveQuery 是 List/Count 的双栈入口：proto AST（q.AST）优先，否则
// ParseMany(queries)。两者同时提供谓词/排序/分页且冲突时 InvalidArgument。
func ResolveQuery(q databases.Query) (*query.Query, error) {
	ast := q.AST
	protoActive := ast.HasPredicate() || ast.HasOrders() || ast.HasPage()
	if protoActive && len(q.Queries) > 0 {
		return nil, status.Error(codes.InvalidArgument, "query and queries cannot both be set")
	}
	if protoActive {
		out := cloneAST(ast)
		if err := mergePage(out, q.PageSize, q.PageToken); err != nil {
			return nil, err
		}
		return out, nil
	}
	parsed, err := query.ParseMany(q.Queries)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid query: %v", err)
	}
	parsed.PageSize = q.PageSize
	parsed.PageToken = q.PageToken
	return parsed, nil
}

func mergePage(ast *query.Query, pageSize int32, pageToken string) error {
	if ast.PageSize == 0 {
		ast.PageSize = pageSize
	} else if pageSize != 0 && pageSize != ast.PageSize {
		return status.Error(codes.InvalidArgument, "query.page_size conflicts with page_size")
	}
	if ast.PageToken == "" {
		ast.PageToken = pageToken
	} else if pageToken != "" && pageToken != ast.PageToken {
		return status.Error(codes.InvalidArgument, "query.page_token conflicts with page_token")
	}
	return nil
}

func cloneAST(src *query.Query) *query.Query {
	if src == nil {
		return &query.Query{}
	}
	cp := *src
	return &cp
}
