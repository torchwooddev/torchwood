// 文档查询面：List/Count/Sum 与 keyset 分页 token（ka:/kb:，W-D）。
package documentdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
)

func (p *postgresDocumentDB) ListDocuments(ctx context.Context, projectID, databaseID, collectionID string, q databases.Query, principal databases.Principal) (*databases.DocumentList, error) {
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return nil, p.mapError(err)
	}
	parsed, err := astFrom(q)
	if err != nil {
		return nil, p.mapError(err)
	}
	tbl := tableName(schema, collectionID)

	// 非 System 路径显式获取集合一次（coll==nil → NotFound，行为从 403 收紧为 404），
	// 复用给权限过滤与字段白名单校验；System 信任路径零额外查询（跳过白名单）。
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	var coll *databases.Collection
	if !principal.BypassesDocumentACL() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return nil, p.mapError(err)
		}
		if coll == nil {
			return nil, status.Error(codes.NotFound, "collection not found")
		}
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.BypassesDocumentACL() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return nil, p.mapError(err)
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}

	if coll != nil {
		if err := p.validateQueryFields(ctx, schema, parsed, coll, collectionID, isSystem); err != nil {
			return nil, p.mapError(err)
		}
	}

	filterWhere, filterArgs, orderSQL, err := buildAppwriteQuery(parsed)
	if err != nil {
		return nil, p.mapError(err)
	}
	if filterWhere != "" {
		whereParts = append(whereParts, filterWhere)
		args = append(args, filterArgs...)
	}

	// DSL 未显式指定 limit 时（ParseMany 不再注入默认值）用 q.PageSize，
	// 仍为 0/负数则回退默认 50；显式 limit 保留上限 clamp（maxQueryLimit）。
	limit := parsed.Limit
	if limit == 0 {
		limit = int(q.PageSize)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	// keyset-only（redesign C2 阶段①收敛）：offset() 算子与非 keyset token
	// 一律拒绝——offset 族 token 已停发（本方法只发 ka:/kb:），旧 token 到此
	// 显式失败，不再静默切回 OFFSET 语义。
	if parsed.Offset != 0 {
		return nil, p.mapError(status.Error(codes.InvalidArgument, "offset() is not supported on ListDocuments; use cursor pagination"))
	}
	if q.PageToken != "" {
		if id, kind, ok := decodeKeysetToken(q.PageToken); ok {
			if kind == "before" {
				parsed.CursorBefore = id
			} else {
				parsed.CursorAfter = id
			}
		} else {
			return nil, p.mapError(status.Error(codes.InvalidArgument, "invalid page token: keyset token required, offset tokens are no longer accepted"))
		}
	}

	cursor := ""
	cursorKind := ""
	if parsed.CursorAfter != "" {
		cursor, cursorKind = parsed.CursorAfter, "after"
	} else if parsed.CursorBefore != "" {
		cursor, cursorKind = parsed.CursorBefore, "before"
	}
	// 排序键：仅取 Orders[0]，无显式排序默认 _created_at DESC；全程追加
	// _id tiebreaker（首页与 cursor 页同构，keyset token 的跨页稳定性保证）。
	// 多排序键在 keyset-only 下无法构造同构续页（token 只编码单键），首页
	// 即拒——R3 的"cursor 拒多键"校验扩展到全路径。
	if len(parsed.Orders) > 1 {
		return nil, p.mapError(status.Error(codes.InvalidArgument, "cursor pagination requires a single order key"))
	}
	sortField := "_created_at"
	sortDir := "DESC"
	if len(parsed.Orders) == 1 {
		sortField = mapQueryField(parsed.Orders[0].Attribute)
		if parsed.Orders[0].Desc {
			sortDir = "DESC"
		} else {
			sortDir = "ASC"
		}
	}
	// 排序字段必须显式校验（不能沿用 ORDER 路径的静默跳过）。
	if !safeNameRe.MatchString(sortField) {
		return nil, p.mapError(status.Error(codes.InvalidArgument, fmt.Sprintf("invalid order field: %s", parsed.Orders[0].Attribute)))
	}
	orderSQL = fmt.Sprintf(`ORDER BY d.%s %s, d._id %s`, quoteIdent(sortField), sortDir, sortDir)

	if cursor != "" {
		if err := validateDocID(cursor); err != nil {
			return nil, p.mapError(err)
		}
		var cursorValue any
		err := p.conn(ctx).QueryRowContext(ctx,
			fmt.Sprintf(`SELECT %s FROM %s WHERE _id = ? AND _tenant = ?`, quoteIdent(sortField), tbl),
			cursor, internalID,
		).Scan(&cursorValue)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, p.mapError(status.Error(codes.InvalidArgument, "cursor document not found"))
			}
			return nil, p.mapError(err)
		}
		op := ">"
		if (sortDir == "ASC" && cursorKind == "before") || (sortDir == "DESC" && cursorKind == "after") {
			op = "<"
		}
		whereParts = append(whereParts, fmt.Sprintf(`(d.%s, d._id) %s (?, ?)`, quoteIdent(sortField), op))
		args = append(args, cursorValue, cursor)
	}

	// W-D：keyset 续页不产出精确 total——COUNT 与数据查询同价（含 EXISTS
	// 权限子查询），对游标续页无意义且把翻页成本翻倍；首页保持精确计数
	//（proto 语义 total_count <= 0 = unknown）。
	var total int64
	if cursor == "" {
		countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s d WHERE %s`, tbl, strings.Join(whereParts, " AND "))
		if err := p.conn(ctx).QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, p.mapError(err)
		}
	}

	querySQL := fmt.Sprintf(`SELECT to_jsonb(d.*) AS doc FROM %s d WHERE %s %s LIMIT ?`, tbl, strings.Join(whereParts, " AND "), orderSQL)
	args = append(args, limit)

	rows, err := p.conn(ctx).QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, p.mapError(err)
	}
	defer func() { _ = rows.Close() }()

	var docs []databases.Document
	for rows.Next() {
		doc, err := scanDocumentJSON(rows)
		if err != nil {
			return nil, p.mapError(err)
		}
		docs = append(docs, *doc)
	}
	if err := rows.Err(); err != nil {
		return nil, p.mapError(err)
	}
	// B6：List 回传 permissions（与 Get 对齐）；W-D 改单条 IN 批量取回。
	if err := p.attachDocumentPermissionsBatch(ctx, schema, collectionID, internalID, docs); err != nil {
		return nil, p.mapError(err)
	}

	if len(parsed.Selects) > 0 {
		selected := make(map[string]struct{}, len(parsed.Selects))
		for _, s := range parsed.Selects {
			selected[mapQueryField(s)] = struct{}{}
		}
		for i := range docs {
			for k := range docs[i].Data {
				if _, ok := selected[k]; !ok {
					delete(docs[i].Data, k)
				}
			}
		}
	}

	// 满页即发 keyset 续页 token（编码边界行 id，方向沿用本次请求）；不满页
	// 无 next=尾页。has-more 以满页判定（续页无精确 total）。
	next := ""
	if len(docs) == limit {
		if cursorKind == "before" {
			next = encodeKeysetToken("before", docs[0].ID)
		} else {
			next = encodeKeysetToken("after", docs[len(docs)-1].ID)
		}
	}
	return &databases.DocumentList{
		Documents:     docs,
		TotalCount:    total,
		NextPageToken: next,
	}, nil
}

// keyset token 是明文 "ka:<docID>" / "kb:<docID>"（与 crud 的结构化 offset
// token 不冲突：那边是版本化编码数据）。简单前缀+docID，无需防篡改——
// token 只承载定位语义，越权由查询 ACL 过滤兜底。
const (
	keysetAfterPrefix  = "ka:"
	keysetBeforePrefix = "kb:"
)

func encodeKeysetToken(kind, docID string) string {
	if kind == "before" {
		return keysetBeforePrefix + docID
	}
	return keysetAfterPrefix + docID
}

func decodeKeysetToken(token string) (id, kind string, ok bool) {
	if after, isAfter := strings.CutPrefix(token, keysetAfterPrefix); isAfter && after != "" {
		return after, "after", true
	}
	if before, isBefore := strings.CutPrefix(token, keysetBeforePrefix); isBefore && before != "" {
		return before, "before", true
	}
	return "", "", false
}

func (p *postgresDocumentDB) CountDocuments(ctx context.Context, projectID, databaseID, collectionID string, q databases.Query, principal databases.Principal) (int64, error) {
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return 0, p.mapError(err)
	}
	parsed, err := astFrom(q)
	if err != nil {
		return 0, p.mapError(err)
	}
	// keyset-only（C2 收敛）：count 是过滤全集语义，offset() 无意义且原先
	// 仅作深翻页上限校验——显式拒绝（不再静默忽略）。
	if parsed.Offset != 0 {
		return 0, p.mapError(status.Error(codes.InvalidArgument, "offset() is not supported; count is over the full filtered set"))
	}
	tbl := tableName(schema, collectionID)

	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	var coll *databases.Collection
	if !principal.BypassesDocumentACL() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return 0, p.mapError(err)
		}
		if coll == nil {
			return 0, p.mapError(status.Error(codes.NotFound, "collection not found"))
		}
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.BypassesDocumentACL() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return 0, p.mapError(err)
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}
	if coll != nil {
		if err := p.validateQueryFields(ctx, schema, parsed, coll, collectionID, isSystem); err != nil {
			return 0, p.mapError(err)
		}
	}
	filterWhere, filterArgs, _, err := buildAppwriteQuery(parsed)
	if err != nil {
		return 0, p.mapError(err)
	}
	if filterWhere != "" {
		whereParts = append(whereParts, filterWhere)
		args = append(args, filterArgs...)
	}

	var total int64
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s d WHERE %s`, tbl, strings.Join(whereParts, " AND "))
	err = p.conn(ctx).QueryRowContext(ctx, sql, args...).Scan(&total)
	return total, p.mapError(err)
}

