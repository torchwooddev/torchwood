package server

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// TeamsService 封装 Server API 的团队管理服务。
type TeamsService struct{ c *Client }

// CreateTeam 创建团队（可附带权限）。
func (t *TeamsService) CreateTeam(ctx context.Context, name string, permissions []string) (*serverv1.Team, error) {
	return t.c.teams.CreateTeam(ctx, &serverv1.CreateTeamRequest{
		Name:        name,
		Permissions: permissions,
	})
}

// GetTeam 按 ID 获取团队。
func (t *TeamsService) GetTeam(ctx context.Context, teamID string) (*serverv1.Team, error) {
	return t.c.teams.GetTeam(ctx, &serverv1.GetTeamRequest{Id: teamID})
}

// ListTeams 按查询 DSL 列出团队。
func (t *TeamsService) ListTeams(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListTeamsResponse, error) {
	return t.c.teams.ListTeams(ctx, &sharedv1.ListRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
		Queries:   queries,
	})
}

// CreateMembership 创建团队成员关系（按 userID 或邮箱）。
func (t *TeamsService) CreateMembership(ctx context.Context, teamID, userID, email, name string, roles []string, status string) (*serverv1.Membership, error) {
	return t.c.teams.CreateMembership(ctx, &serverv1.CreateMembershipRequest{
		TeamId: teamID,
		UserId: userID,
		Email:  email,
		Name:   name,
		Roles:  roles,
		Status: status,
	})
}

// ListMemberships 列出团队成员。
func (t *TeamsService) ListMemberships(ctx context.Context, teamID string) (*serverv1.ListMembershipsResponse, error) {
	return t.c.teams.ListMemberships(ctx, &serverv1.ListMembershipsRequest{TeamId: teamID})
}

// GetMembership 获取成员关系。
func (t *TeamsService) GetMembership(ctx context.Context, teamID, membershipID string) (*serverv1.Membership, error) {
	return t.c.teams.GetMembership(ctx, &serverv1.GetMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
	})
}

// UpdateMembership 更新成员角色。
func (t *TeamsService) UpdateMembership(ctx context.Context, teamID, membershipID string, roles []string) (*serverv1.Membership, error) {
	return t.c.teams.UpdateMembership(ctx, &serverv1.UpdateMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
		Roles:        roles,
	})
}

// UpdateMembershipStatus 更新成员状态。
func (t *TeamsService) UpdateMembershipStatus(ctx context.Context, teamID, membershipID, status string) (*serverv1.Membership, error) {
	return t.c.teams.UpdateMembershipStatus(ctx, &serverv1.UpdateMembershipStatusRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
		Status:       status,
	})
}

// DeleteMembership 移除成员关系。
func (t *TeamsService) DeleteMembership(ctx context.Context, teamID, membershipID string) error {
	_, err := t.c.teams.DeleteMembership(ctx, &serverv1.GetMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
	})
	return err
}
