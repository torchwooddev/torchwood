package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// API key scope 上限（B2）：每项 ≤64 字符、最多 32 项。
const (
	maxAPIKeyScopes      = 32
	maxAPIKeyScopeLength = 64
)

type APIKeys struct {
	repo projects.APIKeyRepository
}

func NewAPIKeys(repo projects.APIKeyRepository) *APIKeys {
	return &APIKeys{repo: repo}
}

type CreateAPIKeyCommand struct {
	ProjectID string
	Name      string
	Scopes    []string
	ExpireAt  *time.Time
}

func (a *APIKeys) Create(ctx context.Context, cmd CreateAPIKeyCommand) (*projects.APIKey, string, error) {
	if cmd.Name == "" {
		return nil, "", status.Error(codes.InvalidArgument, "name is required")
	}
	if len(cmd.Scopes) == 0 {
		return nil, "", status.Error(codes.InvalidArgument, "scopes is required (use \"*\" for all scopes)")
	}
	if len(cmd.Scopes) > maxAPIKeyScopes {
		return nil, "", status.Errorf(codes.InvalidArgument, "scopes exceeds maximum of %d", maxAPIKeyScopes)
	}
	for _, s := range cmd.Scopes {
		if len(s) > maxAPIKeyScopeLength || !interceptor.ValidAPIKeyScope(s) {
			return nil, "", status.Errorf(codes.InvalidArgument, "invalid scope %q (allowed: * | all | <resource> | <resource>.read | <resource>.write)", s)
		}
	}
	id := idgen.UUID().String()
	secret := idgen.UUID().String() + idgen.UUID().String()
	hash := sha256.Sum256([]byte(secret))
	key := &projects.APIKey{
		ID:         id,
		ProjectID:  cmd.ProjectID,
		Name:       cmd.Name,
		SecretHash: hex.EncodeToString(hash[:]),
		Scopes:     cmd.Scopes,
		ExpireAt:   cmd.ExpireAt,
		Enabled:    true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := a.repo.CreateAPIKey(ctx, key); err != nil {
		return nil, "", fmt.Errorf("create api key: %w", err)
	}
	return key, secret, nil
}

func (a *APIKeys) List(ctx context.Context, projectID string) ([]projects.APIKey, error) {
	return a.repo.ListAPIKeys(ctx, projectID)
}

func (a *APIKeys) Get(ctx context.Context, projectID, id string) (*projects.APIKey, error) {
	key, err := a.repo.GetAPIKey(ctx, id)
	if err != nil {
		return nil, err
	}
	if key != nil && key.ProjectID != projectID {
		return nil, nil
	}
	return key, nil
}

func (a *APIKeys) Delete(ctx context.Context, projectID, id string) error {
	key, err := a.repo.GetAPIKey(ctx, id)
	if err != nil {
		return err
	}
	if key == nil || key.ProjectID != projectID {
		return status.Error(codes.NotFound, "api key not found")
	}
	return a.repo.DeleteAPIKey(ctx, id)
}
