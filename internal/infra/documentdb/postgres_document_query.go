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
	"github.com/torchwooddev/torchwood/pkg/crud"
)

func (p *postgresDocumentDB) ListDocuments(ctx context.Context, projectID, databaseID, collectionID string, q databases.Query, principal databases.Principal) (*databases.DocumentList, error) {
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return nil, err
	}
	parsed, err := astFrom(q)
	if err != nil {
		return nil, err
	}
	tbl := tableName(schema, collectionID)

	// 非 System 路径显式获取集合一次（coll==nil → NotFound，行为从 403 收紧为 404），
	// 复用给权限过滤与字段白名单校验；System 信任路径零额外查询（跳过白名单）。
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	var coll *databases.Collection
	if !principal.BypassesDocumentACL() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return nil, err
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
			return nil, err
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}

	if coll != nil {
		if err := p.validateQueryFields(ctx, schema, parsed, coll, collectionID, isSystem); err != nil {
			return nil, err
		}
	}

	filterWhere, filterArgs, orderSQL, err := buildAppwriteQuery(parsed)
	if err != nil {
		return nil, err
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
	offset := parsed.Offset
	if q.PageToken != "" {
		// keyset token（cursor 模式回传，W-D）：映射回 cursorAfter/Before，
		// 客户端在同请求中重发排序/过滤 queries 即可同构续页。
		if id, kind, ok := decodeKeysetToken(q.PageToken); ok {
			if kind == "before" {
				parsed.CursorBefore = id
			} else {
				parsed.CursorAfter = id
			}
		} else {
			off, err := crud.DecodePageToken(q.PageToken)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, "invalid page token")
			}
			offset = int(off)
		}
	}
	if offset > maxQueryOffset {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("offset exceeds maximum of %d", maxQueryOffset))
	}

	cursor := ""
	cursorKind := ""
	if parsed.CursorAfter != "" {
		cursor, cursorKind = parsed.CursorAfter, "after"
	} else if parsed.CursorBefore != "" {
		cursor, cursorKind = parsed.CursorBefore, "before"
	}
	if cursor != "" {
		// cursor 与 offset 同时传时 cursor 优先，offset 恒 0
		offset = 0
		// 排序键与方向：仅取 Orders[0]，无显式排序则默认 _created_at DESC
		sortField := "_created_at"
		sortDir := "DESC"
		if len(parsed.Orders) > 0 {
			sortField = mapQueryField(parsed.Orders[0].Attribute)
			if parsed.Orders[0].Desc {
				sortDir = "DESC"
			} else {
				sortDir = "ASC"
			}
		}
		// 排序字段必须显式校验（不能沿用 ORDER 路径的静默跳过）
		if !safeNameRe.MatchString(sortField) {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid order field: %s", parsed.Orders[0].Attribute))
		}
		if err := validateDocID(cursor); err != nil {
			return nil, err
		}
		var cursorValue any
		err := p.conn(ctx).QueryRowContext(ctx,
			fmt.Sprintf(`SELECT %s FROM %s WHERE _id = ? AND _tenant = ?`, quoteIdent(sortField), tbl),
			cursor, internalID,
		).Scan(&cursorValue)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, status.Error(codes.InvalidArgument, "cursor document not found")
			}
			return nil, err
		}
		op := ">"
		if (sortDir == "ASC" && cursorKind == "before") || (sortDir == "DESC" && cursorKind == "after") {
			op = "<"
		}
		whereParts = append(whereParts, fmt.Sprintf(`(d.%s, d._id) %s (?, ?)`, quoteIdent(sortField), op))
		args = append(args, cursorValue, cursor)
		// cursor 模式下 ORDER BY 必须与谓词同构
		orderSQL = fmt.Sprintf(`ORDER BY d.%s %s, d._id %s`, quoteIdent(sortField), sortDir, sortDir)
	}

	// W-D：keyset 模式不产出精确 total——COUNT 与数据查询同价（含 EXISTS
	// 权限子查询），对游标续页无意义且把翻页成本翻倍；offset 模式保持精确
	// 计数（TotalCount 语义：keyset 分页下为 0=未知/不适用）。
	var total int64
	if cursor == "" {
		countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s d WHERE %s`, tbl, strings.Join(whereParts, " AND "))
		if err := p.conn(ctx).QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, err
		}
	}

	querySQL := fmt.Sprintf(`SELECT to_jsonb(d.*) AS doc FROM %s d WHERE %s %s LIMIT ? OFFSET ?`, tbl, strings.Join(whereParts, " AND "), orderSQL)
	args = append(args, limit, offset)

	rows, err := p.conn(ctx).QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var docs []databases.Document
	for rows.Next() {
		doc, err := scanDocumentJSON(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// B6：List 回传 permissions（与 Get 对齐）；W-D 改单条 IN 批量取回。
	if err := p.attachDocumentPermissionsBatch(ctx, schema, collectionID, internalID, docs); err != nil {
		return nil, err
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

	next := ""
	if cursor != "" {
		// W-D：keyset 模式的续页 token 编码边界行 id（方向沿用本次请求），
		// 不再编码 offset——此前第二页会静默切回 OFFSET 语义（并发写入下
		// 跳/重行，且受 maxQueryOffset 上限约束）。has-more 以满页判定
		// （无精确 total 可用）。
		if len(docs) == limit {
			if cursorKind == "before" {
				next = encodeKeysetToken("before", docs[0].ID)
			} else {
				next = encodeKeysetToken("after", docs[len(docs)-1].ID)
			}
		}
	} else if len(docs) > 0 && int64(offset+len(docs)) < total {
		next = crud.EncodePageToken(offset + len(docs))
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
		return 0, err
	}
	parsed, err := astFrom(q)
	if err != nil {
		return 0, err
	}
	if parsed.Offset > maxQueryOffset {
		return 0, status.Error(codes.InvalidArgument, fmt.Sprintf("offset exceeds maximum of %d", maxQueryOffset))
	}
	tbl := tableName(schema, collectionID)

	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	var coll *databases.Collection
	if !principal.BypassesDocumentACL() {
		coll, err = p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return 0, err
		}
		if coll == nil {
			return 0, status.Error(codes.NotFound, "collection not found")
		}
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.BypassesDocumentACL() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return 0, err
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}
	if coll != nil {
		if err := p.validateQueryFields(ctx, schema, parsed, coll, collectionID, isSystem); err != nil {
			return 0, err
		}
	}
	filterWhere, filterArgs, _, err := buildAppwriteQuery(parsed)
	if err != nil {
		return 0, err
	}
	if filterWhere != "" {
		whereParts = append(whereParts, filterWhere)
		args = append(args, filterArgs...)
	}

	var total int64
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s d WHERE %s`, tbl, strings.Join(whereParts, " AND "))
	err = p.conn(ctx).QueryRowContext(ctx, sql, args...).Scan(&total)
	return total, err
}

