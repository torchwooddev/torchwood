package clientgrpc

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/app/client"
	"github.com/torchwooddev/torchwood/internal/app/documents"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type DatabasesService struct {
	clientv1.UnimplementedDatabasesServiceServer
	databases    *client.Databases
	transactions *client.Transactions
}

func NewDatabasesService(databases *client.Databases, transactions *client.Transactions) *DatabasesService {
	return &DatabasesService{databases: databases, transactions: transactions}
}

func (s *DatabasesService) CreateDocument(ctx context.Context, req *clientv1.CreateDocumentRequest) (*sharedv1.Document, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId())
	if req.GetDocumentId() != "" {
		ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId()+"/documents/"+req.GetDocumentId())
	}
	data := map[string]any{}
	if req.GetData() != nil {
		data = req.GetData().AsMap()
	}
	perms, err := parseOptionalPermissions(req.GetPermissions())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	doc, err := s.databases.CreateDocument(ctx, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId(), data, perms)
	if err != nil {
		return nil, err
	}
	return mapClientDocument(doc)
}

func (s *DatabasesService) ListDocuments(ctx context.Context, req *clientv1.ListDocumentsRequest) (*clientv1.ListDocumentsResponse, error) {
	projectID, err := resolveProjectID(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId())
	q, err := documents.BindListQuery(req.GetQueries(), req.GetPageSize(), req.GetPageToken(), req.GetQuery())
	if err != nil {
		return nil, err
	}
	ast, err := documents.ResolveQuery(q)
	if err != nil {
		return nil, err
	}
	docs, total, next, err := s.databases.ListDocuments(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), q)
	if err != nil {
		return nil, err
	}
	out := make([]*sharedv1.Document, len(docs))
	for i := range docs {
		mapped, err := mapClientDocument(&docs[i])
		if err != nil {
			return nil, err
		}
		out[i] = mapped
	}
	return &clientv1.ListDocumentsResponse{
		Documents: out,
		Meta:      &sharedv1.ListResponseMeta{PageSize: ast.PageSize, TotalCount: int32(total), NextPageToken: next},
	}, nil
}

func (s *DatabasesService) GetDocument(ctx context.Context, req *clientv1.GetDocumentRequest) (*sharedv1.Document, error) {
	projectID, err := resolveProjectID(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId()+"/documents/"+req.GetDocumentId())
	doc, err := s.databases.GetDocument(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId())
	if err != nil {
		return nil, err
	}
	return mapClientDocument(doc)
}

func (s *DatabasesService) UpdateDocument(ctx context.Context, req *clientv1.UpdateDocumentRequest) (*sharedv1.Document, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId()+"/documents/"+req.GetDocumentId())
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
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentId(),
		updateData(req.GetData()),
		perms,
		req.GetIncrement(),
		version,
	)
	if err != nil {
		return nil, err
	}
	return mapClientDocument(doc)
}

func (s *DatabasesService) UpsertDocument(ctx context.Context, req *clientv1.UpsertDocumentRequest) (*sharedv1.Document, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId()+"/documents/"+req.GetDocumentId())
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
		req.GetDatabaseId(),
		req.GetCollectionId(),
		req.GetDocumentId(),
		data,
		req.GetConflictColumns(),
		perms,
	)
	if err != nil {
		return nil, err
	}
	return mapClientDocument(doc)
}

func (s *DatabasesService) DeleteDocument(ctx context.Context, req *clientv1.DeleteDocumentRequest) (*sharedv1.Empty, error) {
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId()+"/documents/"+req.GetDocumentId())
	var version *int64
	if req.Version != nil {
		v := req.GetVersion()
		version = &v
	}
	if err := s.databases.DeleteDocument(ctx, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId(), version); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *DatabasesService) CountDocuments(ctx context.Context, req *clientv1.ListDocumentsRequest) (*clientv1.CountDocumentsResponse, error) {
	projectID, err := resolveProjectID(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, "databases/"+req.GetDatabaseId()+"/collections/"+req.GetCollectionId())
	q, err := documents.BindListQuery(req.GetQueries(), req.GetPageSize(), req.GetPageToken(), req.GetQuery())
	if err != nil {
		return nil, err
	}
	count, err := s.databases.CountDocuments(ctx, projectID, req.GetDatabaseId(), req.GetCollectionId(), q)
	if err != nil {
		return nil, err
	}
	return &clientv1.CountDocumentsResponse{Count: count}, nil
}

func resolveProjectID(ctx context.Context, reqProjectID string) (string, error) {
	if reqProjectID != "" {
		return reqProjectID, nil
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("X-Torchwood-Project"); len(values) > 0 && values[0] != "" {
			return values[0], nil
		}
	}
	if p, ok := contexts.Principal(ctx); ok && p.ProjectID != "" {
		return p.ProjectID, nil
	}
	return "", status.Error(codes.InvalidArgument, "project_id is required")
}

func mapClientDocument(doc *databases.Document) (*sharedv1.Document, error) {
	if doc == nil {
		return nil, nil
	}
	data, err := structpb.NewStruct(doc.Data)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "document data is not serializable")
	}
	out := &sharedv1.Document{
		Id:        doc.ID,
		Data:      data,
		CreatedAt: timestamppb.New(doc.CreatedAt),
		UpdatedAt: timestamppb.New(doc.UpdatedAt),
		Version:   doc.Version,
	}
	for _, p := range doc.Permissions {
		out.Permissions = append(out.Permissions, databases.FormatPermissionString(p))
	}
	return out, nil
}

func updateData(s *structpb.Struct) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return s.AsMap()
}

func parseOptionalPermissions(items []string) ([]databases.Permission, error) {
	if len(items) == 0 {
		return nil, nil
	}
	return databases.ParsePermissionStrings(items)
}
