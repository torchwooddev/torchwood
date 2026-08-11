package projects

import "context"

type AdminProjectRepository interface {
	HasProjectAccess(ctx context.Context, adminID, projectID string) (bool, error)
	GrantProjectAccess(ctx context.Context, adminID, projectID string) error
}
