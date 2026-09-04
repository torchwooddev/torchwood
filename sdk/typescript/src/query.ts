// 文档查询 typed AST（C7 单 AST）：服务端只收 shared.v1.Query；
// 本模块是 TS 侧的类型与构造器。字段名与 protojson 对齐
// （filter/orders/select/pageSize/pageToken；oneof 分支 eq/ne/…/notBetween/
// isNull/and/or）。

/** 比较叶：attribute + values（isNull/isNotNull 无 values；between 恰 2 个）。 */
export interface QueryComparison {
  attribute: string;
  values?: string[];
}

/** 布尔组合：and/or 嵌套（深度 ≤8）。 */
export interface FilterList {
  filters: FilterNode[];
}

export type FilterNode =
  | { eq: QueryComparison }
  | { ne: QueryComparison }
  | { lt: QueryComparison }
  | { lte: QueryComparison }
  | { gt: QueryComparison }
  | { gte: QueryComparison }
  | { in: QueryComparison }
  | { contains: QueryComparison }
  | { notContains: QueryComparison }
  | { startsWith: QueryComparison }
  | { notStartsWith: QueryComparison }
  | { endsWith: QueryComparison }
  | { notEndsWith: QueryComparison }
  | { search: QueryComparison }
  | { notSearch: QueryComparison }
  | { between: QueryComparison }
  | { notBetween: QueryComparison }
  | { isNull: QueryComparison }
  | { isNotNull: QueryComparison }
  | { containsAny: QueryComparison }
  | { containsAll: QueryComparison }
  | { and: FilterList }
  | { or: FilterList };

/** 单个排序键（服务端强制追加 $id tiebreaker）。 */
export interface QueryOrder {
  attribute: string;
  desc?: boolean;
}

/** shared.v1.Query 的 JSON 形态（POST documents:list 的 body 即此对象）。 */
export interface QueryAst {
  filter?: FilterNode;
  orders?: QueryOrder[];
  select?: string[];
  pageSize?: number;
  pageToken?: string;
}

function cmp(attribute: string, values: string[]): QueryComparison {
  return { attribute, values };
}

/** 等值；多值时语义为 IN（与服务端编译一致）。 */
export function eq(attribute: string, ...values: string[]): FilterNode {
  return { eq: cmp(attribute, values) };
}

/** 不等；多值时语义为 NOT IN。 */
export function ne(attribute: string, ...values: string[]): FilterNode {
  return { ne: cmp(attribute, values) };
}

export function lt(attribute: string, value: string): FilterNode {
  return { lt: cmp(attribute, [value]) };
}

export function lte(attribute: string, value: string): FilterNode {
  return { lte: cmp(attribute, [value]) };
}

export function gt(attribute: string, value: string): FilterNode {
  return { gt: cmp(attribute, [value]) };
}

export function gte(attribute: string, value: string): FilterNode {
  return { gte: cmp(attribute, [value]) };
}

export function in_(attribute: string, values: string[]): FilterNode {
  return { in: cmp(attribute, values) };
}

export function contains(attribute: string, value: string): FilterNode {
  return { contains: cmp(attribute, [value]) };
}

export function notContains(attribute: string, value: string): FilterNode {
  return { notContains: cmp(attribute, [value]) };
}

export function startsWith(attribute: string, value: string): FilterNode {
  return { startsWith: cmp(attribute, [value]) };
}

export function notStartsWith(attribute: string, value: string): FilterNode {
  return { notStartsWith: cmp(attribute, [value]) };
}

export function endsWith(attribute: string, value: string): FilterNode {
  return { endsWith: cmp(attribute, [value]) };
}

export function notEndsWith(attribute: string, value: string): FilterNode {
  return { notEndsWith: cmp(attribute, [value]) };
}

/** 全文检索（目标属性须有 fulltext 索引）。 */
export function search(attribute: string, value: string): FilterNode {
  return { search: cmp(attribute, [value]) };
}

export function notSearch(attribute: string, value: string): FilterNode {
  return { notSearch: cmp(attribute, [value]) };
}

/** 闭区间 [min, max]。 */
export function between(attribute: string, min: string, max: string): FilterNode {
  return { between: cmp(attribute, [min, max]) };
}

export function notBetween(attribute: string, min: string, max: string): FilterNode {
  return { notBetween: cmp(attribute, [min, max]) };
}

export function isNull(attribute: string): FilterNode {
  return { isNull: { attribute } };
}

export function isNotNull(attribute: string): FilterNode {
  return { isNotNull: { attribute } };
}

/**
 * 数组算子（§10.5 P0）：仅 array=true 属性可用（服务端按 catalog attrs
 * 白名单校验）。containsAny = 交集非空（PG &&）；containsAll = 子集（PG @>）。
 */
export function containsAny(attribute: string, values: string[]): FilterNode {
  return { containsAny: cmp(attribute, values) };
}

export function containsAll(attribute: string, values: string[]): FilterNode {
  return { containsAll: cmp(attribute, values) };
}

/** and/or 组合（nil 节点由调用方自行过滤）。 */
export function and(...filters: FilterNode[]): FilterNode {
  return { and: { filters } };
}

export function or(...filters: FilterNode[]): FilterNode {
  return { or: { filters } };
}

export function orderAsc(attribute: string): QueryOrder {
  return { attribute };
}

export function orderDesc(attribute: string): QueryOrder {
  return { attribute, desc: true };
}
