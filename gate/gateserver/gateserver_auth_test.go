package gateserver

import (
	"context"
	"errors"
	"testing"

	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/testutil"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// stubGateEventHandler is a test implementation of GateEventHandler.
// Embed NoopGateEventHandler so only OnClientAuthorise needs to be set.
type stubGateEventHandler struct {
	goverseapi.NoopGateEventHandler
	identity *goverseapi.CallerIdentity
	err      error
}

func (s *stubGateEventHandler) OnClientAuthorise(_ context.Context, _ map[string][]string) (*goverseapi.CallerIdentity, error) {
	return s.identity, s.err
}

func authGateServer(t *testing.T, prefix string, handler goverseapi.GateEventHandler) (*GateServer, gate_pb.GateServiceClient) {
	t.Helper()
	listenAddr := testutil.GetFreeAddress()
	cfg := &GateServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
		EventHandler:     handler,
	}
	server, err := NewGateServer(cfg)
	if err != nil {
		t.Fatalf("NewGateServer: %v", err)
	}
	t.Cleanup(func() { server.Stop() })

	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	conn, err := grpc.NewClient(listenAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return server, gate_pb.NewGateServiceClient(conn)
}

// TestGateServerRegister_AuthReject verifies that Register returns Unauthenticated
// when the AuthValidator rejects the incoming connection.
func TestGateServerRegister_AuthReject(t *testing.T) {
	t.Parallel()
	_, client := authGateServer(t, "/test-gate-auth-reject",
		&stubGateEventHandler{err: errors.New("invalid token")})

	stream, err := client.Register(context.Background(), &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Register stream: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected Unauthenticated error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Fatalf("expected codes.Unauthenticated, got %v", code)
	}
}

// TestGateServerCallObject_AuthRequired_EmptyClientID verifies that CallObject
// returns Unauthenticated when auth is configured but ClientId is empty.
func TestGateServerCallObject_AuthRequired_EmptyClientID(t *testing.T) {
	t.Parallel()
	_, client := authGateServer(t, "/test-gate-auth-callobj-empty",
		&stubGateEventHandler{identity: &goverseapi.CallerIdentity{UserID: "alice"}})

	_, err := client.CallObject(context.Background(), &gate_pb.CallObjectRequest{
		ClientId: "",
		Method:   "SomeMethod",
		Type:     "SomeType",
		Id:       "obj1",
	})
	if err == nil {
		t.Fatal("expected Unauthenticated error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Fatalf("expected codes.Unauthenticated, got %v", code)
	}
}

// TestGateServerCallObject_AuthRequired_InvalidClientID verifies that CallObject
// returns Unauthenticated when the ClientId does not correspond to an active
// authenticated Register session.
func TestGateServerCallObject_AuthRequired_InvalidClientID(t *testing.T) {
	t.Parallel()
	_, client := authGateServer(t, "/test-gate-auth-callobj-invalid",
		&stubGateEventHandler{identity: &goverseapi.CallerIdentity{UserID: "alice"}})

	_, err := client.CallObject(context.Background(), &gate_pb.CallObjectRequest{
		ClientId: "not-a-registered-client",
		Method:   "SomeMethod",
		Type:     "SomeType",
		Id:       "obj1",
	})
	if err == nil {
		t.Fatal("expected Unauthenticated error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Fatalf("expected codes.Unauthenticated, got %v", code)
	}
}

// TestGateServerCallObject_AnonymousSession verifies that when OnClientAuthorise
// returns (nil, nil) — anonymous access — CallObject still passes the session
// check for that clientID. Without this, any EventHandler that only overrides
// OnClientDisconnect would break all CallObject calls.
func TestGateServerCallObject_AnonymousSession(t *testing.T) {
	t.Parallel()
	// handler returns (nil, nil): anonymous access allowed
	_, client := authGateServer(t, "/test-gate-auth-anonymous",
		&stubGateEventHandler{identity: nil, err: nil})

	// Register to get a valid clientID
	stream, err := client.Register(context.Background(), &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Register stream: %v", err)
	}
	msgAny, err := stream.Recv()
	if err != nil {
		t.Fatalf("Register recv: %v", err)
	}
	regResp := &gate_pb.RegisterResponse{}
	if err := msgAny.UnmarshalTo(regResp); err != nil {
		t.Fatalf("unmarshal RegisterResponse: %v", err)
	}
	clientID := regResp.ClientId

	// CallObject with the registered clientID must not return Unauthenticated.
	// It may return any other error (e.g. Unavailable — no nodes) but not UNAUTHENTICATED.
	_, err = client.CallObject(context.Background(), &gate_pb.CallObjectRequest{
		ClientId: clientID,
		Method:   "SomeMethod",
		Type:     "SomeType",
		Id:       "obj1",
	})
	if err != nil && status.Code(err) == codes.Unauthenticated {
		t.Fatalf("anonymous session got Unauthenticated on CallObject: %v", err)
	}
}
