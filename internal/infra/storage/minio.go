package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/storage"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// minioObjectStore is an S3-compatible ObjectStore implementation.
type minioObjectStore struct {
	client *minio.Client
	bucket string
}

// NewMinioObjectStore creates a new MinIO-backed object store.
func NewMinioObjectStore(cfg *config.AppConfig) (storage.ObjectStore, error) {
	s := cfg.GetStorage().GetS3()
	endpoint := s.GetEndpoint()
	useSSL := s.GetUseSsl()

	// If endpoint contains a scheme, extract it for SSL detection.
	if u, err := url.Parse(endpoint); err == nil && u.Scheme != "" {
		endpoint = u.Host
		if u.Scheme == "https" {
			useSSL = true
		}
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(s.GetAccessKeyId(), s.GetSecretAccessKey(), ""),
		Secure: useSSL,
		Region: s.GetRegion(),
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	bucket := s.GetBucket()
	if bucket == "" {
		bucket = "Torchwood-files"
	}

	return &minioObjectStore{client: client, bucket: bucket}, nil
}

func (m *minioObjectStore) EnsureBucket(ctx context.Context, name string) error {
	exists, err := m.client.BucketExists(ctx, name)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := m.client.MakeBucket(ctx, name, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		return fmt.Errorf("make bucket: %w", err)
	}
	return nil
}

func (m *minioObjectStore) Put(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := m.client.PutObject(ctx, bucket, key, data, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (m *minioObjectStore) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// Check existence by reading stat.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("object not found: %s/%s", bucket, key)
		}
		return nil, err
	}
	return obj, nil
}

func (m *minioObjectStore) Delete(ctx context.Context, bucket, key string) error {
	return m.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}

func (m *minioObjectStore) bucketName() string {
	if m.bucket != "" {
		return m.bucket
	}
	return "Torchwood-files"
}

// DefaultBucket returns the configured default bucket name.
func (m *minioObjectStore) DefaultBucket() string { return m.bucketName() }
