package servergrpc

import (
	"context"

	serverv1 "github.com/torchwoodio/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwoodio/torchwood/genproto/shared/v1"
	appserver "github.com/torchwoodio/torchwood/internal/app/server"
	"github.com/torchwoodio/torchwood/internal/domain/projects"
	"github.com/torchwoodio/torchwood/pkg/crud"
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
		return nil, nil
	}
	return mapProject(p), nil
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
