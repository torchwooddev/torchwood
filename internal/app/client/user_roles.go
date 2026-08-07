package client

import (
	"context"
	"fmt"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/teams"
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
	doc, err := r.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
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
	teamRoles, err := r.loadTeamRoles(ctx, projectID, userID)
	if err != nil {
		return baseRoles, err
	}
	return append(baseRoles, teamRoles...), nil
}

func (r *UserRoles) loadTeamRoles(ctx context.Context, projectID, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	list, err := r.docDB.ListDocuments(ctx, projectID, "default", "memberships", databases.Query{
		Queries: []string{
			query.BuildEqual("user_id", userID),
			query.BuildEqual("status", teams.StatusAccepted),
		},
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Documents)*3)
	for _, doc := range list.Documents {
		teamID, _ := doc.Data["team_id"].(string)
		if teamID == "" {
			continue
		}
		out = append(out, fmt.Sprintf("team:%s", teamID), fmt.Sprintf("member:%s", doc.ID))
		for _, role := range membershipRoles(doc.Data["roles"]) {
			out = append(out, fmt.Sprintf("team:%s/%s", teamID, role))
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
