package clientgrpc

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// 单库事务 handler（v2 设计 §5）：薄映射到 client.Transactions。

func (s *DatabasesService) CreateTransaction(ctx context.Context, req *clientv1.CreateTransactionRequest) (*clientv1.Transaction, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions")
	tx, err := s.transactions.CreateTransaction(ctx, req.GetDatabaseId())
	if err != nil {
		return nil, err
	}
	return mapClientTransaction(tx, nil)
}

func (s *DatabasesService) GetTransaction(ctx context.Context, req *clientv1.GetTransactionRequest) (*clientv1.Transaction, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	tx, ops, err := s.transactions.GetTransaction(ctx, req.GetDatabaseId(), req.GetTransactionId())
	if err != nil {
		return nil, err
	}
	return mapClientTransaction(tx, ops)
}

func (s *DatabasesService) CreateTransactionDocument(ctx context.Context, req *clientv1.CreateTransactionDocumentRequest) (*clientv1.TransactionOp, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.transactions.CreateTransactionDocument(ctx, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), updateData(req.GetData()), perms)
	if err != nil {
		return nil, err
	}
	return mapClientTransactionOp(op)
}

func (s *DatabasesService) UpdateTransactionDocument(ctx context.Context, req *clientv1.UpdateTransactionDocumentRequest) (*clientv1.TransactionOp, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.transactions.UpdateTransactionDocument(ctx, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), updateData(req.GetData()), perms, req.GetIncrement(), req.Version)
	if err != nil {
		return nil, err
	}
	return mapClientTransactionOp(op)
}

func (s *DatabasesService) DeleteTransactionDocument(ctx context.Context, req *clientv1.DeleteTransactionDocumentRequest) (*clientv1.TransactionOp, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	op, err := s.transactions.DeleteTransactionDocument(ctx, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), req.Version)
	if err != nil {
		return nil, err
	}
	return mapClientTransactionOp(op)
}

func (s *DatabasesService) UpsertTransactionDocument(ctx context.Context, req *clientv1.UpsertTransactionDocumentRequest) (*clientv1.TransactionOp, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.transactions.UpsertTransactionDocument(ctx, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), updateData(req.GetData()), req.GetConflictColumns(), perms)
	if err != nil {
		return nil, err
	}
	return mapClientTransactionOp(op)
}

func (s *DatabasesService) CommitTransaction(ctx context.Context, req *clientv1.CommitTransactionRequest) (*clientv1.Transaction, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	tx, ops, err := s.transactions.CommitTransaction(ctx, req.GetDatabaseId(), req.GetTransactionId())
	if err != nil {
		return nil, err
	}
	return mapClientTransaction(tx, ops)
}

func (s *DatabasesService) RollbackTransaction(ctx context.Context, req *clientv1.RollbackTransactionRequest) (*clientv1.Transaction, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/transactions/"+req.GetTransactionId())
	tx, err := s.transactions.RollbackTransaction(ctx, req.GetDatabaseId(), req.GetTransactionId())
	if err != nil {
		return nil, err
	}
	return mapClientTransaction(tx, nil)
}

func mapClientTransaction(tx *databases.Transaction, ops []databases.TransactionOp) (*clientv1.Transaction, error) {
	if tx == nil {
		return nil, nil
	}
	out := &clientv1.Transaction{
		Id:         tx.ID,
		DatabaseId: tx.DatabaseID,
		Status:     tx.Status,
		CreatedBy:  tx.CreatedBy,
		ExpireAt:   timestamppb.New(tx.ExpireAt),
		CreatedAt:  timestamppb.New(tx.CreatedAt),
		UpdatedAt:  timestamppb.New(tx.UpdatedAt),
	}
	for i := range ops {
		mapped, err := mapClientTransactionOp(&ops[i])
		if err != nil {
			return nil, err
		}
		out.Operations = append(out.Operations, mapped)
	}
	return out, nil
}

func mapClientTransactionOp(op *databases.TransactionOp) (*clientv1.TransactionOp, error) {
	if op == nil {
		return nil, nil
	}
	out := &clientv1.TransactionOp{
		Id:           op.ID,
		Seq:          op.Seq,
		OpType:       op.OpType,
		CollectionId: op.CollectionID,
		DocumentId:   op.DocumentID,
		Increment:    op.Increment,
		Version:      op.Version,
	}
	for _, p := range op.Permissions {
		out.Permissions = append(out.Permissions, databases.FormatPermissionString(p))
	}
	if len(op.Data) > 0 {
		data, err := structpb.NewStruct(op.Data)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "operation data is not serializable")
		}
		out.Data = data
	}
	return out, nil
}
