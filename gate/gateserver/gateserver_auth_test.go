package gateserver

import (
	"context"
	"errors"
	"io"
	"testing"

	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

// registerAndGetSession opens a Register stream with a valid auth token and
// returns the clientID + sessionToken from RegisterResponse.
func registerAndGetSession(t *testing.T, client gate_pb.GateServiceClient) (clientID, sessionToken string, cancelStream func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Register(ctx, &gate_pb.Empty{})
	if err != nil {
		cancel()
		t.Fatalf("Register: %v", err)
	}

	anyMsg, err := stream.Recv()
	if err != nil {
		cancel()
		t.Fatalf("Register Recv: %v", err)
	}

	var regResp gate_pb.RegisterResponse
	if err := proto.Unmarshal(anyMsg.Value, &regResp); err != nil {
		// anyMsg wraps RegisterResponse — unmarshal from Any.Value directly
		// using the typeURL to locate the message descriptor is unnecessary here;
		// try proto.Unmarshal first (works when the Any value is the raw bytes).
		cancel()
		t.Fatalf("unmarshal RegisterResponse: %v", err)
	}
	return regResp.ClientId, regResp.SessionToken, cancel
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

// TestGateServerRegister_AuthSuccess verifies that RegisterResponse contains a
// non-empty session_token when auth is configured and credentials are valid.
func TestGateServerRegister_AuthSuccess(t *testing.T) {
	t.Parallel()
	_, client := authGateServer(t, "/test-gate-auth-success",
		&stubAuthValidator{identity: &callcontext.CallerIdentity{UserID: "alice"}})

	clientID, sessionToken, cancel := registerAndGetSession(t, client)
	defer cancel()

	if clientID == "" {
		t.Error("expected non-empty client_id in RegisterResponse")
	}
	if sessionToken == "" {
		t.Error("expected non-empty session_token in RegisterResponse when auth is configured")
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

// TestGateServerCallObject_AuthRequired_MissingSessionToken verifies that CallObject
// returns Unauthenticated when the session token metadata is absent.
func TestGateServerCallObject_AuthRequired_MissingSessionToken(t *testing.T) {
	t.Parallel()
	_, client := authGateServer(t, "/test-gate-auth-callobj-no-token",
		&stubAuthValidator{identity: &callcontext.CallerIdentity{UserID: "alice"}})

	clientID, _, cancel := registerAndGetSession(t, client)
	defer cancel()

	// Send CallObject with correct clientID but no x-session-token metadata.
	_, err := client.CallObject(context.Background(), &gate_pb.CallObjectRequest{
		ClientId: clientID,
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

// TestGateServerCallObject_AuthRequired_WrongSessionToken verifies that CallObject
// returns Unauthenticated when a wrong session token is presented — i.e. a caller
// who knows the clientID cannot impersonate the owner without the correct token.
func TestGateServerCallObject_AuthRequired_WrongSessionToken(t *testing.T) {
	t.Parallel()
	_, client := authGateServer(t, "/test-gate-auth-callobj-wrong-token",
		&stubAuthValidator{identity: &callcontext.CallerIdentity{UserID: "alice"}})

	clientID, _, cancel := registerAndGetSession(t, client)
	defer cancel()

	ctx := metadata.NewOutgoingContext(context.Background(),
		metadata.Pairs("x-session-token", "not-the-right-token"))

	_, err := client.CallObject(ctx, &gate_pb.CallObjectRequest{
		ClientId: clientID,
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

// TestGateServerRegister_NoAuth_NoSessionToken verifies that when AuthValidator
// is nil, RegisterResponse has an empty session_token (backward compatible).
func TestGateServerRegister_NoAuth_NoSessionToken(t *testing.T) {
	t.Parallel()
	listenAddr := testutil.GetFreeAddress()
	cfg := &GateServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-noauth-token",
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

	client := gate_pb.NewGateServiceClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.Register(ctx, &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	anyMsg, err := stream.Recv()
	if err != nil && err != io.EOF {
		t.Fatalf("Recv: %v", err)
	}
	if anyMsg == nil {
		t.Skip("no RegisterResponse received (gate not fully connected)")
	}
	var regResp gate_pb.RegisterResponse
	if err := proto.Unmarshal(anyMsg.Value, &regResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if regResp.SessionToken != "" {
		t.Errorf("expected empty session_token when auth is disabled, got %q", regResp.SessionToken)
	}
}
