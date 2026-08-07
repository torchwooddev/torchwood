package bunrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

type consoleAdminProjectRepo struct {
	db *clients.Database
}

func NewConsoleAdminProjectRepository(db *clients.Database) projects.ConsoleAdminProjectRepository {
	return &consoleAdminProjectRepo{db: db}
}

func (r *consoleAdminProjectRepo) HasProjectAccess(ctx context.Context, adminID, projectID string) (bool, error) {
	return r.db.NewSelect().Model((*model.ConsoleAdminProject)(nil)).
		Where("admin_id = ? AND project_id = ?", adminID, projectID).
		Exists(ctx)
}

func (r *consoleAdminProjectRepo) GrantProjectAccess(ctx context.Context, adminID, projectID string) error {
	m := &model.ConsoleAdminProject{
		AdminID:   adminID,
		ProjectID: projectID,
		CreatedAt: time.Now(),
	}
	_, err := r.db.NewInsert().Model(m).
		On("CONFLICT (admin_id, project_id) DO NOTHING").
		Exec(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}
