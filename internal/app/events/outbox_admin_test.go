package events_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appevents "github.com/torchwooddev/torchwood/internal/app/events"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubOutboxRepo struct {
	listFn   func(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]domainevents.DeadLetter, int64, string, error)
	replayFn func(ctx context.Context, eventID, projectID string) error
}

func (s *stubOutboxRepo) ListDeadLetters(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]domainevents.DeadLetter, int64, string, error) {
	if s.listFn != nil {
		return s.listFn(ctx, projectID, pageSize, pageToken)
	}
	return nil, 0, "", nil
}
func (s *stubOutboxRepo) ReplayDeadLetter(ctx context.Context, eventID, projectID string) error {
	if s.replayFn != nil {
		return s.replayFn(ctx, eventID, projectID)
	}
	return nil
}

func adminCtx(projectID string) context.Context {
	return contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorKind: shared.ActorKindAdmin,
		AdminID:   "admin-1",
		Roles:     []string{"owner"},
		ProjectID: projectID,
	})
}

func TestOutboxAdmin_ListDeadLetters_ProjectMismatch(t *testing.T) {
	repo := &stubOutboxRepo{}
	uc := appevents.NewOutboxAdmin(repo)
	ctx := adminCtx("p1")
	_, _, _, err := uc.ListDeadLetters(ctx, "p2", 10, "")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestOutboxAdmin_ReplayDeadLetter_ProjectMismatch(t *testing.T) {
	repo := &stubOutboxRepo{}
	uc := appevents.NewOutboxAdmin(repo)
	ctx := adminCtx("p1")
	err := uc.ReplayDeadLetter(ctx, "e1", "p2")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestOutboxAdmin_ReplayDeadLetter_RequiresProjectID(t *testing.T) {
	repo := &stubOutboxRepo{}
	uc := appevents.NewOutboxAdmin(repo)
	ctx := adminCtx("p1")
	err := uc.ReplayDeadLetter(ctx, "e1", "")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
