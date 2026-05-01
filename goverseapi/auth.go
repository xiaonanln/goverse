/*
Package goverseapi provides the public API for Goverse object methods and gate
configuration.

# Authentication and Caller Identity

Goverse gates support pluggable authentication via the [AuthValidator] interface.
When wired in, every client connection is authenticated during Register and the
resulting [CallerIdentity] is automatically injected into the context of every
subsequent [CallObject] call — no boilerplate needed in individual object methods.

## Server side: implementing and wiring AuthValidator

Implement [AuthValidator] with whatever token-validation logic your app needs,
then pass it to [gateserver.GateServerConfig]:

	type myAuth struct{ secret []byte }

	func (a *myAuth) Validate(ctx context.Context, headers map[string][]string) (*goverseapi.CallerIdentity, error) {
	    vals := headers["authorization"] // gRPC metadata key (lowercase)
	    if len(vals) == 0 {
	        return nil, fmt.Errorf("missing authorization header")
	    }
	    token := strings.TrimPrefix(vals[0], "Bearer ")
	    userID, roles, err := verifyJWT(token, a.secret)
	    if err != nil {
	        return nil, err // causes codes.Unauthenticated on the client
	    }
	    return &goverseapi.CallerIdentity{UserID: userID, Roles: roles}, nil
	}

	// Wire at startup:
	cfg := &gateserver.GateServerConfig{
	    ListenAddress: ":7001",
	    AuthValidator: &myAuth{secret: jwtSecret},
	    // ...
	}

When AuthValidator is nil (the default) the gate behaves exactly as in v0.1:
all connections are accepted and CallerUserID(ctx) returns "".

## Client side: sending the authorization token

The client attaches credentials as gRPC metadata before opening the Register
stream. The gate reads the metadata via metadata.FromIncomingContext on the
server side.

	import "google.golang.org/grpc/metadata"

	md := metadata.Pairs("authorization", "Bearer "+myToken)
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	stream, err := gateClient.Register(ctx, &gate_pb.Empty{})

The metadata is sent as HTTP/2 headers when the stream is opened and is
available to Validate immediately, before any messages are exchanged.

## Reading the caller identity inside object methods

Once auth is configured, every object method receives the validated identity
through its context automatically:

	func (m *Match) HandleInput(ctx context.Context, req *pb.PlayerInputRequest) (*pb.PlayerInputResponse, error) {
	    callerID := goverseapi.CallerUserID(ctx)
	    if callerID == "" || callerID != req.PlayerId {
	        return nil, status.Errorf(codes.PermissionDenied, "not your player")
	    }
	    // ...
	}

	func (r *Room) AdminAction(ctx context.Context, req *pb.AdminRequest) (*pb.AdminResponse, error) {
	    if !goverseapi.CallerHasRole(ctx, "admin") {
	        return nil, status.Errorf(codes.PermissionDenied, "admin role required")
	    }
	    // ...
	}
*/
package goverseapi

import (
	"context"

	"github.com/xiaonanln/goverse/util/callcontext"
)

// CallerIdentity is the authenticated identity of a connected client.
// It is populated by the gate after a successful [AuthValidator.Validate] call
// and injected into the context of every CallObject RPC.
//
// UserID is a stable, opaque per-user identifier (e.g. the JWT "sub" claim).
// Roles is an optional, application-defined list of role strings used for
// coarse-grained access control via [CallerHasRole].
type CallerIdentity = callcontext.CallerIdentity

// AuthValidator validates client credentials when a client calls Register.
// Implement this interface and set it on GateServerConfig.AuthValidator.
//
// headers contains the gRPC metadata sent by the client (lowercase keys).
// The conventional key for bearer-token auth is "authorization", which the
// client populates with "Bearer <token>" via metadata.NewOutgoingContext.
//
// Return a non-nil *CallerIdentity on success. Return an error to reject the
// connection; the client receives codes.Unauthenticated with the error text.
//
// When AuthValidator is nil, all connections are accepted and
// CallerUserID(ctx) returns "" (v0.1 behaviour, fully backwards compatible).
type AuthValidator = callcontext.AuthValidator

// CallerUserID returns the authenticated UserID from the call context.
//
// Returns "" if:
//   - No AuthValidator is configured on the gate.
//   - The call originated from a node rather than a client.
func CallerUserID(ctx context.Context) string {
	if id := callcontext.GetCallerIdentity(ctx); id != nil {
		return id.UserID
	}
	return ""
}

// CallerRoles returns the authenticated roles from the call context.
// Returns nil if the call is unauthenticated.
func CallerRoles(ctx context.Context) []string {
	if id := callcontext.GetCallerIdentity(ctx); id != nil {
		return id.Roles
	}
	return nil
}

// CallerHasRole reports whether the authenticated caller holds the given role.
// Returns false if the call is unauthenticated or the role is not present.
func CallerHasRole(ctx context.Context, role string) bool {
	id := callcontext.GetCallerIdentity(ctx)
	if id == nil {
		return false
	}
	for _, r := range id.Roles {
		if r == role {
			return true
		}
	}
	return false
}
