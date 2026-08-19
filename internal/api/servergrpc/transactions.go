package servergrpc

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// 单库事务 handler（v2 设计 §5）：薄映射到 appserver.Transactions，
// 与文档 CRUD handler 同型（audit 资源路径 databases/{db}/transactions/{tx}）。

func auditTransactionResource(databaseID, transactionID string) string {
	if transactionID == "" {
		return "databases/" + databaseID + "/transactions"
	}
	return "databases/" + databaseID + "/transactions/" + transactionID
}

func (s *DatabasesService) CreateTransaction(ctx context.Context, req *serverv1.CreateTransactionRequest) (*serverv1.Transaction, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), ""))
	tx, err := s.transactions.CreateTransaction(ctx, projectID, req.GetDatabaseId())
	if err != nil {
		return nil, err
	}
	return mapTransaction(tx, nil)
}

func (s *DatabasesService) GetTransaction(ctx context.Context, req *serverv1.GetTransactionRequest) (*serverv1.Transaction, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	tx, ops, err := s.transactions.GetTransaction(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId())
	if err != nil {
		return nil, err
	}
	return mapTransaction(tx, ops)
}

func (s *DatabasesService) CreateTransactionDocument(ctx context.Context, req *serverv1.CreateTransactionDocumentRequest) (*serverv1.TransactionOp, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.transactions.CreateTransactionDocument(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), updateData(req.GetData()), perms)
	if err != nil {
		return nil, err
	}
	return mapTransactionOp(op)
}

func (s *DatabasesService) UpdateTransactionDocument(ctx context.Context, req *serverv1.UpdateTransactionDocumentRequest) (*serverv1.TransactionOp, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.transactions.UpdateTransactionDocument(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), updateData(req.GetData()), perms, req.GetIncrement(), req.Version)
	if err != nil {
		return nil, err
	}
	return mapTransactionOp(op)
}

func (s *DatabasesService) DeleteTransactionDocument(ctx context.Context, req *serverv1.DeleteTransactionDocumentRequest) (*serverv1.TransactionOp, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	op, err := s.transactions.DeleteTransactionDocument(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), req.Version)
	if err != nil {
		return nil, err
	}
	return mapTransactionOp(op)
}

func (s *DatabasesService) UpsertTransactionDocument(ctx context.Context, req *serverv1.UpsertTransactionDocumentRequest) (*serverv1.TransactionOp, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.transactions.UpsertTransactionDocument(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId(),
		req.GetCollectionId(), req.GetDocumentId(), updateData(req.GetData()), req.GetConflictColumns(), perms)
	if err != nil {
		return nil, err
	}
	return mapTransactionOp(op)
}

func (s *DatabasesService) CommitTransaction(ctx context.Context, req *serverv1.CommitTransactionRequest) (*serverv1.Transaction, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	tx, ops, err := s.transactions.CommitTransaction(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId())
	if err != nil {
		return nil, err
	}
	return mapTransaction(tx, ops)
}

func (s *DatabasesService) RollbackTransaction(ctx context.Context, req *serverv1.RollbackTransactionRequest) (*serverv1.Transaction, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditTransactionResource(req.GetDatabaseId(), req.GetTransactionId()))
	tx, err := s.transactions.RollbackTransaction(ctx, projectID, req.GetDatabaseId(), req.GetTransactionId())
	if err != nil {
		return nil, err
	}
	return mapTransaction(tx, nil)
}

func mapTransaction(tx *databases.Transaction, ops []databases.TransactionOp) (*serverv1.Transaction, error) {
	if tx == nil {
		return nil, nil
	}
	out := &serverv1.Transaction{
		Id:         tx.ID,
		DatabaseId: tx.DatabaseID,
		Status:     tx.Status,
		CreatedBy:  tx.CreatedBy,
		ExpireAt:   timestamppb.New(tx.ExpireAt),
		CreatedAt:  timestamppb.New(tx.CreatedAt),
		UpdatedAt:  timestamppb.New(tx.UpdatedAt),
	}
	for i := range ops {
		mapped, err := mapTransactionOp(&ops[i])
		if err != nil {
			return nil, err
		}
		out.Operations = append(out.Operations, mapped)
	}
	return out, nil
}

func mapTransactionOp(op *databases.TransactionOp) (*serverv1.TransactionOp, error) {
	if op == nil {
		return nil, nil
	}
	out := &serverv1.TransactionOp{
		Id:              op.ID,
		Seq:             op.Seq,
		OpType:          op.OpType,
		CollectionId:    op.CollectionID,
		DocumentId:      op.DocumentID,
		Permissions:     formatPermissionStrings(op.Permissions),
		Increment:       op.Increment,
		Version:         op.Version,
		ConflictColumns: op.ConflictColumns,
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
