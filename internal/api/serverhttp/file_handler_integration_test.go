package serverhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/torchwoodio/torchwood/internal/app/client"
	appstorage "github.com/torchwoodio/torchwood/internal/app/storage"
	"github.com/torchwoodio/torchwood/internal/infra/auth"
	"github.com/torchwoodio/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	"github.com/torchwoodio/torchwood/internal/pkg/config"
	"github.com/torchwoodio/torchwood/internal/testutil"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/require"
)

type storageHTTPFixture struct {
	t         *testing.T
	projectID string
	apiSecret string
	handler   *FileHandler
	server    *httptest.Server
	bucketID  string
}

func setupStorageHTTPFixture(t *testing.T) *storageHTTPFixture {
	t.Helper()

	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	projectID, internalID, projectCleanup := testutil.CreateTestProject(ctx, db)
	apiSecret, keyCleanup := testutil.CreateTestAPIKey(ctx, db, projectID, nil)

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	cfg := &config.AppConfig{}
	store := testutil.NewMemObjectStore()
	storageUC := appstorage.NewStorage(cfg, bunrepo.NewProjectRepository(db), docDB, store)
	validator := auth.NewValidator(
		cfg,
		bunrepo.NewAPIKeyRepository(db),
		bunrepo.NewConsoleAdminRepository(db),
		bunrepo.NewConsoleAdminProjectRepository(db),
		nil,
		docDB,
	)
	handler, err := NewFileHandler(cfg, validator, storageUC)
	require.NoError(t, err)

	mux := runtime.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)

	bucket, err := storageUC.CreateBucket(ctx, appstorage.CreateBucketCommand{
		ProjectID: projectID,
		Name:      "http-test-bucket",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		server.Close()
		keyCleanup()
		projectCleanup()
		db.Close()
	})

	return &storageHTTPFixture{
		t:         t,
		projectID: projectID,
		apiSecret: apiSecret,
		handler:   handler,
		server:    server,
		bucketID:  bucket.ID,
	}
}

func (f *storageHTTPFixture) upload(content []byte, headers map[string]string) (string, int) {
	f.t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(f.t, err)
	_, err = part.Write(content)
	require.NoError(f.t, err)
	require.NoError(f.t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/storage/buckets/"+f.bucketID+"/files", body)
	require.NoError(f.t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	defer resp.Body.Close()

	var payload struct {
		ID string `json:"id"`
	}
	if resp.StatusCode == http.StatusCreated {
		require.NoError(f.t, json.NewDecoder(resp.Body).Decode(&payload))
	}
	return payload.ID, resp.StatusCode
}

func (f *storageHTTPFixture) download(path string, headers map[string]string) (int, []byte, http.Header) {
	f.t.Helper()

	req, err := http.NewRequest(http.MethodGet, f.server.URL+path, nil)
	require.NoError(f.t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(f.t, err)
	return resp.StatusCode, data, resp.Header
}

// TestFileHandler_Acceptance covers manual checklist §5.1–5.4:
// multipart upload, download, inline view, API Key auth.
func TestFileHandler_Acceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	fix := setupStorageHTTPFixture(t)
	content := []byte("multipart acceptance payload")
	headers := map[string]string{"X-Api-Key": fix.apiSecret}

	fileID, status := fix.upload(content, headers)
	require.Equal(t, http.StatusCreated, status)
	require.NotEmpty(t, fileID)

	downloadPath := "/v1/storage/buckets/" + fix.bucketID + "/files/" + fileID + "/download"
	code, got, _ := fix.download(downloadPath, headers)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, content, got)

	viewPath := "/v1/storage/buckets/" + fix.bucketID + "/files/" + fileID + "/view"
	code, gotView, respHeaders := fix.download(viewPath, headers)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, content, gotView)
	require.NotEmpty(t, respHeaders.Get("Content-Type"))
	require.Contains(t, respHeaders.Get("Content-Disposition"), "inline")
}

