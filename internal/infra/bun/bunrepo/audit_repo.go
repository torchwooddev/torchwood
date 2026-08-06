package bunrepo

import (
	"context"
	"time"

	"github.com/torchwoodio/torchwood/internal/domain/audit"
	"github.com/torchwoodio/torchwood/internal/infra/bun/model"
	"github.com/torchwoodio/torchwood/internal/infra/clients"
	"github.com/torchwoodio/torchwood/pkg/idgen"
)

type auditRepo struct {
	db *clients.Database
}

func NewAuditRepository(db *clients.Database) audit.Repository {
	return &auditRepo{db: db}
}

func (r *auditRepo) Insert(ctx context.Context, entry *audit.Entry) error {
	if entry == nil {
		return nil
	}
	id := entry.ID
	if id == "" {
		id = idgen.UUID().String()
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	metadata := entry.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	m := &model.AuditLog{
		ID:         id,
		ProjectID:  entry.ProjectID,
		ActorID:    entry.ActorID,
		ActorKind:  entry.ActorKind,
		Action:     entry.Action,
		ResourceID: entry.ResourceID,
		Status:     entry.Status,
		IP:         entry.IP,
		UserAgent:  entry.UserAgent,
		Metadata:   metadata,
		CreatedAt:  createdAt,
	}
	_, err := r.db.NewInsert().Model(m).Exec(ctx)
	return err
}
