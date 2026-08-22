// 文档 CRUD：Insert/Upsert/Update/Delete 事务体、OCC、冲突锁、事件发布、行→JSON 扫描。
package documentdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/idgen"
)

func (p *postgresDocumentDB) CreateDocument(ctx context.Context, projectID, databaseID, collectionID string, doc databases.Document, perms []databases.Permission, principal databases.Principal) (databases.Document, error) {
	if clients.InTx(ctx) {
		return p.createDocument(ctx, projectID, databaseID, collectionID, doc, perms, principal)
	}
	var out databases.Document
	if err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		created, err := p.createDocument(txCtx, projectID, databaseID, collectionID, doc, perms, principal)
		if err != nil {
			return err
		}
		out = created
		return nil
	}); err != nil {
		return out, err
	}
	return out, nil
}

// createDocument 是 CreateDocument 的事务体：数据行与 _perms 写入在同一事务内，
// 任一步失败整体回滚，避免权限写入失败时文档数据 fail-open。
func (p *postgresDocumentDB) createDocument(ctx context.Context, projectID, databaseID, collectionID string, doc databases.Document, perms []databases.Permission, principal databases.Principal) (databases.Document, error) {
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return doc, err
	}
	if doc.ID == "" {
		doc.ID = idgen.UUID().String()
	} else if err := validateDocID(doc.ID); err != nil {
		return doc, err
	}
	tbl := tableName(schema, collectionID)
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.requireVersionColumn(ctx, schema, collectionID, isSystem); err != nil {
		return doc, err
	}

	// Check collection-level "create" permission before inserting.
	if !principal.BypassesDocumentACL() {
		if isWriteProtectedSystemCollection(databaseID, collectionID) {
			return doc, ErrPermissionDenied
		}
		coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return doc, err
		}
		if err := p.ensureCollectionAccessible(coll, principal); err != nil {
			return doc, err
		}
		if coll != nil && !databases.CollectionAllows(coll.Permissions, "create", databases.ExpandPermissionRoles(principal.Roles)) {
			return doc, ErrPermissionDenied
		}
	}

	columns, placeholders, args := buildInsertParts(doc)
	createdBy := userIDFromPrincipal(principal)
	if createdBy != "" {
		if columns != "" {
			columns += ", "
			placeholders += ", "
		}
		columns += quoteIdent("_created_by") + ", " + quoteIdent("_updated_by")
		placeholders += "?, ?"
		args = append(args, createdBy, createdBy)
	}
	args = append([]any{doc.ID}, args...)
	allPlaceholders := "?"
	if columns != "" {
		allPlaceholders = "?, " + placeholders
		columns = ", " + columns
	}
	sql := fmt.Sprintf(`INSERT INTO %s (_id%s) VALUES (%s)`, tbl, columns, allPlaceholders)
	if _, err := p.conn(ctx).ExecContext(ctx, sql, args...); err != nil {
		if isUniqueViolation(err) {
			return doc, fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
		}
		return doc, fmt.Errorf("insert document: %w", err)
	}
	if err := p.setPermissions(ctx, schema, collectionID, doc.ID, internalID, perms); err != nil {
		return doc, err
	}
	created, err := p.GetDocument(ctx, projectID, databaseID, collectionID, doc.ID, databases.SystemPrincipal)
	if err != nil {
		return doc, err
	}
	if created == nil {
		return doc, errors.New("document not found after insert")
	}
	// create 事件：acl=写后（读回已含 _perms）；系统集合不发布。
	if err := p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, created.ID,
		domainevents.EventDocumentsCreate, created.Version, created,
		created.Permissions, len(created.Permissions) > 0); err != nil {
		return doc, err
	}
	return *created, nil
}

