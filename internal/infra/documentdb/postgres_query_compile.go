// Appwrite DSL → SQL 编译：谓词编译、字段白/黑名单校验、输入上限、LIKE 转义。
package documentdb

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/query"
)

// escapeLikePattern 转义 ILIKE 模式中的通配符与转义符本身（配合 ESCAPE '\' 子句），
// 使 contains/startsWith/endsWith 将用户输入按字面量匹配。
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func mapQueryField(field string) string {
	switch field {
	case "$id", "_id":
		return "_id"
	case "$createdAt", "_created_at":
		return "_created_at"
	case "$updatedAt", "_updated_at":
		return "_updated_at"
	case "$version", "_version":
		return "_version"
	}
	return field
}

// systemQueryFields 是查询白名单中的系统列（含 $ 别名映射后的内部名）。
var systemQueryFields = []string{"_id", "_created_at", "_updated_at", "_version"}

// sensitiveQueryFields 是 default 库系统集合中禁止作为查询过滤/排序条件的
// 凭据/令牌类列（任何角色不得探测）；仅按 databases.IsSystemCollection 限定，
// 自定义库同名集合不受影响。phone 等 PII 管理列按 D4 决策保留可查。
var sensitiveQueryFields = map[string]map[string]struct{}{
	"users":      {"password_hash": {}, "prefs": {}, "labels": {}},
	"sessions":   {"secret_hash": {}},
	"identities": {"provider_data": {}},
}

// validateQueryFields 校验非 System 查询路径（A7）：Filters/Orders/Selects 字段
// 白名单（系统列 + 声明 attrs）、敏感列黑名单、search 的 fulltext 索引约束。
// _version 特判：系统集合拒绝（无此列）；用户集合列尚未 reconcile（缺列）时返回
// version_column_unavailable，不得落 PG 未定义列错误（读路径不 ALTER）。
// 物理表名（physical）供 _version readiness 检查；collectionID 保留逻辑名供
// 系统集合敏感列黑名单。SystemPrincipal 路径不调用本函数（信任内部调用，
// 零额外元数据查询）。
func (p *postgresDocumentDB) validateQueryFields(ctx context.Context, schema, physical string, parsed *query.Query, coll *databases.Collection, collectionID string, isSystem bool) error {
	allowed := make(map[string]struct{}, len(systemQueryFields)+len(coll.Attributes))
	for _, f := range systemQueryFields {
		allowed[f] = struct{}{}
	}
	for _, attr := range coll.Attributes {
		allowed[attr.Key] = struct{}{}
	}
	fulltextAttrs := map[string]struct{}{}
	for _, idx := range coll.Indexes {
		if strings.ToLower(idx.Type) == "fulltext" {
			for _, a := range idx.Attributes {
				fulltextAttrs[a] = struct{}{}
			}
		}
	}

	checkField := func(name string) error {
		field := mapQueryField(name)
		if _, ok := allowed[field]; !ok {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid query field: %s", name))
		}
		if field == "_version" {
			if isSystem {
				// 系统表无 _version 列，禁止编进 SQL。
				return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid query field: %s", name))
			}
			// 用户集合：列尚未 reconcile → version_column_unavailable；
			// 列已存在但非 bigint → version_column_conflict。
			// 均不落 PG 42703；不得改写成对常量 1 的比较（equal("$version", 2) 会静默语义错误）。
			ready, err := p.versionColumnReady(ctx, schema, physical)
			if err != nil {
				return err
			}
			if !ready {
				return status.Error(codes.InvalidArgument, databases.ErrVersionColumnUnavailable.Error())
			}
		}
		if isSystem {
			if sensitive, ok := sensitiveQueryFields[collectionID]; ok {
				if _, blocked := sensitive[field]; blocked {
					return status.Error(codes.InvalidArgument, fmt.Sprintf("field is not queryable: %s", name))
				}
			}
		}
		return nil
	}

	var fieldErr error
	parsed.WalkLeaves(func(f query.Filter) {
		if fieldErr != nil {
			return
		}
		if err := checkField(f.Attribute); err != nil {
			fieldErr = err
			return
		}
		if f.Op == query.OpSearch {
			field := mapQueryField(f.Attribute)
			if _, ok := fulltextAttrs[field]; !ok {
				fieldErr = status.Error(codes.InvalidArgument, fmt.Sprintf("search requires a fulltext index on: %s", f.Attribute))
			}
		}
	})
	if fieldErr != nil {
		return fieldErr
	}
	for _, o := range parsed.Orders {
		if err := checkField(o.Attribute); err != nil {
			return err
		}
	}
	for _, s := range parsed.Selects {
		if err := checkField(s); err != nil {
			return err
		}
	}
	return nil
}

