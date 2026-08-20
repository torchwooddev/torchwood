package projectschema

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

// Apply 在调用方已打开的 tx（或自行 RunInTx）上执行待应用的项目 DDL。
// SQL 文件替换 {{schema}} 后零查询参数，走 simple protocol 多语句 Exec。
func Apply(ctx context.Context, db *clients.Database, projectID string) error {
	if err := ident.ValidateSchemaResourceID(projectID); err != nil {
		return err
	}
	schema, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		return err
	}
	quoted := quoteIdent(schema)
	return db.RunInTx(ctx, func(txCtx context.Context) error {
		if _, err := db.Conn(txCtx).ExecContext(txCtx,
			`SELECT pg_advisory_xact_lock(hashtext('tw_schema'), hashtext(?))`, projectID); err != nil {
			return fmt.Errorf("project schema lock: %w", err)
		}
		if _, err := db.Conn(txCtx).ExecContext(txCtx,
			fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoted)); err != nil {
			return fmt.Errorf("create project schema: %w", err)
		}
		if _, err := db.Conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s.schema_migrations (
    version BIGINT PRIMARY KEY,
    dirty BOOLEAN NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, quoted)); err != nil {
			return fmt.Errorf("ensure schema_migrations: %w", err)
		}

		var dirty bool
		err := db.Conn(txCtx).QueryRowContext(txCtx,
			fmt.Sprintf(`SELECT COALESCE(bool_or(dirty), false) FROM %s.schema_migrations`, quoted),
		).Scan(&dirty)
		if err != nil {
			return fmt.Errorf("read dirty flag: %w", err)
		}
		if dirty {
			return fmt.Errorf("project schema %s is dirty", schema)
		}

		var applied int64
		if err := db.Conn(txCtx).QueryRowContext(txCtx,
			fmt.Sprintf(`SELECT COALESCE(MAX(version), 0) FROM %s.schema_migrations`, quoted),
		).Scan(&applied); err != nil {
			return fmt.Errorf("read schema version: %w", err)
		}

		files, err := listMigrations()
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.version <= applied {
				continue
			}
			body := strings.ReplaceAll(f.sql, "{{schema}}", quoted)
			if _, err := db.Conn(txCtx).ExecContext(txCtx, body); err != nil {
				_, _ = db.Conn(txCtx).ExecContext(txCtx,
					fmt.Sprintf(`INSERT INTO %s.schema_migrations (version, dirty) VALUES (%d, true)
ON CONFLICT (version) DO UPDATE SET dirty = true`, quoted, f.version))
				return fmt.Errorf("apply %s: %w", f.name, err)
			}
			if _, err := db.Conn(txCtx).ExecContext(txCtx,
				fmt.Sprintf(`INSERT INTO %s.schema_migrations (version, dirty) VALUES (%d, false)`, quoted, f.version)); err != nil {
				return fmt.Errorf("record %s: %w", f.name, err)
			}
		}
		return nil
	})
}

// EnsureAll 对每个项目 Apply；最多 4 路并行。脏项目记入返回 error。
func EnsureAll(ctx context.Context, db *clients.Database, projectIDs []string) error {
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	errCh := make(chan error, len(projectIDs))
	for _, id := range projectIDs {
		id := id
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := Apply(ctx, db, id); err != nil {
				errCh <- fmt.Errorf("project %s: %w", id, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	var errs []string
	for err := range errCh {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("ensure project schemas: %s", strings.Join(errs, "; "))
	}
	return nil
}

type migrationFile struct {
	name    string
	version int64
	sql     string
}

func listMigrations() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]migrationFile, 0, len(names))
	for _, name := range names {
		n := strings.TrimSuffix(name, ".up.sql")
		parts := strings.SplitN(n, "_", 2)
		ver, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration name %s: %w", name, err)
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migrationFile{name: name, version: ver, sql: string(body)})
	}
	return out, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
