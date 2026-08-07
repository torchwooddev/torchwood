package bunrepo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
)

type apiKeyRepo struct {
	db *clients.Database
}

func NewAPIKeyRepository(db *clients.Database) projects.APIKeyRepository {
	return &apiKeyRepo{db: db}
}

func (r *apiKeyRepo) CreateAPIKey(ctx context.Context, key *projects.APIKey) error {
	m := mapAPIKeyToModel(key)
	_, err := r.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *apiKeyRepo) GetAPIKey(ctx context.Context, id string) (*projects.APIKey, error) {
	m := new(model.APIKey)
	err := r.db.NewSelect().Model(m).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapAPIKeyToDomain(m), nil
}

func (r *apiKeyRepo) GetAPIKeyBySecretHash(ctx context.Context, hash string) (*projects.APIKey, error) {
	m := new(model.APIKey)
	err := r.db.NewSelect().Model(m).Where("secret_hash = ?", hash).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapAPIKeyToDomain(m), nil
}

func (r *apiKeyRepo) ListAPIKeys(ctx context.Context, projectID string) ([]projects.APIKey, error) {
	var ms []model.APIKey
	err := r.db.NewSelect().Model(&ms).Where("project_id = ?", projectID).Order("created_at DESC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]projects.APIKey, len(ms))
	for i := range ms {
		out[i] = *mapAPIKeyToDomain(&ms[i])
	}
	return out, nil
}

func (r *apiKeyRepo) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*model.APIKey)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func mapAPIKeyToModel(k *projects.APIKey) *model.APIKey {
	return &model.APIKey{
		ID:         k.ID,
		ProjectID:  k.ProjectID,
		Name:       k.Name,
		SecretHash: k.SecretHash,
		Scopes:     k.Scopes,
		ExpireAt:   k.ExpireAt,
		Enabled:    k.Enabled,
		CreatedAt:  k.CreatedAt,
		UpdatedAt:  k.UpdatedAt,
	}
}

func mapAPIKeyToDomain(m *model.APIKey) *projects.APIKey {
	return &projects.APIKey{
		ID:        m.ID,
		ProjectID: m.ProjectID,
		Name:      m.Name,
		Scopes:    m.Scopes,
		ExpireAt:  m.ExpireAt,
		Enabled:   m.Enabled,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}