// TestFileHandler_UserJWTProjectScope covers manual checklist §5.5:
// end-user JWT operates only on the project embedded in the token.
func TestFileHandler_UserJWTProjectScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	projectA, internalA, cleanupA := testutil.CreateTestProject(ctx, db)
	projectB, internalB, cleanupB := testutil.CreateTestProject(ctx, db)

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectA, internalA))
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectB, internalB))

	cfg := &config.AppConfig{}
	store := testutil.NewMemObjectStore()
	projectRepo := bunrepo.NewProjectRepository(db)
	storageUC := appstorage.NewStorage(cfg, projectRepo, docDB, store)
	account := client.NewTestAccount(cfg, projectRepo, docDB)

	_, tokens, _, err := account.SignUp(ctx, client.SignUpCommand{
		ProjectID: projectA,
		Email:     "storage-http@torchwood.local",
		Password:  "User@123456",
		Name:      "Storage HTTP",
	})
	require.NoError(t, err)

	bucketA, err := storageUC.CreateBucket(ctx, appstorage.CreateBucketCommand{
		ProjectID: projectA,
		Name:      "bucket-a",
	})
	require.NoError(t, err)
	bucketB, err := storageUC.CreateBucket(ctx, appstorage.CreateBucketCommand{
		ProjectID: projectB,
		Name:      "bucket-b",
	})
	require.NoError(t, err)

	validator := auth.NewValidator(
		cfg,
		bunrepo.NewAPIKeyRepository(db),
		bunrepo.NewConsoleAdminRepository(db),
		bunrepo.NewConsoleAdminProjectRepository(db),
		nil,
		docDB,
	)
	handler, err := NewFileHandler(cfg, validator, storageUC)
	require.NoError(t, err)
	mux := runtime.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)

	t.Cleanup(func() {
		server.Close()
		cleanupB()
		cleanupA()
		db.Close()
	})

	userHeaders := map[string]string{"Authorization": "Bearer " + tokens.AccessToken}
	want := []byte("project-a-only")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "owned.txt")
	require.NoError(t, err)
	_, err = part.Write(want)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/storage/buckets/"+bucketA.ID+"/files", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range userHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NotEmpty(t, created.ID)

	// Forged X-Torchwood-Project must not grant access to another project's bucket.
	bodyB := &bytes.Buffer{}
	writerB := multipart.NewWriter(bodyB)
	partB, err := writerB.CreateFormFile("file", "blocked.txt")
	require.NoError(t, err)
	_, err = partB.Write([]byte("should-not-upload"))
	require.NoError(t, err)
	require.NoError(t, writerB.Close())

	reqB, err := http.NewRequest(http.MethodPost, server.URL+"/v1/storage/buckets/"+bucketB.ID+"/files", bodyB)
	require.NoError(t, err)
	reqB.Header.Set("Content-Type", writerB.FormDataContentType())
	reqB.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	reqB.Header.Set("X-Torchwood-Project", projectB)
	respB, err := http.DefaultClient.Do(reqB)
	require.NoError(t, err)
	respB.Body.Close()
	require.NotEqual(t, http.StatusCreated, respB.StatusCode)

	downloadReq, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/v1/storage/buckets/"+bucketA.ID+"/files/"+created.ID+"/download",
		nil,
	)
	require.NoError(t, err)
	for k, v := range userHeaders {
		downloadReq.Header.Set(k, v)
	}
	downloadResp, err := http.DefaultClient.Do(downloadReq)
	require.NoError(t, err)
	defer downloadResp.Body.Close()
	require.Equal(t, http.StatusOK, downloadResp.StatusCode)
	got, err := io.ReadAll(downloadResp.Body)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestFileHandler_APIKeyRequiresStorageScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	projectID, internalID, projectCleanup := testutil.CreateTestProject(ctx, db)
	apiSecret, keyCleanup := testutil.CreateTestAPIKey(ctx, db, projectID, []string{"users"})

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	cfg := &config.AppConfig{}
	store := testutil.NewMemObjectStore()
	storageUC := appstorage.NewStorage(cfg, bunrepo.NewProjectRepository(db), docDB, store)
	validator := auth.NewValidator(
		cfg,
		bunrepo.NewAPIKeyRepository(db),
		bunrepo.NewConsoleAdminRepository(db),
		bunrepo.NewConsoleAdminProjectRepository(db),
		nil,
		docDB,
	)
	handler, err := NewFileHandler(cfg, validator, storageUC)
	require.NoError(t, err)
	mux := runtime.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)

	bucket, err := storageUC.CreateBucket(ctx, appstorage.CreateBucketCommand{
		ProjectID: projectID,
		Name:      "scope-test-bucket",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		server.Close()
		keyCleanup()
		projectCleanup()
		db.Close()
	})

	_, status := (&storageHTTPFixture{
		t:         t,
		projectID: projectID,
		apiSecret: apiSecret,
		handler:   handler,
		server:    server,
		bucketID:  bucket.ID,
	}).upload([]byte("blocked"), map[string]string{"X-Api-Key": apiSecret})
	require.Equal(t, http.StatusForbidden, status)
}

func TestFileHandler_AdminRequiresProjectAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)

	projectID, internalID, projectCleanup := testutil.CreateTestProject(ctx, db)
	otherProjectID, otherInternalID, otherCleanup := testutil.CreateTestProject(ctx, db)

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))
	require.NoError(t, docDB.EnsureSystemCollections(ctx, otherProjectID, otherInternalID))

	cfg := &config.AppConfig{}
	store := testutil.NewMemObjectStore()
	storageUC := appstorage.NewStorage(cfg, bunrepo.NewProjectRepository(db), docDB, store)
	admin, adminCleanup := testutil.CreateTestConsoleAdmin(ctx, db, "member")
	token, err := testutil.SignConsoleAdminToken(cfg, admin)
	require.NoError(t, err)
	require.NoError(t, testutil.GrantConsoleAdminProject(ctx, db, admin.ID, projectID))

	validator := auth.NewValidator(
		cfg,
		bunrepo.NewAPIKeyRepository(db),
		bunrepo.NewConsoleAdminRepository(db),
		bunrepo.NewConsoleAdminProjectRepository(db),
		nil,
		docDB,
	)
	handler, err := NewFileHandler(cfg, validator, storageUC)
	require.NoError(t, err)
	mux := runtime.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)

	bucket, err := storageUC.CreateBucket(ctx, appstorage.CreateBucketCommand{
		ProjectID: otherProjectID,
		Name:      "foreign-bucket",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		server.Close()
		adminCleanup()
		otherCleanup()
		projectCleanup()
		db.Close()
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "blocked.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("should-not-upload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/storage/buckets/"+bucket.ID+"/files", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Torchwood-Project", otherProjectID)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
