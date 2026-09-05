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

/** KNN 距离度量（与 hnsw 索引的 distance_metric 同枚举域）。 */
export type DistanceMetric = "COSINE" | "L2" | "INNER_PRODUCT";

/**
 * 向量近邻查询（会话 #10 §10.5 P0；B2 多页）：非 filter 树节点——距离承载
 * 排序，pageSize 即 k（top-k 可见近邻）。attribute 须为声明的 vector 属性且
 * 存在 metric 匹配的 hnsw 索引；与 orders 互斥（服务端拒绝）。多页翻页：
 * 同 Query 以 pageToken 携带服务端发放的 kvc: 距离游标（跨页不重不漏）。
 * DSL 字符串不支持 vector_search（向量不该手写，typed builder only）。
 */
export interface VectorSearch {
  attribute: string;
  values: number[];
  metric?: DistanceMetric;
  maxDistance?: number;
  /**
   * HNSW 搜索广度（B7）：本次查询的 hnsw.ef_search，合法域 [1,500]
   * （≤0 / >500 服务端 InvalidArgument）。缺省不发送——服务端维持 pgvector
   * 缺省 40。近重复簇边界召回不足时调大（代价：延迟随 ef 增长）。
   */
  efSearch?: number;
}

/** shared.v1.Query 的 JSON 形态（POST documents:list 的 body 即此对象）。 */
export interface QueryAst {
  filter?: FilterNode;
  orders?: QueryOrder[];
  select?: string[];
  pageSize?: number;
  pageToken?: string;
  vectorSearch?: VectorSearch;
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

/**
 * KNN 构造器（metric 缺省 COSINE）。示例：
 *   vectorSearch("emb", [0.1, 0.2, 0.3]).metric("L2").maxDistance(0.5)
 * values 须与目标 vector 属性的声明维度等长（服务端按 catalog 校验）。
 */
export function vectorSearch(attribute: string, values: number[]) {
  const v: VectorSearch = { attribute, values };
  return {
    /** 距离度量（须与目标列 hnsw 索引的 distance_metric 匹配）。 */
    metric(m: DistanceMetric) {
      v.metric = m;
      return this;
    },
    /** 距离阈值：仅保留 top-k 中距离 <= max 的行。 */
    maxDistance(max: number) {
      v.maxDistance = max;
      return this;
    },
    /** HNSW 搜索广度（B7）：hnsw.ef_search，合法域 [1,500]；缺省不发送（服务端用 pgvector 缺省 40）。 */
    efSearch(n: number) {
      v.efSearch = n;
      return this;
    },
    build(): VectorSearch {
      return v;
    },
  };
}
