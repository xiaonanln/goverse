package goverseapi

import "context"

// GateEventHandler handles gate-level events for client connections.
// Set GateServerConfig.EventHandler to receive auth and lifecycle callbacks.
// Embed [NoopGateEventHandler] to only override the methods you need.
//
// Example:
//
//	type MyHandler struct {
//	    goverseapi.NoopGateEventHandler
//	    secret []byte
//	}
//
//	func (h *MyHandler) OnClientAuthorise(ctx context.Context, headers map[string][]string) (*goverseapi.CallerIdentity, error) {
//	    token := headers["authorization"]
//	    if len(token) == 0 {
//	        return nil, fmt.Errorf("missing authorization header")
//	    }
//	    userID, err := verifyJWT(token[0], h.secret)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &goverseapi.CallerIdentity{UserID: userID}, nil
//	}
//
//	func (h *MyHandler) OnClientDisconnect(ctx context.Context, clientID string, identity *goverseapi.CallerIdentity) {
//	    log.Printf("client %s (%s) disconnected", clientID, identity.UserID)
//	}
type GateEventHandler interface {
	// OnClientAuthorise is called when a client calls Register.
	// headers contains gRPC metadata (lowercase keys) or HTTP headers,
	// depending on the transport used by the client.
	//
	// Return a non-nil *CallerIdentity on success, or (nil, nil) for anonymous
	// access. Return a non-nil error to reject the connection — the client
	// receives codes.Unauthenticated with the error text.
	OnClientAuthorise(ctx context.Context, headers map[string][]string) (*CallerIdentity, error)

	// OnClientDisconnect is called when a client stream ends for any reason:
	// clean close, network drop, or server shutdown.
	// identity is the value returned by OnClientAuthorise (nil for anonymous).
	OnClientDisconnect(ctx context.Context, clientID string, identity *CallerIdentity)
}

// NoopGateEventHandler provides no-op default implementations of [GateEventHandler].
// Embed this in your handler struct to only override the events you care about.
type NoopGateEventHandler struct{}

// OnClientAuthorise accepts all connections anonymously.
func (NoopGateEventHandler) OnClientAuthorise(_ context.Context, _ map[string][]string) (*CallerIdentity, error) {
	return nil, nil
}

// OnClientDisconnect does nothing.
func (NoopGateEventHandler) OnClientDisconnect(_ context.Context, _ string, _ *CallerIdentity) {}
