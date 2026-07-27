package auth

import "context"

// Login failure throttling namespaces.
const (
	LoginNamespaceAdmin   = "admin"
	LoginNamespaceEndUser = "end_user"
)

// LoginThrottle rate-limits password sign-in attempts per email and per client IP.
type LoginThrottle interface {
	// Check returns an error when the email or IP has exceeded the failure budget.
	Check(ctx context.Context, namespace, email, ip string) error
	// RecordFailure registers a failed sign-in attempt.
	RecordFailure(ctx context.Context, namespace, email, ip string) error
	// Reset clears recorded failures after a successful sign-in.
	Reset(ctx context.Context, namespace, email, ip string) error
}
