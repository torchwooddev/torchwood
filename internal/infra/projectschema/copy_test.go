package projectschema_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

var stagingTableNames = []string{
	"sys_users", "sys_sessions", "sys_identities", "sys_groups",
	"sys_memberships", "sys_buckets", "sys_files",
}

func TestApply_StagingLeavesDocumentUsers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	quoted := testutil.CatalogQuoted(projectID)
	var version int64
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT MAX(version) FROM "+quoted+".schema_migrations").Scan(&version))
	require.Equal(t, int64(8), version)

	for _, table := range stagingTableNames {
		var reg any
		require.NoError(t, db.DB.QueryRowContext(ctx,
			`SELECT to_regclass(?)`, quoted+"."+table).Scan(&reg), table)
		require.NotNil(t, reg, "expected %s.%s", quoted, table)
	}

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	require.True(t, columnExists(t, ctx, db, schema, "users", "_id"))
	require.True(t, columnExists(t, ctx, db, schema, "sys_users", "id"))
	require.False(t, columnExists(t, ctx, db, schema, "sys_users", "_tenant"))
	require.False(t, columnExists(t, ctx, db, schema, "sys_users", "_perms"))
	require.False(t, columnExists(t, ctx, db, schema, "sys_users", "_version"))
	require.False(t, columnExists(t, ctx, db, schema, "sys_users", "project_id"))
}

func TestApply_CutRenamesToFinalNames(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	quoted := testutil.CatalogQuoted(projectID)
	var version int64
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT MAX(version) FROM "+quoted+".schema_migrations").Scan(&version))
	require.Equal(t, int64(9), version)

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	require.True(t, columnExists(t, ctx, db, schema, "users", "id"))
	require.False(t, columnExists(t, ctx, db, schema, "users", "_id"))
	require.False(t, columnExists(t, ctx, db, schema, "users", "_version"))
	var sys any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regclass(?)`, quoted+".sys_users").Scan(&sys))
	require.Nil(t, sys)
}

func TestApply_CopyFailureDoesNotMarkDirtyOrAdvanceVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID:   "u1",
		Data: map[string]any{"email": "alice@example.com", "status": "active"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Document{
		ID: "s-orphan",
		Data: map[string]any{
			"user_id":     "missing-user",
			"secret_hash": "orphan-secret",
			"expire_at":   time.Now().UTC().Add(time.Hour),
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	err = projectschema.Apply(ctx, db, projectID)
	require.ErrorContains(t, err, "copy system documents")
	require.ErrorContains(t, err, "orphan session")

	quoted := testutil.CatalogQuoted(projectID)
	var version int64
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT MAX(version) FROM "+quoted+".schema_migrations").Scan(&version))
	require.Equal(t, int64(8), version)

	var applied9 bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+quoted+`.schema_migrations WHERE version = 9)`).Scan(&applied9))
	require.False(t, applied9)

	requireNotDirty(t, ctx, db, quoted)

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	require.True(t, columnExists(t, ctx, db, schema, "users", "_id"))
	require.True(t, columnExists(t, ctx, db, schema, "sys_users", "id"))
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "users"))
	var docUserID string
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT _id FROM `+quoted+`.users`).Scan(&docUserID))
	require.Equal(t, "u1", docUserID)
}

func TestApply_CopyThenCutMovesRowsToFinalNames(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID: "u-cut",
		Data: map[string]any{
			"email":         "cut@example.com",
			"password_hash": "hash",
			"name":          "Cut",
			"status":        "active",
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	require.NoError(t, projectschema.Apply(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	var version int64
	require.NoError(t, db.DB.QueryRowContext(ctx,
		"SELECT MAX(version) FROM "+quoted+".schema_migrations").Scan(&version))
	require.Equal(t, int64(9), version)

	schema, err := ident.ProjectSchemaName(projectID)
	require.NoError(t, err)
	require.True(t, columnExists(t, ctx, db, schema, "users", "id"))
	require.False(t, columnExists(t, ctx, db, schema, "users", "_id"))
	require.False(t, columnExists(t, ctx, db, schema, "users", "_version"))
	var sys any
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT to_regclass(?)`, quoted+".sys_users").Scan(&sys))
	require.Nil(t, sys)

	var id, email string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT id, email FROM `+quoted+`.users`).Scan(&id, &email))
	require.Equal(t, "u-cut", id)
	require.Equal(t, "cut@example.com", email)
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "users"))
}

