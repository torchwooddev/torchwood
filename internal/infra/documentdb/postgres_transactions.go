// 事务内核执行器（redesign §4.8 Phase 1 / §11-E1）：异构 op 列表在单个
// RunInTx 内顺序执行——Bulk 的泛化，无暂存表、无会话。
//
//   - 锁纪律（E1）：按 (collection, documentID) 排序对批内全部 op 目标预取
//     pg_advisory_xact_lock，防并发批以不同顺序锁多行互锁；op 仍按请求序
//     执行（批内事件顺序 = op 序），语句级行锁自然与单文档路径串行化。
//   - ATOMIC（默认）：任一 op 失败整批回滚，返回带 op index 的错误。
//   - PARTIAL：逐 op SAVEPOINT 容错（PG 语句失败会 abort 事务，必须回滚到
//     savepoint 才能继续），已成功不回滚，返回 per-op 结果。
package documentdb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
)

const maxTransactionOps = 1000

// ExecuteTransactions 是事务内核的单事务执行器入口。
func (p *postgresDocumentDB) ExecuteTransactions(
	ctx context.Context, projectID, databaseID string,
	ops []databases.TransactionOp, mode databases.TransactionMode,
	principal databases.Principal,
) ([]databases.TransactionOpResult, error) {
	if len(ops) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ops is required")
	}
	if len(ops) > maxTransactionOps {
		return nil, status.Errorf(codes.InvalidArgument, "ops count %d exceeds maximum of %d", len(ops), maxTransactionOps)
	}
	if mode == "" {
		mode = databases.TransactionModeAtomic
	}
	switch mode {
	case databases.TransactionModeAtomic, databases.TransactionModePartial:
	default:
		return nil, status.Errorf(codes.InvalidArgument, "invalid transaction mode %q", mode)
	}

	var results []databases.TransactionOpResult
	err := p.db.RunInTx(ctx, func(txCtx context.Context) error {
		// 批间死锁防护：对批内全部 op 目标（排序后）预取事务级 advisory 锁。
		if err := p.lockTxTargets(txCtx, projectID, databaseID, ops); err != nil {
			return err
		}
		results = results[:0]
		for i, op := range ops {
			// PARTIAL：op 级 savepoint 容错；ATOMIC：失败即中止整批（RunInTx 回滚）。
			sp := "tx_op_" + strconv.Itoa(i)
			if mode == databases.TransactionModePartial {
				if _, err := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`SAVEPOINT %s`, sp)); err != nil {
					return err
				}
			}
			docID, version, err := p.executeTxOp(txCtx, projectID, databaseID, op, principal)
			if err != nil {
				if mode == databases.TransactionModeAtomic {
					return &databases.OpError{Index: i, Err: err}
				}
				if _, rbErr := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`ROLLBACK TO SAVEPOINT %s`, sp)); rbErr != nil {
					return rbErr
				}
				results = append(results, databases.TransactionOpResult{
					Index:      i,
					OK:         false,
					ErrCode:    databases.ErrorDomainCode(err),
					ErrMessage: err.Error(),
				})
				continue
			}
			results = append(results, databases.TransactionOpResult{
				Index:      i,
				OK:         true,
				DocumentID: docID,
				Version:    version,
			})
		}
		return nil
	})
	if err != nil {
		return nil, p.mapError(err)
	}
	return results, nil
}

