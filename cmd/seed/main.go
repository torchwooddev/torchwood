package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/torchwoodio/torchwood/internal/infra/bun/model"
	"github.com/torchwoodio/torchwood/internal/pkg/database"
	"github.com/torchwoodio/torchwood/pkg/idgen"
	"github.com/torchwoodio/torchwood/pkg/password"
	"github.com/joho/godotenv"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func main() {
	_ = godotenv.Load()

	dsn := database.SourceFromEnv()
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	ctx := context.Background()

	// Default project
	project := &model.Project{
		ID:          "default",
		Name:        "Default Project",
		Description: "Auto-created seed project",
		Status:      "active",
		Settings:    map[string]any{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if _, err := db.NewInsert().Model(project).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
		fmt.Println("insert project:", err)
		os.Exit(1)
	}

	// Console admin
	hash, err := password.Hash("Admin@123")
	if err != nil {
		fmt.Println("hash password:", err)
		os.Exit(1)
	}
	admin := &model.ConsoleAdmin{
		ID:           idgen.UUID().String(),
		Email:        "admin@torchwood.local",
		PasswordHash: hash,
		Role:         "owner",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if _, err := db.NewInsert().Model(admin).On("CONFLICT (email) DO NOTHING").Exec(ctx); err != nil {
		fmt.Println("insert admin:", err)
		os.Exit(1)
	}

	// Default API key for the default project. Uses a deterministic id so that
	// re-running seed does not create duplicate keys; the secret is only printed
	// on first creation. To rotate, delete the row and re-run seed.
	apiKeyID := "default-default-api-key"
	exists, err := db.NewSelect().Model((*model.APIKey)(nil)).
		Where("id = ?", apiKeyID).Where("project_id = ?", "default").Exists(ctx)
	if err != nil {
		fmt.Println("check api key:", err)
		os.Exit(1)
	}
	if exists {
		fmt.Println("seeded project=default admin=admin@torchwood.local api_key=(already exists, id=" + apiKeyID + ")")
		return
	}

	apiSecret := "Torchwood-default-api-key-" + idgen.UUID().String()
	apiHash := sha256.Sum256([]byte(apiSecret))
	apiKey := &model.APIKey{
		ID:         apiKeyID,
		ProjectID:  "default",
		Name:       "Default API Key",
		SecretHash: hex.EncodeToString(apiHash[:]),
		Scopes:     []string{"projects.read", "users.read", "users.write", "storage.read", "storage.write", "databases.read", "databases.write", "teams.read", "teams.write"},
		Enabled:    true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if _, err := db.NewInsert().Model(apiKey).Exec(ctx); err != nil {
		fmt.Println("insert api key:", err)
		os.Exit(1)
	}

	fmt.Println("seeded project=default admin=admin@torchwood.local api_key=" + apiSecret)
}
