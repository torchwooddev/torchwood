package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
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
	Public      bool
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
			"public":      cmd.Public,
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
		Public:      cmd.Public,
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

// UpdateBucketCommand 更新 bucket 元数据；空字段表示不修改。
type UpdateBucketCommand struct {
	ProjectID string
	ID        string
	Name      string
	Public    *bool
	Principal databases.Principal
}

func (s *Storage) UpdateBucket(ctx context.Context, cmd UpdateBucketCommand) (*storage.Bucket, error) {
	project, err := s.resolveProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	if cmd.Name != "" {
		data["name"] = cmd.Name
	}
	if cmd.Public != nil {
		data["public"] = *cmd.Public
	}
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "nothing to update")
	}
	updated, err := s.docDB.UpdateDocument(ctx, project.ID, "default", "buckets", databases.DocumentUpdate{
		Document: databases.Document{ID: cmd.ID, Data: data},
	}, cmd.Principal)
	if err != nil {
		return nil, err
	}
	return mapBucketDoc(&updated), nil
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

// UpdateFileCommand 携带可更新的文件元数据字段；空值表示不修改。
type UpdateFileCommand struct {
	ProjectID string
	BucketID  string
	FileID    string
	Name      string
	MimeType  string
	Metadata  map[string]string // nil 表示不修改；非 nil（含空 map）整体替换
	Principal databases.Principal
}

func (s *Storage) UpdateFile(ctx context.Context, cmd UpdateFileCommand) (*storage.File, error) {
	project, err := s.resolveProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	doc, err := s.docDB.GetDocument(ctx, project.ID, "default", "files", cmd.FileID, cmd.Principal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	file := mapFileDoc(doc)
	if file.BucketID != cmd.BucketID {
		return nil, status.Error(codes.NotFound, "file not found in bucket")
	}

	data := map[string]any{}
	if cmd.Name != "" {
		data["name"] = cmd.Name
	}
	if cmd.MimeType != "" {
		data["mime_type"] = normalizeMimeType(cmd.MimeType)
	}
	if cmd.Metadata != nil {
		data["metadata"] = cmd.Metadata
	}
	if len(data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "nothing to update")
	}
	updated, err := s.docDB.UpdateDocument(ctx, project.ID, "default", "files", databases.DocumentUpdate{
		Document: databases.Document{ID: cmd.FileID, Data: data},
	}, cmd.Principal)
	if err != nil {
		return nil, err
	}
	return mapFileDoc(&updated), nil
}

// GetStorageUsage 统计项目级 bucket/文件数量与总容量（按调用方读权限过滤）。
func (s *Storage) GetStorageUsage(ctx context.Context, projectID string, principal databases.Principal) (*storage.Usage, error) {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, err
	}
	buckets, err := s.docDB.CountDocuments(ctx, project.ID, "default", "buckets", nil, principal)
	if err != nil {
		return nil, err
	}
	files, err := s.docDB.CountDocuments(ctx, project.ID, "default", "files", nil, principal)
	if err != nil {
		return nil, err
	}
	totalSize, err := s.docDB.SumDocumentField(ctx, project.ID, "default", "files", "size", principal)
	if err != nil {
		return nil, err
	}
	return &storage.Usage{Buckets: buckets, Files: files, TotalSize: totalSize}, nil
}

// maxFileTokenLifetime caps token validity at 7 days.
const maxFileTokenLifetime = 7 * 24 * 3600

// defaultFileTokenLifetime is the validity when expires_in is not provided.
const defaultFileTokenLifetime = 3600

// FileToken 是短期匿名下载凭证。
type FileToken struct {
	Token     string
	ExpiresAt time.Time
}

// CreateFileToken 为文件签发 HMAC 签名的短期匿名下载 token（绑定 bucket/file/过期时间）。
func (s *Storage) CreateFileToken(ctx context.Context, projectID, bucketID, fileID string, expiresIn int64, principal databases.Principal) (*FileToken, error) {
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	doc, err := s.docDB.GetDocument(ctx, project.ID, "default", "files", fileID, principal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	file := mapFileDoc(doc)
	if file.BucketID != bucketID {
		return nil, status.Error(codes.NotFound, "file not found in bucket")
	}

	if expiresIn <= 0 {
		expiresIn = defaultFileTokenLifetime
	}
	if expiresIn > maxFileTokenLifetime {
		expiresIn = maxFileTokenLifetime
	}
	secret := s.cfg.GetSecurity().GetJwt().GetSecret()
	if secret == "" {
		return nil, status.Error(codes.Internal, "file token secret is not configured")
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	token := signFileToken(secret, project.ID, bucketID, fileID, expiresAt.Unix())
	return &FileToken{Token: token, ExpiresAt: expiresAt}, nil
}

// ParseFileToken 校验匿名下载 token：签名正确、未过期，返回绑定的
// project/bucket/file。任何一项不符即返回错误（调用方仍需比对路径参数）。
// token 格式："{expiresAt}.{projectID}.{bucketID}.{fileID}.{hex(hmac)}"。
func (s *Storage) ParseFileToken(token string) (projectID, bucketID, fileID string, err error) {
	secret := s.cfg.GetSecurity().GetJwt().GetSecret()
	if secret == "" {
		return "", "", "", status.Error(codes.Internal, "file token secret is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 5 {
		return "", "", "", status.Error(codes.Unauthenticated, "invalid file token")
	}
	expiresAt, parseErr := strconv.ParseInt(parts[0], 10, 64)
	if parseErr != nil {
		return "", "", "", status.Error(codes.Unauthenticated, "invalid file token")
	}
	if time.Now().Unix() >= expiresAt {
		return "", "", "", status.Error(codes.Unauthenticated, "file token expired")
	}
	pid, bid, fid := parts[1], parts[2], parts[3]
	expected := signFileToken(secret, pid, bid, fid, expiresAt)
	if !hmac.Equal([]byte(expected), []byte(token)) {
		return "", "", "", status.Error(codes.Unauthenticated, "invalid file token")
	}
	return pid, bid, fid, nil
}

// signFileToken 计算 token = "{expiresAt}.{projectID}.{bucketID}.{fileID}.{hex(hmac)}"。
func signFileToken(secret, projectID, bucketID, fileID string, expiresAt int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s.%s.%s.%d", projectID, bucketID, fileID, expiresAt)
	return fmt.Sprintf("%d.%s.%s.%s.%s", expiresAt, projectID, bucketID, fileID, hex.EncodeToString(mac.Sum(nil)))
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
	if v, ok := doc.Data["public"].(bool); ok {
		b.Public = v
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
