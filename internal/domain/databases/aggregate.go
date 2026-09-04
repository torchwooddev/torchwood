package databases

// 聚合模型（redesign §4.1 + §10.5 P1；§11-J D1）：sum/avg/min/max +
// 可选单键 group_by。count 已有独立 RPC，不并入。
//
// D1 规范（已裁决）：聚合一律在权限过滤后的可见行集上执行——过滤链先于
// GROUP BY，不可见行的值与 group 键都不会出现；最小桶/k-匿名是可选产品
// 功能（默认关）；权限变更前后聚合结果不可比属固有属性。

// AggregateFunction 是聚合函数。
type AggregateFunction string

const (
	AggregateSum AggregateFunction = "sum"
	AggregateAvg AggregateFunction = "avg"
	AggregateMin AggregateFunction = "min"
	AggregateMax AggregateFunction = "max"
)

// AggregateSpec 是单个聚合项：目标属性必须为集合声明的数值属性
// （integer/float）。group_by 出现时 field 仍必填（每组的聚合目标）。
type AggregateSpec struct {
	Function AggregateFunction
	Field    string
}

// AggregateNumberKind 标记聚合结果的标量类型（预决策 5）：integer 属性的
// sum/min/max → int64（int64 精度，>2^53 精确）；avg 恒 double；
// float 属性恒 double。
type AggregateNumberKind int

const (
	// AggregateValueNone 是空集下的 avg/min/max（无值；sum 空集按属性类型返回 0）。
	AggregateValueNone AggregateNumberKind = iota
	AggregateValueInt64
	AggregateValueDouble
)

// AggregateValue 是单个聚合项的结果：Kind 决定读 Int64 还是 Double。
type AggregateValue struct {
	Function AggregateFunction
	Field    string
	Kind     AggregateNumberKind
	Int64    int64
	Double   float64
}

// AggregateGroup 是一个聚合组：无 group_by 时恰有一组且 GroupKey 为 nil；
// 有 group_by 时键只来自可见行（D1：不泄露不可见行的键）。
type AggregateGroup struct {
	GroupKey *string
	Values   []AggregateValue
}
