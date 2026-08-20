package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/pkg/ident"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

var testDBSeq atomic.Uint64

// TestDSN returns the DSN for integration tests, read from the
// TORCHWOOD_TEST_DATABASE_SOURCE environment variable. There is no
// hard-coded fallback: ports are owned by the local compose/.env setup,
// so a missing variable fails fast in SetupTestDB.
func TestDSN() string {
	return os.Getenv("TORCHWOOD_TEST_DATABASE_SOURCE")
}

// AdminDSN returns a DSN to the postgres maintenance database, read from the
// TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE environment variable.
func AdminDSN() string {
	return os.Getenv("TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE")
}

// SetupTestDB creates a fresh test database, runs migrations, and returns a bun DB client.
func SetupTestDB(t *testing.T) *clients.Database {
	t.Helper()
	adminDSN := AdminDSN()
	baseDSN := TestDSN()
	if adminDSN == "" {
		t.Fatal("TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE is not set (run via `task test`, which loads .env, or export it manually)")
	}
	if baseDSN == "" {
		t.Fatal("TORCHWOOD_TEST_DATABASE_SOURCE is not set (run via `task test`, which loads .env, or export it manually)")
	}
	dbName := uniqueTestDBName()

	testDSN, err := replaceDatabaseName(baseDSN, dbName)
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}

	adminDB := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(adminDSN))), pgdialect.New())
	defer adminDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupDB := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(adminDSN))), pgdialect.New())
		defer cleanupDB.Close()
		if err := dropTestDatabase(cleanupCtx, cleanupDB, dbName); err != nil {
			t.Errorf("drop test db %s: %v", dbName, err)
		}
	})

	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(testDSN),
		// 默认写缓冲 4KB 放不下单个 >4KB 的参数（如 outbox 截断测试的
		// 300KiB 文档 JSON）；测试库统一放宽到 2MiB。
		pgdriver.WithBufferSize(2<<20),
	))
	db := &clients.Database{DB: bun.NewDB(sqldb, pgdialect.New())}
	t.Cleanup(func() { _ = db.Close() })

	if err := runMigrations(ctx, db.DB); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return db
}

func uniqueTestDBName() string {
	// Postgres folds unquoted identifiers to lowercase; keep the generated
	// name lowercase so the DSN database name matches the created database.
	return strings.ToLower(fmt.Sprintf("%s_%d_%d", testDBPrefix(), os.Getpid(), testDBSeq.Add(1)))
}

func testDBPrefix() string {
	dsn := TestDSN()
	u, err := url.Parse(dsn)
	if err != nil || u.Path == "" || u.Path == "/" {
		return "TORCHWOOD_test"
	}
	return strings.TrimPrefix(u.Path, "/")
}

func replaceDatabaseName(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

func dropTestDatabase(ctx context.Context, admin *bun.DB, dbName string) error {
	if _, err := admin.ExecContext(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = ? AND pid <> pg_backend_pid()
	`, dbName); err != nil {
		return err
	}
	_, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+dbName)
	return err
}

func runMigrations(ctx context.Context, db *bun.DB) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(root, "db", "migrations", "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		sqlBytes, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("execute migration %s: %w", f, err)
		}
	}
	return nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate testutil package")
	}
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
}

// CreateTestProject inserts a test project and returns its public id, internal id, and cleanup func.
func CreateTestProject(ctx context.Context, db *clients.Database) (string, int64, func()) {
	project := &model.Project{
		ID:        fmt.Sprintf("t%x", time.Now().UnixNano()),
		Name:      fmt.Sprintf("Test Project %d", time.Now().UnixNano()),
		Status:    "active",
		Settings:  map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if _, err := db.NewInsert().Model(project).Exec(ctx); err != nil {
		panic(err)
	}
	var internalID int64
	if err := db.NewSelect().Model((*model.Project)(nil)).Column("internal_id").Where("id = ?", project.ID).Scan(ctx, &internalID); err != nil {
		panic(err)
	}
	if err := projectschema.Apply(ctx, db, project.ID); err != nil {
		panic(err)
	}
	cleanup := func() {
		_, _ = db.NewDelete().Model((*model.Project)(nil)).Where("id = ?", project.ID).Exec(ctx)
	}
	return project.ID, internalID, cleanup
}

// CatalogIdent 返回项目数据面 schema 的 bun.Ident，供测试 ModelTableExpr。
func CatalogIdent(projectID string) bun.Ident {
	s, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		panic(err)
	}
	return bun.Ident(s)
}

// CatalogQuoted 返回 quoteIdent 后的项目 schema 名，供测试拼接 Raw SQL。
func CatalogQuoted(projectID string) string {
	s, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		panic(err)
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// InsertCatalogDatabase 向项目 schema 的 document_databases 插入一行。
func InsertCatalogDatabase(ctx context.Context, db *clients.Database, projectID, id, name string) {
	now := time.Now()
	if _, err := db.NewInsert().Model(&model.DocumentDatabase{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}).ModelTableExpr("?.document_databases AS ddb", CatalogIdent(projectID)).Exec(ctx); err != nil {
		panic(err)
	}
}
