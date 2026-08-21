package client

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// DatabasesService 封装 Client API 的文档服务（动态文档 CRUD）。
// 默认数据库见 [WithDatabaseID]，可用 [Client.UseDatabase] 切换。
type DatabasesService struct {
	c  *Client
	db string
}

// newDatabasesService 构造绑定指定 db 的文档服务副本。
func newDatabasesService(c *Client, db string) *DatabasesService {
	return &DatabasesService{c: c, db: db}
}

// CreateDocument 在集合中创建文档。data 为任意 JSON 兼容结构。
func (d *DatabasesService) CreateDocument(ctx context.Context, collectionID, documentID string, data map[string]any, permissions []string) (*sharedv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.CreateDocument(ctx, &clientv1.CreateDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         st,
		Permissions:  permissions,
	})
}

// GetDocument 读取文档，不存在时返回 codes.NotFound。
func (d *DatabasesService) GetDocument(ctx context.Context, collectionID, documentID string) (*sharedv1.Document, error) {
	return d.c.databases.GetDocument(ctx, &clientv1.GetDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
	})
}

// UpdateDocument 更新文档字段；increment 对数字字段做原子增量
// （如 {"last_seq": 1}），data 与 increment 可同时使用。
// version 为用户集合 OCC 版本（GetDocument 返回的 version），必填。
func (d *DatabasesService) UpdateDocument(ctx context.Context, collectionID, documentID string, data map[string]any, increment map[string]int64, permissions []string, version int64) (*sharedv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpdateDocument(ctx, &clientv1.UpdateDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         st,
		Permissions:  permissions,
		Increment:    increment,
		Version:      &version,
	})
}

// UpsertDocument 按 conflictColumns（须匹配集合唯一索引）插入或更新文档。
func (d *DatabasesService) UpsertDocument(ctx context.Context, collectionID, documentID string, data map[string]any, conflictColumns, permissions []string) (*sharedv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpsertDocument(ctx, &clientv1.UpsertDocumentRequest{
		DatabaseId:      d.db,
		CollectionId:    collectionID,
		DocumentId:      documentID,
		Data:            st,
		Permissions:     permissions,
		ConflictColumns: conflictColumns,
	})
}

// DeleteDocument 删除文档；version 为用户集合 OCC 版本（GetDocument 返回的 version），必填。
func (d *DatabasesService) DeleteDocument(ctx context.Context, collectionID, documentID string, version int64) error {
	_, err := d.c.databases.DeleteDocument(ctx, &clientv1.DeleteDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Version:      &version,
	})
	return err
}

// ListDocuments 按查询 DSL 列出文档，返回文档列表与下一页游标（空表示无更多）。
func (d *DatabasesService) ListDocuments(ctx context.Context, collectionID string, queries []string, pageSize int32, pageToken string) ([]*sharedv1.Document, string, error) {
	resp, err := d.c.databases.ListDocuments(ctx, &clientv1.ListDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Queries:      queries,
		PageSize:     pageSize,
		PageToken:    pageToken,
	})
	if err != nil {
		return nil, "", err
	}
	var next string
	if resp.Meta != nil {
		next = resp.Meta.NextPageToken
	}
	return resp.Documents, next, nil
}

// CountDocuments 按查询 DSL 统计文档数量。
func (d *DatabasesService) CountDocuments(ctx context.Context, collectionID string, queries []string) (int64, error) {
	resp, err := d.c.databases.CountDocuments(ctx, &clientv1.ListDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Queries:      queries,
	})
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

func toStruct(data map[string]any) (*structpb.Struct, error) {
	if len(data) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(data)
}

// CreateTransaction 创建单库事务（pending，TTL 60s）；同一调用方同一库
// 仅允许一笔 pending（重复创建返回 transaction_already_pending）。
func (d *DatabasesService) CreateTransaction(ctx context.Context) (*clientv1.Transaction, error) {
	return d.c.databases.CreateTransaction(ctx, &clientv1.CreateTransactionRequest{
		DatabaseId: d.db,
	})
}

// GetTransaction 读取事务及其已暂存操作；仅创建者（或有权限的 admin/key）可见。
func (d *DatabasesService) GetTransaction(ctx context.Context, transactionID string) (*clientv1.Transaction, error) {
	return d.c.databases.GetTransaction(ctx, &clientv1.GetTransactionRequest{
		DatabaseId:    d.db,
		TransactionId: transactionID,
	})
}

// CreateTransactionDocument 向事务追加一条 create 操作（Commit 时才落库）。
func (d *DatabasesService) CreateTransactionDocument(ctx context.Context, transactionID, collectionID, documentID string, data map[string]any, permissions []string) (*clientv1.TransactionOp, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.CreateTransactionDocument(ctx, &clientv1.CreateTransactionDocumentRequest{
		DatabaseId:    d.db,
		TransactionId: transactionID,
		CollectionId:  collectionID,
		DocumentId:    documentID,
		Data:          st,
		Permissions:   permissions,
	})
}

// UpdateTransactionDocument 向事务追加一条 update 操作；version 为 OCC 版本，必填。
func (d *DatabasesService) UpdateTransactionDocument(ctx context.Context, transactionID, collectionID, documentID string, data map[string]any, increment map[string]int64, permissions []string, version int64) (*clientv1.TransactionOp, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpdateTransactionDocument(ctx, &clientv1.UpdateTransactionDocumentRequest{
		DatabaseId:    d.db,
		TransactionId: transactionID,
		CollectionId:  collectionID,
		DocumentId:    documentID,
		Data:          st,
		Permissions:   permissions,
		Increment:     increment,
		Version:       &version,
	})
}

// DeleteTransactionDocument 向事务追加一条 delete 操作；version 为 OCC 版本，必填。
func (d *DatabasesService) DeleteTransactionDocument(ctx context.Context, transactionID, collectionID, documentID string, version int64) (*clientv1.TransactionOp, error) {
	return d.c.databases.DeleteTransactionDocument(ctx, &clientv1.DeleteTransactionDocumentRequest{
		DatabaseId:    d.db,
		TransactionId: transactionID,
		CollectionId:  collectionID,
		DocumentId:    documentID,
		Version:       &version,
	})
}

// UpsertTransactionDocument 向事务追加一条 upsert 操作；conflictColumns 须匹配集合唯一索引。
func (d *DatabasesService) UpsertTransactionDocument(ctx context.Context, transactionID, collectionID, documentID string, data map[string]any, conflictColumns, permissions []string) (*clientv1.TransactionOp, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpsertTransactionDocument(ctx, &clientv1.UpsertTransactionDocumentRequest{
		DatabaseId:      d.db,
		TransactionId:   transactionID,
		CollectionId:    collectionID,
		DocumentId:      documentID,
		Data:            st,
		Permissions:     permissions,
		ConflictColumns: conflictColumns,
	})
}

// CommitTransaction 在单段事务内按序应用全部操作并写 outbox（同一
// transaction_id）；任一操作 version/perm 失败则整单回滚。
func (d *DatabasesService) CommitTransaction(ctx context.Context, transactionID string) (*clientv1.Transaction, error) {
	return d.c.databases.CommitTransaction(ctx, &clientv1.CommitTransactionRequest{
		DatabaseId:    d.db,
		TransactionId: transactionID,
	})
}

// RollbackTransaction 放弃 pending 事务（置 rolled_back）。
func (d *DatabasesService) RollbackTransaction(ctx context.Context, transactionID string) (*clientv1.Transaction, error) {
	return d.c.databases.RollbackTransaction(ctx, &clientv1.RollbackTransactionRequest{
		DatabaseId:    d.db,
		TransactionId: transactionID,
	})
}
