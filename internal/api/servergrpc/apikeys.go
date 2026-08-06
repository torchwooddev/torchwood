package servergrpc

import (
	"context"
	"time"

	serverv1 "github.com/torchwoodio/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwoodio/torchwood/genproto/shared/v1"
	appserver "github.com/torchwoodio/torchwood/internal/app/server"
	"github.com/torchwoodio/torchwood/internal/domain/projects"
	"github.com/torchwoodio/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type APIKeysService struct {
	serverv1.UnimplementedAPIKeysServiceServer
	apiKeys *appserver.APIKeys
}

func NewAPIKeysService(apiKeys *appserver.APIKeys) *APIKeysService {
	return &APIKeysService{apiKeys: apiKeys}
}

func (s *APIKeysService) projectID(ctx context.Context) string {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return ""
	}
	return p.ProjectID
}

func (s *APIKeysService) CreateAPIKey(ctx context.Context, req *serverv1.CreateAPIKeyRequest) (*serverv1.APIKeyWithSecret, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	var expireAt *time.Time
	if req.GetExpireAt() > 0 {
		t := time.Unix(req.GetExpireAt(), 0)
		expireAt = &t
	}
	key, secret, err := s.apiKeys.Create(ctx, appserver.CreateAPIKeyCommand{
		ProjectID: projectID,
		Name:      req.GetName(),
		Scopes:    req.GetScopes(),
		ExpireAt:  expireAt,
	})
	if err != nil {
		return nil, err
	}
	return &serverv1.APIKeyWithSecret{
		ApiKey: mapAPIKey(key),
		Secret: secret,
	}, nil
}

func (s *APIKeysService) ListAPIKeys(ctx context.Context, _ *sharedv1.ListRequest) (*serverv1.ListAPIKeysResponse, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	keys, err := s.apiKeys.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*serverv1.APIKey, len(keys))
	for i := range keys {
		out[i] = mapAPIKey(&keys[i])
	}
	return &serverv1.ListAPIKeysResponse{
		ApiKeys: out,
		Meta:    &sharedv1.ListResponseMeta{},
	}, nil
}

func (s *APIKeysService) GetAPIKey(ctx context.Context, req *serverv1.GetAPIKeyRequest) (*serverv1.APIKey, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	key, err := s.apiKeys.Get(ctx, projectID, req.GetId())
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, status.Error(codes.NotFound, "api key not found")
	}
	return mapAPIKey(key), nil
}

func (s *APIKeysService) DeleteAPIKey(ctx context.Context, req *serverv1.GetAPIKeyRequest) (*sharedv1.Empty, error) {
	projectID := s.projectID(ctx)
	if projectID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing project context")
	}
	if err := s.apiKeys.Delete(ctx, projectID, req.GetId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapAPIKey(k *projects.APIKey) *serverv1.APIKey {
	if k == nil {
		return nil
	}
	out := &serverv1.APIKey{
		Id:        k.ID,
		Name:      k.Name,
		Scopes:    k.Scopes,
		Enabled:   k.Enabled,
		CreatedAt: timestamppb.New(k.CreatedAt),
		UpdatedAt: timestamppb.New(k.UpdatedAt),
	}
	if k.ExpireAt != nil {
		out.ExpireAt = timestamppb.New(*k.ExpireAt)
	}
	return out
}
