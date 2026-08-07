package servergrpc

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type UsersService struct {
	serverv1.UnimplementedUsersServiceServer
	users *appserver.Users
}

func NewUsersService(users *appserver.Users) *UsersService {
	return &UsersService{users: users}
}

func (s *UsersService) projectID(ctx context.Context) string {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return ""
	}
	return p.ProjectID
}

func (s *UsersService) ListUsers(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListUsersResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	docs, total, _, err := s.users.ListUsers(ctx, projectID, databases.Query{
		Queries:   req.GetQueries(),
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	}, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.User, len(docs))
	for i := range docs {
		out[i] = mapUserDoc(&docs[i])
	}
	return &serverv1.ListUsersResponse{
		Users: out,
		Meta:  &sharedv1.ListResponseMeta{PageSize: req.GetPageSize(), TotalCount: int32(total)},
	}, nil
}

func (s *UsersService) GetUser(ctx context.Context, req *serverv1.GetUserRequest) (*serverv1.User, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	doc, err := s.users.GetUser(ctx, projectID, req.GetId(), dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return mapUserDoc(doc), nil
}

func (s *UsersService) UpdateUser(ctx context.Context, req *serverv1.UpdateUserRequest) (*serverv1.User, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	updates := map[string]any{}
	if req.GetStatus() != "" {
		updates["status"] = req.GetStatus()
	}
	if req.GetLabels() != nil {
		updates["labels"] = req.GetLabels().AsMap()
	}
	if req.GetPrefs() != nil {
		updates["prefs"] = req.GetPrefs().AsMap()
	}
	doc, err := s.users.UpdateUser(ctx, projectID, req.GetId(), updates, dbPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	return mapUserDoc(doc), nil
}

func (s *UsersService) DeleteUser(ctx context.Context, req *serverv1.GetUserRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if err := s.users.DeleteUser(ctx, projectID, req.GetId(), dbPrincipal(ctx)); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapUserDoc(doc *databases.Document) *serverv1.User {
	if doc == nil {
		return nil
	}
	u := &serverv1.User{
		Id:        doc.ID,
		CreatedAt: timestamppb.New(doc.CreatedAt),
		UpdatedAt: timestamppb.New(doc.UpdatedAt),
	}
	if v, ok := doc.Data["email"].(string); ok {
		u.Email = v
	}
	if v, ok := doc.Data["name"].(string); ok {
		u.Name = v
	}
	if v, ok := doc.Data["status"].(string); ok {
		u.Status = v
	}
	if v, ok := doc.Data["email_verified"].(bool); ok {
		u.EmailVerified = v
	}
	return u
}
