package servergrpc

import (
	"bytes"
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appstorage "github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainstorage "github.com/torchwooddev/torchwood/internal/domain/storage"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StorageService struct {
	serverv1.UnimplementedStorageServiceServer
	storage *appstorage.Storage
}

func NewStorageService(storage *appstorage.Storage) *StorageService {
	return &StorageService{storage: storage}
}

func (s *StorageService) projectID(ctx context.Context) string {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return ""
	}
	return p.ProjectID
}

func (s *StorageService) CreateBucket(ctx context.Context, req *serverv1.CreateBucketRequest) (*serverv1.Bucket, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	bucket, err := s.storage.CreateBucket(ctx, appstorage.CreateBucketCommand{
		ProjectID:   projectID,
		Name:        req.GetName(),
		Permissions: req.GetPermissions(),
	})
	if err != nil {
		return nil, err
	}
	return mapBucket(bucket), nil
}

func (s *StorageService) ListBuckets(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListBucketsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	buckets, total, err := s.storage.ListBuckets(ctx, projectID, databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.Bucket, len(buckets))
	for i := range buckets {
		out[i] = mapBucket(&buckets[i])
	}
	return &serverv1.ListBucketsResponse{
		Buckets: out,
		Meta:    &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total)},
	}, nil
}

func (s *StorageService) GetBucket(ctx context.Context, req *serverv1.GetBucketRequest) (*serverv1.Bucket, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	buckets, _, err := s.storage.ListBuckets(ctx, projectID, databases.Query{
		Queries:  []string{"equal(\"$id\",\"" + req.GetId() + "\")"},
		PageSize: 1,
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	if len(buckets) == 0 {
		return nil, status.Error(codes.NotFound, "bucket not found")
	}
	return mapBucket(&buckets[0]), nil
}

func (s *StorageService) DeleteBucket(ctx context.Context, req *serverv1.GetBucketRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if err := s.storage.DeleteBucket(ctx, projectID, req.GetId(), dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *StorageService) CreateFile(ctx context.Context, req *serverv1.CreateFileRequest) (*serverv1.File, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	data := req.GetData()
	p, ok := contexts.Principal(ctx)
	ownerUserID := ""
	if ok {
		ownerUserID = p.UserID
	}
	file, err := s.storage.CreateFile(ctx, appstorage.CreateFileCommand{
		ProjectID:   projectID,
		OwnerUserID: ownerUserID,
		BucketID:    req.GetBucketId(),
		Name:        req.GetName(),
		MimeType:    req.GetMimeType(),
		Metadata:    req.GetMetadata(),
		Permissions: req.GetPermissions(),
	}, bytes.NewReader(data), int64(len(data)), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapFile(file), nil
}

func (s *StorageService) ListFiles(ctx context.Context, req *serverv1.ListFilesRequest) (*serverv1.ListFilesResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	q := databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	}
	q.Queries = append([]string{query.BuildEqual("bucket_id", req.GetBucketId())}, q.Queries...)
	files, total, _, err := s.storage.ListFiles(ctx, projectID, req.GetBucketId(), q, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.File, len(files))
	for i := range files {
		out[i] = mapFile(&files[i])
	}
	return &serverv1.ListFilesResponse{
		Files: out,
		Meta:  &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total)},
	}, nil
}

func (s *StorageService) GetFile(ctx context.Context, req *serverv1.GetFileRequest) (*serverv1.File, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	file, _, err := s.storage.GetFile(ctx, projectID, req.GetBucketId(), req.GetFileId(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapFile(file), nil
}

func (s *StorageService) DeleteFile(ctx context.Context, req *serverv1.GetFileRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if err := s.storage.DeleteFile(ctx, projectID, req.GetBucketId(), req.GetFileId(), dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapBucket(b *domainstorage.Bucket) *serverv1.Bucket {
	if b == nil {
		return nil
	}
	return &serverv1.Bucket{
		Id:          b.ID,
		Name:        b.Name,
		Permissions: b.Permissions,
		CreatedAt:   timestamppb.New(b.CreatedAt),
		UpdatedAt:   timestamppb.New(b.UpdatedAt),
	}
}

func mapFile(f *domainstorage.File) *serverv1.File {
	if f == nil {
		return nil
	}
	return &serverv1.File{
		Id:        f.ID,
		BucketId:  f.BucketID,
		Name:      f.Name,
		MimeType:  f.MimeType,
		Size:      f.Size,
		Metadata:  f.Metadata,
		CreatedAt: timestamppb.New(f.CreatedAt),
		UpdatedAt: timestamppb.New(f.UpdatedAt),
	}
}

func dbPrincipal(ctx context.Context) databases.Principal {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return databases.Principal{}
	}
	return databases.Principal{Roles: p.Roles, PlatformAdmin: p.IsPlatformAdmin}
}
