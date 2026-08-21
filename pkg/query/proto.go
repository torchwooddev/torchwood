package query

import (
	"fmt"

	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// FromProto decodes a proto Query into the AST. nil input yields nil.
func FromProto(src *sharedv1.Query) (*Query, error) {
	if src == nil {
		return nil, nil
	}
	filter, err := filterFromProto(src.GetFilter(), 0)
	if err != nil {
		return nil, err
	}
	out := &Query{
		Filter:    filter,
		PageSize:  src.GetPageSize(),
		PageToken: src.GetPageToken(),
	}
	if filter != nil && filter.Op != OpAnd && filter.Op != OpOr {
		out.Filters = []Filter{*filter}
	} else if filter != nil && filter.Op == OpAnd {
		for _, c := range filter.Children {
			if c != nil && c.Op != OpAnd && c.Op != OpOr {
				out.Filters = append(out.Filters, *c)
			}
		}
	}
	for _, o := range src.GetOrders() {
		if o == nil || o.GetAttribute() == "" {
			return nil, fmt.Errorf("order attribute is required")
		}
		out.Orders = append(out.Orders, Order{Attribute: o.GetAttribute(), Desc: o.GetDesc()})
	}
	return out, nil
}

func filterFromProto(src *sharedv1.Filter, depth int) (*Filter, error) {
	if src == nil {
		return nil, nil
	}
	if depth > MaxDepth {
		return nil, fmt.Errorf("filter nesting exceeds maximum of %d", MaxDepth)
	}
	switch e := src.GetExpr().(type) {
	case *sharedv1.Filter_Eq:
		return comparisonFilter(OpEqual, e.Eq)
	case *sharedv1.Filter_Ne:
		return comparisonFilter(OpNotEqual, e.Ne)
	case *sharedv1.Filter_Lt:
		return comparisonFilter(OpLessThan, e.Lt)
	case *sharedv1.Filter_Lte:
		return comparisonFilter(OpLessThanEqual, e.Lte)
	case *sharedv1.Filter_Gt:
		return comparisonFilter(OpGreaterThan, e.Gt)
	case *sharedv1.Filter_Gte:
		return comparisonFilter(OpGreaterThanEqual, e.Gte)
	case *sharedv1.Filter_In:
		return comparisonFilter(OpIn, e.In)
	case *sharedv1.Filter_Contains:
		return comparisonFilter(OpContains, e.Contains)
	case *sharedv1.Filter_StartsWith:
		return comparisonFilter(OpStartsWith, e.StartsWith)
	case *sharedv1.Filter_EndsWith:
		return comparisonFilter(OpEndsWith, e.EndsWith)
	case *sharedv1.Filter_Search:
		return comparisonFilter(OpSearch, e.Search)
	case *sharedv1.Filter_And:
		return boolFilter(OpAnd, e.And, depth)
	case *sharedv1.Filter_Or:
		return boolFilter(OpOr, e.Or, depth)
	case nil:
		return nil, fmt.Errorf("filter expr is required")
	default:
		return nil, fmt.Errorf("unsupported filter expr")
	}
}

func comparisonFilter(op string, c *sharedv1.Comparison) (*Filter, error) {
	if c == nil {
		return nil, fmt.Errorf("%s comparison is required", op)
	}
	if c.GetAttribute() == "" {
		return nil, fmt.Errorf("%s attribute is required", op)
	}
	return &Filter{Op: op, Attribute: c.GetAttribute(), Values: append([]string{}, c.GetValues()...)}, nil
}

func boolFilter(op string, list *sharedv1.FilterList, depth int) (*Filter, error) {
	if list == nil || len(list.GetFilters()) == 0 {
		return nil, fmt.Errorf("%s requires at least one filter", op)
	}
	children := make([]*Filter, 0, len(list.GetFilters()))
	for _, child := range list.GetFilters() {
		f, err := filterFromProto(child, depth+1)
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
	return &Filter{Op: op, Children: children}, nil
}
