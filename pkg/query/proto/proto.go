// Package queryproto 把 shared.v1.Query 编进 AST。独立子包以免 pkg/query
// 把 genproto 带进 worker 等不碰 RPC 的进程。
package queryproto

import (
	"fmt"

	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/pkg/query"
)

// FromProto decodes a proto Query into the AST. nil input yields nil.
func FromProto(src *sharedv1.Query) (*query.Query, error) {
	if src == nil {
		return nil, nil
	}
	leaves := 0
	filter, err := filterFromProto(src.GetFilter(), 0, &leaves)
	if err != nil {
		return nil, err
	}
	out := &query.Query{
		Filter:    filter,
		PageSize:  src.GetPageSize(),
		PageToken: src.GetPageToken(),
	}
	if filter != nil && filter.Op != query.OpAnd && filter.Op != query.OpOr {
		out.Filters = []query.Filter{*filter}
	} else if filter != nil && filter.Op == query.OpAnd {
		for _, c := range filter.Children {
			if c != nil && c.Op != query.OpAnd && c.Op != query.OpOr {
				out.Filters = append(out.Filters, *c)
			}
		}
	}
	for _, o := range src.GetOrders() {
		if o == nil || o.GetAttribute() == "" {
			return nil, fmt.Errorf("order attribute is required")
		}
		out.Orders = append(out.Orders, query.Order{Attribute: o.GetAttribute(), Desc: o.GetDesc()})
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

func filterFromProto(src *sharedv1.Filter, depth int, leaves *int) (*query.Filter, error) {
	if src == nil {
		return nil, nil
	}
	if depth > query.MaxDepth {
		return nil, fmt.Errorf("filter nesting exceeds maximum of %d", query.MaxDepth)
	}
	switch e := src.GetExpr().(type) {
	case *sharedv1.Filter_Eq:
		return comparisonFilter(query.OpEqual, e.Eq, leaves)
	case *sharedv1.Filter_Ne:
		return comparisonFilter(query.OpNotEqual, e.Ne, leaves)
	case *sharedv1.Filter_Lt:
		return comparisonFilter(query.OpLessThan, e.Lt, leaves)
	case *sharedv1.Filter_Lte:
		return comparisonFilter(query.OpLessThanEqual, e.Lte, leaves)
	case *sharedv1.Filter_Gt:
		return comparisonFilter(query.OpGreaterThan, e.Gt, leaves)
	case *sharedv1.Filter_Gte:
		return comparisonFilter(query.OpGreaterThanEqual, e.Gte, leaves)
	case *sharedv1.Filter_In:
		return comparisonFilter(query.OpIn, e.In, leaves)
	case *sharedv1.Filter_Contains:
		return comparisonFilter(query.OpContains, e.Contains, leaves)
	case *sharedv1.Filter_StartsWith:
		return comparisonFilter(query.OpStartsWith, e.StartsWith, leaves)
	case *sharedv1.Filter_EndsWith:
		return comparisonFilter(query.OpEndsWith, e.EndsWith, leaves)
	case *sharedv1.Filter_Search:
		return comparisonFilter(query.OpSearch, e.Search, leaves)
	case *sharedv1.Filter_And:
		return boolFilter(query.OpAnd, e.And, depth, leaves)
	case *sharedv1.Filter_Or:
		return boolFilter(query.OpOr, e.Or, depth, leaves)
	case nil:
		return nil, fmt.Errorf("filter expr is required")
	default:
		return nil, fmt.Errorf("unsupported filter expr")
	}
}

func comparisonFilter(op string, c *sharedv1.Comparison, leaves *int) (*query.Filter, error) {
	if c == nil {
		return nil, fmt.Errorf("%s comparison is required", op)
	}
	if c.GetAttribute() == "" {
		return nil, fmt.Errorf("%s attribute is required", op)
	}
	values := append([]string{}, c.GetValues()...)
	if len(values) < 1 {
		return nil, fmt.Errorf("%s requires at least 1 value", op)
	}
	if leaves != nil {
		*leaves++
		if *leaves > query.MaxQueries {
			return nil, fmt.Errorf("filter node count exceeds maximum of %d", query.MaxQueries)
		}
	}
	return &query.Filter{Op: op, Attribute: c.GetAttribute(), Values: values}, nil
}

func boolFilter(op string, list *sharedv1.FilterList, depth int, leaves *int) (*query.Filter, error) {
	if list == nil || len(list.GetFilters()) == 0 {
		return nil, fmt.Errorf("%s requires at least one filter", op)
	}
	if len(list.GetFilters()) > query.MaxQueries {
		return nil, fmt.Errorf("filter node count exceeds maximum of %d", query.MaxQueries)
	}
	children := make([]*query.Filter, 0, len(list.GetFilters()))
	for _, child := range list.GetFilters() {
		f, err := filterFromProto(child, depth+1, leaves)
		if err != nil {
			return nil, err
		}
		if f != nil {
			children = append(children, f)
		}
	}
	if len(children) == 0 {
		return nil, fmt.Errorf("%s requires at least one filter", op)
	}
	if len(children) == 1 {
		return children[0], nil
	}
	return &query.Filter{Op: op, Children: children}, nil
}
