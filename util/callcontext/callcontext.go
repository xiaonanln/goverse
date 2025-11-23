package callcontext

import (
	"context"
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

// GetClientID retrieves the client ID from the context
// Returns empty string if no client ID is present
func GetClientID(ctx context.Context) string {
	if clientID, ok := ctx.Value(clientIDKey).(string); ok {
		return clientID
	}
	return ""
}

// HasClientID checks if the context contains a client ID
func HasClientID(ctx context.Context) bool {
	return ctx.Value(clientIDKey) != nil
}
