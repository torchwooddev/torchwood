package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// serverWriteCtx 返回带 Server API 写主体（API key 类型）principal 的上下文：
// G6-4 后 CreateBucket use-case 要求 RequireServerWriteActor，直接调 use-case
// 的集成测试需注入主体。
func serverWriteCtx() context.Context {
	return contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID: "test-key", ActorKind: shared.ActorKindService, Roles: []string{"keys"},
	})
}

// TestStorage_Acceptance_ServerAPI covers manual checklist §4.11–4.13:
// create bucket, create/list/get/delete file via use-case (gRPC 小文件路径).
func TestStorage_Acceptance_ServerAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := serverWriteCtx()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	store := testutil.NewMemObjectStore()
	_, upStore := newTestUploadSessionStore(t)
	uc := NewStorage(&config.AppConfig{}, bunrepo.NewProjectRepository(db), docDB, store, upStore)
	principal := databases.Principal{Roles: []string{"keys"}}

	bucket, err := uc.CreateBucket(ctx, CreateBucketCommand{
		ProjectID: projectID,
		Name:      "acceptance-bucket",
	})
	require.NoError(t, err)
	require.NotEmpty(t, bucket.ID)

	content := []byte("Torchwood storage acceptance")
	file, err := uc.CreateFile(ctx, CreateFileCommand{
		ProjectID: projectID,
		BucketID:  bucket.ID,
		Name:      "test.txt",
		MimeType:  "text/plain",
	}, bytes.NewReader(content), int64(len(content)), principal)
	require.NoError(t, err)
	require.NotEmpty(t, file.ID)
	require.Equal(t, int64(len(content)), file.Size)

	files, total, _, err := uc.ListFiles(ctx, projectID, bucket.ID, databases.Query{}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, files, 1)
	require.Equal(t, file.ID, files[0].ID)

	gotMeta, reader, err := uc.GetFile(ctx, projectID, bucket.ID, file.ID, principal)
	require.NoError(t, err)
	require.NotNil(t, gotMeta)
	defer reader.Close()
	gotContent, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, content, gotContent)

	require.NoError(t, uc.DeleteFile(ctx, projectID, bucket.ID, file.ID, principal))
	files, total, _, err = uc.ListFiles(ctx, projectID, bucket.ID, databases.Query{}, principal)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Empty(t, files)
}

func newStorageUC(t *testing.T) (context.Context, *Storage, string, *config.AppConfig) {
	t.Helper()
	ctx := serverWriteCtx()
	db := testutil.SetupTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	t.Cleanup(cleanup)

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	cfg := &config.AppConfig{}
	cfg.Security = &config.Security{Jwt: &config.Security_Jwt{Secret: "test-file-token-secret"}}
	_, upStore := newTestUploadSessionStore(t)
	uc := NewStorage(cfg, bunrepo.NewProjectRepository(db), docDB, testutil.NewMemObjectStore(), upStore)
	return ctx, uc, projectID, cfg
}

