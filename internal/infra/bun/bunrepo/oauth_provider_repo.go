package bunrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/torchwoodio/torchwood/internal/domain/projects"
	"github.com/torchwoodio/torchwood/internal/infra/bun/model"
	"github.com/torchwoodio/torchwood/internal/infra/clients"
	"github.com/torchwoodio/torchwood/internal/pkg/config"
	"github.com/torchwoodio/torchwood/pkg/secretbox"
)

type oauthProviderRepo struct {
	db            *clients.Database
	encryptionKey string
}

func NewOAuthProviderRepository(db *clients.Database, cfg *config.AppConfig) projects.OAuthProviderRepository {
	key := ""
	if cfg != nil && cfg.GetSecurity() != nil && cfg.GetSecurity().GetJwt() != nil {
		key = cfg.GetSecurity().GetJwt().GetSecret()
	}
	return &oauthProviderRepo{db: db, encryptionKey: key}
}

func (r *oauthProviderRepo) GetOAuthProvider(ctx context.Context, projectID, provider string) (*projects.OAuthProvider, error) {
	m := new(model.ProjectOAuthProvider)
	err := r.db.NewSelect().Model(m).
		Where("project_id = ? AND provider = ?", projectID, provider).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapOAuthProvider(m, r.encryptionKey)
}

func (r *oauthProviderRepo) ListOAuthProviders(ctx context.Context, projectID string) ([]projects.OAuthProvider, error) {
	var rows []model.ProjectOAuthProvider
	err := r.db.NewSelect().Model(&rows).
		Where("project_id = ?", projectID).
		Order("provider ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]projects.OAuthProvider, len(rows))
	for i := range rows {
		mapped, err := mapOAuthProvider(&rows[i], r.encryptionKey)
		if err != nil {
			return nil, err
		}
		out[i] = *mapped
	}
	return out, nil
}

func (r *oauthProviderRepo) UpsertOAuthProvider(ctx context.Context, cfg *projects.OAuthProvider) error {
	if cfg == nil {
		return errors.New("oauth provider is nil")
	}
	secret := cfg.ClientSecret
	if secret != "" {
		enc, err := secretbox.Encrypt(secret, r.encryptionKey)
		if err != nil {
			return err
		}
		secret = enc
	}
	now := time.Now()
	m := &model.ProjectOAuthProvider{
		ProjectID:    cfg.ProjectID,
		Provider:     cfg.Provider,
		Enabled:      cfg.Enabled,
		ClientID:     cfg.ClientID,
		ClientSecret: secret,
		Scopes:       append([]string(nil), cfg.Scopes...),
		UpdatedAt:    now,
	}
	_, err := r.db.NewInsert().Model(m).
		On("CONFLICT (project_id, provider) DO UPDATE").
		Set("enabled = EXCLUDED.enabled").
		Set("client_id = EXCLUDED.client_id").
		Set("client_secret = EXCLUDED.client_secret").
		Set("scopes = EXCLUDED.scopes").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

func (r *oauthProviderRepo) DeleteOAuthProvider(ctx context.Context, projectID, provider string) error {
	_, err := r.db.NewDelete().Model((*model.ProjectOAuthProvider)(nil)).
		Where("project_id = ? AND provider = ?", projectID, provider).
		Exec(ctx)
	return err
}

func mapOAuthProvider(m *model.ProjectOAuthProvider, encryptionKey string) (*projects.OAuthProvider, error) {
	if m == nil {
		return nil, nil
	}
	secret, err := secretbox.Decrypt(m.ClientSecret, encryptionKey)
	if err != nil {
		return nil, err
	}
	return &projects.OAuthProvider{
		ProjectID:    m.ProjectID,
		Provider:     m.Provider,
		Enabled:      m.Enabled,
		ClientID:     m.ClientID,
		ClientSecret: secret,
		Scopes:       append([]string(nil), m.Scopes...),
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}
