package goverseapi

import (
	"context"

	"github.com/xiaonanln/goverse/util/callcontext"
)

// CallerIdentity is the authenticated identity of a connected client.
type CallerIdentity = callcontext.CallerIdentity

// AuthValidator validates client credentials on Register.
// Implement this interface and pass it to GateServerConfig.AuthValidator.
//
// headers contains gRPC metadata keys/values. Typically you'll read
// headers["authorization"] for a bearer token.
//
// Return a non-nil *CallerIdentity on success, or an error to reject the
// connection with codes.Unauthenticated.
type AuthValidator = callcontext.AuthValidator

// CallerUserID returns the authenticated UserID from the call context.
// Returns empty string if the call was not authenticated (no AuthValidator
// configured, or call originated from a node rather than a client).
func CallerUserID(ctx context.Context) string {
	if id := callcontext.GetCallerIdentity(ctx); id != nil {
		return id.UserID
	}
	return ""
}

// CallerRoles returns the authenticated roles from the call context.
// Returns nil if the call was not authenticated.
func CallerRoles(ctx context.Context) []string {
	if id := callcontext.GetCallerIdentity(ctx); id != nil {
		return id.Roles
	}
	return nil
}
