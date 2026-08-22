package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/storage"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateUploadCommand 创建分片上传会话。projectID 由 handler 传入，OwnerUserID 已废弃（A8）——统一从 principal 派生（EndUser 时取 user:<id>），请求中的 OwnerUserID/Permissions 仅为兼容旧调用方，服务端以 principal 为准忽略。
type CreateUploadCommand struct {
	ProjectID   string
	BucketID    string
	Name        string
	MimeType    string
	Size        int64
	Metadata    map[string]string
	Permissions []string
	OwnerUserID string
}

// CreateUploadSession 创建上传会话：校验 size 上限（part_count ≤ 10000）、
// bucket 存在，mime 归一化，预生成 uploadID/fileID。
func (s *Storage) CreateUploadSession(ctx context.Context, cmd CreateUploadCommand, principal databases.Principal) (*storage.UploadSession, error) {
	if cmd.BucketID == "" {
		return nil, status.Error(codes.InvalidArgument, "bucket_id is required")
	}
	if cmd.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if cmd.Size <= 0 {
		return nil, status.Error(codes.InvalidArgument, "size must be positive")
	}
	if cmd.Size > storage.MaxUploadSize {
		return nil, status.Error(codes.InvalidArgument, "file too large")
	}
	project, err := s.resolveProject(ctx, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	bucket, err := s.buckets.GetByID(ctx, project.ID, cmd.BucketID)
	if err != nil {
		return nil, err
	}
	if bucket == nil {
		return nil, status.Error(codes.NotFound, "bucket not found")
	}

	// A8：OwnerUserID 从 principal 派生（EndUser），丢弃未落地的 Permissions。兼容旧测试：若 principal 非 EndUser 但 cmd 带 OwnerUserID，则保留。
	ownerUserID := storageEndUserID(principal)
	if ownerUserID == "" && cmd.OwnerUserID != "" {
		ownerUserID = cmd.OwnerUserID
	}

	chunkSize := int64(storage.DefaultChunkSize)
	partCount := int((cmd.Size + chunkSize - 1) / chunkSize)
	now := time.Now()
	session := &storage.UploadSession{
		ID:          idgen.UUID().String(),
		ProjectID:   project.ID,
		BucketID:    cmd.BucketID,
		FileID:      idgen.UUID().String(),
		OwnerUserID: ownerUserID,
		Name:        cmd.Name,
		MimeType:    normalizeMimeType(cmd.MimeType),
		Size:        cmd.Size,
		Metadata:    cmd.Metadata,
		ChunkSize:   chunkSize,
		PartCount:   partCount,
		Received:    map[int]bool{},
		CreatedAt:   now,
		ExpiresAt:   now.Add(storage.UploadSessionTTL),
	}
	if err := s.uploads.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("create upload session: %w", err)
	}
	return session, nil
}

