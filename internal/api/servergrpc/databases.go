package servergrpc

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/app/documents"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type DatabasesService struct {
	serverv1.UnimplementedDatabasesServiceServer
	databases *appserver.Databases
}

func NewDatabasesService(databases *appserver.Databases) *DatabasesService {
	return &DatabasesService{databases: databases}
}

func (s *DatabasesService) projectID(ctx context.Context) string {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return ""
	}
	return p.ProjectID
}

// auditResource* 构建审计资源路径（A8）：ResourceID 形如
// databases/{db}、databases/{db}/collections/{coll}、…/documents/{doc}。
func auditDatabaseResource(databaseID string) string {
	return "databases/" + databaseID
}

func auditCollectionResource(databaseID, collectionID string) string {
	return "databases/" + databaseID + "/collections/" + collectionID
}

func auditDocumentResource(databaseID, collectionID, documentID string) string {
	return "databases/" + databaseID + "/collections/" + collectionID + "/documents/" + documentID
}

func (s *DatabasesService) CreateDatabase(ctx context.Context, req *serverv1.CreateDatabaseRequest) (*serverv1.Database, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDatabaseResource(req.GetId()))
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.databases.CreateDatabase(ctx, projectID, req.GetId(), req.GetName()); err != nil {
		return nil, err
	}
	return &serverv1.Database{Id: req.GetId(), Name: req.GetName()}, nil
}

func (s *DatabasesService) ListDatabases(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListDatabasesResponse, error) {
	if err := rejectListFilterOrderBy(req); err != nil {
		return nil, err
	}
	params, err := crud.ParseListParams(req.GetPageSize(), req.GetPageToken(), "", "")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, "databases")
	dbs, err := s.databases.ListDatabases(ctx, projectID)
	if err != nil {
		return nil, err
	}
	start := params.Offset
	if start > len(dbs) {
		start = len(dbs)
	}
	end := start + int(params.PageSize)
	if end > len(dbs) {
		end = len(dbs)
	}
	page := dbs[start:end]
	hasMore := end < len(dbs)
	info := crud.BuildPaginationInfo(params, len(dbs), hasMore)
	var nextToken, prevToken string
	if info.HasNext {
		nextToken = crud.EncodePageToken(info.NextOffset)
	}
	if info.HasPrevious {
		prevToken = crud.EncodePageToken(info.PreviousOffset)
	}
	out := make([]*serverv1.Database, len(page))
	for i := range page {
		out[i] = mapDatabase(&page[i])
	}
	return &serverv1.ListDatabasesResponse{
		Databases: out,
		Meta: &sharedv1.ListResponseMeta{
			PageSize:      info.PageSize,
			TotalCount:    int32(info.TotalCount),
			NextPageToken: nextToken,
			PrevPageToken: prevToken,
		},
	}, nil
}

func (s *DatabasesService) GetDatabase(ctx context.Context, req *serverv1.GetDatabaseRequest) (*serverv1.Database, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDatabaseResource(req.GetId()))
	db, err := s.databases.GetDatabase(ctx, projectID, req.GetId())
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, status.Error(codes.NotFound, "database not found")
	}
	return mapDatabase(db), nil
}

func (s *DatabasesService) DeleteDatabase(ctx context.Context, req *serverv1.GetDatabaseRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDatabaseResource(req.GetId()))
	if err := s.databases.DeleteDatabase(ctx, projectID, req.GetId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) CreateCollection(ctx context.Context, req *serverv1.CreateCollectionRequest) (*serverv1.Collection, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetId()))
	perms, err := databases.ParsePermissionStrings(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	documentSecurity := true
	if req.DocumentSecurity != nil {
		documentSecurity = req.GetDocumentSecurity()
	}
	if err := s.databases.CreateCollection(ctx, projectID, req.GetDatabaseId(), req.GetId(), req.GetName(), nil, nil, perms, documentSecurity); err != nil {
		return nil, err
	}
	// R02-P3-1：移除尾随 GetCollection 重查（adapter 已完成元数据写入），
	// 直接基于请求构造响应（与 CreateDatabase handler 模式一致）。
	return &serverv1.Collection{
		Id:               req.GetId(),
		DatabaseId:       req.GetDatabaseId(),
		Name:             req.GetName(),
		DocumentSecurity: documentSecurity,
		Permissions:      formatPermissionStrings(perms),
	}, nil
}

func (s *DatabasesService) ListCollections(ctx context.Context, req *serverv1.ListCollectionsRequest) (*serverv1.ListCollectionsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections")
	cols, total, next, err := s.databases.ListCollections(ctx, projectID, req.GetDatabaseId(), databases.ListQuery{
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.Collection, len(cols))
	for i := range cols {
		out[i] = mapCollection(&cols[i])
	}
	return &serverv1.ListCollectionsResponse{
		Collections: out,
		Meta:        &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total), NextPageToken: next},
	}, nil
}

func (s *DatabasesService) GetCollection(ctx context.Context, req *serverv1.GetCollectionRequest) (*serverv1.Collection, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	col, err := s.databases.GetCollection(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId())
	if err != nil {
		return nil, err
	}
	if col == nil {
		return nil, status.Error(codes.NotFound, "collection not found")
	}
	return mapCollection(col), nil
}

