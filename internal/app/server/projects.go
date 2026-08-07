package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/crud"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Projects struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	db          *clients.Database
}

func NewProjects(projectRepo projects.Repository, docDB databases.DocumentDB, db *clients.Database) *Projects {
	return &Projects{projectRepo: projectRepo, docDB: docDB, db: db}
}

type CreateProjectCommand struct {
	Name        string
	Description string
}

func (s *Projects) CreateProject(ctx context.Context, cmd CreateProjectCommand) (*projects.Project, error) {
	if cmd.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	id := strings.ToLower(strings.ReplaceAll(cmd.Name, " ", "-"))
	id = strings.Trim(id, "-")
	if id == "" {
		id = "project-" + idgen.UUID().String()
	}
	p := &projects.Project{
		ID:          id,
		Name:        cmd.Name,
		Description: cmd.Description,
		Status:      "active",
		Settings:    map[string]any{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err := s.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.projectRepo.CreateProject(txCtx, p); err != nil {
			return fmt.Errorf("insert project: %w", err)
		}
		if err := s.docDB.EnsureSystemCollections(txCtx, p.ID, p.InternalID); err != nil {
			return fmt.Errorf("ensure system collections: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

func (s *Projects) ListProjects(ctx context.Context, pageSize int32, pageToken, filter, orderBy string) ([]projects.Project, *crud.PaginationInfo, error) {
	if _, ok := contexts.Principal(ctx); !ok {
		return nil, nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	params, err := crud.ParseListParams(pageSize, pageToken, filter, orderBy)
	if err != nil {
		return nil, nil, err
	}
	all, err := s.projectRepo.ListProjects(ctx)
	if err != nil {
		return nil, nil, err
	}
	start := params.Offset
	if start > len(all) {
		start = len(all)
	}
	end := start + int(params.PageSize)
	if end > len(all) {
		end = len(all)
	}
	page := all[start:end]
	hasMore := end < len(all)
	info := crud.BuildPaginationInfo(params, len(all), hasMore)
	return page, &info, nil
}

func (s *Projects) GetProject(ctx context.Context, id string) (*projects.Project, error) {
	if _, ok := contexts.Principal(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return s.projectRepo.GetProject(ctx, id)
}