// GetUploadSession 查询会话（续传：received 已收分片）；过期/不存在 NotFound，
// 项目归属不符 PermissionDenied。
func (s *Storage) GetUploadSession(ctx context.Context, projectID, uploadID string, principal databases.Principal) (*storage.UploadSession, error) {
	if uploadID == "" {
		return nil, status.Error(codes.InvalidArgument, "upload_id is required")
	}
	session, err := s.uploads.Get(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.ExpiresAt.Before(time.Now()) {
		return nil, status.Error(codes.NotFound, "upload session not found or expired")
	}
	if session.ProjectID != projectID {
		return nil, status.Error(codes.PermissionDenied, "upload session does not belong to project")
	}
	if _, err := s.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return session, nil
}

// UploadChunk 上传一个分片（同号覆盖 = 幂等）。校验顺序：
// size 上限 → part 越界 → 分片大小严格校验（非末片 == chunkSize，末片 1..chunkSize）。
// 返回该会话已收分片总数（CountChunks，原子准确）。
func (s *Storage) UploadChunk(ctx context.Context, projectID, uploadID string, partNumber int, content io.Reader, size int64, ownerUserID string, principal databases.Principal) (int, error) {
	session, err := s.uploads.Get(ctx, uploadID)
	if err != nil {
		return 0, err
	}
	if session == nil || session.ExpiresAt.Before(time.Now()) {
		return 0, status.Error(codes.NotFound, "upload session not found or expired")
	}
	if session.ProjectID != projectID {
		return 0, status.Error(codes.PermissionDenied, "upload session does not belong to project")
	}
	if err := checkUploadOwner(session, ownerUserID, principal); err != nil {
		return 0, err
	}
	if partNumber < 1 || partNumber > session.PartCount {
		return 0, status.Error(codes.InvalidArgument, "part number out of range")
	}
	if size > storage.MaxChunkSize {
		return 0, status.Error(codes.InvalidArgument, "chunk exceeds maximum size")
	}
	if partNumber < session.PartCount && size != session.ChunkSize {
		return 0, status.Error(codes.InvalidArgument, "chunk size must equal chunk_size for non-final parts")
	}
	if partNumber == session.PartCount && (size < 1 || size > session.ChunkSize) {
		return 0, status.Error(codes.InvalidArgument, "chunk size out of range for final part")
	}
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if err := s.store.EnsureBucket(ctx, defaultBucketName(s.cfg)); err != nil {
		return 0, fmt.Errorf("ensure storage bucket: %w", err)
	}
	key := chunkKey(project.ID, session.BucketID, session.FileID, partNumber)
	if err := s.store.Put(ctx, defaultBucketName(s.cfg), key, content, size, "application/octet-stream"); err != nil {
		return 0, fmt.Errorf("upload chunk: %w", err)
	}
	if err := s.uploads.MarkChunk(ctx, uploadID, partNumber); err != nil {
		return 0, fmt.Errorf("mark chunk: %w", err)
	}
	return s.uploads.CountChunks(ctx, uploadID)
}

// CompleteUpload 合并分片并创建文件文档。时序：
// Lock → 缺片校验 → Compose → 建文档 → 删分片 → 删会话 → Unlock（defer）。
func (s *Storage) CompleteUpload(ctx context.Context, projectID, uploadID, ownerUserID string, principal databases.Principal) (*storage.File, error) {
	session, err := s.uploads.Get(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.ExpiresAt.Before(time.Now()) {
		return nil, status.Error(codes.NotFound, "upload session not found or expired")
	}
	if session.ProjectID != projectID {
		return nil, status.Error(codes.PermissionDenied, "upload session does not belong to project")
	}
	if err := checkUploadOwner(session, ownerUserID, principal); err != nil {
		return nil, err
	}
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	token, locked, err := s.uploads.LockComplete(ctx, uploadID)
	if err != nil {
		return nil, fmt.Errorf("lock complete: %w", err)
	}
	if !locked {
		return nil, status.Error(codes.FailedPrecondition, "upload is already being completed")
	}
	// 锁在会话删除之后释放（defer 在函数返回时执行）。
	defer func() { _ = s.uploads.UnlockComplete(ctx, uploadID) }()

	// 竞态修复（G6-6/R07-P2-4）：加锁成功后重新读取会话——锁前快照可能已过期
	// （并发 UploadChunk 在快照后补传了缺片，或并发 AbortUpload 已删除会话）。
	// 缺片判定必须基于锁内最新状态。
	session, err = s.uploads.Get(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, status.Error(codes.NotFound, "upload session not found")
	}

	// 缺片校验：锁在缺片时释放，会话保留可继续传（续传关键）。
	missing := make([]int, 0, session.PartCount)
	for i := 1; i <= session.PartCount; i++ {
		if !session.Received[i] {
			missing = append(missing, i)
		}
	}
	if len(missing) > 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "missing chunks: %v", missing)
	}

	if err := s.store.EnsureBucket(ctx, defaultBucketName(s.cfg)); err != nil {
		return nil, fmt.Errorf("ensure storage bucket: %w", err)
	}

	dstKey := objectKey(project.ID, session.BucketID, session.FileID)
	chunkKeys := make([]string, 0, session.PartCount)
	for i := 1; i <= session.PartCount; i++ {
		chunkKeys = append(chunkKeys, chunkKey(project.ID, session.BucketID, session.FileID, i))
	}
	if err := s.store.Compose(ctx, defaultBucketName(s.cfg), dstKey, chunkKeys); err != nil {
		// Compose 失败不删任何东西：可重试（分片与会话保留）。
		return nil, fmt.Errorf("compose file: %w", err)
	}

	// A8：CompleteUpload 的 OwnerUserID 从会话或 principal 派生，丢弃未落地 Permissions。
	derivedOwner := session.OwnerUserID
	if derivedOwner == "" {
		if uid := storageEndUserID(principal); uid != "" {
			derivedOwner = uid
		} else {
			derivedOwner = ownerUserID
		}
	}
	// 若会话 OwnerUserID 非空但调用方为同 EndUser 的另一身份，需以会话 owner 为准已在 checkUploadOwner 校验；此处不覆写。

	now := time.Now()
	file := &storage.File{
		ID:          session.FileID,
		ProjectID:   project.ID,
		BucketID:    session.BucketID,
		Name:        session.Name,
		MimeType:    session.MimeType,
		Size:        session.Size,
		Metadata:    session.Metadata,
		OwnerUserID: derivedOwner,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.files.Insert(ctx, project.ID, file); err != nil {
		// 回滚：确认「自己仍是锁持有者 + 会话仍存在」双重条件后才删最终对象。
		// 锁 TTL（1h）若已过期，第二个 complete 可能已重新加锁并成功建文档，
		// 此时无条件删对象会误删其成果 → 数据损坏。
		if owner, oerr := s.uploads.IsLockOwner(ctx, uploadID, token); oerr == nil && owner {
			if s2, gerr := s.uploads.Get(ctx, uploadID); gerr == nil && s2 != nil {
				_ = s.store.Delete(ctx, defaultBucketName(s.cfg), dstKey)
			}
		}
		return nil, fmt.Errorf("create file document: %w", err)
	}

	// 清理暂存分片（失败仅记日志，孤儿分片由未来后台清理任务覆盖）。
	for i := 1; i <= session.PartCount; i++ {
		if derr := s.store.Delete(ctx, defaultBucketName(s.cfg), chunkKeys[i-1]); derr != nil {
			slog.Warn("delete chunk object failed", "key", chunkKeys[i-1], "error", derr)
		}
	}
	if err := s.uploads.Delete(ctx, uploadID); err != nil {
		return nil, fmt.Errorf("delete upload session: %w", err)
	}

	return &storage.File{
		ID:          session.FileID,
		ProjectID:   project.ID,
		BucketID:    session.BucketID,
		Name:        session.Name,
		MimeType:    session.MimeType,
		Size:        session.Size,
		Metadata:    session.Metadata,
		OwnerUserID: derivedOwner,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// AbortUpload 取消上传：删会话后清理全部暂存分片对象（逐片 Delete，幂等）。
// 竞态修复（G6-6/R07-P2-5）：先尝试获取 complete 互斥锁——complete 进行中
// （Compose 合并分片）时 abort 删除会话/分片会破坏其输出；锁获取失败返回
// FailedPrecondition 提示重试。
func (s *Storage) AbortUpload(ctx context.Context, projectID, uploadID, ownerUserID string, principal databases.Principal) error {
	session, err := s.uploads.Get(ctx, uploadID)
	if err != nil {
		return err
	}
	if session == nil || session.ExpiresAt.Before(time.Now()) {
		return status.Error(codes.NotFound, "upload session not found or expired")
	}
	if session.ProjectID != projectID {
		return status.Error(codes.PermissionDenied, "upload session does not belong to project")
	}
	if err := checkUploadOwner(session, ownerUserID, principal); err != nil {
		return err
	}
	project, err := s.resolveProject(ctx, projectID)
	if err != nil {
		return err
	}
	_, locked, err := s.uploads.LockComplete(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("lock complete: %w", err)
	}
	if !locked {
		return status.Error(codes.FailedPrecondition, "upload is being completed, retry later")
	}
	// abort 删除会话时 Redis 实现会一并删除锁 key，defer 释放为幂等兜底。
	defer func() { _ = s.uploads.UnlockComplete(ctx, uploadID) }()

	if err := s.uploads.Delete(ctx, uploadID); err != nil {
		return fmt.Errorf("delete upload session: %w", err)
	}
	for i := 1; i <= session.PartCount; i++ {
		if derr := s.store.Delete(ctx, defaultBucketName(s.cfg), chunkKey(project.ID, session.BucketID, session.FileID, i)); derr != nil {
			slog.Warn("delete chunk object failed", "part", i, "error", derr)
		}
	}
	return nil
}

// checkUploadOwner 校验调用方是否有权操作该上传会话：
//   - 会话 OwnerUserID 为空（API key 创建）→ 仅走项目归属 + scope 门禁，不校验 owner；
//   - 否则要求调用方 UserID 与会话 OwnerUserID 一致；admin/keys/system 主体豁免
//     （与文件权限模型一致：keys/admin 具备 update/delete 权限）。
func checkUploadOwner(session *storage.UploadSession, callerUserID string, principal databases.Principal) error {
	if session.OwnerUserID == "" {
		return nil
	}
	if callerUserID == session.OwnerUserID {
		return nil
	}
	if principal.BypassesDocumentACL() || principal.HasRole("keys") {
		return nil
	}
	// admin console 角色同样豁免会话 owner 校验（A8：admin 可管理全部文件/会话）
	for _, r := range principal.Roles {
		switch r {
		case "owner", "admin", "member", "viewer":
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "upload session does not belong to caller")
}

// chunkKey 分片对象 key：`{objectKey}/chunks/{part:03d}`，与最终对象 key 不冲突。
func chunkKey(projectID, bucketID, fileID string, part int) string {
	return fmt.Sprintf("%s/chunks/%03d", objectKey(projectID, bucketID, fileID), part)
}
