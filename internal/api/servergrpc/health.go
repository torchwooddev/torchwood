package servergrpc

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"github.com/torchwooddev/torchwood/internal/infra/health"
	"github.com/torchwooddev/torchwood/internal/pkg/buildinfo"
)

type HealthService struct {
	serverv1.UnimplementedHealthServiceServer
	checkers *health.Checkers
	info     buildinfo.BuildInfo
}

func NewHealthService(checkers *health.Checkers, info buildinfo.BuildInfo) *HealthService {
	return &HealthService{checkers: checkers, info: info}
}

// Check 并行探测各依赖，任一失败整体 status=unavailable（gRPC 返回码保持
// OK；503 语义由 /healthz/readiness 承担）。
func (s *HealthService) Check(ctx context.Context, _ *serverv1.HealthCheckRequest) (*serverv1.HealthCheckResponse, error) {
	deps := s.checkers.Details(ctx)
	status := "ok"
	for _, d := range deps {
		if d.GetStatus() != "ok" {
			status = "unavailable"
			break
		}
	}
	return &serverv1.HealthCheckResponse{Status: status, Dependencies: deps}, nil
}

func (s *HealthService) GetVersion(_ context.Context, _ *serverv1.GetVersionRequest) (*serverv1.GetVersionResponse, error) {
	return &serverv1.GetVersionResponse{
		Version: s.info.Version,
		Commit:  s.info.Commit,
		Date:    s.info.Date,
	}, nil
}
