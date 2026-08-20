package bunrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/secretbox"
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
	if err := projectschema.Apply(ctx, r.db, projectID); err != nil {
		return nil, err
	}
	sch, expr, err := ProjectTable(projectID, "project_oauth_providers", "pop")
	if err != nil {
		return nil, err
	}
	m := new(model.ProjectOAuthProvider)
	err = r.db.Conn(ctx).NewSelect().Model(m).ModelTableExpr(expr, sch).
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
	if err := projectschema.Apply(ctx, r.db, projectID); err != nil {
		return nil, err
	}
	sch, expr, err := ProjectTable(projectID, "project_oauth_providers", "pop")
	if err != nil {
		return nil, err
	}
	var rows []model.ProjectOAuthProvider
	err = r.db.Conn(ctx).NewSelect().Model(&rows).ModelTableExpr(expr, sch).
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
	if err := projectschema.Apply(ctx, r.db, cfg.ProjectID); err != nil {
		return err
	}
	sch, expr, err := ProjectTable(cfg.ProjectID, "project_oauth_providers", "pop")
	if err != nil {
		return err
	}
	_, err = r.db.Conn(ctx).NewInsert().Model(m).ModelTableExpr(expr, sch).
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
	if err := projectschema.Apply(ctx, r.db, projectID); err != nil {
		return err
	}
	sch, expr, err := ProjectTable(projectID, "project_oauth_providers", "pop")
	if err != nil {
		return err
	}
	_, err = r.db.Conn(ctx).NewDelete().Model((*model.ProjectOAuthProvider)(nil)).
		ModelTableExpr(expr, sch).
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
