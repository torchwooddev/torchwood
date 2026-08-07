package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/storage"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Storage struct {
	cfg         *config.AppConfig
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	store       storage.ObjectStore
}

func NewStorage(
	cfg *config.AppConfig,
	projectRepo projects.Repository,
	docDB databases.DocumentDB,
	store storage.ObjectStore,
) *Storage {
	return &Storage{cfg: cfg, projectRepo: projectRepo, docDB: docDB, store: store}
}

type CreateBucketCommand struct {
	ProjectID   string
	Name        string
	Permissions []string
}

type CreateFileCommand struct {
	ProjectID   string
	OwnerUserID string
	BucketID    string
	Name        string
	MimeType    string
	Metadata    map[string]string
	Permissions []string
}

func (s *Storage) CreateBucket(ctx context.Context, cmd CreateBucketCommand) (*storage.Bucket, error) {
	if cmd.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	project, err := s.resolveProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, err
	}

	bucketID := idgen.UUID().String()
	now := time.Now()
	bucketDoc := databases.Document{
		ID: bucketID,
		Data: map[string]any{
			"name":        cmd.Name,
			"permissions": cmd.Permissions,
		},
	}
	perms := bucketPermissions(bucketID, cmd.Permissions)
	if _, err := s.docDB.CreateDocument(ctx, project.ID, "default", "buckets", bucketDoc, perms, databases.SystemPrincipal); err != nil {
		return nil, fmt.Errorf("create bucket document: %w", err)
	}

	return &storage.Bucket{
		ID:          bucketID,
		ProjectID:   project.ID,
		Name:        cmd.Name,
		Permissions: cmd.Permissions,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *Storage) ListBuckets(ctx context.Context, projectID string, q databases.Query, principal databases.Principal) ([]storage.Bucket, int64, error) {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	if err := s.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, 0, err
	}

	list, err := s.docDB.ListDocuments(ctx, project.ID, "default", "buckets", q, principal)
	if err != nil {
		return nil, 0, err
	}
	buckets := make([]storage.Bucket, 0, len(list.Documents))
	for _, d := range list.Documents {
		buckets = append(buckets, *mapBucketDoc(&d))
	}
	return buckets, list.TotalCount, nil
}

func (s *Storage) DeleteBucket(ctx context.Context, projectID, bucketID string, principal databases.Principal) error {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return err
	}
	// Delete all file objects in this bucket by paginating through every file.
	var pageToken string
	for {
		files, total, next, err := s.ListFiles(ctx, projectID, bucketID, databases.Query{PageSize: 1000, PageToken: pageToken}, principal)
		if err != nil {
			return err
		}
		for _, f := range files {
			_ = s.store.Delete(ctx, defaultBucketName(s.cfg), objectKey(project.ID, bucketID, f.ID))
		}
		if next == "" || len(files) == 0 {
			break
		}
		pageToken = next
		_ = total
	}
	return s.docDB.DeleteDocument(ctx, project.ID, "default", "buckets", bucketID, principal)
}

func (s *Storage) CreateFile(ctx context.Context, cmd CreateFileCommand, content io.Reader, size int64, principal databases.Principal) (*storage.File, error) {
	cmd.MimeType = normalizeMimeType(cmd.MimeType)
	if cmd.BucketID == "" {
		return nil, status.Error(codes.InvalidArgument, "bucket_id is required")
	}
	if cmd.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	project, err := s.resolveProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, err
	}

	// Verify bucket exists.
	bucketDoc, err := s.docDB.GetDocument(ctx, project.ID, "default", "buckets", cmd.BucketID, principal)
	if err != nil {
		return nil, err
	}
	if bucketDoc == nil {
		return nil, status.Error(codes.NotFound, "bucket not found")
	}

	fileID := idgen.UUID().String()
	now := time.Now()
	fileDoc := databases.Document{
		ID: fileID,
		Data: map[string]any{
			"bucket_id": cmd.BucketID,
			"name":      cmd.Name,
			"mime_type": cmd.MimeType,
			"size":      size,
			"metadata":  cmd.Metadata,
		},
	}
	perms := filePermissions(fileID, cmd.OwnerUserID, cmd.Permissions)
	if _, err := s.docDB.CreateDocument(ctx, project.ID, "default", "files", fileDoc, perms, principal); err != nil {
		return nil, fmt.Errorf("create file document: %w", err)
	}

	if err := s.store.EnsureBucket(ctx, defaultBucketName(s.cfg)); err != nil {
		return nil, fmt.Errorf("ensure storage bucket: %w", err)
	}
	if err := s.store.Put(ctx, defaultBucketName(s.cfg), objectKey(project.ID, cmd.BucketID, fileID), content, size, cmd.MimeType); err != nil {
		// Attempt rollback metadata.
		_ = s.docDB.DeleteDocument(ctx, project.ID, "default", "files", fileID, databases.SystemPrincipal)
		return nil, fmt.Errorf("upload file: %w", err)
	}

	return &storage.File{
		ID:        fileID,
		ProjectID: project.ID,
		BucketID:  cmd.BucketID,
		Name:      cmd.Name,
		MimeType:  cmd.MimeType,
		Size:      size,
		Metadata:  cmd.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Storage) GetFile(ctx context.Context, projectID, bucketID, fileID string, principal databases.Principal) (*storage.File, io.ReadCloser, error) {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	doc, err := s.docDB.GetDocument(ctx, project.ID, "default", "files", fileID, principal)
	if err != nil {
		return nil, nil, err
	}
	if doc == nil {
		return nil, nil, status.Error(codes.NotFound, "file not found")
	}
	file := mapFileDoc(doc)
	if file.BucketID != bucketID {
		return nil, nil, status.Error(codes.NotFound, "file not found in bucket")
	}
	reader, err := s.store.Get(ctx, defaultBucketName(s.cfg), objectKey(project.ID, bucketID, fileID))
	if err != nil {
		return file, nil, err
	}
	return file, reader, nil
}