func (s *DatabasesService) DeleteCollection(ctx context.Context, req *serverv1.GetCollectionRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	if err := s.databases.DeleteCollection(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) UpdateCollection(ctx context.Context, req *serverv1.UpdateCollectionRequest) (*serverv1.Collection, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	patch := databases.CollectionPatch{}
	// name 为 optional（R10-P1-6）：未设置 = 不修改；设置非空串 = 更新
	// （adapter 对空串同样按不修改处理，清空语义暂不支持）。
	if req.Name != nil {
		patch.Name = req.GetName()
	}
	if req.Permissions != nil {
		perms, err := databases.ParsePermissionStrings(req.GetPermissions().GetValues())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		patch.Permissions = &perms
	}
	if req.DocumentSecurity != nil {
		v := req.GetDocumentSecurity()
		patch.DocumentSecurity = &v
	}
	if req.Disabled != nil {
		v := req.GetDisabled()
		patch.Disabled = &v
	}
	if err := s.databases.UpdateCollection(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), patch, dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	col, err := s.databases.GetCollection(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId())
	if err != nil {
		return nil, err
	}
	if col == nil {
		return nil, status.Error(codes.NotFound, "collection not found")
	}
	return mapCollection(col), nil
}

func (s *DatabasesService) CreateAttribute(ctx context.Context, req *serverv1.CreateAttributeRequest) (*serverv1.Attribute, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	attr := databases.Attribute{
		ID:       req.GetKey(),
		Key:      req.GetKey(),
		Type:     req.GetType(),
		Size:     int(req.GetSize()),
		Required: req.GetRequired(),
		Array:    req.GetArray(),
	}
	if req.GetDefaultValue() != "" {
		attr.Default = req.GetDefaultValue()
	}
	if err := s.databases.CreateAttribute(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), attr); err != nil {
		return nil, err
	}
	return &serverv1.Attribute{
		Id:           attr.ID,
		Key:          attr.Key,
		Type:         attr.Type,
		Size:         int32(attr.Size),
		Required:     attr.Required,
		Array:        attr.Array,
		DefaultValue: req.GetDefaultValue(),
	}, nil
}

func (s *DatabasesService) CreateIndex(ctx context.Context, req *serverv1.CreateIndexRequest) (*serverv1.Index, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	idx := databases.Index{
		ID:         req.GetId(),
		Type:       req.GetType(),
		Attributes: req.GetAttributes(),
		Orders:     req.GetOrders(),
	}
	if err := s.databases.CreateIndex(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), idx); err != nil {
		return nil, err
	}
	return &serverv1.Index{
		Id:         idx.ID,
		Type:       idx.Type,
		Attributes: idx.Attributes,
		Orders:     idx.Orders,
	}, nil
}

