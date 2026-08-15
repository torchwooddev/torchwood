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
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = rdb.Ping(pingCtx).Err()
	cancel()
	if err != nil {
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

	// WithBufferSize(2<<20)：pgdriver 默认写缓冲仅 4KB。PR2 起每次用户集合
	// 写都会把整份 outbox 信封 JSON 作为单个 JSONB 参数插入
	// document_events_outbox（256KiB 上限 + 余量），默认缓冲下 >4KB 参数报
	// bufio: buffer full，整段 RunInTx 回滚、文档写失败。测试库已在
	// testutil.SetupTestDB 对齐同值。
	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(source),
		pgdriver.WithBufferSize(2<<20),
	))
	if pool := cfg.GetPool(); pool != nil {
		maxOpen, maxIdle := normalizePoolSizes(int(pool.GetMaxOpenConns()), int(pool.GetMaxIdleConns()), logger)
		sqldb.SetMaxOpenConns(maxOpen)
		sqldb.SetMaxIdleConns(maxIdle)
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
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	pingErr := db.PingContext(pingCtx)
	cancel()
	if pingErr != nil {
		_ = db.Close()
		return nil, func() {}, fmt.Errorf("database ping failed: %w", pingErr)
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

// normalizePoolSizes 将 pool 配置的 ≤0 值落为安全默认并 Warn：
//   - max_open ≤ 0 时取 4*GOMAXPROCS——database/sql 的 0 表示无上限，
//     长期运行下会积累远超实际需要的连接数，属于零值陷阱；
//   - max_idle ≤ 0 时取 max_open——database/sql 的 0 表示不保留空闲连接，
//     每次请求都重新建连，短查询场景性能劣化明显。
func normalizePoolSizes(maxOpen, maxIdle int, logger *slog.Logger) (int, int) {
	if logger == nil {
		logger = slog.Default()
	}
	if maxOpen <= 0 {
		logger.Warn("data.database.pool.max_open_conns <= 0, falling back to safe default",
			slog.Int("configured", maxOpen), slog.Int("effective", 4*runtime.GOMAXPROCS(0)))
		maxOpen = 4 * runtime.GOMAXPROCS(0)
	}
	if maxIdle <= 0 {
		logger.Warn("data.database.pool.max_idle_conns <= 0, falling back to safe default",
			slog.Int("configured", maxIdle), slog.Int("effective", maxOpen))
		maxIdle = maxOpen
	}
	return maxOpen, maxIdle
}
