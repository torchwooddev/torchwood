package shared

import (
	"context"
	"errors"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 事务额度（v2 设计 §5.1/§5.2.1）：TTL 60s 不可续，单事务最多 100 条 op。
const (
	TransactionTTL    = 60 * time.Second
	MaxTransactionOps = 100
)

// TransactionActor 计算事务的 created_by 标识：user:{UserID} /
// key:{APIKeyID} / admin:{ActorID}（v2 设计 §5.1）。
func TransactionActor(p *domainshared.Principal) string {
	switch p.ActorKind {
	case domainshared.ActorKindEndUser:
		if p.UserID != "" {
			return "user:" + p.UserID
		}
	case domainshared.ActorKindService:
		if p.APIKeyID != "" {
			return "key:" + p.APIKeyID
		}
	case domainshared.ActorKindAdmin:
		if p.ActorID != "" {
			return "admin:" + string(p.ActorID)
		}
	}
	return ""
}

// transactionActorFromCtx 从 ctx 主体计算 created_by；无法识别时报 Unauthenticated。
func transactionActorFromCtx(ctx context.Context) (string, error) {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "unauthenticated")
	}
	actor := TransactionActor(p)
	if actor == "" {
		return "", status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return actor, nil
}

// CanOperateTransaction 报告主体是否可追加/提交/回滚/查看事务（v2 设计 §5.2）：
// 创建者本人、platform admin、带 databases 写 scope 的 API Key 可操作任意 pending；
// 其余主体（含非创建者端用户）拒绝。
func CanOperateTransaction(p *domainshared.Principal, createdBy string) bool {
	if createdBy != "" && TransactionActor(p) == createdBy {
		return true
	}
	if p.ActorKind == domainshared.ActorKindAdmin && p.IsPlatformAdmin {
		return true
	}
	if p.ActorKind == domainshared.ActorKindService && hasDatabasesWriteScope(p.Permissions) {
		return true
	}
	return false
}

// hasDatabasesWriteScope 与 interceptor.APIKeyScopeAllowed 同口径：
// databases.write / 裸 databases / * / all 视为具备 databases 写权限。
func hasDatabasesWriteScope(scopes []string) bool {
	for _, s := range scopes {
		if s == "*" || s == "all" || s == "databases" || s == "databases.write" {
			return true
		}
	}
	return false
}

// PrepareTransactionOpFunc 在追加 op 时（行锁内）校验并归一化 op：
// Client/Server 各自提供（权限展开、敏感字段过滤、默认值不同）。
// 追加阶段校验失败不改事务 status（设计 §5.2）。
type PrepareTransactionOpFunc func(ctx context.Context, op databases.TransactionOp) (databases.TransactionOp, error)

// Transactions 是 Client/Server 共用的事务用例核心：额度、操作者规则、
// 过期、Commit 语义（锁行 + 按 seq 应用 + WithTransactionID 写 outbox）。
// 不依赖 bun 模型；*clients.Database 仅为 RunInTx 注入。
type Transactions struct {
	txRepo databases.TransactionRepository
	docDB  databases.DocumentDB
	db     *clients.Database
}

func NewTransactions(txRepo databases.TransactionRepository, docDB databases.DocumentDB, db *clients.Database) *Transactions {
	return &Transactions{txRepo: txRepo, docDB: docDB, db: db}
}

// DocumentDB 返回注入的文档数据库端口（供 Client/Server 包装层复用
// loadProject 等既有 helper）。
func (t *Transactions) DocumentDB() databases.DocumentDB { return t.docDB }

