package clients

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type Database struct {
	*bun.DB
}

type DataClients struct {
	DB  *Database
	RDB *redis.Client
}

func NewDataClients(cfg *config.AppConfig, logger *slog.Logger) (*DataClients, func(), error) {
	ctx := context.Background()
	db, closeDb, err := newDatabase(cfg.GetData().GetDatabase(), logger)
	if err != nil {
		return nil, nil, err
	}

	rdb := newRedis(cfg.GetData().GetRedis())
	if err := rdb.Ping(ctx).Err(); err != nil {
		closeDb()
		return nil, nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &DataClients{
			DB:  db,
			RDB: rdb,
		}, func() {
			closeDb()
			_ = rdb.Close()
		}, nil
}

func NewDatabase(dataClients *DataClients) *Database {
	return dataClients.DB
}

func NewRedis(dataClients *DataClients) *redis.Client {
	return dataClients.RDB
}

func newDatabase(cfg *config.Database, logger *slog.Logger) (*Database, func(), error) {
	source := strings.TrimSpace(cfg.GetSource())
	if source == "" {
		return nil, func() {}, fmt.Errorf("database source is empty: set data.database.source or TORCHWOOD_DATA_DATABASE_SOURCE")
	}
	u, err := url.Parse(source)
	if err != nil {
		return nil, func() {}, fmt.Errorf("invalid database source: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, func() {}, fmt.Errorf("invalid database scheme %q: expected postgres", u.Scheme)
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(source)))
	if pool := cfg.GetPool(); pool != nil {
		sqldb.SetMaxIdleConns(int(pool.GetMaxIdleConns()))
		sqldb.SetMaxOpenConns(int(pool.GetMaxOpenConns()))
		if d, err := time.ParseDuration(pool.GetConnMaxIdleTime()); err == nil {
			sqldb.SetConnMaxIdleTime(d)
		}
		if d, err := time.ParseDuration(pool.GetConnMaxLifetime()); err == nil {
			sqldb.SetConnMaxLifetime(d)
		}
	} else {
		maxOpen := 4 * runtime.GOMAXPROCS(0)
		sqldb.SetMaxOpenConns(maxOpen)
		sqldb.SetMaxIdleConns(maxOpen)
	}

	db := bun.NewDB(sqldb, pgdialect.New())
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, func() {}, fmt.Errorf("database ping failed: %w", err)
	}
	if hook := NewSlowQueryHook(cfg.GetSlowQueryThreshold(), cfg.GetDebug(), logger); hook != nil {
		db.AddQueryHook(hook)
	}
	return &Database{db}, func() { _ = db.Close() }, nil
}

func newRedis(cfg *config.Redis) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.GetAddr(),
		Password: cfg.GetPassword(),
		DB:       int(cfg.GetDb()),
	})
}
