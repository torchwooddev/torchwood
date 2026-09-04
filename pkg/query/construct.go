package query

// 程序化 AST 构造器（C7 单 AST）：服务端不再解析 DSL 字符串后，过滤/排序/
// 分页的构造入口统一为这些叶子构造函数 + Query 结构体字面量。DSL 字符串
// 仅作为客户端语法糖存在（Parse/ParseMany）。

// 值数量约束与 validateLeaf 同源：Eq/Ne/Lt/Lte/Gt/Gte/In/Contains 族 ≥1；
// Between/NotBetween 恰 2；IsNull/IsNotNull 0。

func Eq(attr string, values ...string) *Filter {
	return &Filter{Op: OpEqual, Attribute: attr, Values: values}
}

func Ne(attr string, values ...string) *Filter {
	return &Filter{Op: OpNotEqual, Attribute: attr, Values: values}
}

func Lt(attr, value string) *Filter {
	return &Filter{Op: OpLessThan, Attribute: attr, Values: []string{value}}
}

func Lte(attr, value string) *Filter {
	return &Filter{Op: OpLessThanEqual, Attribute: attr, Values: []string{value}}
}

func Gt(attr, value string) *Filter {
	return &Filter{Op: OpGreaterThan, Attribute: attr, Values: []string{value}}
}

func Gte(attr, value string) *Filter {
	return &Filter{Op: OpGreaterThanEqual, Attribute: attr, Values: []string{value}}
}

func In(attr string, values ...string) *Filter {
	return &Filter{Op: OpIn, Attribute: attr, Values: values}
}

// ContainsAny / ContainsAll 是数组算子（§10.5 P0）：仅 array=true 属性可用，
// 服务端按 catalog attrs 白名单校验后编译为 PG &&（交集非空）/ @>（子集）。
func ContainsAny(attr string, values ...string) *Filter {
	return &Filter{Op: OpContainsAny, Attribute: attr, Values: values}
}

func ContainsAll(attr string, values ...string) *Filter {
	return &Filter{Op: OpContainsAll, Attribute: attr, Values: values}
}

func Contains(attr, value string) *Filter {
	return &Filter{Op: OpContains, Attribute: attr, Values: []string{value}}
}

func NotContains(attr, value string) *Filter {
	return &Filter{Op: OpNotContains, Attribute: attr, Values: []string{value}}
}

func StartsWith(attr, value string) *Filter {
	return &Filter{Op: OpStartsWith, Attribute: attr, Values: []string{value}}
}

func NotStartsWith(attr, value string) *Filter {
	return &Filter{Op: OpNotStartsWith, Attribute: attr, Values: []string{value}}
}

func EndsWith(attr, value string) *Filter {
	return &Filter{Op: OpEndsWith, Attribute: attr, Values: []string{value}}
}

func NotEndsWith(attr, value string) *Filter {
	return &Filter{Op: OpNotEndsWith, Attribute: attr, Values: []string{value}}
}

func Search(attr, value string) *Filter {
	return &Filter{Op: OpSearch, Attribute: attr, Values: []string{value}}
}

func NotSearch(attr, value string) *Filter {
	return &Filter{Op: OpNotSearch, Attribute: attr, Values: []string{value}}
}

func Between(attr, min, max string) *Filter {
	return &Filter{Op: OpBetween, Attribute: attr, Values: []string{min, max}}
}

func NotBetween(attr, min, max string) *Filter {
	return &Filter{Op: OpNotBetween, Attribute: attr, Values: []string{min, max}}
}

func IsNull(attr string) *Filter {
	return &Filter{Op: OpIsNull, Attribute: attr}
}

func IsNotNull(attr string) *Filter {
	return &Filter{Op: OpIsNotNull, Attribute: attr}
}

// And / Or 组合布尔节点；children 为空时返回 nil（视为无过滤）。
func And(children ...*Filter) *Filter {
	return boolNode(OpAnd, children)
}

func Or(children ...*Filter) *Filter {
	return boolNode(OpOr, children)
}

func boolNode(op string, children []*Filter) *Filter {
	kept := make([]*Filter, 0, len(children))
	for _, c := range children {
		if c != nil {
			kept = append(kept, c)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		return kept[0]
	default:
		return &Filter{Op: op, Children: kept}
	}
}