// Create 创建 pending 事务；同 created_by+project+database 已有 pending 时
// 命中部分唯一索引 → transaction_already_pending。
func (t *Transactions) Create(ctx context.Context, projectID, databaseID string) (*databases.Transaction, error) {
	actor, err := transactionActorFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := RejectExternalDatabaseID(databaseID); err != nil {
		return nil, err
	}
	dbRow, err := t.docDB.GetDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, err
	}
	if dbRow == nil {
		return nil, status.Error(codes.NotFound, "database not found")
	}
	now := time.Now()
	tx := databases.Transaction{
		ID:         idgen.UUID().String(),
		ProjectID:  projectID,
		DatabaseID: databaseID,
		Status:     databases.TransactionStatusPending,
		CreatedBy:  actor,
		ExpireAt:   now.Add(TransactionTTL),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := t.txRepo.Create(ctx, tx); err != nil {
		return nil, MapDocumentDBError(err)
	}
	return &tx, nil
}

// Get 读取事务及其 ops；仅创建者 / platform admin / databases 写 Key 可见。
func (t *Transactions) Get(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, []databases.TransactionOp, error) {
	if err := RejectExternalDatabaseID(databaseID); err != nil {
		return nil, nil, err
	}
	tx, err := t.txRepo.Get(ctx, projectID, databaseID, txID)
	if err != nil {
		return nil, nil, err
	}
	if tx == nil {
		return nil, nil, status.Error(codes.NotFound, "transaction not found")
	}
	p, ok := contexts.Principal(ctx)
	if !ok || !CanOperateTransaction(p, tx.CreatedBy) {
		return nil, nil, status.Error(codes.PermissionDenied, "permission denied")
	}
	ops, err := t.txRepo.ListOps(ctx, txID)
	if err != nil {
		return nil, nil, err
	}
	return tx, ops, nil
}

// Append 在行锁内追加一条 op（设计 §5.2.1：禁止裸 INSERT；seq 在锁内按
// COALESCE(MAX(seq),0)+1 分配；非 pending / 过期 / 超 100 上限在此拒绝）。
func (t *Transactions) Append(ctx context.Context, projectID, databaseID, txID string, op databases.TransactionOp, prepare PrepareTransactionOpFunc) (*databases.TransactionOp, error) {
	if err := RejectExternalDatabaseID(databaseID); err != nil {
		return nil, err
	}
	var (
		out       *databases.TransactionOp
		expiredTx *databases.Transaction
	)
	err := t.db.RunInTx(ctx, func(txCtx context.Context) error {
		locked, expired, err := t.lockForWrite(txCtx, projectID, databaseID, txID)
		if err != nil {
			return err
		}
		if expired {
			// SET expired 随本事务 COMMIT（返回错误会回滚掉置位）。
			expiredTx = locked
			return nil
		}
		ops, err := t.txRepo.ListOps(txCtx, txID)
		if err != nil {
			return err
		}
		if len(ops) >= MaxTransactionOps {
			return databases.ErrTransactionOpsLimit
		}
		prepared, err := prepare(txCtx, op)
		if err != nil {
			return err
		}
		var maxSeq int32
		for _, o := range ops {
			if o.Seq > maxSeq {
				maxSeq = o.Seq
			}
		}
		prepared.ID = idgen.UUID().String()
		prepared.TransactionID = txID
		prepared.Seq = maxSeq + 1
		prepared.CreatedAt = time.Now()
		if err := t.txRepo.AppendOp(txCtx, prepared); err != nil {
			return err
		}
		out = &prepared
		return nil
	})
	if err != nil {
		return nil, MapDocumentDBError(err)
	}
	if expiredTx != nil {
		return nil, MapDocumentDBError(databases.ErrTransactionExpired)
	}
	return out, nil
}

