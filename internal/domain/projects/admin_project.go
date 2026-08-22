package projects

import "context"

type AdminProjectRepository interface {
	HasProjectAccess(ctx context.Context, adminID, projectID string) (bool, error)
	GrantProjectAccess(ctx context.Context, adminID, projectID string) error
	ListProjectIDs(ctx context.Context, adminID string) ([]string, error)
}
