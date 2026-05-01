package gateserver

import (
	"context"
	"errors"
	"testing"

	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type stubAuthValidator struct {
	identity *callcontext.CallerIdentity
	err      error
}

func (s *stubAuthValidator) Validate(_ context.Context, _ map[string][]string) (*callcontext.CallerIdentity, error) {
	return s.identity, s.err
}

func authGateServer(t *testing.T, prefix string, validator callcontext.AuthValidator) (*GateServer, gate_pb.GateServiceClient) {
	t.Helper()
	listenAddr := testutil.GetFreeAddress()
	cfg := &GateServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
		AuthValidator:    validator,
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
		&stubAuthValidator{err: errors.New("invalid token")})

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
		&stubAuthValidator{identity: &callcontext.CallerIdentity{UserID: "alice"}})

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
		&stubAuthValidator{identity: &callcontext.CallerIdentity{UserID: "alice"}})

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
