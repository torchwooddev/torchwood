package bunrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/uptrace/bun/driver/pgdriver"
)

type transactionRepo struct {
	db *clients.Database
}

func NewTransactionRepository(db *clients.Database) databases.TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) Create(ctx context.Context, tx databases.Transaction) error {
	_, err := r.db.Conn(ctx).NewInsert().Model(mapTransactionToModel(&tx)).Exec(ctx)
	if err != nil {
		// 部分唯一索引 document_transactions_one_pending：同 created_by+project+
		// database 已有 pending → transaction_already_pending（设计 §5.1）。
		// 约束名优先取 SQLSTATE 字段 'N'，驱动未解析时回退错误文本匹配。
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" &&
			pgErr.Field('N') == "document_transactions_one_pending" {
			return databases.ErrTransactionAlreadyPending
		}
		if strings.Contains(err.Error(), "document_transactions_one_pending") {
			return databases.ErrTransactionAlreadyPending
		}
		return err
	}
	return nil
}

func (r *transactionRepo) Get(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, error) {
	m := new(model.DocumentTransaction)
	err := r.db.Conn(ctx).NewSelect().Model(m).
		Where("id = ?", txID).
		Where("project_id = ?", projectID).
		Where("database_id = ?", databaseID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapTransactionToDomain(m), nil
}

func (r *transactionRepo) LockPending(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, error) {
	m := new(model.DocumentTransaction)
	err := r.db.Conn(ctx).NewSelect().Model(m).
		Where("id = ?", txID).
		Where("project_id = ?", projectID).
		Where("database_id = ?", databaseID).
		For("UPDATE").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapTransactionToDomain(m), nil
}

func (r *transactionRepo) AppendOp(ctx context.Context, op databases.TransactionOp) error {
	m, err := mapTransactionOpToModel(&op)
	if err != nil {
		return err
	}
	_, err = r.db.Conn(ctx).NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *transactionRepo) ListOps(ctx context.Context, txID string) ([]databases.TransactionOp, error) {
	var ms []model.DocumentTransactionOp
	if err := r.db.Conn(ctx).NewSelect().Model(&ms).
		Where("transaction_id = ?", txID).
		Order("seq ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]databases.TransactionOp, len(ms))
	for i := range ms {
		op, err := mapTransactionOpToDomain(&ms[i])
		if err != nil {
			return nil, err
		}
		out[i] = *op
	}
	return out, nil
}

func (r *transactionRepo) SetStatus(ctx context.Context, txID, status string) error {
	_, err := r.db.Conn(ctx).NewUpdate().Model((*model.DocumentTransaction)(nil)).
		Set("status = ?", status).
		Set("updated_at = NOW()").
		Where("id = ?", txID).
		Exec(ctx)
	return err
}

func mapTransactionToModel(tx *databases.Transaction) *model.DocumentTransaction {
	return &model.DocumentTransaction{
		ID:         tx.ID,
		ProjectID:  tx.ProjectID,
		DatabaseID: tx.DatabaseID,
		Status:     tx.Status,
		CreatedBy:  tx.CreatedBy,
		ExpireAt:   tx.ExpireAt,
		CreatedAt:  tx.CreatedAt,
		UpdatedAt:  tx.UpdatedAt,
	}
}

func mapTransactionToDomain(m *model.DocumentTransaction) *databases.Transaction {
	return &databases.Transaction{
		ID:         m.ID,
		ProjectID:  m.ProjectID,
		DatabaseID: m.DatabaseID,
		Status:     m.Status,
		CreatedBy:  m.CreatedBy,
		ExpireAt:   m.ExpireAt,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

func mapTransactionOpToModel(op *databases.TransactionOp) (*model.DocumentTransactionOp, error) {
	m := &model.DocumentTransactionOp{
		ID:              op.ID,
		TransactionID:   op.TransactionID,
		Seq:             op.Seq,
		OpType:          op.OpType,
		CollectionID:    op.CollectionID,
		DocumentID:      op.DocumentID,
		Version:         op.Version,
		ConflictColumns: op.ConflictColumns,
		CreatedAt:       op.CreatedAt,
	}
	if op.Data != nil {
		b, err := json.Marshal(op.Data)
		if err != nil {
			return nil, fmt.Errorf("marshal op data: %w", err)
		}
		m.Data = b
	}
	if op.Increment != nil {
		b, err := json.Marshal(op.Increment)
		if err != nil {
			return nil, fmt.Errorf("marshal op increment: %w", err)
		}
		m.Increment = b
	}
	if len(op.Permissions) > 0 {
		m.Permissions = make([]string, len(op.Permissions))
		for i, p := range op.Permissions {
			m.Permissions[i] = databases.FormatPermissionString(p)
		}
	}
	return m, nil
}

func mapTransactionOpToDomain(m *model.DocumentTransactionOp) (*databases.TransactionOp, error) {
	op := &databases.TransactionOp{
		ID:              m.ID,
		TransactionID:   m.TransactionID,
		Seq:             m.Seq,
		OpType:          m.OpType,
		CollectionID:    m.CollectionID,
		DocumentID:      m.DocumentID,
		Version:         m.Version,
		ConflictColumns: m.ConflictColumns,
		CreatedAt:       m.CreatedAt,
	}
	if len(m.Data) > 0 {
		if err := json.Unmarshal(m.Data, &op.Data); err != nil {
			return nil, fmt.Errorf("unmarshal op data: %w", err)
		}
	}
	if len(m.Increment) > 0 {
		if err := json.Unmarshal(m.Increment, &op.Increment); err != nil {
			return nil, fmt.Errorf("unmarshal op increment: %w", err)
		}
	}
	if len(m.Permissions) > 0 {
		perms, err := databases.ParsePermissionStrings(m.Permissions)
		if err != nil {
			return nil, fmt.Errorf("parse op permissions: %w", err)
		}
		op.Permissions = perms
	}
	return op, nil
}
