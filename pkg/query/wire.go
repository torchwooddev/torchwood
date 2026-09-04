package query

// ToWireJSON 把 AST 序列化为 shared.v1.Query 的 protojson 形态
// （map[string]any；字段名与 protojson lowerCamel 对齐：filter/orders/
// select/pageSize/pageToken，oneof 分支名 eq/ne/lt/.../notBetween/isNull/
// and/or）。供 CLI/工具直连 JSON 请求面（服务端零 DSL 消费，C7）；值全为
// string（AST 的值域即 string，类型化比较由 PG 列类型驱动）。
//
// 零值省略规则与 protojson 一致：pageSize=0、pageToken=""、desc=false、
// 空 values 不输出（服务端按未设置处理）。
func (q *Query) ToWireJSON() map[string]any {
	if q == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if q.Filter != nil {
		out["filter"] = filterToWire(q.Filter)
	}
	if len(q.Orders) > 0 {
		orders := make([]map[string]any, 0, len(q.Orders))
		for _, o := range q.Orders {
			m := map[string]any{"attribute": o.Attribute}
			if o.Desc {
				m["desc"] = true
			}
			orders = append(orders, m)
		}
		out["orders"] = orders
	}
	if len(q.Selects) > 0 {
		out["select"] = append([]string{}, q.Selects...)
	}
	if q.PageSize != 0 {
		out["pageSize"] = q.PageSize
	}
	if q.PageToken != "" {
		out["pageToken"] = q.PageToken
	}
	return out
}

// wireArmByOp：AST 算子 → shared.v1.Filter oneof 分支名（protojson）。
var wireArmByOp = map[string]string{
	OpEqual:            "eq",
	OpNotEqual:         "ne",
	OpLessThan:         "lt",
	OpLessThanEqual:    "lte",
	OpGreaterThan:      "gt",
	OpGreaterThanEqual: "gte",
	OpIn:               "in",
	OpContains:         "contains",
	OpNotContains:      "notContains",
	OpStartsWith:       "startsWith",
	OpNotStartsWith:    "notStartsWith",
	OpEndsWith:         "endsWith",
	OpNotEndsWith:      "notEndsWith",
	OpSearch:           "search",
	OpNotSearch:        "notSearch",
	OpBetween:          "between",
	OpNotBetween:       "notBetween",
	OpIsNull:           "isNull",
	OpIsNotNull:        "isNotNull",
	OpAnd:              "and",
	OpOr:               "or",
}

func filterToWire(f *Filter) map[string]any {
	if f == nil {
		return nil
	}
	arm, ok := wireArmByOp[f.Op]
	if !ok {
		// 未知算子：交给服务端 codec 报错，此处保守输出 attribute 占位。
		return map[string]any{"eq": map[string]any{"attribute": f.Attribute}}
	}
	switch f.Op {
	case OpAnd, OpOr:
		children := make([]map[string]any, 0, len(f.Children))
		for _, c := range f.Children {
			children = append(children, filterToWire(c))
		}
		return map[string]any{arm: map[string]any{"filters": children}}
	default:
		cmp := map[string]any{"attribute": f.Attribute}
		if len(f.Values) > 0 {
			cmp["values"] = append([]string{}, f.Values...)
		}
		return map[string]any{arm: cmp}
	}
}
