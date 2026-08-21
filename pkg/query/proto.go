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
	leaves := 0
	filter, err := filterFromProto(src.GetFilter(), 0, &leaves)
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
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

func filterFromProto(src *sharedv1.Filter, depth int, leaves *int) (*Filter, error) {
	if src == nil {
		return nil, nil
	}
	if depth > MaxDepth {
		return nil, fmt.Errorf("filter nesting exceeds maximum of %d", MaxDepth)
	}
	switch e := src.GetExpr().(type) {
	case *sharedv1.Filter_Eq:
		return comparisonFilter(OpEqual, e.Eq, leaves)
	case *sharedv1.Filter_Ne:
		return comparisonFilter(OpNotEqual, e.Ne, leaves)
	case *sharedv1.Filter_Lt:
		return comparisonFilter(OpLessThan, e.Lt, leaves)
	case *sharedv1.Filter_Lte:
		return comparisonFilter(OpLessThanEqual, e.Lte, leaves)
	case *sharedv1.Filter_Gt:
		return comparisonFilter(OpGreaterThan, e.Gt, leaves)
	case *sharedv1.Filter_Gte:
		return comparisonFilter(OpGreaterThanEqual, e.Gte, leaves)
	case *sharedv1.Filter_In:
		return comparisonFilter(OpIn, e.In, leaves)
	case *sharedv1.Filter_Contains:
		return comparisonFilter(OpContains, e.Contains, leaves)
	case *sharedv1.Filter_StartsWith:
		return comparisonFilter(OpStartsWith, e.StartsWith, leaves)
	case *sharedv1.Filter_EndsWith:
		return comparisonFilter(OpEndsWith, e.EndsWith, leaves)
	case *sharedv1.Filter_Search:
		return comparisonFilter(OpSearch, e.Search, leaves)
	case *sharedv1.Filter_And:
		return boolFilter(OpAnd, e.And, depth, leaves)
	case *sharedv1.Filter_Or:
		return boolFilter(OpOr, e.Or, depth, leaves)
	case nil:
		return nil, fmt.Errorf("filter expr is required")
	default:
		return nil, fmt.Errorf("unsupported filter expr")
	}
}

func comparisonFilter(op string, c *sharedv1.Comparison, leaves *int) (*Filter, error) {
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
		if *leaves > MaxQueries {
			return nil, fmt.Errorf("filter node count exceeds maximum of %d", MaxQueries)
		}
	}
	return &Filter{Op: op, Attribute: c.GetAttribute(), Values: values}, nil
}

func boolFilter(op string, list *sharedv1.FilterList, depth int, leaves *int) (*Filter, error) {
	if list == nil || len(list.GetFilters()) == 0 {
		return nil, fmt.Errorf("%s requires at least one filter", op)
	}
	if len(list.GetFilters()) > MaxQueries {
		return nil, fmt.Errorf("filter node count exceeds maximum of %d", MaxQueries)
	}
	children := make([]*Filter, 0, len(list.GetFilters()))
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
	return &Filter{Op: op, Children: children}, nil
}
