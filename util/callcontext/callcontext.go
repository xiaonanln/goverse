package callcontext

import (
	"context"
	"time"

	"google.golang.org/grpc/metadata"
)

// Metadata keys used to propagate CallerIdentity across gRPC node boundaries.
// Owned entirely by this package — no other package should reference these strings.
const (
	mdKeyCallerUserID = "x-caller-user-id"
	mdKeyCallerRoles  = "x-caller-roles" // repeated: one value per role
)

// contextKey is a private type for context keys to avoid collisions
type contextKey int

const (
	clientIDKey       contextKey = iota
	callerIdentityKey contextKey = iota
)

// CallerIdentity holds the authenticated identity of a client connection.
// Populated by the gate after a successful AuthValidator.Validate call.
type CallerIdentity struct {
	UserID string
	Roles  []string
}

// AuthValidator validates client credentials during Register.
// headers contains gRPC metadata (or HTTP headers) from the incoming request.
// Return a non-nil CallerIdentity on success, or an error to reject the connection.
type AuthValidator interface {
	Validate(ctx context.Context, headers map[string][]string) (*CallerIdentity, error)
}

// WithCallerIdentity returns a new context with the CallerIdentity stored.
func WithCallerIdentity(ctx context.Context, id *CallerIdentity) context.Context {
	return context.WithValue(ctx, callerIdentityKey, id)
}

// GetCallerIdentity retrieves the CallerIdentity from the context.
// Returns nil if no identity is present.
func GetCallerIdentity(ctx context.Context) *CallerIdentity {
	if id, ok := ctx.Value(callerIdentityKey).(*CallerIdentity); ok {
		return id
	}
	return nil
}

// CallerUserID returns the authenticated UserID from the context.
// Returns "" if no identity is present.
func CallerUserID(ctx context.Context) string {
	if id := GetCallerIdentity(ctx); id != nil {
		return id.UserID
	}
	return ""
}

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

// InjectCallerToOutgoing appends the full CallerIdentity (UserID + Roles) from
// ctx as gRPC outgoing metadata so the receiving node can restore it via
// ExtractCallerFromIncoming. If no CallerIdentity is present, ctx is returned
// unchanged.
func InjectCallerToOutgoing(ctx context.Context) context.Context {
	id := GetCallerIdentity(ctx)
	if id == nil || id.UserID == "" {
		return ctx
	}
	pairs := []string{mdKeyCallerUserID, id.UserID}
	for _, role := range id.Roles {
		pairs = append(pairs, mdKeyCallerRoles, role)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// ExtractCallerFromIncoming reads the full CallerIdentity (UserID + Roles) from
// gRPC incoming metadata and injects it into the returned context.
// If the user-ID key is absent or empty, ctx is returned unchanged.
func ExtractCallerFromIncoming(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	userIDVals := md[mdKeyCallerUserID]
	if len(userIDVals) == 0 || userIDVals[0] == "" {
		return ctx
	}
	id := &CallerIdentity{
		UserID: userIDVals[0],
		Roles:  md[mdKeyCallerRoles], // nil if key absent — that's fine
	}
	return WithCallerIdentity(ctx, id)
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
