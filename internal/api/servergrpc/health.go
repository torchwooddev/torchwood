package servergrpc

import (
	"context"

	serverv1 "github.com/torchwoodio/torchwood/genproto/server/v1"
)

type HealthService struct {
	serverv1.UnimplementedHealthServiceServer
}

func NewHealthService() *HealthService {
	return &HealthService{}
}

func (s *HealthService) Check(ctx context.Context, _ *serverv1.HealthCheckRequest) (*serverv1.HealthCheckResponse, error) {
	return &serverv1.HealthCheckResponse{Status: "ok"}, nil
}
