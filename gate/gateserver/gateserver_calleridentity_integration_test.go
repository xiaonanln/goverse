package gateserver

import (
	"context"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// callerCapture is a test object that reads CallerIdentity from its invocation
// context and returns UserID + Roles in the response.
type callerCapture struct {
	object.BaseObject
}

func (o *callerCapture) OnCreated() {}

func (o *callerCapture) Capture(ctx context.Context, _ *structpb.Struct) (*structpb.Struct, error) {
	id := callcontext.GetCallerIdentity(ctx)
	if id == nil {
		return &structpb.Struct{Fields: map[string]*structpb.Value{
			"authenticated": structpb.NewBoolValue(false),
		}}, nil
	}
	roleValues := make([]*structpb.Value, len(id.Roles))
	for i, r := range id.Roles {
		roleValues[i] = structpb.NewStringValue(r)
	}
	return &structpb.Struct{Fields: map[string]*structpb.Value{
		"authenticated": structpb.NewBoolValue(true),
		"userID":        structpb.NewStringValue(id.UserID),
		"roles":         structpb.NewListValue(&structpb.ListValue{Values: roleValues}),
	}}, nil
}

// TestCallerIdentity_FullStack exercises the complete production auth →
// propagation chain through real GateServer and node (MockGoverseServer):
//
//  1. GateServer.Register: AuthValidator fires, clientID → CallerIdentity stored
//  2. GateServer.CallObject: looks up CallerIdentity by clientID, injects into ctx
//  3. cluster.CallObject: InjectCallerToOutgoing writes identity to gRPC metadata
//  4. MockGoverseServer.CallObject: ExtractCallerFromIncoming restores identity
//  5. Object method: receives full CallerIdentity (UserID + Roles)
func TestCallerIdentity_FullStack(t *testing.T) {
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

	// --- Gate side ---
	gateAddr := testutil.GetFreeAddress()
	identity := &callcontext.CallerIdentity{UserID: "alice", Roles: []string{"admin", "viewer"}}
	gateServer, err := NewGateServer(&GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
		AuthValidator:    &stubAuthValidator{identity: identity},
	})
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	t.Cleanup(func() { gateServer.Stop() })
	if err := gateServer.Start(ctx); err != nil {
		t.Fatalf("gateServer.Start: %v", err)
	}

	// Wait for the node cluster to be ready (shard mapping stable)
	testutil.WaitForClusterReady(t, nodeCluster)

	// gRPC client to the gate
	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	gateClient := gate_pb.NewGateServiceClient(conn)

	// --- Register: send auth metadata, receive clientID ---
	// The AuthValidator ignores the actual headers (stubbed to always return the
	// fixed identity). Real apps would send e.g. "authorization: Bearer <token>".
	regMD := metadata.Pairs("x-username", "alice", "x-password", "secret")
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

	// Keep the Register stream alive for the duration of the test
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				return
			}
		}
	}()

	// --- Create object via gate ---
	// Retry until the gate cluster has a stable shard mapping; without this,
	// CreateObject can fail with "shard mapping not available" if the gate's
	// internal cluster hasn't finished loading shard state yet.
	objID := "calleridentity-fullstack-1"
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
			t.Fatalf("CreateObject via gate still failing after 30s: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	waitForObjectCreatedOnNode(t, testNode, objID, 10*time.Second)

	// --- CallObject via gate with clientID ---
	// The gate looks up CallerIdentity for clientID, injects it, and routes to
	// the node. The node's MockGoverseServer (fixed in PR #558) calls
	// ExtractCallerFromIncoming so the object method receives the full identity.
	reqAny, err := protohelper.MsgToAny(&structpb.Struct{})
	if err != nil {
		t.Fatalf("protohelper.MsgToAny: %v", err)
	}

	// Retry until the gate cluster has routed to the node (shard mapping stable)
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

	// --- Verify CallerIdentity arrived at the object method ---
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
	wantRoles := []string{"admin", "viewer"}
	for i, r := range wantRoles {
		if got := gotRoles[i].GetStringValue(); got != r {
			t.Errorf("roles[%d]: want %q, got %q", i, r, got)
		}
	}
}
