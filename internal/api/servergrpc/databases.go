package servergrpc

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
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

func (s *DatabasesService) ListDatabases(ctx context.Context, _ *sharedv1.ListRequest) (*serverv1.ListDatabasesResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, "databases")
	cols, err := s.databases.ListDatabases(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.Database, len(cols))
	for i := range cols {
		out[i] = mapDatabase(&cols[i])
	}
	return &serverv1.ListDatabasesResponse{Databases: out, Meta: &sharedv1.ListResponseMeta{}}, nil
}

func (s *DatabasesService) GetDatabase(ctx context.Context, req *serverv1.GetDatabaseRequest) (*serverv1.Database, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDatabaseResource(req.GetId()))
	col, err := s.databases.GetDatabase(ctx, projectID, req.GetId())
	if err != nil {
		return nil, err
	}
	if col == nil {
		return nil, status.Error(codes.NotFound, "database not found")
	}
	return mapDatabase(col), nil
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
	col, err := s.databases.GetCollection(ctx, projectID, req.GetDatabaseId(), req.GetId())
	if err != nil {
		return nil, err
	}
	if col == nil {
		return nil, status.Error(codes.Internal, "collection not found after create")
	}
	return mapCollection(col), nil
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
	patch := databases.CollectionPatch{Name: req.GetName()}
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

func (s *DatabasesService) CreateDocument(ctx context.Context, req *serverv1.CreateDocumentRequest) (*serverv1.Document, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	if req.GetDocumentId() != "" {
		ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
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
	docs, total, next, err := s.databases.ListDocuments(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.Document, len(docs))
	for i := range docs {
		mapped, err := mapDocument(&docs[i])
		if err != nil {
			return nil, err
		}
		out[i] = mapped
	}
	return &serverv1.ListDocumentsResponse{
		Documents: out,
		Meta:      &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total), NextPageToken: next},
	}, nil
}

func (s *DatabasesService) GetDocument(ctx context.Context, req *serverv1.GetDocumentRequest) (*serverv1.Document, error) {
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

func (s *DatabasesService) UpdateDocument(ctx context.Context, req *serverv1.UpdateDocumentRequest) (*serverv1.Document, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
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
	)
	if err != nil {
		return nil, err
	}
	return mapDocument(doc)
}

func (s *DatabasesService) UpsertDocument(ctx context.Context, req *serverv1.UpsertDocumentRequest) (*serverv1.Document, error) {
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

func (s *DatabasesService) DeleteDocument(ctx context.Context, req *serverv1.GetDocumentRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditDocumentResource(req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId()))
	if err := s.databases.DeleteDocument(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId(), dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) CountDocuments(ctx context.Context, req *serverv1.ListDocumentsRequest) (*serverv1.CountDocumentsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	ctx = contexts.WithAuditResource(ctx, auditCollectionResource(req.GetDatabaseId(), req.GetCollectionId()))
	count, err := s.databases.CountDocuments(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetQueries(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return &serverv1.CountDocumentsResponse{Count: count}, nil
}

func mapDatabase(c *databases.Collection) *serverv1.Database {
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

func mapDocument(doc *databases.Document) (*serverv1.Document, error) {
	if doc == nil {
		return nil, nil
	}
	data, err := structpb.NewStruct(doc.Data)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "document data is not serializable")
	}
	return &serverv1.Document{
		Id:          doc.ID,
		Data:        data,
		CreatedAt:   timestamppb.New(doc.CreatedAt),
		UpdatedAt:   timestamppb.New(doc.UpdatedAt),
		Permissions: formatPermissionStrings(doc.Permissions),
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
