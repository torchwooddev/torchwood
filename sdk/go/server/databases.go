package server

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// DatabasesService 封装 Server API 的数据库管理服务
// （库/集合/属性/索引/文档，含 upsert 与批量操作）。
// 默认数据库见 [WithDatabaseID]，可用 [Client.UseDatabase] 切换。
type DatabasesService struct {
	c  *Client
	db string
}

// newDatabasesService 构造绑定指定 db 的文档服务副本。
func newDatabasesService(c *Client, db string) *DatabasesService {
	return &DatabasesService{c: c, db: db}
}

// CreateDatabase 创建数据库。
func (d *DatabasesService) CreateDatabase(ctx context.Context, id, name string) (*serverv1.Database, error) {
	return d.c.databases.CreateDatabase(ctx, &serverv1.CreateDatabaseRequest{
		Id:   id,
		Name: name,
	})
}

// GetDatabase 获取数据库。
func (d *DatabasesService) GetDatabase(ctx context.Context, id string) (*serverv1.Database, error) {
	return d.c.databases.GetDatabase(ctx, &serverv1.GetDatabaseRequest{Id: id})
}

// ListDatabases 列出数据库。
func (d *DatabasesService) ListDatabases(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListDatabasesResponse, error) {
	return d.c.databases.ListDatabases(ctx, &sharedv1.ListRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
		Queries:   queries,
	})
}

// CreateCollection 创建集合。documentSecurity 开启文档级权限。
func (d *DatabasesService) CreateCollection(ctx context.Context, collectionID, name string, permissions []string, documentSecurity bool) (*serverv1.Collection, error) {
	return d.c.databases.CreateCollection(ctx, &serverv1.CreateCollectionRequest{
		DatabaseId:       d.db,
		Id:               collectionID,
		Name:             name,
		Permissions:      permissions,
		DocumentSecurity: &documentSecurity,
	})
}

// GetCollection 获取集合。
func (d *DatabasesService) GetCollection(ctx context.Context, collectionID string) (*serverv1.Collection, error) {
	return d.c.databases.GetCollection(ctx, &serverv1.GetCollectionRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
	})
}

// UpdateCollection 更新集合；name 空串与 permissions 为 nil 表示不修改，
// documentSecurity/disabled 用指针区分「不修改」与「显式设 false」。
func (d *DatabasesService) UpdateCollection(ctx context.Context, collectionID, name string, permissions []string, documentSecurity, disabled *bool) (*serverv1.Collection, error) {
	req := &serverv1.UpdateCollectionRequest{
		DatabaseId:       d.db,
		CollectionId:     collectionID,
		DocumentSecurity: documentSecurity,
		Disabled:         disabled,
	}
	// name 为 proto3 optional（R10-P1-6）：空串 = 不修改（保持旧签名语义）。
	if name != "" {
		req.Name = &name
	}
	if permissions != nil {
		req.Permissions = &serverv1.PermissionsUpdate{Values: permissions}
	}
	return d.c.databases.UpdateCollection(ctx, req)
}

// DeleteCollection 删除集合。
func (d *DatabasesService) DeleteCollection(ctx context.Context, collectionID string) error {
	_, err := d.c.databases.DeleteCollection(ctx, &serverv1.GetCollectionRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
	})
	return err
}

// ListCollections 按查询 DSL 列出集合。
func (d *DatabasesService) ListCollections(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListCollectionsResponse, error) {
	return d.c.databases.ListCollections(ctx, &serverv1.ListCollectionsRequest{
		DatabaseId: d.db,
		Queries:    queries,
		PageSize:   pageSize,
		PageToken:  pageToken,
	})
}

// CreateAttribute 为集合添加属性（type 支持 string/integer/float/boolean/datetime/email/url/json 等）。
func (d *DatabasesService) CreateAttribute(ctx context.Context, collectionID, key, attrType string, size int32, required, array bool) (*serverv1.Attribute, error) {
	return d.c.databases.CreateAttribute(ctx, &serverv1.CreateAttributeRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Key:          key,
		Type:         attrType,
		Size:         size,
		Required:     required,
		Array:        array,
	})
}

