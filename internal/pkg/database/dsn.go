package database

import (
	"fmt"
	"os"
)

// SourceFromEnv resolves the Postgres DSN from environment variables.
// It prefers TORCHWOOD_DATA_DATABASE_SOURCE and falls back to POSTGRES_* compose vars.
// 回退 DSN 默认 sslmode=require：不显式声明 TLS 时拒绝明文传输。
func SourceFromEnv() string {
	if dsn := os.Getenv("TORCHWOOD_DATA_DATABASE_SOURCE"); dsn != "" {
		return dsn
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=require",
		envOr("POSTGRES_USER", "torchwood"),
		envOr("POSTGRES_PASSWORD", "torchwood"),
		envOr("POSTGRES_HOST", "127.0.0.1"),
		envOr("POSTGRES_PORT", "5432"),
		envOr("POSTGRES_DB", "torchwood"),
	)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