func TestCopySystemDocuments_NoDocumentTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	require.NoError(t, projectschema.CopySystemDocuments(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	for _, table := range stagingTableNames {
		require.Zero(t, countRows(t, ctx, db, quoted, table), table)
	}
}

func TestCopySystemDocuments_UsersAndSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	const secret = "11111111-1111-1111-1111-111111111111"
	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID: "u1",
		Data: map[string]any{
			"email":         "alice@example.com",
			"password_hash": "hash",
			"name":          "Alice",
			"status":        "active",
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	expireAt := time.Now().UTC().Add(24 * time.Hour)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Document{
		ID: "s1",
		Data: map[string]any{
			"user_id":     "u1",
			"secret_hash": secret,
			"provider":    "email",
			"expire_at":   expireAt,
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	require.NoError(t, projectschema.CopySystemDocuments(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_users"))
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_sessions"))

	var id, email string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT id, email FROM `+quoted+`.sys_users`).Scan(&id, &email))
	require.Equal(t, "u1", id)
	require.Equal(t, "alice@example.com", email)

	var userID, gotSecret string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT user_id, secret_hash FROM `+quoted+`.sys_sessions WHERE id = 's1'`).Scan(&userID, &gotSecret))
	require.Equal(t, "u1", userID)
	require.Equal(t, secret, gotSecret)

	var fkHolds bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM `+quoted+`.sys_sessions s
			JOIN `+quoted+`.sys_users u ON u.id = s.user_id
			WHERE s.id = 's1'
		)`).Scan(&fkHolds))
	require.True(t, fkHolds)
}

func TestCopySystemDocuments_OrphanSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID:   "u1",
		Data: map[string]any{"email": "alice@example.com", "status": "active"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	expireAt := time.Now().UTC().Add(time.Hour)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Document{
		ID: "s-ok",
		Data: map[string]any{
			"user_id":     "u1",
			"secret_hash": "ok-secret",
			"expire_at":   expireAt,
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	require.NoError(t, projectschema.CopySystemDocuments(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_sessions"))

	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "sessions", databases.Document{
		ID: "s-orphan",
		Data: map[string]any{
			"user_id":     "missing-user",
			"secret_hash": "orphan-secret",
			"expire_at":   expireAt,
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	err = projectschema.CopySystemDocuments(ctx, db, projectID)
	require.ErrorContains(t, err, "orphan session")

	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_sessions"))
	var sid string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT id FROM `+quoted+`.sys_sessions`).Scan(&sid))
	require.Equal(t, "s-ok", sid)

	var dirty bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COALESCE(bool_or(dirty), false) FROM `+quoted+`.schema_migrations`).Scan(&dirty))
	require.False(t, dirty)
}

func TestCopySystemDocuments_MissingStaging(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	quoted := testutil.CatalogQuoted(projectID)
	_, err := db.DB.ExecContext(ctx, `DROP TABLE `+quoted+`.sys_users CASCADE`)
	require.NoError(t, err)

	err = projectschema.CopySystemDocuments(ctx, db, projectID)
	require.ErrorContains(t, err, "000008 not applied")
}

func TestCopySystemDocuments_InsertFailRollsBackStaging(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID:   "u-old",
		Data: map[string]any{"email": "old@example.com", "status": "active"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NoError(t, projectschema.CopySystemDocuments(ctx, db, projectID))

	quoted := testutil.CatalogQuoted(projectID)
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_users"))

	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID:   "u-bad",
		Data: map[string]any{"email": "bad@example.com", "status": "not-a-status"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	err = projectschema.CopySystemDocuments(ctx, db, projectID)
	require.ErrorContains(t, err, "insert sys_users")

	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_users"))
	var id, status string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT id, status FROM `+quoted+`.sys_users`).Scan(&id, &status))
	require.Equal(t, "u-old", id)
	require.Equal(t, "active", status)
	requireNotDirty(t, ctx, db, quoted)
}

