package testutil

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// TestMigrations_UpDownUpCycle（J6-2 / 审计 P1 #17）：testutil.SetupTestDB 的
// runMigrations 只执行 *.up.sql，22 组 down 迁移全仓零验证。本测试以
// golang-migrate 库代码驱动，在独立临时库上跑完整 up→down(全部)→up 循环，
// 并断言 down 全量后 public schema 无业务表残留——任何 down SQL 损坏在此红。
//
// 环境快失败模式与 SetupTestDB 一致：TORCHWOOD_TEST_* 未设置时跳过
// （CI backend job 与本地 `task up` + .env 均提供）。
func TestMigrations_UpDownUpCycle(t *testing.T) {
	adminDSN := AdminDSN()
	baseDSN := TestDSN()
	if adminDSN == "" || baseDSN == "" {
		t.Skip("TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE / TORCHWOOD_TEST_DATABASE_SOURCE not set; skipping migration cycle test")
	}

	wantLatest := latestMigrationVersion(t)
	sqldb := newMigrationTestDB(t, adminDSN, baseDSN)

	srcDrv, err := iofs.New(os.DirFS(migrationsDir(t)), ".")
	require.NoError(t, err)
	dbDrv, err := migratepg.WithInstance(sqldb, &migratepg.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithInstance("iofs", srcDrv, "postgres", dbDrv)
	require.NoError(t, err)

	// 1) up 全量。
	require.NoError(t, m.Up())
	version, dirty, err := m.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.Equal(t, wantLatest, version, "up 后应停在最新迁移")

	// 2) down 全量：逐级退到空。
	require.NoError(t, m.Down())
	version, dirty, err = m.Version()
	require.ErrorIs(t, err, migrate.ErrNilVersion)
	require.False(t, dirty)
	require.Zero(t, version)
	require.Empty(t, publicTablesExcluding(t, sqldb, "schema_migrations"),
		"down 全量后 public schema 不应残留业务表")

	// 3) 再 up 一轮：验证 down 之后的库可完整重建。
	require.NoError(t, m.Up())
	version, dirty, err = m.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.Equal(t, wantLatest, version, "down→up 循环后应回到最新迁移")
}

// latestMigrationVersion 从 db/migrations 目录枚举 *.up.sql 的最大版本号，
// 使断言随新增迁移自动收紧而非硬编码 22。
func latestMigrationVersion(t *testing.T) uint {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(migrationsDir(t), "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, matches, "no up migrations found under db/migrations")
	sort.Strings(matches)
	last := filepath.Base(matches[len(matches)-1])
	version, err := strconv.ParseUint(strings.SplitN(last, "_", 2)[0], 10, 32)
	require.NoError(t, err)
	return uint(version)
}

// migrationsDir 返回仓库内 db/migrations 绝对路径。
func migrationsDir(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	require.NoError(t, err)
	return filepath.Join(root, "db", "migrations")
}

// newMigrationTestDB 创建独立临时库并返回裸 *sql.DB；cleanup 负责删库与断连。
func newMigrationTestDB(t *testing.T, adminDSN, baseDSN string) *sql.DB {
	t.Helper()
	dbName := uniqueTestDBName()
	testDSN, err := replaceDatabaseName(baseDSN, dbName)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminDB := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(adminDSN))), pgdialect.New())
	defer func() { _ = adminDB.Close() }()
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create migration test db: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupDB := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(adminDSN))), pgdialect.New())
		defer func() { _ = cleanupDB.Close() }()
		if err := dropTestDatabase(cleanupCtx, cleanupDB, dbName); err != nil {
			t.Errorf("drop test db %s: %v", dbName, err)
		}
	})

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(testDSN)))
	t.Cleanup(func() { _ = sqldb.Close() })
	return sqldb
}

// publicTablesExcluding 列出 public schema 下除 exclude 外的全部表名。
func publicTablesExcluding(t *testing.T, db *sql.DB, exclude ...string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public'
	`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	excluded := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		excluded[e] = true
	}
	var out []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		if !excluded[name] {
			out = append(out, name)
		}
	}
	require.NoError(t, rows.Err())
	return out
}