// UpsertDocument inserts doc, or when a row already matches conflictColumns
// (which must correspond to a unique index on the collection table), updates
// its data, _updated_at, _updated_by and replaces document permissions with
// perms (Appwrite-style replace semantics). The insert and update are
// performed atomically by a single INSERT ... ON CONFLICT (...) DO UPDATE SET
// statement.
func (p *postgresDocumentDB) UpsertDocument(ctx context.Context, projectID, databaseID, collectionID string, doc databases.Document, conflictColumns []string, perms []databases.Permission, principal databases.Principal) (databases.Document, error) {
	if err := validateDocID(doc.ID); err != nil {
		return doc, err
	}
	if len(conflictColumns) == 0 {
		return doc, status.Error(codes.InvalidArgument, "conflict columns are required")
	}
	conflictCols := make([]string, 0, len(conflictColumns))
	for _, col := range conflictColumns {
		if !safeNameRe.MatchString(col) || strings.HasPrefix(col, "_") {
			return doc, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid conflict column: %s", col))
		}
		conflictCols = append(conflictCols, quoteIdent(col))
	}
	if clients.InTx(ctx) {
		return p.upsertDocument(ctx, projectID, databaseID, collectionID, doc, conflictCols, conflictColumns, perms, principal)
	}
	var out databases.Document
	if err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		upserted, err := p.upsertDocument(txCtx, projectID, databaseID, collectionID, doc, conflictCols, conflictColumns, perms, principal)
		if err != nil {
			return err
		}
		out = upserted
		return nil
	}); err != nil {
		return out, err
	}
	return out, nil
}

