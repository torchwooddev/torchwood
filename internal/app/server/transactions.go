package server

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Transactions 是 Server API 的单库事务用例（v2 设计 §5）：共用逻辑在
// internal/app/shared.Transactions，本层负责项目解析、Server 写主体守卫
// （RequireServerWriteActor）与 op 追加时的 Server 侧权限校验。
type Transactions struct {
	projectRepo projects.Repository
	core        *shared.Transactions
}

func NewTransactions(projectRepo projects.Repository, core *shared.Transactions) *Transactions {
	return &Transactions{projectRepo: projectRepo, core: core}
}

func (t *Transactions) resolveProject(ctx context.Context, projectID string) error {
	p, err := t.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if p == nil {
		return status.Error(codes.NotFound, "project not found")
	}
	return nil
}

// serverTxPrincipal 以调用方 admin/key 主体构造文档写 principal（与
// servergrpc.dbPrincipal 同口径）。
func serverTxPrincipal(ctx context.Context) databases.Principal {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return databases.Principal{}
	}
	return databases.Principal{Roles: p.Roles, PlatformAdmin: p.IsPlatformAdmin}
}

func (t *Transactions) CreateTransaction(ctx context.Context, projectID, databaseID string) (*databases.Transaction, error) {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return nil, err
	}
	if err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return t.core.Create(ctx, projectID, databaseID)
}

func (t *Transactions) GetTransaction(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, []databases.TransactionOp, error) {
	if err := t.resolveProject(ctx, projectID); err != nil {
		return nil, nil, err
	}
	return t.core.Get(ctx, projectID, databaseID, txID)
}

func (t *Transactions) CreateTransactionDocument(ctx context.Context, projectID, databaseID, txID, collectionID, documentID string, data map[string]any, perms []databases.Permission) (*databases.TransactionOp, error) {
	op := databases.TransactionOp{
		OpType:       databases.TransactionOpCreate,
		CollectionID: collectionID,
		DocumentID:   documentID,
		Data:         data,
		Permissions:  perms,
	}
	return t.appendOp(ctx, projectID, databaseID, txID, op)
}

func (t *Transactions) UpdateTransactionDocument(ctx context.Context, projectID, databaseID, txID, collectionID, documentID string, data map[string]any, perms []databases.Permission, increment map[string]int64, version *int64) (*databases.TransactionOp, error) {
	op := databases.TransactionOp{
		OpType:       databases.TransactionOpUpdate,
		CollectionID: collectionID,
		DocumentID:   documentID,
		Data:         data,
		Permissions:  perms,
		Increment:    increment,
		Version:      version,
	}
	return t.appendOp(ctx, projectID, databaseID, txID, op)
}

func (t *Transactions) DeleteTransactionDocument(ctx context.Context, projectID, databaseID, txID, collectionID, documentID string, version *int64) (*databases.TransactionOp, error) {
	op := databases.TransactionOp{
		OpType:       databases.TransactionOpDelete,
		CollectionID: collectionID,
		DocumentID:   documentID,
		Version:      version,
	}
	return t.appendOp(ctx, projectID, databaseID, txID, op)
}

func (t *Transactions) UpsertTransactionDocument(ctx context.Context, projectID, databaseID, txID, collectionID, documentID string, data map[string]any, conflictColumns []string, perms []databases.Permission) (*databases.TransactionOp, error) {
	op := databases.TransactionOp{
		OpType:          databases.TransactionOpUpsert,
		CollectionID:    collectionID,
		DocumentID:      documentID,
		Data:            data,
		Permissions:     perms,
		ConflictColumns: conflictColumns,
	}
	return t.appendOp(ctx, projectID, databaseID, txID, op)
}

func (t *Transactions) CommitTransaction(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, []databases.TransactionOp, error) {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return nil, nil, err
	}
	if err := t.resolveProject(ctx, projectID); err != nil {
		return nil, nil, err
	}
	return t.core.Commit(ctx, projectID, databaseID, txID, serverTxPrincipal(ctx))
}

func (t *Transactions) RollbackTransaction(ctx context.Context, projectID, databaseID, txID string) (*databases.Transaction, error) {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return nil, err
	}
	if err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return t.core.Rollback(ctx, projectID, databaseID, txID)
}

func (t *Transactions) appendOp(ctx context.Context, projectID, databaseID, txID string, op databases.TransactionOp) (*databases.TransactionOp, error) {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return nil, err
	}
	if err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	principal := serverTxPrincipal(ctx)
	return t.core.Append(ctx, projectID, databaseID, txID, op, t.prepareOp(projectID, databaseID, principal))
}

// prepareOp 是 Server 侧 op 追加校验/归一化：系统集合拒
// （system_collection_not_allowed）、权限模板展开与可授予校验——与单条
// CreateDocument/UpdateDocument 同口径（无 Client 侧敏感字段过滤与 owner
// 默认权限）。
func (t *Transactions) prepareOp(projectID, databaseID string, principal databases.Principal) shared.PrepareTransactionOpFunc {
	allowPrivilegedGrant := principal.PlatformAdmin || principal.HasRole("keys")
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
			op.Permissions = databases.ExpandPermissionTemplates(op.Permissions, principal.Roles)
			if err := databases.ValidateGrantablePermissions(principal, op.Permissions, allowPrivilegedGrant); err != nil {
				return op, status.Error(codes.InvalidArgument, err.Error())
			}
		case databases.TransactionOpUpdate:
			if op.Version == nil {
				return op, status.Error(codes.FailedPrecondition, databases.ErrVersionRequired.Error())
			}
			if len(op.Data) == 0 && len(op.Permissions) == 0 && len(op.Increment) == 0 {
				return op, status.Error(codes.InvalidArgument, "data, permissions, or increment is required")
			}
			if len(op.Permissions) > 0 {
				op.Permissions = databases.ExpandPermissionTemplates(op.Permissions, principal.Roles)
				if err := databases.ValidateGrantablePermissions(principal, op.Permissions, allowPrivilegedGrant); err != nil {
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