// astFrom 是 SQL 编译前的 AST 入口（C7 单 AST）：caller（app 层
// ResolveQuery）已填好 AST，此处合并 GET 面分页字段后校验。服务端不再
// 解析 DSL 字符串（queries 回退分支已随双栈退役删除）。
func astFrom(q databases.Query) (*query.Query, error) {
	ast := cloneQuery(q.AST)
	if ast.PageSize == 0 {
		ast.PageSize = q.PageSize
	}
	if ast.PageToken == "" {
		ast.PageToken = q.PageToken
	}
	if err := ast.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid query: %v", err)
	}
	return ast, nil
}

func cloneQuery(src *query.Query) *query.Query {
	if src == nil {
		return &query.Query{}
	}
	cp := *src
	return &cp
}

func buildAppwriteQuery(parsed *query.Query) (string, []any, string, error) {
	var where string
	var args []any
	var err error
	if parsed.Filter != nil {
		where, args, err = compileFilter(parsed.Filter)
	} else if len(parsed.Filters) > 0 {
		children := make([]*query.Filter, len(parsed.Filters))
		for i := range parsed.Filters {
			f := parsed.Filters[i]
			children[i] = &f
		}
		where, args, err = compileBool(children, "AND")
	}
	if err != nil {
		return "", nil, "", err
	}
	// 跨 filter 绑定参数累计上限：单 filter 已限 maxFilterValues，但 100 条
	// query × 1000 值可累积 10 万绑定参数，超出 PG 单语句 65535 参数上限后
	// 以运行时错误暴露。此处封死总量（List/Count/Sum 共用）。
	if len(args) > maxTotalFilterParams {
		return "", nil, "", status.Errorf(codes.InvalidArgument,
			"query filters bind %d parameters in total, exceeds maximum of %d", len(args), maxTotalFilterParams)
	}

	// R08-P2-4：默认排序带 _id tiebreaker，同 _created_at 的多行分页保持稳定。
	orderSQL := "ORDER BY d._created_at DESC, d._id DESC"
	if len(parsed.Orders) > 0 {
		var parts []string
		for _, o := range parsed.Orders {
			field := mapQueryField(o.Attribute)
			if !safeNameRe.MatchString(field) {
				return "", nil, "", status.Error(codes.InvalidArgument, fmt.Sprintf("invalid order field: %s", o.Attribute))
			}
			dir := "ASC"
			if o.Desc {
				dir = "DESC"
			}
			parts = append(parts, fmt.Sprintf("d.%s %s", quoteIdent(field), dir))
		}
		if len(parts) > 0 {
			// 与 cursor 续页路径（postgres_document_query.go 的 ORDER BY / keyset
			// 谓词 `<field>, d._id` 二元组）同构：分页各页必须同一全序，否则同键
			// 多行跨页丢/重。不补 _created_at 中段——那会使首页与 cursor 页序不同
			// 构（R08-P2-4 的遗留缺陷）。_id 方向跟随首个排序键（与 cursor 路径一致）。
			dir := "ASC"
			if parsed.Orders[0].Desc {
				dir = "DESC"
			}
			orderSQL = "ORDER BY " + strings.Join(parts, ", ") + ", d._id " + dir
		}
	}
	return where, args, orderSQL, nil
}

func compileFilter(f *query.Filter) (string, []any, error) {
	if f == nil {
		return "", nil, nil
	}
	switch f.Op {
	case query.OpAnd:
		return compileBool(f.Children, "AND")
	case query.OpOr:
		return compileBool(f.Children, "OR")
	default:
		return compilePredicate(f)
	}
}