// upsertDocument 是 UpsertDocument 的事务体（P0-1 TOCTOU 修复）：
// 预查与 INSERT ... ON CONFLICT DO UPDATE 包在同一事务，并对
// (_tenant, 冲突列值) 取 pg_advisory_xact_lock 串行化并发 upsert——
// 攻击者的预查发生在受害者提交之后（取锁后重查命中行），按 update
// 权限拒绝，无法再借 create 权限改写他人行；权限替换与读回同事务。
func (p *postgresDocumentDB) upsertDocument(ctx context.Context, projectID, databaseID, collectionID string, doc databases.Document, conflictCols, conflictColumns []string, perms []databases.Permission, principal databases.Principal) (databases.Document, error) {
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return doc, err
	}
	tbl := tableName(schema, collectionID)
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.requireVersionColumn(ctx, schema, collectionID, isSystem); err != nil {
		return doc, err
	}

	conflictValues, err := conflictArgs(conflictColumns, doc.Data)
	if err != nil {
		return doc, err
	}

	// 串行化同一冲突值上的并发 upsert（锁键仅用于互斥，碰撞仅导致轻微串行）。
	// 对所有主体（含 System）生效：System 并发插入若发生在预查与 INSERT 之间，
	// 会令 DO UPDATE 分支静默命中他人行（P0-1 残余窗口）。
	if _, err := p.conn(ctx).ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`,
		strconv.FormatInt(internalID, 10), conflictLockKey(conflictValues)); err != nil {
		return doc, fmt.Errorf("acquire upsert lock: %w", err)
	}

	// 预查冲突目标行（所有主体）：命中的 _id 决定后续权限检查、权限替换与读回
	// 的目标（P2-1：新 _id + 冲突值命中他人行时，数据/权限作用在目标行上）。
	var targetID string
	row := p.conn(ctx).QueryRowContext(ctx, fmt.Sprintf(
		`SELECT _id FROM %s WHERE _tenant = ? AND %s LIMIT 1`,
		tbl, conflictWhereClause(conflictColumns)),
		append([]any{internalID}, conflictValues...)...)
	if err := row.Scan(&targetID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return doc, err
	}

	if !principal.BypassesDocumentACL() {
		if isWriteProtectedSystemCollection(databaseID, collectionID) {
			return doc, ErrPermissionDenied
		}
		coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
		if err != nil {
			return doc, err
		}
		if err := p.ensureCollectionAccessible(coll, principal); err != nil {
			return doc, err
		}
		// 权限分叉以「conflictColumns 命中的目标行」为准，而非 doc.ID（P0-1）：
		// 命中已存在行 → 对该行做文档级 update 检查（UpdateDocument 语义）；
		// 未命中 → 纯插入 → 集合级 create 权限（CreateDocument 语义）。
		if targetID != "" {
			if err := p.checkDocumentPermission(ctx, projectID, databaseID, schema, collectionID, targetID, internalID, "update", principal, coll); err != nil {
				return doc, err
			}
		} else if !databases.CollectionAllows(coll.Permissions, "create", databases.ExpandPermissionRoles(principal.Roles)) {
			return doc, ErrPermissionDenied
		}
	}
	// 目标行的实际 ID：预查命中用目标行，纯插入用 doc.ID。
	effectiveID := doc.ID
	if targetID != "" {
		effectiveID = targetID
	}

	// INSERT 部分（含 _created_by/_updated_by 审计列，与 CreateDocument 一致）。
	columns, placeholders, args := buildInsertParts(doc)
	createdBy := userIDFromPrincipal(principal)
	if createdBy != "" {
		if columns != "" {
			columns += ", "
			placeholders += ", "
		}
		columns += quoteIdent("_created_by") + ", " + quoteIdent("_updated_by")
		placeholders += "?, ?"
		args = append(args, createdBy, createdBy)
	}
	args = append([]any{doc.ID}, args...)
	allPlaceholders := "?"
	if columns != "" {
		allPlaceholders = "?, " + placeholders
		columns = ", " + columns
	}
	// ON CONFLICT 的 SET 部分（含 _updated_at/_updated_by 更新，与 UpdateDocument 一致）。
	setParts, setArgs := buildUpdateParts(doc, userIDFromPrincipal(principal))
	if len(setParts) == 0 {
		return doc, status.Error(codes.InvalidArgument, "no fields to upsert")
	}
	// Upsert 更新支：盲写（不做 OCC），但用户集合 _version 仍 +1。
	// ON CONFLICT DO UPDATE 中未限定列名在目标行与 excluded 之间歧义（42702），
	// 用表别名 t 显式限定目标行。
	if !isSystem {
		setParts = append(setParts, "_version = t._version + 1")
	}
	args = append(args, setArgs...)
	sql := fmt.Sprintf(`INSERT INTO %s AS t (_id%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s`,
		tbl, columns, allPlaceholders, strings.Join(conflictCols, ", "), strings.Join(setParts, ", "))
	if _, err := p.conn(ctx).ExecContext(ctx, sql, args...); err != nil {
		if isUniqueViolation(err) {
			return doc, fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
		}
		return doc, fmt.Errorf("upsert document: %w", err)
	}
	// 文档级权限替换语义（与 UpdateDocument 一致）：非空时先清后写。
	// 作用于目标行（effectiveID），而非调用方传入的 doc.ID。
	// upsert 更新支事件 acl=写前：在替换 _perms 之前抓拍。
	var prePerms []databases.Permission
	var preHasPerms bool
	if targetID != "" && !isSystem && p.pub != nil {
		prePerms, preHasPerms, err = p.getDocumentPermissions(ctx, schema, collectionID, effectiveID, internalID)
		if err != nil {
			return doc, err
		}
	}
	if len(perms) > 0 {
		if err := p.clearPermissions(ctx, schema, collectionID, effectiveID, internalID); err != nil {
			return doc, err
		}
		if err := p.setPermissions(ctx, schema, collectionID, effectiveID, internalID, perms); err != nil {
			return doc, err
		}
	}
	upserted, err := p.GetDocument(ctx, projectID, databaseID, collectionID, effectiveID, databases.SystemPrincipal)
	if err != nil {
		return doc, err
	}
	if upserted == nil {
		return doc, errors.New("document not found after upsert")
	}
	// upsert 事件：插入支 → create（acl=写后），更新支 → update（acl=写前）；
	// 系统集合不发布。
	if targetID == "" {
		if err := p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, upserted.ID,
			domainevents.EventDocumentsCreate, upserted.Version, upserted,
			upserted.Permissions, len(upserted.Permissions) > 0); err != nil {
			return doc, err
		}
	} else if err := p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, upserted.ID,
		domainevents.EventDocumentsUpdate, upserted.Version, upserted, prePerms, preHasPerms); err != nil {
		return doc, err
	}
	return *upserted, nil
}

// conflictLockKey 序列化冲突列值作为 advisory lock 键；仅用于互斥，
// 碰撞只造成轻微串行化，不影响正确性。采用 JSON 编码（而非分隔符拼接）：
// 拼接 + "\x00" 分隔在值本身含 \x00 或空串时存在歧义（R02-P2-2），
// JSON 对字符串转义与类型（int64 vs 字符串 "1"）均无歧义。
func conflictLockKey(values []any) string {
	b, err := json.Marshal(values)
	if err != nil {
		// 文档数据理论上均为 JSON 兼容类型；失败时回退空串，
		// 仅导致这些值共享同一锁键（轻微串行化），不影响正确性。
		return ""
	}
	return string(b)
}

func (p *postgresDocumentDB) GetDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, principal databases.Principal) (*databases.Document, error) {
	if err := validateDocID(docID); err != nil {
		return nil, err
	}
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return nil, err
	}
	row := p.conn(ctx).QueryRowContext(ctx, fmt.Sprintf(`SELECT to_jsonb(d.*) AS doc FROM %s d WHERE d._id = ? AND d._tenant = ?`, tableName(schema, collectionID)), docID, internalID)
	doc, err := scanDocumentJSON(row)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, nil
	}
	if err := p.checkDocumentPermission(ctx, projectID, databaseID, schema, collectionID, docID, internalID, "read", principal, nil); err != nil {
		return nil, err
	}
	if err := p.attachDocumentPermissions(ctx, schema, collectionID, internalID, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (p *postgresDocumentDB) UpdateDocument(ctx context.Context, projectID, databaseID, collectionID string, update databases.DocumentUpdate, principal databases.Principal) (databases.Document, error) {
	if clients.InTx(ctx) {
		return p.updateDocument(ctx, projectID, databaseID, collectionID, update, principal)
	}
	var out databases.Document
	if err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		updated, err := p.updateDocument(txCtx, projectID, databaseID, collectionID, update, principal)
		if err != nil {
			return err
		}
		out = updated
		return nil
	}); err != nil {
		return out, err
	}
	return out, nil
}

// updateDocument 是 UpdateDocument 的事务体：数据语句与 _perms 替换在同一
// 事务内，任一步失败整体回滚；目标文档不存在时返回 ErrDocumentNotFound。
// 用户集合（非系统）强制 OCC：ExpectedVersion 必填且须等于当前行 _version；
// SkipVersion（仅 Bulk 内部循环）跳过校验但同样 _version = _version + 1。
func (p *postgresDocumentDB) updateDocument(ctx context.Context, projectID, databaseID, collectionID string, update databases.DocumentUpdate, principal databases.Principal) (databases.Document, error) {
	doc := update.Document
	if err := validateDocID(doc.ID); err != nil {
		return doc, err
	}
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return doc, err
	}
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.requireVersionColumn(ctx, schema, collectionID, isSystem); err != nil {
		return doc, err
	}
	occ := false
	if !isSystem && !update.SkipVersion {
		if update.ExpectedVersion <= 0 {
			return doc, databases.ErrVersionRequired
		}
		occ = true
	}
	// 非 System 且非文档 owner（user:<id> 匹配）时，禁止写入写保护系统集合（纵深防御，
	// 与 CreateDocument/DeleteDocument 对齐，安全评审 C1 第 2 层）。
	// owner 例外：end-user 自助路径（UpdateAccount/UpdatePrefs）以 user:<id> 角色更新自己的 users 文档。
	if !principal.BypassesDocumentACL() &&
		!principal.HasRole(fmt.Sprintf("user:%s", doc.ID)) &&
		isWriteProtectedSystemCollection(databaseID, collectionID) {
		return doc, ErrPermissionDenied
	}
	// D3：UpdateDocument 仅检查 update 权限，不再强制 read 预检
	// （对齐 Appwrite/Supabase：update 策略独立于 select 策略；B1 文档级优先下
	// "仅持 update 权限"的文档对持权者可用）。
	if err := p.checkDocumentPermission(ctx, projectID, databaseID, schema, collectionID, doc.ID, internalID, "update", principal, nil); err != nil {
		return doc, err
	}
	// update 事件 acl=写前：在 SET / _perms 替换之前抓拍当前文档 _perms。
	var prePerms []databases.Permission
	var preHasPerms bool
	if !isSystem && p.pub != nil {
		prePerms, preHasPerms, err = p.getDocumentPermissions(ctx, schema, collectionID, doc.ID, internalID)
		if err != nil {
			return doc, err
		}
	}
	tbl := tableName(schema, collectionID)
	updatedBy := userIDFromPrincipal(principal)
	setParts, args := buildUpdateParts(doc, updatedBy)
	incParts, incArgs := buildIncrementParts(update.Increment)
	setParts = append(setParts, incParts...)
	args = append(args, incArgs...)
	if len(setParts) == 0 && len(update.Permissions) == 0 {
		return doc, fmt.Errorf("%w", databases.ErrNoFieldsToUpdate)
	}
	if len(setParts) == 0 {
		// R02-P1-4：仅权限变更时同样刷新审计列，保证 _updated_at/_updated_by
		// 反映"最后修改"语义（buildUpdateParts 对空 Data 返回空 setParts）。
		setParts = append(setParts, "_updated_at = ?")
		args = append(args, time.Now())
		if updatedBy != "" {
			setParts = append(setParts, quoteIdent("_updated_by")+" = ?")
			args = append(args, updatedBy)
		}
	}
	if len(setParts) > 0 {
		if !isSystem {
			// 每次成功写 +1（含权限-only 更新与 Increment；SkipVersion 同样 +1）。
			setParts = append(setParts, "_version = _version + 1")
		}
		args = append(args, doc.ID, internalID)
		where := "_id = ? AND _tenant = ?"
		if occ {
			where += " AND _version = ?"
			args = append(args, update.ExpectedVersion)
		}
		sqlText := fmt.Sprintf(`UPDATE %s SET %s WHERE %s`, tbl, strings.Join(setParts, ", "), where)
		res, err := p.conn(ctx).ExecContext(ctx, sqlText, args...)
		if err != nil {
			if isUniqueViolation(err) {
				return doc, fmt.Errorf("%w: %s", ErrDuplicateKey, err.Error())
			}
			return doc, fmt.Errorf("update document: %w", err)
		}
		if occ {
			affected, err := res.RowsAffected()
			if err != nil {
				return doc, err
			}
			if affected == 0 {
				// 区分"行不存在"与"version 不匹配"（不落 PG 未定义列错误）。
				var existsID string
				err := p.conn(ctx).QueryRowContext(ctx,
					fmt.Sprintf(`SELECT _id FROM %s WHERE _id = ? AND _tenant = ?`, tbl),
					doc.ID, internalID,
				).Scan(&existsID)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return doc, fmt.Errorf("%w", databases.ErrDocumentNotFound)
					}
					return doc, err
				}
				return doc, databases.ErrVersionMismatch
			}
		}
	}
	if len(update.Permissions) > 0 {
		if err := p.clearPermissions(ctx, schema, collectionID, doc.ID, internalID); err != nil {
			return doc, err
		}
		if err := p.setPermissions(ctx, schema, collectionID, doc.ID, internalID, update.Permissions); err != nil {
			return doc, err
		}
	}
	// D5：尾随读回用 SystemPrincipal（与 CreateDocument 一致）——B1 下把文档权限
	// 改成不含自己 read 的集合后数据已提交，若仍以调用方 principal 读回会返回
	// PermissionDenied（半完成状态）。
	updated, err := p.GetDocument(ctx, projectID, databaseID, collectionID, doc.ID, databases.SystemPrincipal)
	if err != nil {
		return doc, err
	}
	if updated == nil {
		return doc, fmt.Errorf("%w", databases.ErrDocumentNotFound)
	}
	// update / increment 事件：acl=写前快照；系统集合不发布。
	if err := p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, updated.ID,
		domainevents.EventDocumentsUpdate, updated.Version, updated, prePerms, preHasPerms); err != nil {
		return doc, err
	}
	return *updated, nil
}

func (p *postgresDocumentDB) DeleteDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, opts databases.DeleteOptions, principal databases.Principal) error {
	if err := validateDocID(docID); err != nil {
		return err
	}
	if clients.InTx(ctx) {
		return p.deleteDocument(ctx, projectID, databaseID, collectionID, docID, opts, principal)
	}
	return p.db.RunInTx(ctx, func(txCtx context.Context) error {
		return p.deleteDocument(txCtx, projectID, databaseID, collectionID, docID, opts, principal)
	})
}

// deleteDocument 是 DeleteDocument 的事务体：_perms 清理与数据行删除在同一
// 事务内，删除失败时不会残留权限行。用户集合（非系统）强制 OCC：
// ExpectedVersion 必填且须等于当前行 _version（行锁下比较，防止竞态）。
func (p *postgresDocumentDB) deleteDocument(ctx context.Context, projectID, databaseID, collectionID, docID string, opts databases.DeleteOptions, principal databases.Principal) error {
	internalID, schema, err := p.documentSchema(ctx, projectID, databaseID)
	if err != nil {
		return err
	}
	isSystem := databases.IsSystemCollection(projectID, databaseID, collectionID)
	if err := p.requireVersionColumn(ctx, schema, collectionID, isSystem); err != nil {
		return err
	}
	if !principal.BypassesDocumentACL() && isWriteProtectedSystemCollection(databaseID, collectionID) {
		return ErrPermissionDenied
	}
	if err := p.checkDocumentPermission(ctx, projectID, databaseID, schema, collectionID, docID, internalID, "delete", principal, nil); err != nil {
		return err
	}
	if !isSystem {
		// 用户集合：取删除前 _version（行锁下比较，防止竞态）；delete 事件
		// 的 version 与 acl 都基于写前状态。
		var currentVersion int64
		err := p.conn(ctx).QueryRowContext(ctx,
			fmt.Sprintf(`SELECT _version FROM %s WHERE _id = ? AND _tenant = ? FOR UPDATE`, tableName(schema, collectionID)),
			docID, internalID,
		).Scan(&currentVersion)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w", databases.ErrDocumentNotFound)
			}
			return err
		}
		if !opts.SkipVersion {
			if opts.ExpectedVersion <= 0 {
				return databases.ErrVersionRequired
			}
			if currentVersion != opts.ExpectedVersion {
				return databases.ErrVersionMismatch
			}
		}
		// 分叉：有 publisher 时必须在清 _perms 之前抓拍写前 ACL，并带删除前
		// version 发 delete 事件后直接返回；无 publisher 走下方公共路径。
		// 两条路径都必须清 _perms、都必须删行（事件失败同样随事务回滚）。
		if p.pub != nil {
			prePerms, preHasPerms, err := p.getDocumentPermissions(ctx, schema, collectionID, docID, internalID)
			if err != nil {
				return err
			}
			if err := p.clearPermissions(ctx, schema, collectionID, docID, internalID); err != nil {
				return err
			}
			if _, err := p.conn(ctx).ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE _id = ? AND _tenant = ?`, tableName(schema, collectionID)), docID, internalID); err != nil {
				return err
			}
			return p.publishDocumentEvent(ctx, projectID, databaseID, collectionID, docID,
				domainevents.EventDocumentsDelete, currentVersion, nil, prePerms, preHasPerms)
		}
	}
	if err := p.clearPermissions(ctx, schema, collectionID, docID, internalID); err != nil {
		return err
	}
	_, err = p.conn(ctx).ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE _id = ? AND _tenant = ?`, tableName(schema, collectionID)), docID, internalID)
	return err
}

// publishDocumentEvent 在文档写成功读回之后、函数返回之前（仍在外层
// RunInTx 内）把事件写入 outbox，与文档行、_perms 同 COMMIT（v2 设计 §3.3）。
// 未注入 EventPublisher（单测）或系统集合时为空操作；acl 快照取当时
// collection ACL + 调用方提供的文档 _perms（create=写后 / update、delete=写前）。
func (p *postgresDocumentDB) publishDocumentEvent(
	ctx context.Context,
	projectID, databaseID, collectionID, docID, event string,
	version int64,
	data *databases.Document,
	docPerms []databases.Permission,
	docHasPerms bool,
) error {
	if p.pub == nil {
		return nil
	}
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return nil
	}
	coll, err := p.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return err
	}
	if coll == nil || coll.IsSystem {
		return nil
	}
	return p.pub.Publish(ctx, domainevents.Envelope{
		Event:        event,
		ProjectID:    projectID,
		DatabaseID:   databaseID,
		CollectionID: collectionID,
		DocumentID:   docID,
		Version:      version,
		CreatedAt:    time.Now(),
		Data:         data,
		ACL: domainevents.ACLSnapshot{
			DocumentSecurity:      coll.DocumentSecurity,
			CollectionPermissions: coll.Permissions,
			DocumentPermissions:   docPerms,
			DocHasPerms:           docHasPerms,
		},
	})
}

func buildInsertParts(doc databases.Document) (columns string, placeholders string, args []any) {
	if len(doc.Data) == 0 {
		return "", "", nil
	}
	var cols []string
	var phs []string
	for k, v := range doc.Data {
		if !safeNameRe.MatchString(k) || strings.HasPrefix(k, "_") {
			continue
		}
		cols = append(cols, quoteIdent(k))
		phs = append(phs, "?")
		args = append(args, v)
	}
	return strings.Join(cols, ", "), strings.Join(phs, ", "), args
}

// conflictArgs extracts the conflict column values from doc.Data in
// conflictColumns order; a missing value is an InvalidArgument error.
func conflictArgs(conflictColumns []string, data map[string]any) ([]any, error) {
	args := make([]any, 0, len(conflictColumns))
	for _, col := range conflictColumns {
		v, ok := data[col]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("missing value for conflict column: %s", col))
		}
		args = append(args, v)
	}
	return args, nil
}

// conflictWhereClause builds an equality predicate matching rows on the
// conflict columns, e.g. `"email" = ? AND "user_id" = ?`.
func conflictWhereClause(conflictColumns []string) string {
	parts := make([]string, 0, len(conflictColumns))
	for _, col := range conflictColumns {
		parts = append(parts, fmt.Sprintf("%s = ?", quoteIdent(col)))
	}
	return strings.Join(parts, " AND ")
}

func buildUpdateParts(doc databases.Document, updatedBy string) (setParts []string, args []any) {
	for k, v := range doc.Data {
		if !safeNameRe.MatchString(k) || strings.HasPrefix(k, "_") {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = ?", quoteIdent(k)))
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return nil, nil
	}
	setParts = append(setParts, "_updated_at = ?")
	args = append(args, time.Now())
	if updatedBy != "" {
		setParts = append(setParts, quoteIdent("_updated_by")+" = ?")
		args = append(args, updatedBy)
	}
	return setParts, args
}

// userIDFromPrincipal extracts the first "user:"-prefixed role ID from the
// principal's roles, or "" when no user role is held.
func userIDFromPrincipal(p databases.Principal) string {
	for _, r := range p.Roles {
		if strings.HasPrefix(r, "user:") {
			return strings.TrimPrefix(r, "user:")
		}
	}
	return ""
}

func scanDocumentJSON(scanner interface{ Scan(dest ...any) error }) (*databases.Document, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	doc := &databases.Document{Data: make(map[string]any)}
	if v, ok := payload["_id"].(string); ok {
		doc.ID = v
	}
	if v, ok := payload["_tenant"].(float64); ok {
		doc.Tenant = int64(v)
	}
	if v, ok := payload["_created_at"].(string); ok {
		doc.CreatedAt, _ = time.Parse(time.RFC3339Nano, v)
	}
	if v, ok := payload["_updated_at"].(string); ok {
		doc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, v)
	}
	if v, ok := payload["_created_by"].(string); ok {
		doc.CreatedBy = v
	}
	if v, ok := payload["_updated_by"].(string); ok {
		doc.UpdatedBy = v
	}
	// _version：用户集合有列时读取；存量表尚未 reconcile（缺列）视为 1，不当硬错
	// （读路径禁止 DDL；与成功补列后的 DEFAULT 1 回填语义一致）。
	if v, ok := payload["_version"].(float64); ok {
		doc.Version = int64(v)
	} else {
		doc.Version = 1
	}
	for k, v := range payload {
		if strings.HasPrefix(k, "_") {
			continue
		}
		doc.Data[k] = v
	}
	return doc, nil
}

func isUniqueViolation(err error) bool {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23505"
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505") || strings.Contains(err.Error(), "unique constraint")
}

func validateDocID(docID string) error {
	if docID == "" {
		return status.Error(codes.InvalidArgument, "document id is required")
	}
	if !docIDRe.MatchString(docID) {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("invalid document id: %s", docID))
	}
	return nil
}
