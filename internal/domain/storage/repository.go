package storage

import (
	"context"
	"errors"
)

var (
	ErrBucketIDRequired = errors.New("bucket id is required")
	ErrFileIDRequired   = errors.New("file id is required")
	ErrInvalidUpdate    = errors.New("invalid storage update")
)

// BucketRepository 把 bucket 元数据持久化到项目 schema。
type BucketRepository interface {
	Insert(ctx context.Context, projectID string, bucket *Bucket) error
	GetByID(ctx context.Context, projectID, id string) (*Bucket, error)
	List(ctx context.Context, projectID string) ([]*Bucket, error)
	Count(ctx context.Context, projectID string) (int64, error)
	// Update 只 SET 点名列（name/public/permissions）；permissions JSON 为 PUT last-write-wins。
	Update(ctx context.Context, projectID, id string, cols map[string]any) error
	Delete(ctx context.Context, projectID, id string) error
}

// FileRepository 把 file 元数据持久化到项目 schema。
type FileRepository interface {
	Insert(ctx context.Context, projectID string, file *File) error
	GetByID(ctx context.Context, projectID, id string) (*File, error)
	ListByBucket(ctx context.Context, projectID, bucketID string) ([]*File, error)
	Count(ctx context.Context, projectID string) (int64, error)
	// Update 只 SET 点名列（name/mime_type/metadata）；metadata 为 PUT last-write-wins。
	Update(ctx context.Context, projectID, id string, cols map[string]any) error
	Delete(ctx context.Context, projectID, id string) error
	// SumSize 供 billing 替代 SumDocumentField；无 project_id 列，靠 schema 限定。
	SumSize(ctx context.Context, projectID string) (int64, error)
}