func (s *Storage) DeleteFile(ctx context.Context, projectID, bucketID, fileID string, principal databases.Principal) error {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, defaultBucketName(s.cfg), objectKey(project.ID, bucketID, fileID)); err != nil {
		// Continue to delete metadata even if object missing.
	}
	return s.docDB.DeleteDocument(ctx, project.ID, "default", "files", fileID, principal)
}

func (s *Storage) ListFiles(ctx context.Context, projectID, bucketID string, q databases.Query, principal databases.Principal) ([]storage.File, int64, string, error) {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return nil, 0, "", err
	}
	if err := s.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, 0, "", err
	}

	list, err := s.docDB.ListDocuments(ctx, project.ID, "default", "files", q, principal)
	if err != nil {
		return nil, 0, "", err
	}
	files := make([]storage.File, 0, len(list.Documents))
	for _, d := range list.Documents {
		files = append(files, *mapFileDoc(&d))
	}
	return files, list.TotalCount, list.NextPageToken, nil
}

func (s *Storage) resolveProject(ctx context.Context, projectID string) (*projects.Project, error) {
	p, err := s.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	return p, nil
}

// normalizeMimeType 归一化客户端声明的 Content-Type：
// 空值与可执行/危险类型（含带参数的，取分号前 base 判断）一律改判为 application/octet-stream，
// 防止存储型 XSS 经 /view 端点内联执行。
func normalizeMimeType(mime string) string {
	base := strings.Split(mime, ";")[0]
	switch base {
	case "text/html", "application/xhtml+xml", "application/javascript",
		"text/javascript", "application/xml", "text/xml":
		return "application/octet-stream"
	}
	if base == "" {
		return "application/octet-stream"
	}
	return mime
}

func defaultBucketName(cfg *config.AppConfig) string {
	b := cfg.GetStorage().GetS3().GetBucket()
	if b == "" {
		return "Torchwood-files"
	}
	return b
}

func objectKey(projectID, bucketID, fileID string) string {
	return fmt.Sprintf("%s/%s/%s", projectID, bucketID, fileID)
}

func bucketPermissions(bucketID string, explicit []string) []databases.Permission {
	if len(explicit) > 0 {
		return parseRawPermissions(explicit)
	}
	return []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "create", Role: "users"},
		{Type: "update", Role: "users"},
		{Type: "delete", Role: "users"},
	}
}

func filePermissions(fileID, ownerUserID string, explicit []string) []databases.Permission {
	if len(explicit) > 0 {
		return parseRawPermissions(explicit)
	}
	perms := []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: "keys"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: "keys"},
		{Type: "delete", Role: "admin"},
	}
	if ownerUserID != "" {
		perms = append(perms,
			databases.Permission{Type: "update", Role: fmt.Sprintf("user:%s", ownerUserID)},
			databases.Permission{Type: "delete", Role: fmt.Sprintf("user:%s", ownerUserID)},
		)
	}
	return perms
}

func parseRawPermissions(raw []string) []databases.Permission {
	var perms []databases.Permission
	for _, r := range raw {
		parts := strings.SplitN(r, ":", 2)
		if len(parts) == 2 {
			perms = append(perms, databases.Permission{Type: parts[0], Role: parts[1]})
		}
	}
	return perms
}

func mapBucketDoc(doc *databases.Document) *storage.Bucket {
	b := &storage.Bucket{
		ID:        doc.ID,
		ProjectID: "",
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
	if v, ok := doc.Data["name"].(string); ok {
		b.Name = v
	}
	if arr, ok := doc.Data["permissions"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				b.Permissions = append(b.Permissions, s)
			}
		}
	}
	return b
}

func mapFileDoc(doc *databases.Document) *storage.File {
	f := &storage.File{
		ID:        doc.ID,
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
		Metadata:  map[string]string{},
	}
	if v, ok := doc.Data["bucket_id"].(string); ok {
		f.BucketID = v
	}
	if v, ok := doc.Data["name"].(string); ok {
		f.Name = v
	}
	if v, ok := doc.Data["mime_type"].(string); ok {
		f.MimeType = v
	}
	if v, ok := doc.Data["size"].(float64); ok {
		f.Size = int64(v)
	}
	if v, ok := doc.Data["size"].(int64); ok {
		f.Size = v
	}
	if m, ok := doc.Data["metadata"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				f.Metadata[k] = s
			}
		}
	}
	return f
}