// AggregateDocuments 在权限过滤后的可见行集上执行 sum/avg/min/max（可选
// 单键 group_by）。D1（§11-J 已裁决）：listPermissionFilter 的过滤链先于
// GROUP BY——不可见行的值不进聚合、group 键不泄露。聚合目标必须为声明的
// 数值属性（integer/float，System 主体一视同仁，防拼入任意列名）；group_by
// 须为已声明属性。空集语义：sum=0（COALESCE）、avg/min/max 无值（Value=nil）。
func (p *postgresDocumentDB) AggregateDocuments(ctx context.Context, projectID, databaseID, collectionID string, q databases.Query, aggs []databases.AggregateSpec, groupBy string, principal databases.Principal) ([]databases.AggregateGroup, error) {
	if len(aggs) == 0 {
		return nil, p.mapError(status.Error(codes.InvalidArgument, "aggregations is required"))
	}
	for _, agg := range aggs {
		switch agg.Function {
		case databases.AggregateSum, databases.AggregateAvg, databases.AggregateMin, databases.AggregateMax:
		default:
			return nil, p.mapError(status.Error(codes.InvalidArgument, fmt.Sprintf("invalid aggregate function %q", agg.Function)))
		}
		if !validColumnName(agg.Field) {
			return nil, p.mapError(status.Error(codes.InvalidArgument, "invalid aggregate field name"))
		}
	}
	if groupBy != "" && !validColumnName(groupBy) {
		return nil, p.mapError(status.Error(codes.InvalidArgument, "invalid group_by field name"))
	}

	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return nil, p.mapError(err)
	}
	tbl := tableName(schema, collectionID)

	// 白名单校验（与旧 SumDocumentField 同纪律）：聚合目标 ∈ 声明属性且为
	// integer/float；group_by ∈ 声明属性（任意类型，键按 text 序列化）。
	coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return nil, p.mapError(err)
	}
	if coll == nil {
		return nil, p.mapError(status.Error(codes.NotFound, "collection not found"))
	}
	attrs := map[string]string{}
	for _, attr := range coll.Attributes {
		attrs[attr.Key] = strings.ToLower(attr.Type)
	}
	for _, agg := range aggs {
		switch attrs[agg.Field] {
		case "integer", "float":
		default:
			return nil, p.mapError(status.Error(codes.InvalidArgument, fmt.Sprintf("field %s is not a numeric attribute", agg.Field)))
		}
	}
	if groupBy != "" {
		if _, ok := attrs[groupBy]; !ok {
			return nil, p.mapError(status.Error(codes.InvalidArgument, fmt.Sprintf("group_by field %s is not a declared attribute", groupBy)))
		}
	}

	parsed, err := astFrom(q)
	if err != nil {
		return nil, p.mapError(err)
	}
	// 过滤/排序字段白名单与兄弟路径（List/Count）同源校验（R6）：未声明列
	// 不落 PG 42703、search 需 fulltext 索引、$version 过 readiness 检查。
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.validateQueryFields(ctx, schema, parsed, coll, collectionID, isSystem); err != nil {
		return nil, p.mapError(err)
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.BypassesDocumentACL() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return nil, p.mapError(err)
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}
	// 聚合只消费过滤算子；排序/分页算子无意义（与 CountDocuments 同纪律，
	// 排序在 buildAppwriteQuery 出口校验后丢弃）。
	filterWhere, filterArgs, _, err := buildAppwriteQuery(parsed)
	if err != nil {
		return nil, p.mapError(err)
	}
	if filterWhere != "" {
		whereParts = append(whereParts, filterWhere)
		args = append(args, filterArgs...)
	}

	selects := make([]string, 0, len(aggs)+1)
	if groupBy != "" {
		selects = append(selects, fmt.Sprintf(`d.%s::text AS __group_key`, quoteIdent(groupBy)))
	}
	for _, agg := range aggs {
		fn := strings.ToUpper(string(agg.Function))
		// sum 空集（全 NULL）定义为 0；avg/min/max 空集保持 NULL（Value=nil）。
		expr := fmt.Sprintf(`%s(d.%s)::float8`, fn, quoteIdent(agg.Field))
		if agg.Function == databases.AggregateSum {
			expr = fmt.Sprintf(`COALESCE(%s(d.%s), 0)::float8`, fn, quoteIdent(agg.Field))
		}
		selects = append(selects, expr)
	}
	querySQL := fmt.Sprintf(`SELECT %s FROM %s d WHERE %s`, strings.Join(selects, ", "), tbl, strings.Join(whereParts, " AND "))
	if groupBy != "" {
		querySQL += fmt.Sprintf(` GROUP BY d.%s ORDER BY d.%s`, quoteIdent(groupBy), quoteIdent(groupBy))
	}

	rows, err := p.conn(ctx).QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, p.mapError(err)
	}
	defer func() { _ = rows.Close() }()

	scanVals := make([]any, 0, len(aggs)+1)
	var groupKey sql.NullString
	if groupBy != "" {
		scanVals = append(scanVals, &groupKey)
	}
	values := make([]sql.NullFloat64, len(aggs))
	for i := range values {
		scanVals = append(scanVals, &values[i])
	}

	var groups []databases.AggregateGroup
	for rows.Next() {
		if err := rows.Scan(scanVals...); err != nil {
			return nil, p.mapError(err)
		}
		g := databases.AggregateGroup{Values: make([]databases.AggregateValue, 0, len(aggs))}
		if groupBy != "" {
			if groupKey.Valid {
				key := groupKey.String
				g.GroupKey = &key
			}
		}
		for i, agg := range aggs {
			v := databases.AggregateValue{Function: agg.Function, Field: agg.Field}
			if values[i].Valid {
				fv := values[i].Float64
				v.Value = &fv
			}
			g.Values = append(g.Values, v)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, p.mapError(err)
	}
	if groups == nil && groupBy == "" {
		// 无 group_by 时空集也返回一组（sum=0 / avg=min=max=nil）。
		g := databases.AggregateGroup{Values: make([]databases.AggregateValue, 0, len(aggs))}
		for _, agg := range aggs {
			v := databases.AggregateValue{Function: agg.Function, Field: agg.Field}
			if agg.Function == databases.AggregateSum {
				zero := 0.0
				v.Value = &zero
			}
			g.Values = append(g.Values, v)
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// validColumnName 限制字段名为安全的小写标识符（防 SQL 注入）。
func validColumnName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		case r == '_' && i > 0:
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