// executeTxOp 在事务上下文内执行单个 op，返回（目标/实际文档 ID, 写后版本）。
// 复用单文档路径的事务体（它们感知 clients.InTx 并跳过自身 RunInTx），
// 权限判定、OCC、conflictColumns 前置校验与事件发布语义与单文档 API 完全同源。
func (p *postgresDocumentDB) executeTxOp(
	ctx context.Context, projectID, databaseID string,
	op databases.TransactionOp, principal databases.Principal,
) (string, int64, error) {
	switch op.Type {
	case databases.TransactionOpCreate:
		doc, err := p.createDocument(ctx, projectID, databaseID, op.CollectionID, databases.Document{
			ID: op.DocumentID, Data: op.Data,
		}, op.Permissions, principal)
		if err != nil {
			return "", 0, err
		}
		return doc.ID, doc.Version, nil

	case databases.TransactionOpUpdate:
		// Phase 1 裁决②：显式 ≤0 → InvalidArgument（version_invalid）；
		// 缺省（nil）→ 盲写 +1（LWW 契约，复用 SkipVersion 机制）。
		if op.ExpectedVersion != nil && *op.ExpectedVersion <= 0 {
			return "", 0, databases.ErrVersionInvalid
		}
		update := databases.DocumentUpdate{
			Document:    databases.Document{ID: op.DocumentID, Data: op.Data},
			Permissions: op.Permissions,
			Increment:   op.Increment,
		}
		if op.ExpectedVersion != nil {
			update.ExpectedVersion = *op.ExpectedVersion
		} else {
			update.SkipVersion = true
		}
		doc, err := p.updateDocument(ctx, projectID, databaseID, op.CollectionID, update, principal)
		if err != nil {
			return "", 0, err
		}
		return doc.ID, doc.Version, nil

	case databases.TransactionOpUpsert:
		if len(op.ConflictColumns) == 0 {
			return "", 0, status.Error(codes.InvalidArgument, "conflict columns are required")
		}
		// 与 UpsertDocument 入口一致的列校验（非法列精确报错，不静默丢弃）。
		conflictCols := make([]string, 0, len(op.ConflictColumns))
		for _, col := range op.ConflictColumns {
			if !safeNameRe.MatchString(col) || strings.HasPrefix(col, "_") {
				return "", 0, status.Errorf(codes.InvalidArgument, "invalid conflict column: %s", col)
			}
			conflictCols = append(conflictCols, quoteIdent(col))
		}
		doc, err := p.upsertDocument(ctx, projectID, databaseID, op.CollectionID, databases.Document{
			ID: op.DocumentID, Data: op.Data,
		}, conflictCols, op.ConflictColumns, op.Permissions, principal)
		if err != nil {
			return "", 0, err
		}
		return doc.ID, doc.Version, nil

	case databases.TransactionOpDelete:
		// delete 不支持盲删：缺省/≤0 均 version 类错误；显式 ≤0 按 裁决②
		// 为 version_invalid（缺省为 version_required，与单文档 API 一致）。
		if op.ExpectedVersion != nil && *op.ExpectedVersion <= 0 {
			return "", 0, databases.ErrVersionInvalid
		}
		var version int64
		if op.ExpectedVersion != nil {
			version = *op.ExpectedVersion
		}
		if err := p.deleteDocument(ctx, projectID, databaseID, op.CollectionID, op.DocumentID, databases.DeleteOptions{
			ExpectedVersion: version,
		}, principal); err != nil {
			return "", 0, err
		}
		return op.DocumentID, 0, nil

	default:
		return "", 0, status.Errorf(codes.InvalidArgument, "invalid transaction op type %q", op.Type)
	}
}

// lockTxTargets 对批内全部 op 目标按 (collection, documentID) 排序预取事务级
// advisory 锁：并发批以相同顺序拿锁，消除"批 A 锁 d1 等 d2、批 B 锁 d2 等 d1"
// 的批间互锁；单文档路径不受影响（语句级行锁仍与 op 串行化）。
func (p *postgresDocumentDB) lockTxTargets(ctx context.Context, projectID, databaseID string, ops []databases.TransactionOp) error {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return err
	}
	type target struct{ collection, document string }
	targets := make([]target, 0, len(ops))
	seen := make(map[target]struct{}, len(ops))
	for _, op := range ops {
		t := target{op.CollectionID, op.DocumentID}
		// upsert 的目标是冲突行（预查前未知），锁键取 collection+conflict 语义
		// 近似：与 upsertDocument 自身的 advisory lock 键（冲突值）不同键不互
		// 斓——由 op 执行时的行锁兜底，此处只需覆盖确定 ID 的 op。
		if t.document == "" || t.collection == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].collection != targets[j].collection {
			return targets[i].collection < targets[j].collection
		}
		return targets[i].document < targets[j].document
	})
	tenant := strconv.FormatInt(internalID, 10)
	for _, t := range targets {
		if _, err := p.conn(ctx).ExecContext(ctx,
			`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`,
			tenant, t.collection+"."+t.document); err != nil {
			return fmt.Errorf("acquire tx target lock: %w", err)
		}
	}
	return nil
}
