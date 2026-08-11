package bunrepo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

type adminRepo struct {
	db *clients.Database
}

func NewAdminRepository(db *clients.Database) projects.AdminRepository {
	return &adminRepo{db: db}
}

func (r *adminRepo) GetAdmin(ctx context.Context, id string) (*projects.Admin, error) {
	m := new(model.Admin)
	err := r.db.NewSelect().Model(m).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &projects.Admin{
		ID:           m.ID,
		Email:        m.Email,
		PasswordHash: m.PasswordHash,
		Role:         m.Role,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}

func (r *adminRepo) GetAdminByEmail(ctx context.Context, email string) (*projects.Admin, error) {
	m := new(model.Admin)
	err := r.db.NewSelect().Model(m).Where("email = ?", email).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &projects.Admin{
		ID:           m.ID,
		Email:        m.Email,
		PasswordHash: m.PasswordHash,
		Role:         m.Role,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}

func (r *adminRepo) ListAdmins(ctx context.Context) ([]projects.Admin, error) {
	var ms []model.Admin
	if err := r.db.NewSelect().Model(&ms).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]projects.Admin, len(ms))
	for i := range ms {
		out[i] = projects.Admin{
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

func (r *adminRepo) CreateAdmin(ctx context.Context, admin *projects.Admin) error {
	m := &model.Admin{
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

func (r *adminRepo) UpdateAdmin(ctx context.Context, admin *projects.Admin) error {
	m := &model.Admin{
		ID:           admin.ID,
		Email:        admin.Email,
		PasswordHash: admin.PasswordHash,
		Role:         admin.Role,
		UpdatedAt:    admin.UpdatedAt,
	}
	_, err := r.db.NewUpdate().Model(m).Column("password_hash", "role", "updated_at").Where("id = ?", admin.ID).Exec(ctx)
	return err
}

func (r *adminRepo) DeleteAdmin(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*model.Admin)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *adminRepo) CountAdminsByRole(ctx context.Context, role string) (int64, error) {
	count, err := r.db.NewSelect().Model((*model.Admin)(nil)).Where("role = ?", role).Count(ctx)
	return int64(count), err
}
