package client

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Transactions 是 Client API 的单库事务用例（v2 设计 §5）：共用逻辑在
// internal/app/shared.Transactions，本层负责项目/主体解析与 op 追加时的
// Client 侧归一化（敏感字段过滤、owner 默认权限、权限模板展开）。
type Transactions struct {
	projectRepo projects.Repository
	core        *shared.Transactions
}

func NewTransactions(projectRepo projects.Repository, core *shared.Transactions) *Transactions {
	return &Transactions{projectRepo: projectRepo, core: core}
}

func (t *Transactions) resolveProject(ctx context.Context) (*projects.Project, databases.Principal, *domainshared.Principal, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.ProjectID == "" || p.UserID == "" {
		return nil, databases.Principal{}, nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	d := &Databases{projectRepo: t.projectRepo, docDB: t.core.DocumentDB()}
	project, err := d.loadProject(ctx, p.ProjectID)
	if err != nil {
		return nil, databases.Principal{}, nil, err
	}
	return project, p.DocPrincipal(), p, nil
}

func (t *Transactions) CreateTransaction(ctx context.Context, databaseID string) (*databases.Transaction, error) {
	project, _, _, err := t.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	return t.core.Create(ctx, project.ID, databaseID)
}

func (t *Transactions) GetTransaction(ctx context.Context, databaseID, txID string) (*databases.Transaction, []databases.TransactionOp, error) {
	project, _, _, err := t.resolveProject(ctx)
	if err != nil {
		return nil, nil, err
	}
	return t.core.Get(ctx, project.ID, databaseID, txID)
}

// CreateTransactionDocument 追加一条 create op。
func (t *Transactions) CreateTransactionDocument(ctx context.Context, databaseID, txID, collectionID, documentID string, data map[string]any, perms []databases.Permission) (*databases.TransactionOp, error) {
	project, principal, p, err := t.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	op := databases.TransactionOp{
		OpType:       databases.TransactionOpCreate,
		CollectionID: collectionID,
		DocumentID:   documentID,
		Data:         data,
		Permissions:  perms,
	}
	return t.core.Append(ctx, project.ID, databaseID, txID, op, t.prepareOp(project.ID, databaseID, principal, p))
}

// UpdateTransactionDocument 追加一条 update op（version 必填 presence）。
func (t *Transactions) UpdateTransactionDocument(ctx context.Context, databaseID, txID, collectionID, documentID string, data map[string]any, perms []databases.Permission, increment map[string]int64, version *int64) (*databases.TransactionOp, error) {
	project, principal, p, err := t.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	op := databases.TransactionOp{
		OpType:       databases.TransactionOpUpdate,
		CollectionID: collectionID,
		DocumentID:   documentID,
		Data:         data,
		Permissions:  perms,
		Increment:    increment,
		Version:      version,
	}
	return t.core.Append(ctx, project.ID, databaseID, txID, op, t.prepareOp(project.ID, databaseID, principal, p))
}

// DeleteTransactionDocument 追加一条 delete op（version 必填 presence）。
func (t *Transactions) DeleteTransactionDocument(ctx context.Context, databaseID, txID, collectionID, documentID string, version *int64) (*databases.TransactionOp, error) {
	project, principal, p, err := t.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	op := databases.TransactionOp{
		OpType:       databases.TransactionOpDelete,
		CollectionID: collectionID,
		DocumentID:   documentID,
		Version:      version,
	}
	return t.core.Append(ctx, project.ID, databaseID, txID, op, t.prepareOp(project.ID, databaseID, principal, p))
}

// UpsertTransactionDocument 追加一条 upsert op。
func (t *Transactions) UpsertTransactionDocument(ctx context.Context, databaseID, txID, collectionID, documentID string, data map[string]any, conflictColumns []string, perms []databases.Permission) (*databases.TransactionOp, error) {
	project, principal, p, err := t.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	op := databases.TransactionOp{
		OpType:          databases.TransactionOpUpsert,
		CollectionID:    collectionID,
		DocumentID:      documentID,
		Data:            data,
		Permissions:     perms,
		ConflictColumns: conflictColumns,
	}
	return t.core.Append(ctx, project.ID, databaseID, txID, op, t.prepareOp(project.ID, databaseID, principal, p))
}

// CommitTransaction 应用全部 ops 并置 committed（空事务成功、无事件）。
func (t *Transactions) CommitTransaction(ctx context.Context, databaseID, txID string) (*databases.Transaction, []databases.TransactionOp, error) {
	project, principal, _, err := t.resolveProject(ctx)
	if err != nil {
		return nil, nil, err
	}
	return t.core.Commit(ctx, project.ID, databaseID, txID, principal)
}

func (t *Transactions) RollbackTransaction(ctx context.Context, databaseID, txID string) (*databases.Transaction, error) {
	project, _, _, err := t.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	return t.core.Rollback(ctx, project.ID, databaseID, txID)
}

// prepareOp 是 Client 侧 op 追加校验/归一化：系统集合拒
// （system_collection_not_allowed）、敏感字段过滤、空权限补 owner 默认、
// 权限模板展开与可授予校验——与单条 CreateDocument/UpdateDocument 同口径。
func (t *Transactions) prepareOp(projectID, databaseID string, principal databases.Principal, p *domainshared.Principal) shared.PrepareTransactionOpFunc {
	return func(ctx context.Context, op databases.TransactionOp) (databases.TransactionOp, error) {
		if err := t.core.CheckTransactionCollection(ctx, projectID, databaseID, op.CollectionID); err != nil {
			return op, err
		}
		switch op.OpType {
		case databases.TransactionOpCreate, databases.TransactionOpUpsert:
			if len(op.Data) == 0 {
				return op, status.Error(codes.InvalidArgument, "data is required")
			}
			if op.OpType == databases.TransactionOpUpsert && len(op.ConflictColumns) == 0 {
				return op, status.Error(codes.InvalidArgument, "conflict_columns is required")
			}
			if len(op.Permissions) == 0 {
				op.Permissions = ownerDocumentPermissions(p.UserID)
			}
			op.Data = filterClientProtectedFields(op.Data)
			if len(op.Data) == 0 {
				return op, status.Error(codes.InvalidArgument, "no updatable fields supplied")
			}
			op.Permissions = databases.ExpandPermissionTemplates(op.Permissions, principal.Roles)
			if err := databases.ValidateGrantablePermissions(principal, op.Permissions, false); err != nil {
				return op, status.Error(codes.InvalidArgument, err.Error())
			}
		case databases.TransactionOpUpdate:
			if op.Version == nil {
				return op, status.Error(codes.FailedPrecondition, databases.ErrVersionRequired.Error())
			}
			if len(op.Data) == 0 && len(op.Permissions) == 0 && len(op.Increment) == 0 {
				return op, status.Error(codes.InvalidArgument, "data, permissions, or increment is required")
			}
			op.Data = filterClientProtectedFields(op.Data)
			if len(op.Data) == 0 && len(op.Permissions) == 0 && len(op.Increment) == 0 {
				return op, status.Error(codes.InvalidArgument, "no updatable fields supplied")
			}
			if len(op.Permissions) > 0 {
				op.Permissions = databases.ExpandPermissionTemplates(op.Permissions, principal.Roles)
				if err := databases.ValidateGrantablePermissions(principal, op.Permissions, false); err != nil {
					return op, status.Error(codes.InvalidArgument, err.Error())
				}
			}
		case databases.TransactionOpDelete:
			if op.Version == nil {
				return op, status.Error(codes.FailedPrecondition, databases.ErrVersionRequired.Error())
			}
		default:
			return op, status.Error(codes.InvalidArgument, "unknown op_type")
		}
		return op, nil
	}
}

// filterClientProtectedFields 剔除客户端不可直写的敏感字段
// （与 client.Databases.UpdateDocument 的过滤清单一致）。
func filterClientProtectedFields(data map[string]any) map[string]any {
	filtered := make(map[string]any, len(data))
	for k, v := range data {
		if _, ok := clientDocumentUpdateProtectedFields[k]; ok {
			continue
		}
		filtered[k] = v
	}
	return filtered
}
