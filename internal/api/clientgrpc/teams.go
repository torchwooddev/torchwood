package clientgrpc

import (
	"context"
	"time"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/app/client"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TeamsService struct {
	clientv1.UnimplementedTeamsServiceServer
	teams *client.Teams
}

func NewTeamsService(teams *client.Teams) *TeamsService {
	return &TeamsService{teams: teams}
}

func (s *TeamsService) CreateTeam(ctx context.Context, req *clientv1.CreateTeamRequest) (*clientv1.Team, error) {
	doc, err := s.teams.CreateTeam(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	return mapClientTeamDoc(doc), nil
}

func (s *TeamsService) ListTeams(ctx context.Context, req *sharedv1.ListRequest) (*clientv1.ListTeamsResponse, error) {
	docs, total, _, err := s.teams.ListTeams(ctx, databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*clientv1.Team, len(docs))
	for i := range docs {
		out[i] = mapClientTeamDoc(&docs[i])
	}
	return &clientv1.ListTeamsResponse{
		Teams: out,
		Meta:  &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total)},
	}, nil
}

func (s *TeamsService) GetTeam(ctx context.Context, req *clientv1.GetTeamRequest) (*clientv1.Team, error) {
	doc, err := s.teams.GetTeam(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "team not found")
	}
	return mapClientTeamDoc(doc), nil
}

func (s *TeamsService) DeleteTeam(ctx context.Context, req *clientv1.GetTeamRequest) (*sharedv1.Empty, error) {
	if err := s.teams.DeleteTeam(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *TeamsService) CreateMembership(ctx context.Context, req *clientv1.CreateMembershipRequest) (*clientv1.Membership, error) {
	doc, err := s.teams.CreateMembership(ctx, req.GetTeamId(), req.GetEmail(), req.GetName(), req.GetRoles())
	if err != nil {
		return nil, err
	}
	return mapClientMembershipDoc(doc), nil
}

func (s *TeamsService) ListMemberships(ctx context.Context, req *clientv1.ListMembershipsRequest) (*clientv1.ListMembershipsResponse, error) {
	docs, err := s.teams.ListMemberships(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	out := make([]*clientv1.Membership, len(docs))
	for i := range docs {
		out[i] = mapClientMembershipDoc(&docs[i])
	}
	return &clientv1.ListMembershipsResponse{Memberships: out}, nil
}

func (s *TeamsService) UpdateMembershipStatus(ctx context.Context, req *clientv1.UpdateMembershipStatusRequest) (*clientv1.Membership, error) {
	doc, err := s.teams.UpdateMembershipStatus(ctx, req.GetTeamId(), req.GetMembershipId(), req.GetStatus())
	if err != nil {
		return nil, err
	}
	return mapClientMembershipDoc(doc), nil
}

func (s *TeamsService) DeleteMembership(ctx context.Context, req *clientv1.GetMembershipRequest) (*sharedv1.Empty, error) {
	if err := s.teams.DeleteMembership(ctx, req.GetTeamId(), req.GetMembershipId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapClientTeamDoc(doc *databases.Document) *clientv1.Team {
	if doc == nil {
		return nil
	}
	t := &clientv1.Team{
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
	return t
}

func mapClientMembershipDoc(doc *databases.Document) *clientv1.Membership {
	if doc == nil {
		return nil
	}
	m := &clientv1.Membership{
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
	m.InvitedAt = clientDocTimeField(doc.Data, "invited_at")
	m.JoinedAt = clientDocTimeField(doc.Data, "joined_at")
	return m
}

func clientDocTimeField(data map[string]any, key string) *timestamppb.Timestamp {
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
