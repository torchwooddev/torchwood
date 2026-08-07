package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Users struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
}

func NewUsers(projectRepo projects.Repository, docDB databases.DocumentDB) *Users {
	return &Users{projectRepo: projectRepo, docDB: docDB}
}

func (u *Users) resolveProject(ctx context.Context, projectID string) (*projects.Project, error) {
	p, err := u.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := u.docDB.EnsureSystemCollections(ctx, p.ID, p.InternalID); err != nil {
		return nil, err
	}
	return p, nil
}

func (u *Users) ListUsers(ctx context.Context, projectID string, q databases.Query, principal databases.Principal) ([]databases.Document, int64, string, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, 0, "", err
	}
	list, err := u.docDB.ListDocuments(ctx, projectID, "default", "users", q, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (u *Users) GetUser(ctx context.Context, projectID, userID string, principal databases.Principal) (*databases.Document, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return u.docDB.GetDocument(ctx, projectID, "default", "users", userID, principal)
}

var userUpdateProtectedFields = map[string]struct{}{
	"password_hash":  {},
	"email_verified": {},
	"status":         {},
}

func (u *Users) UpdateUser(ctx context.Context, projectID, userID string, updates map[string]any, principal databases.Principal) (*databases.Document, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	if raw, ok := updates["status"]; ok {
		s, ok := raw.(string)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "status must be a string")
		}
		if err := users.ValidateStatus(s); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	filtered := make(map[string]any, len(updates))
	for k, v := range updates {
		if strings.HasPrefix(k, "_") {
			continue
		}
		if _, blocked := userUpdateProtectedFields[k]; blocked {
			continue
		}
		filtered[k] = v
	}
	if v, ok := filtered["email"].(string); ok && v != "" {
		filtered["email_verified"] = false
	}
	if len(filtered) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied (password_hash, email_verified, status are managed via dedicated endpoints)")
	}
	doc := databases.Document{ID: userID, Data: filtered}
	updated, err := u.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(doc, nil), principal)
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return &updated, nil
}

func (u *Users) DeleteUser(ctx context.Context, projectID, userID string, principal databases.Principal) error {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return err
	}
	return u.docDB.DeleteDocument(ctx, projectID, "default", "users", userID, principal)
}

func (u *Users) UpdateUserStatus(ctx context.Context, projectID, userID, userStatus string, principal databases.Principal) (*databases.Document, error) {
	if userStatus == "" {
		userStatus = users.StatusActive
	}
	if err := users.ValidateStatus(userStatus); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	updated, err := u.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userID,
		Data: map[string]any{"status": userStatus, "updated_at": time.Now().Format(time.RFC3339Nano)},
	}, nil), principal)
	if err != nil {
		return nil, fmt.Errorf("update user status: %w", err)
	}
	return &updated, nil
}
