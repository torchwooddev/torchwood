package bunrepo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

type consoleAdminRepo struct {
	db *clients.Database
}

func NewConsoleAdminRepository(db *clients.Database) projects.ConsoleAdminRepository {
	return &consoleAdminRepo{db: db}
}

func (r *consoleAdminRepo) GetConsoleAdmin(ctx context.Context, id string) (*projects.ConsoleAdmin, error) {
	m := new(model.ConsoleAdmin)
	err := r.db.NewSelect().Model(m).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &projects.ConsoleAdmin{
		ID:           m.ID,
		Email:        m.Email,
		PasswordHash: m.PasswordHash,
		Role:         m.Role,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}

func (r *consoleAdminRepo) GetConsoleAdminByEmail(ctx context.Context, email string) (*projects.ConsoleAdmin, error) {
	m := new(model.ConsoleAdmin)
	err := r.db.NewSelect().Model(m).Where("email = ?", email).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &projects.ConsoleAdmin{
		ID:           m.ID,
		Email:        m.Email,
		PasswordHash: m.PasswordHash,
		Role:         m.Role,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}
