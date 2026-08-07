package contexts

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

func WithPrincipal(ctx context.Context, p *shared.Principal) context.Context {
	return context.WithValue(ctx, ContextKeyPrincipal, p)
}

func Principal(ctx context.Context) (*shared.Principal, bool) {
	v := ctx.Value(ContextKeyPrincipal)
	p, ok := v.(*shared.Principal)
	return p, ok && p != nil
}

func WithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, ContextKeyProjectID, projectID)
}

func ProjectID(ctx context.Context) (string, bool) {
	v := ctx.Value(ContextKeyProjectID)
	s, ok := v.(string)
	return s, ok && s != ""
}

// WithAuditResource attaches the resource id being acted upon (e.g. the
// project id targeted by a delete) so the audit interceptor can record it.
func WithAuditResource(ctx context.Context, resourceID string) context.Context {
	return context.WithValue(ctx, ContextKeyAuditResource, resourceID)
}

// AuditResource returns the audit resource id stored in ctx, if any.
func AuditResource(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyAuditResource).(string)
	return v
}
