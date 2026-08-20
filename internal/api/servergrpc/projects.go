package servergrpc

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ProjectsService struct {
	serverv1.UnimplementedProjectsServiceServer
	projects *appserver.Projects
}

func NewProjectsService(projects *appserver.Projects) *ProjectsService {
	return &ProjectsService{projects: projects}
}

func (s *ProjectsService) CreateProject(ctx context.Context, req *serverv1.CreateProjectRequest) (*serverv1.Project, error) {
	p, err := s.projects.CreateProject(ctx, appserver.CreateProjectCommand{
		ID:          req.GetId(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
	})
	if err != nil {
		return nil, err
	}
	return mapProject(p), nil
}

func (s *ProjectsService) ListProjects(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListProjectsResponse, error) {
	list, info, err := s.projects.ListProjects(ctx, req.GetPageSize(), req.GetPageToken(), req.GetFilter(), req.GetOrderBy())
	if err != nil {
		return nil, err
	}
	var nextToken, prevToken string
	if info.HasNext {
		nextToken = crud.EncodePageToken(info.NextOffset)
	}
	if info.HasPrevious {
		prevToken = crud.EncodePageToken(info.PreviousOffset)
	}
	resp := &serverv1.ListProjectsResponse{
		Projects: make([]*serverv1.Project, len(list)),
		Meta: &sharedv1.ListResponseMeta{
			PageSize:      info.PageSize,
			NextPageToken: nextToken,
			PrevPageToken: prevToken,
			TotalCount:    int32(info.TotalCount),
		},
	}
	for i, p := range list {
		resp.Projects[i] = mapProject(&p)
	}
	return resp, nil
}

func (s *ProjectsService) GetProject(ctx context.Context, req *serverv1.GetProjectRequest) (*serverv1.Project, error) {
	p, err := s.projects.GetProject(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if p == nil {
		// 对齐 GetUser 模式：use-case 返回 nil,nil 时显式转 NotFound，
		// 避免 gRPC OK + 空响应的错误分类。
		return nil, status.Error(codes.NotFound, "project not found")
	}
	return mapProject(p), nil
}

func (s *ProjectsService) UpdateProject(ctx context.Context, req *serverv1.UpdateProjectRequest) (*serverv1.Project, error) {
	ctx = contexts.WithAuditResource(ctx, req.GetId())
	cmd := appserver.UpdateProjectCommand{ProjectID: req.GetId()}
	if req.Name != nil {
		cmd.Name = req.Name
	}
	if req.Description != nil {
		cmd.Description = req.Description
	}
	p, err := s.projects.UpdateProject(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return mapProject(p), nil
}

func (s *ProjectsService) DeleteProject(ctx context.Context, req *serverv1.GetProjectRequest) (*sharedv1.Empty, error) {
	ctx = contexts.WithAuditResource(ctx, req.GetId())
	if err := s.projects.DeleteProject(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapProject(p *projects.Project) *serverv1.Project {
	if p == nil {
		return nil
	}
	return &serverv1.Project{
		Id:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Status:      p.Status,
		CreatedAt:   timestamppb.New(p.CreatedAt),
		UpdatedAt:   timestamppb.New(p.UpdatedAt),
	}
}
