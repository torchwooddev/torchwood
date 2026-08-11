package server

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// StorageService 封装 Server API 的 Storage 服务（元数据操作；
// 文件上传/下载走独立 HTTP handler）。
type StorageService struct {
	c   *Client
	api serverv1.StorageServiceClient
}

// CreateBucket 创建存储桶。
func (s *StorageService) CreateBucket(ctx context.Context, req *serverv1.CreateBucketRequest) (*serverv1.Bucket, error) {
	return s.api.CreateBucket(ctx, req)
}

// ListBuckets 列出存储桶。
func (s *StorageService) ListBuckets(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListBucketsResponse, error) {
	return s.api.ListBuckets(ctx, req)
}

// GetBucket 按 ID 获取存储桶。
func (s *StorageService) GetBucket(ctx context.Context, req *serverv1.GetBucketRequest) (*serverv1.Bucket, error) {
	return s.api.GetBucket(ctx, req)
}

// DeleteBucket 删除存储桶。
func (s *StorageService) DeleteBucket(ctx context.Context, req *serverv1.GetBucketRequest) error {
	_, err := s.api.DeleteBucket(ctx, req)
	return err
}

// UpdateBucket 更新存储桶（仅更新显式传入的字段）。
func (s *StorageService) UpdateBucket(ctx context.Context, req *serverv1.UpdateBucketRequest) (*serverv1.Bucket, error) {
	return s.api.UpdateBucket(ctx, req)
}

// CreateFile 创建文件记录（bytes 上传通道）。
func (s *StorageService) CreateFile(ctx context.Context, req *serverv1.CreateFileRequest) (*serverv1.File, error) {
	return s.api.CreateFile(ctx, req)
}

// ListFiles 列出桶内文件。
func (s *StorageService) ListFiles(ctx context.Context, req *serverv1.ListFilesRequest) (*serverv1.ListFilesResponse, error) {
	return s.api.ListFiles(ctx, req)
}

// GetFile 获取文件元数据。
func (s *StorageService) GetFile(ctx context.Context, req *serverv1.GetFileRequest) (*serverv1.File, error) {
	return s.api.GetFile(ctx, req)
}

// DeleteFile 删除文件。
func (s *StorageService) DeleteFile(ctx context.Context, req *serverv1.GetFileRequest) error {
	_, err := s.api.DeleteFile(ctx, req)
	return err
}

// UpdateFile 更新文件元数据（仅更新显式传入的字段）。
func (s *StorageService) UpdateFile(ctx context.Context, req *serverv1.UpdateFileRequest) (*serverv1.File, error) {
	return s.api.UpdateFile(ctx, req)
}

// CreateFileToken 签发 HTTP 下载签名 token。
func (s *StorageService) CreateFileToken(ctx context.Context, req *serverv1.CreateFileTokenRequest) (*serverv1.FileToken, error) {
	return s.api.CreateFileToken(ctx, req)
}

// GetStorageUsage 获取存储用量。
func (s *StorageService) GetStorageUsage(ctx context.Context, req *serverv1.GetStorageUsageRequest) (*serverv1.StorageUsage, error) {
	return s.api.GetStorageUsage(ctx, req)
}
