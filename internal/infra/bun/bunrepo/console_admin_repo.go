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

func (r *consoleAdminRepo) ListConsoleAdmins(ctx context.Context) ([]projects.ConsoleAdmin, error) {
	var ms []model.ConsoleAdmin
	if err := r.db.NewSelect().Model(&ms).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]projects.ConsoleAdmin, len(ms))
	for i := range ms {
		out[i] = projects.ConsoleAdmin{
			ID:           ms[i].ID,
			Email:        ms[i].Email,
			PasswordHash: ms[i].PasswordHash,
			Role:         ms[i].Role,
			CreatedAt:    ms[i].CreatedAt,
			UpdatedAt:    ms[i].UpdatedAt,
		}
	}
	return out, nil
}

func (r *consoleAdminRepo) CreateConsoleAdmin(ctx context.Context, admin *projects.ConsoleAdmin) error {
	m := &model.ConsoleAdmin{
		ID:           admin.ID,
		Email:        admin.Email,
		PasswordHash: admin.PasswordHash,
		Role:         admin.Role,
		CreatedAt:    admin.CreatedAt,
		UpdatedAt:    admin.UpdatedAt,
	}
	_, err := r.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *consoleAdminRepo) UpdateConsoleAdmin(ctx context.Context, admin *projects.ConsoleAdmin) error {
	m := &model.ConsoleAdmin{
		ID:           admin.ID,
		Email:        admin.Email,
		PasswordHash: admin.PasswordHash,
		Role:         admin.Role,
		UpdatedAt:    admin.UpdatedAt,
	}
	_, err := r.db.NewUpdate().Model(m).Column("password_hash", "role", "updated_at").Where("id = ?", admin.ID).Exec(ctx)
	return err
}

func (r *consoleAdminRepo) DeleteConsoleAdmin(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*model.ConsoleAdmin)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *consoleAdminRepo) CountConsoleAdminsByRole(ctx context.Context, role string) (int64, error) {
	count, err := r.db.NewSelect().Model((*model.ConsoleAdmin)(nil)).Where("role = ?", role).Count(ctx)
	return int64(count), err
}
