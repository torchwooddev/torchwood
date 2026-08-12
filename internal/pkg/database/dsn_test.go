package database

import (
	"testing"
)

func TestSourceFromEnv(t *testing.T) {
	t.Setenv("TORCHWOOD_DATA_DATABASE_SOURCE", "")
	t.Setenv("POSTGRES_USER", "user")
	t.Setenv("POSTGRES_PASSWORD", "pass")
	t.Setenv("POSTGRES_HOST", "db.local")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_DB", "app")

	got := SourceFromEnv()
	want := "postgres://user:pass@db.local:5433/app?sslmode=require"
	if got != want {
		t.Fatalf("SourceFromEnv() = %q, want %q", got, want)
	}
}

func TestSourceFromEnvPrefersFleetDSN(t *testing.T) {
	t.Setenv("TORCHWOOD_DATA_DATABASE_SOURCE", "postgres://torchwood:torchwood@127.0.0.1 :5433/torchwood?sslmode=disable")
	t.Setenv("POSTGRES_PORT", "9999")

	got := SourceFromEnv()
	want := "postgres://torchwood:torchwood@127.0.0.1 :5433/torchwood?sslmode=disable"
	if got != want {
		t.Fatalf("SourceFromEnv() = %q, want %q", got, want)
	}
}