func (s *DatabasesService) DeleteAttribute(ctx context.Context, req *serverv1.DeleteAttributeRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	if err := s.databases.DeleteAttribute(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetKey()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) DeleteIndex(ctx context.Context, req *serverv1.DeleteIndexRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	if err := s.databases.DeleteIndex(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetIndexId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) CreateDocument(ctx context.Context, req *serverv1.CreateDocumentRequest) (*sharedv1.Document, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if req.GetDocumentId() != "" {
		ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	} else {
		ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	}
	data := map[string]any{}
	if req.GetData() != nil {
		data = req.GetData().AsMap()
	}
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	doc, err := s.databases.CreateDocument(
		ctx,
		projectID,
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentId(),
		data,
		perms,
		dbPrincipal(ctx),
	)
	if err != nil {
		return nil, err
	}
	return mapDocument(doc)
}

func (s *DatabasesService) ListDocuments(ctx context.Context, req *serverv1.ListDocumentsRequest) (*serverv1.ListDocumentsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	q, err := documents.BindListQuery(req.GetQueries(), req.GetPageSize(), req.GetPageToken(), req.GetQuery())
	if err != nil {
		return nil, err
	}
	ast, err := documents.ResolveQuery(q)
	if err != nil {
		return nil, err
	}
	docs, total, next, err := s.databases.ListDocuments(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), q, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*sharedv1.Document, len(docs))
	for i := range docs {
		mapped, err := mapDocument(&docs[i])
		if err != nil {
			return nil, err
		}
		out[i] = mapped
	}
	return &serverv1.ListDocumentsResponse{
		Documents: out,
		Meta:      &sharedv1.ListResponseMeta{PageSize: ast.PageSize, TotalCount: int32(total), NextPageToken: next},
	}, nil
}

func (s *DatabasesService) GetDocument(ctx context.Context, req *serverv1.GetDocumentRequest) (*sharedv1.Document, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	doc, err := s.databases.GetDocument(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapDocument(doc)
}

func (s *DatabasesService) UpdateDocument(ctx context.Context, req *serverv1.UpdateDocumentRequest) (*sharedv1.Document, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	var version *int64
	if req.Version != nil {
		v := req.GetVersion()
		version = &v
	}
	doc, err := s.databases.UpdateDocument(
		ctx,
		projectID,
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentId(),
		updateData(req.GetData()),
		perms,
		req.GetIncrement(),
		dbPrincipal(ctx),
		version,
	)
	if err != nil {
		return nil, err
	}
	return mapDocument(doc)
}

func (s *DatabasesService) UpsertDocument(ctx context.Context, req *serverv1.UpsertDocumentRequest) (*sharedv1.Document, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	data := map[string]any{}
	if req.GetData() != nil {
		data = req.GetData().AsMap()
	}
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	doc, err := s.databases.UpsertDocument(
		ctx,
		projectID,
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentId(),
		data,
		req.GetConflictColumns(),
		perms,
		dbPrincipal(ctx),
	)
	if err != nil {
		return nil, err
	}
	return mapDocument(doc)
}

func (s *DatabasesService) BulkUpdateDocuments(ctx context.Context, req *serverv1.BulkUpdateDocumentsRequest) (*serverv1.BulkDocumentsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	n, err := s.databases.BulkUpdateDocuments(
		ctx,
		projectID,
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentIds(),
		updateData(req.GetData()),
		perms,
		dbPrincipal(ctx),
	)
	if err != nil {
		return nil, err
	}
	return &serverv1.BulkDocumentsResponse{Affected: n}, nil
}

func (s *DatabasesService) BulkDeleteDocuments(ctx context.Context, req *serverv1.BulkDeleteDocumentsRequest) (*serverv1.BulkDocumentsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	n, err := s.databases.BulkDeleteDocuments(
		ctx,
		projectID,
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentIds(),
		dbPrincipal(ctx),
	)
	if err != nil {
		return nil, err
	}
	return &serverv1.BulkDocumentsResponse{Affected: n}, nil
}

func (s *DatabasesService) DeleteDocument(ctx context.Context, req *serverv1.DeleteDocumentRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	var version *int64
	if req.Version != nil {
		v := req.GetVersion()
		version = &v
	}
	if err := s.databases.DeleteDocument(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId(), dbPrincipal(ctx), version); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) CountDocuments(ctx context.Context, req *serverv1.CountDocumentsRequest) (*serverv1.CountDocumentsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	q, err := documents.BindListQuery(req.GetQueries(), 0, "", req.GetQuery())
	if err != nil {
		return nil, err
	}
	count, err := s.databases.CountDocuments(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), q, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return &serverv1.CountDocumentsResponse{Count: count}, nil
}

func mapDatabase(c *databases.Database) *serverv1.Database {
	if c == nil {
		return nil
	}
	return &serverv1.Database{
		Id:        c.ID,
		Name:      c.Name,
		CreatedAt: timestamppb.New(c.CreatedAt),
		UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
}

func mapCollection(c *databases.Collection) *serverv1.Collection {
	if c == nil {
		return nil
	}
	out := &serverv1.Collection{
		Id:               c.ID,
		DatabaseId:       c.DatabaseID,
		Name:             c.Name,
		DocumentSecurity: c.DocumentSecurity,
		Disabled:         c.Disabled,
		IsSystem:         c.IsSystem,
		CreatedAt:        timestamppb.New(c.CreatedAt),
		UpdatedAt:        timestamppb.New(c.UpdatedAt),
	}
	for _, p := range c.Permissions {
		out.Permissions = append(out.Permissions, p.Type+":"+p.Role)
	}
	for _, a := range c.Attributes {
		out.Attributes = append(out.Attributes, &serverv1.Attribute{
			Id:       a.ID,
			Key:      a.Key,
			Type:     a.Type,
			Size:     int32(a.Size),
			Required: a.Required,
			Array:    a.Array,
		})
	}
	for _, i := range c.Indexes {
		out.Indexes = append(out.Indexes, &serverv1.Index{
			Id:         i.ID,
			Type:       i.Type,
			Attributes: i.Attributes,
			Orders:     i.Orders,
		})
	}
	return out
}

func mapDocument(doc *databases.Document) (*sharedv1.Document, error) {
	if doc == nil {
		return nil, nil
	}
	data, err := structpb.NewStruct(doc.Data)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "document data is not serializable")
	}
	return &sharedv1.Document{
		Id:          doc.ID,
		Data:        data,
		CreatedAt:   timestamppb.New(doc.CreatedAt),
		UpdatedAt:   timestamppb.New(doc.UpdatedAt),
		Permissions: formatPermissionStrings(doc.Permissions),
		Version:     doc.Version,
	}, nil
}

func formatPermissionStrings(perms []databases.Permission) []string {
	if len(perms) == 0 {
		return nil
	}
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = databases.FormatPermissionString(p)
	}
	return out
}

func updateData(s *structpb.Struct) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return s.AsMap()
}

// parseOptionalPermissions parses explicit permission strings into Permission
// values. An empty list yields nil (no document-level permissions), unlike
// ParsePermissionStrings which expands to DefaultCollectionPermissions.
func parseOptionalPermissions(items []string) ([]databases.Permission, error) {
	if len(items) == 0 {
		return nil, nil
	}
	return databases.ParsePermissionStrings(items)
}
