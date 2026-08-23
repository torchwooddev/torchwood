package events

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type OutboxAdmin struct {
	repo events.OutboxRepository
}

func NewOutboxAdmin(repo events.OutboxRepository) *OutboxAdmin {
	return &OutboxAdmin{repo: repo}
}

func (o *OutboxAdmin) ListDeadLetters(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]events.DeadLetter, int64, string, error) {
	if _, ok := contexts.Principal(ctx); !ok {
		return nil, 0, "", status.Error(codes.Unauthenticated, "unauthenticated")
	}
	if projectID == "" {
		return nil, 0, "", status.Error(codes.InvalidArgument, "project_id is required")
	}
	if p, ok := contexts.Principal(ctx); ok && p != nil && p.ProjectID != "" && p.ProjectID != projectID {
		return nil, 0, "", status.Error(codes.PermissionDenied, "project mismatch")
	}
	return o.repo.ListDeadLetters(ctx, projectID, pageSize, pageToken)
}

func (o *OutboxAdmin) ReplayDeadLetter(ctx context.Context, eventID, projectID string) error {
	if err := shared.RequireServerWriteActor(ctx); err != nil {
		return err
	}
	if eventID == "" {
		return status.Error(codes.InvalidArgument, "event_id is required")
	}
	if projectID == "" {
		return status.Error(codes.InvalidArgument, "project_id is required")
	}
	if p, ok := contexts.Principal(ctx); ok && p != nil && p.ProjectID != "" && p.ProjectID != projectID {
		return status.Error(codes.PermissionDenied, "project mismatch")
	}
	return o.repo.ReplayDeadLetter(ctx, eventID, projectID)
}