// DeleteAttribute 删除集合属性。
func (d *DatabasesService) DeleteAttribute(ctx context.Context, collectionID, key string) error {
	_, err := d.c.databases.DeleteAttribute(ctx, &serverv1.DeleteAttributeRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Key:          key,
	})
	return err
}

// CreateIndex 为集合创建索引（type 支持 key/unique/fulltext）。
func (d *DatabasesService) CreateIndex(ctx context.Context, collectionID, id, indexType string, attributes []string) (*serverv1.Index, error) {
	return d.c.databases.CreateIndex(ctx, &serverv1.CreateIndexRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Id:           id,
		Type:         indexType,
		Attributes:   attributes,
	})
}

// DeleteIndex 删除集合索引。
func (d *DatabasesService) DeleteIndex(ctx context.Context, collectionID, indexID string) error {
	_, err := d.c.databases.DeleteIndex(ctx, &serverv1.DeleteIndexRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		IndexId:      indexID,
	})
	return err
}

// CreateDocument 在集合中创建文档。
func (d *DatabasesService) CreateDocument(ctx context.Context, collectionID, documentID string, data map[string]any, permissions []string) (*serverv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.CreateDocument(ctx, &serverv1.CreateDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         st,
		Permissions:  permissions,
	})
}

// GetDocument 读取文档，不存在时返回 codes.NotFound。
func (d *DatabasesService) GetDocument(ctx context.Context, collectionID, documentID string) (*serverv1.Document, error) {
	return d.c.databases.GetDocument(ctx, &serverv1.GetDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
	})
}

// UpdateDocument 更新文档字段；increment 对数字字段做原子增量。
func (d *DatabasesService) UpdateDocument(ctx context.Context, collectionID, documentID string, data map[string]any, increment map[string]int64, permissions []string) (*serverv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpdateDocument(ctx, &serverv1.UpdateDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         st,
		Permissions:  permissions,
		Increment:    increment,
	})
}

// UpsertDocument 按 conflictColumns（须匹配集合唯一索引）插入或更新文档。
func (d *DatabasesService) UpsertDocument(ctx context.Context, collectionID, documentID string, data map[string]any, conflictColumns, permissions []string) (*serverv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpsertDocument(ctx, &serverv1.UpsertDocumentRequest{
		DatabaseId:      d.db,
		CollectionId:    collectionID,
		DocumentId:      documentID,
		Data:            st,
		Permissions:     permissions,
		ConflictColumns: conflictColumns,
	})
}

// DeleteDocument 删除文档。
func (d *DatabasesService) DeleteDocument(ctx context.Context, collectionID, documentID string) error {
	_, err := d.c.databases.DeleteDocument(ctx, &serverv1.GetDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
	})
	return err
}

// ListDocuments 按查询 DSL 列出文档，返回文档列表与下一页游标（空表示无更多）。
func (d *DatabasesService) ListDocuments(ctx context.Context, collectionID string, queries []string, pageSize int32, pageToken string) ([]*serverv1.Document, string, error) {
	resp, err := d.c.databases.ListDocuments(ctx, &serverv1.ListDocumentsRequest{
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
	resp, err := d.c.databases.CountDocuments(ctx, &serverv1.ListDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Queries:      queries,
	})
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// BulkUpdateDocuments 批量更新文档，返回受影响数量。
func (d *DatabasesService) BulkUpdateDocuments(ctx context.Context, collectionID string, documentIDs []string, data map[string]any, permissions []string) (*serverv1.BulkDocumentsResponse, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.BulkUpdateDocuments(ctx, &serverv1.BulkUpdateDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentIds:  documentIDs,
		Data:         st,
		Permissions:  permissions,
	})
}

// BulkDeleteDocuments 批量删除文档，返回受影响数量。
func (d *DatabasesService) BulkDeleteDocuments(ctx context.Context, collectionID string, documentIDs []string) (*serverv1.BulkDocumentsResponse, error) {
	return d.c.databases.BulkDeleteDocuments(ctx, &serverv1.BulkDeleteDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentIds:  documentIDs,
	})
}

// UseDatabase 返回绑定指定 databaseID 的文档服务副本。
func (c *Client) UseDatabase(databaseID string) *DatabasesService {
	return newDatabasesService(c, databaseID)
}
