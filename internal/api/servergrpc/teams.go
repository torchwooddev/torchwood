package servergrpc

import (
	"context"
	"time"

	serverv1 "github.com/torchwoodio/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwoodio/torchwood/genproto/shared/v1"
	appserver "github.com/torchwoodio/torchwood/internal/app/server"
	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TeamsService struct {
	serverv1.UnimplementedTeamsServiceServer
	teams *appserver.Teams
}

func NewTeamsService(teams *appserver.Teams) *TeamsService {
	return &TeamsService{teams: teams}
}

func (s *TeamsService) projectID(ctx context.Context) string {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return ""
	}
	return p.ProjectID
}

func (s *TeamsService) CreateTeam(ctx context.Context, req *serverv1.CreateTeamRequest) (*serverv1.Team, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.teams.CreateTeam(ctx, projectID, req.GetName(), req.GetPermissions())
	if err != nil {
		return nil, err
	}
	return mapTeamDoc(doc), nil
}

func (s *TeamsService) ListTeams(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListTeamsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	docs, total, _, err := s.teams.ListTeams(ctx, projectID, databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.Team, len(docs))
	for i := range docs {
		out[i] = mapTeamDoc(&docs[i])
	}
	return &serverv1.ListTeamsResponse{
		Teams: out,
		Meta:  &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total)},
	}, nil
}

func (s *TeamsService) GetTeam(ctx context.Context, req *serverv1.GetTeamRequest) (*serverv1.Team, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.teams.GetTeam(ctx, projectID, req.GetId(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "team not found")
	}
	return mapTeamDoc(doc), nil
}

func (s *TeamsService) DeleteTeam(ctx context.Context, req *serverv1.GetTeamRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if err := s.teams.DeleteTeam(ctx, projectID, req.GetId(), dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *TeamsService) CreateMembership(ctx context.Context, req *serverv1.CreateMembershipRequest) (*serverv1.Membership, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.teams.CreateMembership(ctx, projectID, appserver.CreateMembershipCommand{
		TeamID: req.GetTeamId(),
		UserID: req.GetUserId(),
		Email:  req.GetEmail(),
		Name:   req.GetName(),
		Roles:  req.GetRoles(),
		Status: req.GetStatus(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapMembershipDoc(doc), nil
}

func (s *TeamsService) ListMemberships(ctx context.Context, req *serverv1.ListMembershipsRequest) (*serverv1.ListMembershipsResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	docs, total, _, err := s.teams.ListMemberships(ctx, projectID, req.GetTeamId(), databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.Membership, len(docs))
	for i := range docs {
		out[i] = mapMembershipDoc(&docs[i])
	}
	return &serverv1.ListMembershipsResponse{
		Memberships: out,
		Meta:        &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total)},
	}, nil
}

func (s *TeamsService) GetMembership(ctx context.Context, req *serverv1.GetMembershipRequest) (*serverv1.Membership, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.teams.GetMembership(ctx, projectID, req.GetTeamId(), req.GetMembershipId(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "membership not found")
	}
	return mapMembershipDoc(doc), nil
}

func (s *TeamsService) UpdateMembership(ctx context.Context, req *serverv1.UpdateMembershipRequest) (*serverv1.Membership, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.teams.UpdateMembership(ctx, projectID, req.GetTeamId(), req.GetMembershipId(), appserver.UpdateMembershipCommand{
		Roles: req.GetRoles(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapMembershipDoc(doc), nil
}

func (s *TeamsService) UpdateMembershipStatus(ctx context.Context, req *serverv1.UpdateMembershipStatusRequest) (*serverv1.Membership, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.teams.UpdateMembershipStatus(ctx, projectID, req.GetTeamId(), req.GetMembershipId(), req.GetStatus(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapMembershipDoc(doc), nil
}

func (s *TeamsService) DeleteMembership(ctx context.Context, req *serverv1.GetMembershipRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if err := s.teams.DeleteMembership(ctx, projectID, req.GetTeamId(), req.GetMembershipId(), dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapTeamDoc(doc *databases.Document) *serverv1.Team {
	if doc == nil {
		return nil
	}
	t := &serverv1.Team{
		Id:        doc.ID,
		CreatedAt: timestamppb.New(doc.CreatedAt),
		UpdatedAt: timestamppb.New(doc.UpdatedAt),
	}
	if v, ok := doc.Data["name"].(string); ok {
		t.Name = v
	}
	if v, ok := doc.Data["total"].(float64); ok {
		t.Total = int32(v)
	}
	if v, ok := doc.Data["total"].(int64); ok {
		t.Total = int32(v)
	}
	if arr, ok := doc.Data["permissions"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				t.Permissions = append(t.Permissions, s)
			}
		}
	}
	return t
}

func mapMembershipDoc(doc *databases.Document) *serverv1.Membership {
	if doc == nil {
		return nil
	}
	m := &serverv1.Membership{
		Id:        doc.ID,
		CreatedAt: timestamppb.New(doc.CreatedAt),
		UpdatedAt: timestamppb.New(doc.UpdatedAt),
	}
	if v, ok := doc.Data["team_id"].(string); ok {
		m.TeamId = v
	}
	if v, ok := doc.Data["user_id"].(string); ok {
		m.UserId = v
	}
	if v, ok := doc.Data["email"].(string); ok {
		m.Email = v
	}
	if v, ok := doc.Data["name"].(string); ok {
		m.Name = v
	}
	if v, ok := doc.Data["status"].(string); ok {
		m.Status = v
	}
	if arr, ok := doc.Data["roles"].([]any); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok {
				m.Roles = append(m.Roles, s)
			}
		}
	}
	m.InvitedAt = docTimeField(doc.Data, "invited_at")
	m.JoinedAt = docTimeField(doc.Data, "joined_at")
	return m
}

func docTimeField(data map[string]any, key string) *timestamppb.Timestamp {
	v, ok := data[key].(string)
	if !ok || v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}