func compileBool(children []*query.Filter, join string) (string, []any, error) {
	var parts []string
	var args []any
	for _, c := range children {
		if c == nil {
			continue
		}
		w, a, err := compileFilter(c)
		if err != nil {
			return "", nil, err
		}
		if w == "" {
			continue
		}
		parts = append(parts, w)
		args = append(args, a...)
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	if len(parts) == 1 {
		return parts[0], args, nil
	}
	return "(" + strings.Join(parts, " "+join+" ") + ")", args, nil
}

func compilePredicate(f *query.Filter) (string, []any, error) {
	field := mapQueryField(f.Attribute)
	if !safeNameRe.MatchString(field) {
		return "", nil, fmt.Errorf("invalid query field: %s", f.Attribute)
	}
	col := "d." + quoteIdent(field)
	switch f.Op {
	case query.OpEqual, query.OpIn:
		if len(f.Values) < 1 {
			return "", nil, status.Errorf(codes.InvalidArgument, "%s requires at least 1 value", f.Op)
		}
		if len(f.Values) > maxFilterValues {
			return "", nil, status.Error(codes.InvalidArgument, fmt.Sprintf("filter values exceed maximum of %d", maxFilterValues))
		}
		if len(f.Values) == 1 {
			return fmt.Sprintf("%s = ?", col), []any{f.Values[0]}, nil
		}
		phs := strings.TrimSuffix(strings.Repeat("?, ", len(f.Values)), ", ")
		args := make([]any, len(f.Values))
		for i, v := range f.Values {
			args[i] = v
		}
		return fmt.Sprintf("%s IN (%s)", col, phs), args, nil
	case query.OpNotEqual:
		if len(f.Values) < 1 {
			return "", nil, status.Errorf(codes.InvalidArgument, "%s requires at least 1 value", f.Op)
		}
		if len(f.Values) > maxFilterValues {
			return "", nil, status.Error(codes.InvalidArgument, fmt.Sprintf("filter values exceed maximum of %d", maxFilterValues))
		}
		if len(f.Values) == 1 {
			return fmt.Sprintf("%s != ?", col), []any{f.Values[0]}, nil
		}
		phs := strings.TrimSuffix(strings.Repeat("?, ", len(f.Values)), ", ")
		args := make([]any, len(f.Values))
		for i, v := range f.Values {
			args[i] = v
		}
		return fmt.Sprintf("%s NOT IN (%s)", col, phs), args, nil
	case query.OpLessThan, query.OpLessThanEqual, query.OpGreaterThan, query.OpGreaterThanEqual,
		query.OpContains, query.OpStartsWith, query.OpEndsWith, query.OpSearch,
		query.OpNotContains, query.OpNotStartsWith, query.OpNotEndsWith, query.OpNotSearch:
		if len(f.Values) < 1 {
			return "", nil, status.Errorf(codes.InvalidArgument, "%s requires at least 1 value", f.Op)
		}
		switch f.Op {
		case query.OpLessThan:
			return fmt.Sprintf("%s < ?", col), []any{f.Values[0]}, nil
		case query.OpLessThanEqual:
			return fmt.Sprintf("%s <= ?", col), []any{f.Values[0]}, nil
		case query.OpGreaterThan:
			return fmt.Sprintf("%s > ?", col), []any{f.Values[0]}, nil
		case query.OpGreaterThanEqual:
			return fmt.Sprintf("%s >= ?", col), []any{f.Values[0]}, nil
		case query.OpContains:
			return fmt.Sprintf(`%s ILIKE ? ESCAPE '\'`, col), []any{"%" + escapeLikePattern(f.Values[0]) + "%"}, nil
		case query.OpStartsWith:
			return fmt.Sprintf(`%s ILIKE ? ESCAPE '\'`, col), []any{escapeLikePattern(f.Values[0]) + "%"}, nil
		case query.OpEndsWith:
			return fmt.Sprintf(`%s ILIKE ? ESCAPE '\'`, col), []any{"%" + escapeLikePattern(f.Values[0])}, nil
		case query.OpSearch:
			return fmt.Sprintf("to_tsvector('simple', %s::text) @@ plainto_tsquery('simple', ?)", col), []any{f.Values[0]}, nil
		// not* 变体（C7 预决策 1）：NOT 包裹正算子。三值逻辑下 NULL 键行
		// 对 NOT(比较) 求值为 NULL 而被排除——与 Appwrite/SQL NOT 语义一致。
		case query.OpNotContains:
			return fmt.Sprintf(`%s NOT ILIKE ? ESCAPE '\'`, col), []any{"%" + escapeLikePattern(f.Values[0]) + "%"}, nil
		case query.OpNotStartsWith:
			return fmt.Sprintf(`%s NOT ILIKE ? ESCAPE '\'`, col), []any{escapeLikePattern(f.Values[0]) + "%"}, nil
		case query.OpNotEndsWith:
			return fmt.Sprintf(`%s NOT ILIKE ? ESCAPE '\'`, col), []any{"%" + escapeLikePattern(f.Values[0])}, nil
		default:
			return fmt.Sprintf("NOT (to_tsvector('simple', %s::text) @@ plainto_tsquery('simple', ?))", col), []any{f.Values[0]}, nil
		}
	case query.OpIsNull:
		return fmt.Sprintf("%s IS NULL", col), nil, nil
	case query.OpIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", col), nil, nil
	case query.OpBetween:
		if len(f.Values) != 2 {
			return "", nil, fmt.Errorf("between requires 2 values")
		}
		return fmt.Sprintf("%s BETWEEN ? AND ?", col), []any{f.Values[0], f.Values[1]}, nil
	case query.OpNotBetween:
		if len(f.Values) != 2 {
			return "", nil, fmt.Errorf("notBetween requires 2 values")
		}
		return fmt.Sprintf("%s NOT BETWEEN ? AND ?", col), []any{f.Values[0], f.Values[1]}, nil
	default:
		return "", nil, fmt.Errorf("unsupported filter operator: %s", f.Op)
	}
}