// Commit 单段事务应用全部 ops（设计 §5.3）：锁行 FOR UPDATE，ctx 注入
// WithTransactionID 后按 seq 调现有 CRUD（outbox 由写路径同事务写入，
// 不二次 INSERT）；空事务直接置 committed、无事件。apply 因 version/perm
// 失败时 applyTx 整段回滚，另开短事务标 rolled_back。
func (t *Transactions) Commit(ctx context.Context, projectID, databaseID, txID string, principal databases.Principal) (*databases.Transaction, []databases.TransactionOp, error) {
	if err := RejectExternalDatabaseID(databaseID); err != nil {
		return nil, nil, err
	}
	var (
		committed *databases.Transaction
		ops       []databases.TransactionOp
		expiredTx *databases.Transaction
	)
	applyErr := t.db.RunInTx(ctx, func(txCtx context.Context) error {
		locked, expired, err := t.lockForWrite(txCtx, projectID, databaseID, txID)
		if err != nil {
			return err
		}
		if expired {
			expiredTx = locked
			return nil
		}
		ops, err = t.txRepo.ListOps(txCtx, txID)
		if err != nil {
			return err
		}
		if len(ops) > 0 {
			applyCtx := domainevents.WithTransactionID(txCtx, txID)
			// 同文档多 op 版本接力：create/upsert-insert 记 1，update 记 prev+1；
			// OCC 期望值以 op 携带的 version 为准（同事务内读己之写）。
			versions := map[string]int64{}
			for i := range ops {
				if err := t.applyOp(applyCtx, projectID, databaseID, principal, &ops[i], versions); err != nil {
					return err
				}
			}
		}
		if err := t.txRepo.SetStatus(txCtx, txID, databases.TransactionStatusCommitted); err != nil {
			return err
		}
		locked.Status = databases.TransactionStatusCommitted
		locked.UpdatedAt = time.Now()
		committed = locked
		return nil
	})
	if applyErr != nil {
		if isVersionOrPermError(applyErr) {
			// applyTx 回滚会把 status 一并滚回 pending；必须另开短事务置
			// rolled_back，否则二次 Commit 会重试同一 tx id（设计 §5.3）。
			_ = t.db.RunInTx(ctx, func(txCtx context.Context) error {
				locked, err := t.txRepo.LockPending(txCtx, projectID, databaseID, txID)
				if err != nil || locked == nil {
					return err
				}
				if locked.Status == databases.TransactionStatusPending {
					return t.txRepo.SetStatus(txCtx, txID, databases.TransactionStatusRolledBack)
				}
				return nil
			})
		}
		return nil, nil, MapDocumentDBError(applyErr)
	}
	if expiredTx != nil {
		return expiredTx, nil, MapDocumentDBError(databases.ErrTransactionExpired)
	}
	return committed, ops, nil
}

// Rollback 将 pending 事务置 rolled_back；非 pending → transaction_not_pending。
func (t *Transactions) Rollback(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, error) {
	if err := RejectExternalDatabaseID(databaseID); err != nil {
		return nil, err
	}
	var (
		out       *databases.Transaction
		expiredTx *databases.Transaction
	)
	err := t.db.RunInTx(ctx, func(txCtx context.Context) error {
		locked, expired, err := t.lockForWrite(txCtx, projectID, databaseID, txID)
		if err != nil {
			return err
		}
		if expired {
			expiredTx = locked
			return nil
		}
		if err := t.txRepo.SetStatus(txCtx, txID, databases.TransactionStatusRolledBack); err != nil {
			return err
		}
		locked.Status = databases.TransactionStatusRolledBack
		locked.UpdatedAt = time.Now()
		out = locked
		return nil
	})
	if err != nil {
		return nil, MapDocumentDBError(err)
	}
	if expiredTx != nil {
		return expiredTx, MapDocumentDBError(databases.ErrTransactionExpired)
	}
	return out, nil
}

// lockForWrite 以 FOR UPDATE 锁定事务行并做公共校验：存在性、操作者规则、
// 非 pending 拒（transaction_not_pending）、过期就地 SET expired（expired=true
// 返回，由调用方提交本事务后再报错，避免置位被回滚）。
func (t *Transactions) lockForWrite(ctx context.Context, projectID, databaseID, txID string) (locked *databases.Transaction, expired bool, err error) {
	locked, err = t.txRepo.LockPending(ctx, projectID, databaseID, txID)
	if err != nil {
		return nil, false, err
	}
	if locked == nil {
		return nil, false, status.Error(codes.NotFound, "transaction not found")
	}
	p, ok := contexts.Principal(ctx)
	if !ok || !CanOperateTransaction(p, locked.CreatedBy) {
		return nil, false, status.Error(codes.PermissionDenied, "permission denied")
	}
	if locked.Status != databases.TransactionStatusPending {
		return nil, false, databases.ErrTransactionNotPending
	}
	if !time.Now().Before(locked.ExpireAt) {
		if err := t.txRepo.SetStatus(ctx, txID, databases.TransactionStatusExpired); err != nil {
			return nil, false, err
		}
		locked.Status = databases.TransactionStatusExpired
		return locked, true, nil
	}
	return locked, false, nil
}

