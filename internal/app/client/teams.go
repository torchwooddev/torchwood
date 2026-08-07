package client

import (
	"context"
	"slices"

	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/teams"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Teams struct {
	teams *server.Teams
	docDB databases.DocumentDB
}

func NewTeams(teams *server.Teams, docDB databases.DocumentDB) *Teams {
	return &Teams{teams: teams, docDB: docDB}
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
	projectID, userID, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return err
	}
	labels, err := t.teams.AcceptedTeamRoleLabels(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if !slices.Contains(labels[teamID], teams.RoleOwner) {
		return status.Error(codes.PermissionDenied, "only the team owner can delete the team")
	}
	return t.teams.DeleteTeam(ctx, projectID, teamID, principal)
}

func (t *Teams) CreateMembership(ctx context.Context, teamID, inviteEmail, name string, roles []string) (*databases.Document, error) {
	projectID, userID, _, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	labels, err := t.teams.AcceptedTeamRoleLabels(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	if len(labels[teamID]) == 0 {
		return nil, status.Error(codes.PermissionDenied, "not a member of this team")
	}
	for _, role := range roles {
		if role != teams.RoleMember {
			return nil, status.Error(codes.PermissionDenied, "only member role can be assigned when creating membership")
		}
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
	if statusVal == teams.StatusAccepted {
		// 接受邀请一律要求调用者邮箱已验证：SignUp 不强制验证邮箱，
		// 若邀请创建时目标邮箱已被未验证账号抢注（user_id 绑定为该账号），
		// 仅按 memUserID 判断会绕过验证，因此这里无条件校验 email_verified。
		userDoc, err := t.docDB.GetDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
		if err != nil {
			return nil, err
		}
		verified := false
		if userDoc != nil {
			verified, _ = userDoc.Data["email_verified"].(bool)
		}
		if !verified {
			return nil, status.Error(codes.FailedPrecondition, "email verification required to accept team invitation")
		}
	}
	return t.teams.UpdateMembershipStatus(ctx, projectID, teamID, membershipID, statusVal, principal)
}

func (t *Teams) DeleteMembership(ctx context.Context, teamID, membershipID string) error {
	projectID, userID, email, principal, err := t.dbPrincipal(ctx)
	if err != nil {
		return err
	}
	doc, err := t.teams.GetMembership(ctx, projectID, teamID, membershipID, principal)
	if err != nil {
		return err
	}
	memUserID, _ := doc.Data["user_id"].(string)
	memEmail, _ := doc.Data["email"].(string)
	if memUserID != userID && memEmail != email {
		labels, err := t.teams.AcceptedTeamRoleLabels(ctx, projectID, userID)
		if err != nil {
			return err
		}
		if !slices.Contains(labels[teamID], teams.RoleOwner) {
			return status.Error(codes.PermissionDenied, "only the team owner can remove other members")
		}
	}
	return t.teams.DeleteMembership(ctx, projectID, teamID, membershipID, principal)
}