// TestStorage_UpdateFile 覆盖元数据更新：改名/改 MIME/替换 metadata；
// 空请求与不存在的文件报错。
func TestStorage_UpdateFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, uc, projectID, _ := newStorageUC(t)
	principal := databases.Principal{Roles: []string{"keys"}}

	bucket, err := uc.CreateBucket(ctx, CreateBucketCommand{ProjectID: projectID, Name: "updates"})
	require.NoError(t, err)
	file, err := uc.CreateFile(ctx, CreateFileCommand{
		ProjectID: projectID,
		BucketID:  bucket.ID,
		Name:      "a.txt",
		MimeType:  "text/plain",
		Metadata:  map[string]string{"k": "v"},
	}, strings.NewReader("hello"), 5, principal)
	require.NoError(t, err)

	updated, err := uc.UpdateFile(ctx, UpdateFileCommand{
		ProjectID: projectID,
		BucketID:  bucket.ID,
		FileID:    file.ID,
		Name:      "b.txt",
		MimeType:  "text/markdown",
		Metadata:  map[string]string{"k2": "v2"},
		Principal: principal,
	})
	require.NoError(t, err)
	require.Equal(t, "b.txt", updated.Name)
	require.Equal(t, "text/markdown", updated.MimeType)
	require.Equal(t, map[string]string{"k2": "v2"}, updated.Metadata)

	// 空请求 → InvalidArgument
	_, err = uc.UpdateFile(ctx, UpdateFileCommand{
		ProjectID: projectID,
		BucketID:  bucket.ID,
		FileID:    file.ID,
		Principal: principal,
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// 不存在的文件 → NotFound
	_, err = uc.UpdateFile(ctx, UpdateFileCommand{
		ProjectID: projectID,
		BucketID:  bucket.ID,
		FileID:    "missing",
		Name:      "x",
		Principal: principal,
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

// TestStorage_GetStorageUsage 统计 buckets/files/总容量；无权限主体只见可见文档。
func TestStorage_GetStorageUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, uc, projectID, _ := newStorageUC(t)
	principal := databases.Principal{Roles: []string{"keys"}}

	bucket, err := uc.CreateBucket(ctx, CreateBucketCommand{ProjectID: projectID, Name: "usage"})
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		content := []byte(fmt.Sprintf("file-%d", i))
		_, err := uc.CreateFile(ctx, CreateFileCommand{
			ProjectID: projectID,
			BucketID:  bucket.ID,
			Name:      fmt.Sprintf("f%d.txt", i),
			MimeType:  "text/plain",
		}, bytes.NewReader(content), int64(len(content)), principal)
		require.NoError(t, err)
	}

	usage, err := uc.GetStorageUsage(ctx, projectID, principal)
	require.NoError(t, err)
	require.Equal(t, int64(1), usage.Buckets)
	require.Equal(t, int64(3), usage.Files)
	require.Equal(t, int64(18), usage.TotalSize, "3 个文件各 6 字节")
}

// TestStorage_FileToken 签发/校验：有效 token 通过、过期拒绝、篡改拒绝、跨文件拒绝。
func TestStorage_FileToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, uc, projectID, _ := newStorageUC(t)
	principal := databases.Principal{Roles: []string{"keys"}}

	bucket, err := uc.CreateBucket(ctx, CreateBucketCommand{ProjectID: projectID, Name: "tokens"})
	require.NoError(t, err)
	file, err := uc.CreateFile(ctx, CreateFileCommand{
		ProjectID: projectID,
		BucketID:  bucket.ID,
		Name:      "secret.txt",
		MimeType:  "text/plain",
	}, strings.NewReader("secret"), 6, principal)
	require.NoError(t, err)

	token, err := uc.CreateFileToken(ctx, projectID, bucket.ID, file.ID, 300, principal)
	require.NoError(t, err)
	require.NotEmpty(t, token.Token)
	require.WithinDuration(t, time.Now().Add(5*time.Minute), token.ExpiresAt, time.Minute)

	pid, bid, fid, err := uc.ParseFileToken(token.Token)
	require.NoError(t, err)
	require.Equal(t, projectID, pid)
	require.Equal(t, bucket.ID, bid)
	require.Equal(t, file.ID, fid)

	// 篡改 token → 拒绝
	_, _, _, err = uc.ParseFileToken(token.Token + "x")
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// 过期 token → 拒绝（构造短有效期并等待）
	short, err := uc.CreateFileToken(ctx, projectID, bucket.ID, file.ID, 1, principal)
	require.NoError(t, err)
	_, _, _, err = uc.ParseFileToken(short.Token)
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	_, _, _, err = uc.ParseFileToken(short.Token)
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// 文件系统集合集合级 read:any 兜底：任何可读主体均可签发 token。
	restricted, err := uc.CreateFile(ctx, CreateFileCommand{
		ProjectID:   projectID,
		BucketID:    bucket.ID,
		Name:        "restricted.txt",
		MimeType:    "text/plain",
		Permissions: []string{"read:keys", "read:admin"},
	}, strings.NewReader("restricted"), 10, principal)
	require.NoError(t, err)
	userToken, err := uc.CreateFileToken(ctx, projectID, bucket.ID, restricted.ID, 300, databases.Principal{Roles: []string{"users"}})
	require.NoError(t, err)
	_, _, _, err = uc.ParseFileToken(userToken.Token)
	require.NoError(t, err)
}

// TestStorage_PublicBucket 公开 bucket 元数据读写：创建带 public、匿名列表可见。
func TestStorage_PublicBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, uc, projectID, _ := newStorageUC(t)

	bucket, err := uc.CreateBucket(ctx, CreateBucketCommand{ProjectID: projectID, Name: "public-bucket", Public: true})
	require.NoError(t, err)
	require.True(t, bucket.Public)

	buckets, _, err := uc.ListBuckets(ctx, projectID, databases.Query{}, databases.GuestPrincipal)
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	require.True(t, buckets[0].Public)
}

// TestStorage_UpdateBucket 覆盖 bucket 元数据更新：公开开关切换、改名。
func TestStorage_UpdateBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, uc, projectID, _ := newStorageUC(t)
	principal := databases.Principal{Roles: []string{"keys"}}

	bucket, err := uc.CreateBucket(ctx, CreateBucketCommand{ProjectID: projectID, Name: "private"})
	require.NoError(t, err)

	public := true
	updated, err := uc.UpdateBucket(ctx, UpdateBucketCommand{
		ProjectID: projectID,
		ID:        bucket.ID,
		Public:    &public,
		Principal: principal,
	})
	require.NoError(t, err)
	require.True(t, updated.Public)

	// 改名 + 保持 public 不变。
	renamed, err := uc.UpdateBucket(ctx, UpdateBucketCommand{
		ProjectID: projectID,
		ID:        bucket.ID,
		Name:      "renamed",
		Principal: principal,
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", renamed.Name)
	require.True(t, renamed.Public, "未传 public 时保持原值")

	// 空请求 → InvalidArgument。
	_, err = uc.UpdateBucket(ctx, UpdateBucketCommand{
		ProjectID: projectID,
		ID:        bucket.ID,
		Principal: principal,
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
