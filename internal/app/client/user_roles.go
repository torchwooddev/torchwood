package client

import (
	"context"
	"fmt"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/groups"
	"github.com/torchwooddev/torchwood/pkg/query"
)

// UserRoles resolves JWT role claims for a user from document collections.
type UserRoles struct {
	docDB databases.DocumentDB
}

func NewUserRoles(docDB databases.DocumentDB) *UserRoles {
	return &UserRoles{docDB: docDB}
}

func (r *UserRoles) LoadUserRoles(ctx context.Context, projectID, userID string) ([]string, error) {
	baseRoles := []string{"users", fmt.Sprintf("user:%s", userID)}
	doc, err := r.docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, "users", userID, databases.SystemPrincipal)
	if err != nil {
		return baseRoles, err
	}
	if doc == nil {
		return baseRoles, nil
	}
	if emailVerified, _ := doc.Data["email_verified"].(bool); emailVerified {
		baseRoles = append(baseRoles, fmt.Sprintf("user:%s/verified", userID))
	}
	for _, label := range userLabels(doc.Data["labels"]) {
		baseRoles = append(baseRoles, "label:"+label)
	}
	groupRoles, err := r.loadGroupRoles(ctx, projectID, userID)
	if err != nil {
		return baseRoles, err
	}
	return append(baseRoles, groupRoles...), nil
}

func (r *UserRoles) loadGroupRoles(ctx context.Context, projectID, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	list, err := r.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "memberships", databases.Query{
		Queries: []string{
			query.BuildEqual("user_id", userID),
			query.BuildEqual("status", groups.StatusAccepted),
		},
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Documents)*3)
	for _, doc := range list.Documents {
		groupID, _ := doc.Data["group_id"].(string)
		if groupID == "" {
			continue
		}
		out = append(out, fmt.Sprintf("group:%s", groupID), fmt.Sprintf("member:%s", doc.ID))
		for _, role := range membershipRoles(doc.Data["roles"]) {
			out = append(out, fmt.Sprintf("group:%s/%s", groupID, role))
		}
	}
	return out, nil
}

func membershipRoles(raw any) []string {
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}

func userLabels(raw any) []string {
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}
