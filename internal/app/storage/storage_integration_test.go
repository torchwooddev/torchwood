package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	"github.com/torchwoodio/torchwood/internal/pkg/config"
	"github.com/torchwoodio/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestStorage_Acceptance_ServerAPI covers manual checklist §4.11–4.13:
// create bucket, create/list/get/delete file via use-case (gRPC 小文件路径).
func TestStorage_Acceptance_ServerAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	store := testutil.NewMemObjectStore()
	uc := NewStorage(&config.AppConfig{}, bunrepo.NewProjectRepository(db), docDB, store)
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
