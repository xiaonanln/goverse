package gateserver

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/goverseapi/authjwt"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

var jwtTestKey = []byte("gate-auth-integration-test-secret")

func makeToken(key []byte, sub string, roles []string, expiry time.Time) string {
	claims := jwt.MapClaims{
		"sub": sub,
		"exp": expiry.Unix(),
	}
	if len(roles) > 0 {
		claims["roles"] = roles
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	return tok
}

// TestGateAuth_ValidJWT verifies the end-to-end JWT path:
//  1. Gate is wired with authjwt.New(HS256 key).
//  2. Client sends Authorization: Bearer <valid-token> on Register.
//  3. CallerIdentity (sub + roles) flows through the gate → node → object method.
func TestGateAuth_ValidJWT(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// --- Node side ---
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	testNode.RegisterObjectType((*callerCapture)(nil))

	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeHelper := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	if err := nodeHelper.Start(ctx); err != nil {
		t.Fatalf("node mock server Start: %v", err)
	}
	t.Cleanup(func() { nodeHelper.Stop() })

	// --- Gate wired with JWT validator ---
	gateAddr := testutil.GetFreeAddress()
	gateServer, err := NewGateServer(&GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
		AuthValidator:    authjwt.New(authjwt.Options{SigningKey: jwtTestKey}),
	})
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	t.Cleanup(func() { gateServer.Stop() })
	if err := gateServer.Start(ctx); err != nil {
		t.Fatalf("gateServer.Start: %v", err)
	}

	testutil.WaitForClusterReady(t, nodeCluster)

	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	gateClient := gate_pb.NewGateServiceClient(conn)

	// --- Register with a valid JWT ---
	token := makeToken(jwtTestKey, "alice", []string{"admin", "viewer"}, time.Now().Add(time.Hour))
	regMD := metadata.Pairs("authorization", "Bearer "+token)
	regCtx := metadata.NewOutgoingContext(ctx, regMD)
	stream, err := gateClient.Register(regCtx, &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	firstMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Register Recv: %v", err)
	}
	regResp := &gate_pb.RegisterResponse{}
	if err := firstMsg.UnmarshalTo(regResp); err != nil {
		t.Fatalf("UnmarshalTo RegisterResponse: %v", err)
	}
	clientID := regResp.ClientId

	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				return
			}
		}
	}()

	// --- Create and call object ---
	objID := "gate-auth-jwt-integration-1"
	createDeadline := time.Now().Add(30 * time.Second)
	for {
		_, err = gateClient.CreateObject(ctx, &gate_pb.CreateObjectRequest{
			Type: "callerCapture",
			Id:   objID,
		})
		if err == nil {
			break
		}
		if time.Now().After(createDeadline) {
			t.Fatalf("CreateObject still failing after 30s: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	waitForObjectCreatedOnNode(t, testNode, objID, 10*time.Second)

	reqAny, err := protohelper.MsgToAny(&structpb.Struct{})
	if err != nil {
		t.Fatalf("protohelper.MsgToAny: %v", err)
	}

	var callResp *gate_pb.CallObjectResponse
	deadline := time.Now().Add(30 * time.Second)
	for {
		callResp, err = gateClient.CallObject(ctx, &gate_pb.CallObjectRequest{
			ClientId: clientID,
			Type:     "callerCapture",
			Id:       objID,
			Method:   "Capture",
			Request:  reqAny,
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CallObject still failing after 30s: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	result := &structpb.Struct{}
	if err := callResp.Response.UnmarshalTo(result); err != nil {
		t.Fatalf("UnmarshalTo response: %v", err)
	}
	if !result.Fields["authenticated"].GetBoolValue() {
		t.Fatal("expected authenticated=true — CallerIdentity was nil in object method")
	}
	if got := result.Fields["userID"].GetStringValue(); got != "alice" {
		t.Errorf("userID: want %q, got %q", "alice", got)
	}
	gotRoles := result.Fields["roles"].GetListValue().GetValues()
	if len(gotRoles) != 2 {
		t.Fatalf("roles len: want 2, got %d", len(gotRoles))
	}
	for i, want := range []string{"admin", "viewer"} {
		if got := gotRoles[i].GetStringValue(); got != want {
			t.Errorf("roles[%d]: want %q, got %q", i, want, got)
		}
	}
}

// TestGateAuth_ExpiredJWT verifies that an expired token is rejected at Register
// with codes.Unauthenticated.
func TestGateAuth_ExpiredJWT(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	gateAddr := testutil.GetFreeAddress()
	gateServer, err := NewGateServer(&GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
		AuthValidator:    authjwt.New(authjwt.Options{SigningKey: jwtTestKey}),
	})
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	t.Cleanup(func() { gateServer.Stop() })
	if err := gateServer.Start(ctx); err != nil {
		t.Fatalf("gateServer.Start: %v", err)
	}

	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	gateClient := gate_pb.NewGateServiceClient(conn)

	expiredToken := makeToken(jwtTestKey, "alice", nil, time.Now().Add(-time.Hour))
	regMD := metadata.Pairs("authorization", "Bearer "+expiredToken)
	regCtx := metadata.NewOutgoingContext(ctx, regMD)
	stream, err := gateClient.Register(regCtx, &gate_pb.Empty{})
	if err != nil {
		// gRPC may reject immediately
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unauthenticated {
			return
		}
		t.Fatalf("Register unexpected error: %v", err)
	}
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if s, ok := status.FromError(err); ok {
		if s.Code() != codes.Unauthenticated {
			t.Errorf("want codes.Unauthenticated, got %v", s.Code())
		}
	}
}

// TestGateAuth_MissingToken verifies that a connection with no authorization
// header is rejected with codes.Unauthenticated.
func TestGateAuth_MissingToken(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	gateAddr := testutil.GetFreeAddress()
	gateServer, err := NewGateServer(&GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
		AuthValidator:    authjwt.New(authjwt.Options{SigningKey: jwtTestKey}),
	})
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	t.Cleanup(func() { gateServer.Stop() })
	if err := gateServer.Start(ctx); err != nil {
		t.Fatalf("gateServer.Start: %v", err)
	}

	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	gateClient := gate_pb.NewGateServiceClient(conn)

	// Register with no auth metadata
	stream, err := gateClient.Register(ctx, &gate_pb.Empty{})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unauthenticated {
			return
		}
		t.Fatalf("Register unexpected error: %v", err)
	}
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
	if s, ok := status.FromError(err); ok {
		if s.Code() != codes.Unauthenticated {
			t.Errorf("want codes.Unauthenticated, got %v", s.Code())
		}
	}
}
