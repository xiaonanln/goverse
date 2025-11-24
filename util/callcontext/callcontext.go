package callcontext

import (
	"context"
	"time"
)

// contextKey is a private type for context keys to avoid collisions
type contextKey int

const (
	// clientIDKey is the context key for storing the client ID
	clientIDKey contextKey = iota
)

// WithClientID returns a new context with the client ID stored
// clientID format: "gateAddress/uniqueId" (e.g., "localhost:7001/abc123")
func WithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey, clientID)
}

// ClientID retrieves the client ID from the context
// Returns empty string if no client ID is present
func ClientID(ctx context.Context) string {
	if clientID, ok := ctx.Value(clientIDKey).(string); ok {
		return clientID
	}
	return ""
}

// FromClient checks if the context contains a client ID
func FromClient(ctx context.Context) bool {
	return ctx.Value(clientIDKey) != nil
}

// WithDefaultTimeout returns a context with the specified timeout applied only if
// the context doesn't already have a deadline. This ensures that:
// 1. If the caller provided a deadline, it is preserved (respects client timeouts)
// 2. If no deadline exists, the default timeout is applied to prevent indefinite hangs
//
// The returned cancel function should be called when the operation completes to release resources.
// If no timeout was applied (because context already had a deadline), cancel is a no-op function.
//
// Usage:
//
//	ctx, cancel := callcontext.WithDefaultTimeout(ctx, 300*time.Second)
//	defer cancel()
func WithDefaultTimeout(ctx context.Context, defaultTimeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		return context.WithTimeout(ctx, defaultTimeout)
	}
	// Return a no-op cancel function if deadline already exists
	return ctx, func() {}
}