// SumDocumentField 对集合内某数值列求和（如 files.size 用于 storage usage），
// 非 System 主体按 read 权限过滤（仅统计可见文档）。field 白名单校验防注入。
func (p *postgresDocumentDB) SumDocumentField(ctx context.Context, projectID, databaseID, collectionID, field string, principal databases.Principal) (int64, error) {
	if !validColumnName(field) {
		return 0, status.Error(codes.InvalidArgument, "invalid field name")
	}
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return 0, err
	}
	tbl := tableName(schema, collectionID)

	// 白名单校验：字段必须 ∈ 集合声明属性且为数值类型（integer/float），
	// System 与普通主体一视同仁（防拼入任意列名）。
	coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return 0, err
	}
	if coll == nil {
		return 0, status.Error(codes.NotFound, "collection not found")
	}
	allowed := false
	for _, attr := range coll.Attributes {
		if attr.Key != field {
			continue
		}
		switch strings.ToLower(attr.Type) {
		case "integer", "float":
			allowed = true
		}
		break
	}
	if !allowed {
		return 0, status.Error(codes.InvalidArgument, fmt.Sprintf("field %s is not a numeric attribute", field))
	}

	whereParts := []string{"d._tenant = ?"}
	args := []any{internalID}
	if !principal.BypassesDocumentACL() {
		permWhere, permArgs, err := p.listPermissionFilter(ctx, projectID, databaseID, collectionID, schema, coll, principal)
		if err != nil {
			return 0, err
		}
		if permWhere != "" {
			whereParts = append(whereParts, permWhere)
			args = append(args, permArgs...)
		}
	}

	var total int64
	sql := fmt.Sprintf(`SELECT COALESCE(SUM(d.%s), 0) FROM %s d WHERE %s`, quoteIdent(field), tbl, strings.Join(whereParts, " AND "))
	err = p.conn(ctx).QueryRowContext(ctx, sql, args...).Scan(&total)
	return total, err
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
