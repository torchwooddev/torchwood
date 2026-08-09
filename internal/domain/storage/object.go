package storage

import (
	"context"
	"io"
	"time"
)

// Bucket represents a storage bucket (metadata lives in the dynamic document DB).
type Bucket struct {
	ID          string
	ProjectID   string
	Name        string
	Permissions []string
	Public      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// File represents a stored file (metadata lives in the dynamic document DB).
type File struct {
	ID        string
	ProjectID string
	BucketID  string
	Name      string
	MimeType  string
	Size      int64
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Usage aggregates storage statistics for a project.
type Usage struct {
	Buckets   int64
	Files     int64
	TotalSize int64
}

// ObjectStore abstracts binary object storage (S3 / MinIO).
type ObjectStore interface {
	// EnsureBucket creates the underlying S3 bucket if it does not exist.
	EnsureBucket(ctx context.Context, name string) error
	// Put uploads an object with the given key.
	Put(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string) error
	// Get downloads an object.
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	// Delete removes an object.
	Delete(ctx context.Context, bucket, key string) error
	// Ping probes connectivity to the underlying store.
	Ping(ctx context.Context) error
}