// CheckTransactionCollection 校验事务 op 的目标集合（Client/Server 追加共用）：
// 系统集合（含 is_system=true）→ system_collection_not_allowed；集合不存在 →
// NotFound；停用集合拒绝。追加阶段校验失败不改事务 status（设计 §5.2）。
func (t *Transactions) CheckTransactionCollection(ctx context.Context, projectID, databaseID, collectionID string) error {
	if databases.IsSystemCollection(projectID, databaseID, collectionID) {
		return databases.ErrSystemCollectionNotAllowed
	}
	col, err := t.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return err
	}
	if col == nil {
		return status.Error(codes.NotFound, "collection not found")
	}
	if col.IsSystem {
		return databases.ErrSystemCollectionNotAllowed
	}
	if col.Disabled {
		return databases.ErrPermissionDenied
	}
	return nil
}

// applyOp 按 seq 顺序应用单条 op（设计 §5.3）：权限与单条 CRUD 完全一致
// （update 只查 update 权限）；版本接力经 versions map 记录，OCC 期望值
// 取 op 携带的 version（同一事务内可见本 Tx 前序写入）。
func (t *Transactions) applyOp(ctx context.Context, projectID, databaseID string, principal databases.Principal, op *databases.TransactionOp, versions map[string]int64) error {
	key := op.CollectionID + "/" + op.DocumentID
	var expected int64
	if op.Version != nil {
		expected = *op.Version
	}
	switch op.OpType {
	case databases.TransactionOpCreate:
		created, err := t.docDB.CreateDocument(ctx, projectID, databaseID, op.CollectionID, databases.Document{
			ID:   op.DocumentID,
			Data: op.Data,
		}, op.Permissions, principal)
		if err != nil {
			return err
		}
		versions[key] = created.Version
	case databases.TransactionOpUpdate:
		updated, err := t.docDB.UpdateDocument(ctx, projectID, databaseID, op.CollectionID, databases.DocumentUpdate{
			Document:        databases.Document{ID: op.DocumentID, Data: op.Data},
			Permissions:     op.Permissions,
			Increment:       op.Increment,
			ExpectedVersion: expected,
		}, principal)
		if err != nil {
			return err
		}
		versions[key] = updated.Version
	case databases.TransactionOpDelete:
		if err := t.docDB.DeleteDocument(ctx, projectID, databaseID, op.CollectionID, op.DocumentID,
			databases.DeleteOptions{ExpectedVersion: expected}, principal); err != nil {
			return err
		}
		delete(versions, key)
	case databases.TransactionOpUpsert:
		upserted, err := t.docDB.UpsertDocument(ctx, projectID, databaseID, op.CollectionID, databases.Document{
			ID:   op.DocumentID,
			Data: op.Data,
		}, op.ConflictColumns, op.Permissions, principal)
		if err != nil {
			return err
		}
		// upsert-insert → 1；upsert-update → prev+1（写后读回值即接力值）。
		versions[key] = upserted.Version
	default:
		return status.Error(codes.InvalidArgument, "unknown op_type")
	}
	return nil
}

// isVersionOrPermError 判定 apply 失败是否属于 version/perm 类（触发另开短
// 事务标 rolled_back，设计 §5.3）。
func isVersionOrPermError(err error) bool {
	return errors.Is(err, databases.ErrVersionMismatch) ||
		errors.Is(err, databases.ErrVersionRequired) ||
		errors.Is(err, databases.ErrPermissionDenied)
}
