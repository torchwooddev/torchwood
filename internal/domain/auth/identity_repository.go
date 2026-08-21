package auth

import (
	"context"
	"errors"
)

var (
	ErrIdentityAlreadyLinked = errors.New("identity already linked")
	ErrIdentityIDRequired    = errors.New("identity id is required")
)

// IdentityRepository 把第三方身份持久化到项目 schema。
type IdentityRepository interface {
	Insert(ctx context.Context, projectID string, identity *Identity) error
	GetByID(ctx context.Context, projectID, id string) (*Identity, error)
	GetByProviderUID(ctx context.Context, projectID, provider, uid string) (*Identity, error)
	ListByUser(ctx context.Context, projectID, userID string) ([]*Identity, error)
	Delete(ctx context.Context, projectID, id string) error
}