func TestCopySystemDocuments_FileOwnerMissingUserSetNull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "buckets", databases.Document{
		ID:   "b1",
		Data: map[string]any{"name": "media", "public": false},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	ownerPrincipal := databases.Principal{Roles: []string{"user:deleted-user", "__system__"}}
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "files", databases.Document{
		ID: "f1",
		Data: map[string]any{
			"bucket_id": "b1",
			"name":      "a.txt",
			"mime_type": "text/plain",
			"size":      int64(1),
		},
	}, nil, ownerPrincipal)
	require.NoError(t, err)

	quoted := testutil.CatalogQuoted(projectID)
	var createdBy string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT _created_by FROM `+quoted+`.files WHERE _id = 'f1'`).Scan(&createdBy))
	require.Equal(t, "deleted-user", createdBy)

	require.NoError(t, projectschema.CopySystemDocuments(ctx, db, projectID))

	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_files"))
	var ownerNull bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT owner_user_id IS NULL FROM `+quoted+`.sys_files WHERE id = 'f1'`).Scan(&ownerNull))
	require.True(t, ownerNull)
}

func TestCopySystemDocuments_OrphanFileBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "buckets", databases.Document{
		ID:   "b1",
		Data: map[string]any{"name": "media"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "files", databases.Document{
		ID: "f-ok",
		Data: map[string]any{
			"bucket_id": "b1",
			"name":      "ok.txt",
			"size":      int64(1),
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	require.NoError(t, projectschema.CopySystemDocuments(ctx, db, projectID))
	quoted := testutil.CatalogQuoted(projectID)
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_files"))
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_buckets"))

	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "files", databases.Document{
		ID: "f-orphan",
		Data: map[string]any{
			"bucket_id": "missing-bucket",
			"name":      "orphan.txt",
			"size":      int64(1),
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	err = projectschema.CopySystemDocuments(ctx, db, projectID)
	require.ErrorContains(t, err, "orphan file bucket_id")

	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_files"))
	require.Equal(t, int64(1), countRows(t, ctx, db, quoted, "sys_buckets"))
	var fid string
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT id FROM `+quoted+`.sys_files`).Scan(&fid))
	require.Equal(t, "f-ok", fid)
}

func TestCopySystemDocuments_DuplicateMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProjectThrough(ctx, db, 8)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	_, err := docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.Document{
		ID:   "u1",
		Data: map[string]any{"email": "alice@example.com", "status": "active"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "groups", databases.Document{
		ID:   "g1",
		Data: map[string]any{"name": "Team"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "memberships", databases.Document{
		ID: "m1",
		Data: map[string]any{
			"group_id": "g1",
			"user_id":  "u1",
			"status":   "accepted",
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)
	_, err = docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "memberships", databases.Document{
		ID: "m2",
		Data: map[string]any{
			"group_id": "g1",
			"user_id":  "u1",
			"status":   "accepted",
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	err = projectschema.CopySystemDocuments(ctx, db, projectID)
	require.ErrorContains(t, err, "duplicate memberships")

	quoted := testutil.CatalogQuoted(projectID)
	require.Zero(t, countRows(t, ctx, db, quoted, "sys_memberships"))
	require.Zero(t, countRows(t, ctx, db, quoted, "sys_users"))
}

func countRows(t *testing.T, ctx context.Context, db *clients.Database, quoted, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted+"."+table).Scan(&n))
	return n
}

func columnExists(t *testing.T, ctx context.Context, db *clients.Database, schema, table, column string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?`,
		schema, table, column).Scan(&n))
	return n > 0
}

func requireNotDirty(t *testing.T, ctx context.Context, db *clients.Database, quoted string) {
	t.Helper()
	var dirty bool
	require.NoError(t, db.DB.QueryRowContext(ctx,
		`SELECT COALESCE(bool_or(dirty), false) FROM `+quoted+`.schema_migrations`).Scan(&dirty))
	require.False(t, dirty)
}
