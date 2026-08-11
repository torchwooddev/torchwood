package torchwood

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// TeamsService 封装 Client API 的 Teams 服务。
type TeamsService struct{ c *Client }

// CreateTeam 创建团队。
func (t *TeamsService) CreateTeam(ctx context.Context, name string) (*clientv1.Team, error) {
	return t.c.teams.CreateTeam(t.c.AuthContext(ctx), &clientv1.CreateTeamRequest{Name: name})
}

// GetTeam 按 ID 获取团队。
func (t *TeamsService) GetTeam(ctx context.Context, teamID string) (*clientv1.Team, error) {
	return t.c.teams.GetTeam(t.c.AuthContext(ctx), &clientv1.GetTeamRequest{Id: teamID})
}

// ListTeams 列出当前用户所属团队。
func (t *TeamsService) ListTeams(ctx context.Context) (*clientv1.ListTeamsResponse, error) {
	return t.c.teams.ListTeams(t.c.AuthContext(ctx), &sharedv1.ListRequest{})
}

// CreateMembership 邀请成员加入团队（按邮箱）。
func (t *TeamsService) CreateMembership(ctx context.Context, teamID, email, name string, roles []string) (*clientv1.Membership, error) {
	return t.c.teams.CreateMembership(t.c.AuthContext(ctx), &clientv1.CreateMembershipRequest{
		TeamId: teamID,
		Email:  email,
		Name:   name,
		Roles:  roles,
	})
}

// ListMemberships 列出团队成员。
func (t *TeamsService) ListMemberships(ctx context.Context, teamID string) (*clientv1.ListMembershipsResponse, error) {
	return t.c.teams.ListMemberships(t.c.AuthContext(ctx), &clientv1.ListMembershipsRequest{TeamId: teamID})
}

// UpdateMembershipStatus 更新成员状态（如 active / invited / banned）。
func (t *TeamsService) UpdateMembershipStatus(ctx context.Context, teamID, membershipID, status string) (*clientv1.Membership, error) {
	return t.c.teams.UpdateMembershipStatus(t.c.AuthContext(ctx), &clientv1.UpdateMembershipStatusRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
		Status:       status,
	})
}

// DeleteMembership 移除团队成员。
func (t *TeamsService) DeleteMembership(ctx context.Context, teamID, membershipID string) error {
	_, err := t.c.teams.DeleteMembership(t.c.AuthContext(ctx), &clientv1.GetMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
	})
	return err
}
