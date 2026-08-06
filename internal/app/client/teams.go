package client

import (
	"context"

	"github.com/torchwoodio/torchwood/internal/app/server"
	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/domain/teams"
	"github.com/torchwoodio/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Teams struct {
	teams *server.Teams
}

func NewTeams(teams *server.Teams) *Teams {
	return &Teams{teams: teams}
}

func (t *Teams) dbPrincipal(ctx context.Context) (projectID, userID, email string, principal databases.Principal, err error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.ProjectID == "" || p.UserID == "" {
		return "", "", "", databases.Principal{}, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return p.ProjectID, p.UserID, p.Email, databases.Principal{Roles: p.Roles, PlatformAdmin: p.IsPlatformAdmin}, nil
}

func (t *Teams) CreateTeam(ctx context.Context, name string) (*databases.Document, error) {
	projectID, userID, email, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	team, _, err := t.teams.CreateTeamWithOwner(ctx, projectID, name, userID, email, principal)
	return team, err
}

func (t *Teams) ListTeams(ctx context.Context, q databases.Query) ([]databases.Document, int64, string, error) {
	projectID, _, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	return t.teams.ListTeams(ctx, projectID, q, principal)
}

func (t *Teams) GetTeam(ctx context.Context, teamID string) (*databases.Document, error) {
	projectID, _, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	return t.teams.GetTeam(ctx, projectID, teamID, principal)
}

func (t *Teams) DeleteTeam(ctx context.Context, teamID string) error {
	projectID, _, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return err
	}
	return t.teams.DeleteTeam(ctx, projectID, teamID, principal)
}

func (t *Teams) CreateMembership(ctx context.Context, teamID, inviteEmail, name string, roles []string) (*databases.Document, error) {
	projectID, _, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	return t.teams.CreateMembership(ctx, projectID, server.CreateMembershipCommand{
		TeamID: teamID,
		Email:  inviteEmail,
		Name:   name,
		Roles:  roles,
		Status: teams.StatusPending,
	}, principal)
}

func (t *Teams) ListMemberships(ctx context.Context, teamID string) ([]databases.Document, error) {
	projectID, _, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	docs, _, _, err := t.teams.ListMemberships(ctx, projectID, teamID, databases.Query{}, principal)
	return docs, err
}

func (t *Teams) UpdateMembershipStatus(ctx context.Context, teamID, membershipID, statusVal string) (*databases.Document, error) {
	projectID, userID, email, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	doc, err := t.teams.GetMembership(ctx, projectID, teamID, membershipID, principal)
	if err != nil {
		return nil, err
	}
	memUserID, _ := doc.Data["user_id"].(string)
	memEmail, _ := doc.Data["email"].(string)
	if memUserID != userID && memEmail != email {
		return nil, status.Error(codes.PermissionDenied, "cannot update another user's membership")
	}
	return t.teams.UpdateMembershipStatus(ctx, projectID, teamID, membershipID, statusVal, principal)
}

func (t *Teams) DeleteMembership(ctx context.Context, teamID, membershipID string) error {
	projectID, _, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return err
	}
	return t.teams.DeleteMembership(ctx, projectID, teamID, membershipID, principal)
}
