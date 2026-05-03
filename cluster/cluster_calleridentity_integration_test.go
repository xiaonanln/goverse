package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/structpb"
)

// CallerCaptureObject is a test object that reads CallerIdentity from its
// invocation context and returns it in the response so the test can verify
// end-to-end propagation.
type CallerCaptureObject struct {
	object.BaseObject
}

func (o *CallerCaptureObject) OnCreated() {}

func (o *CallerCaptureObject) Capture(ctx context.Context, _ *structpb.Struct) (*structpb.Struct, error) {
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

// assertCallerCapture verifies that the structpb response from CallerCaptureObject.Capture
// contains the expected CallerIdentity.
func assertCallerCapture(t *testing.T, resp interface{}, wantUserID string, wantRoles []string) {
	t.Helper()
	s, ok := resp.(*structpb.Struct)
	if !ok {
		t.Fatalf("expected *structpb.Struct response, got %T", resp)
	}
	if auth := s.Fields["authenticated"].GetBoolValue(); !auth {
		t.Fatal("expected authenticated=true in response")
	}
	if got := s.Fields["userID"].GetStringValue(); got != wantUserID {
		t.Errorf("userID: want %q, got %q", wantUserID, got)
	}
	gotRoles := s.Fields["roles"].GetListValue().GetValues()
	if len(gotRoles) != len(wantRoles) {
		t.Fatalf("roles len: want %d, got %d", len(wantRoles), len(gotRoles))
	}
	for i, r := range wantRoles {
		if got := gotRoles[i].GetStringValue(); got != r {
			t.Errorf("roles[%d]: want %q, got %q", i, r, got)
		}
	}
}

// TestCallerIdentity_GateToNode verifies that a CallerIdentity injected into a
// gate-cluster context by the gate (after AuthValidator succeeds) is propagated
// through the gRPC node boundary and arrives intact inside the object method.
func TestCallerIdentity_GateToNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Gate cluster
	gateAddr := testutil.GetFreeAddress()
	gw, err := gate.NewGate(&gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	})
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	gateCluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	if err := gateCluster.Start(ctx, nil); err != nil {
		t.Fatalf("gateCluster.Start: %v", err)
	}
	t.Cleanup(func() { gateCluster.Stop(ctx); gw.Stop() })

	// Node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	testNode := nodeCluster.GetThisNode()
	testNode.RegisterObjectType((*CallerCaptureObject)(nil))

	// Start mock gRPC server for the node
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(testNode)
	testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
	if err := testServer.Start(ctx); err != nil {
		t.Fatalf("testServer.Start: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	testutil.WaitForClustersReadyWithoutGateConnections(t, nodeCluster, gateCluster)

	// Create object on the node
	objID := "calleridentity-gate-to-node-1"
	if _, err := gateCluster.CreateObject(ctx, "CallerCaptureObject", objID); err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	waitForObjectCreatedOnNode(t, testNode, objID, 10*time.Second)

	// Call via gate cluster with CallerIdentity in context (simulates gate injecting
	// the identity after a successful AuthValidator.Validate call)
	identity := &callcontext.CallerIdentity{UserID: "alice", Roles: []string{"admin", "viewer"}}
	callCtx := callcontext.WithCallerIdentity(ctx, identity)

	resp, err := gateCluster.CallObject(callCtx, "CallerCaptureObject", objID, "Capture", &structpb.Struct{})
	if err != nil {
		t.Fatalf("CallObject: %v", err)
	}

	assertCallerCapture(t, resp, "alice", []string{"admin", "viewer"})
}

// TestCallerIdentity_CrossNode verifies that CallerIdentity propagates correctly
// across a second gRPC hop — i.e., when a request is routed from one node's
// cluster to a different node in the cluster. This mirrors the production path
// where a gate or nodeA calls an object that happens to live on nodeB.
func TestCallerIdentity_CrossNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Two node clusters
	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	cluster1 := mustNewCluster(ctx, t, addr1, testPrefix)
	cluster2 := mustNewCluster(ctx, t, addr2, testPrefix)

	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.RegisterObjectType((*CallerCaptureObject)(nil))
	node2.RegisterObjectType((*CallerCaptureObject)(nil))

	// Start mock gRPC servers for both nodes
	for _, pair := range []struct {
		addr string
		node *node.Node
	}{
		{addr1, node1},
		{addr2, node2},
	} {
		ms := testutil.NewMockGoverseServer()
		ms.SetNode(pair.node)
		ts := testutil.NewTestServerHelper(pair.addr, ms)
		if err := ts.Start(ctx); err != nil {
			t.Fatalf("testServer.Start(%s): %v", pair.addr, err)
		}
		t.Cleanup(func() { ts.Stop() })
	}

	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Find an object ID that the shard mapping routes to cluster2/node2.
	// We iterate candidates until GetCurrentNodeForObject returns addr2.
	var objIDOnNode2 string
	for i := 0; i < 200; i++ {
		candidate := fmt.Sprintf("CallerCaptureObject-crossnode-%d", i)
		targetNode, err := cluster1.GetCurrentNodeForObject(ctx, candidate)
		if err != nil {
			continue
		}
		if targetNode == addr2 {
			objIDOnNode2 = candidate
			break
		}
	}
	if objIDOnNode2 == "" {
		t.Skip("could not find an object ID routed to node2 — shard distribution unexpected")
	}

	// Create the object; it lands on node2 per the shard mapping
	if _, err := cluster1.CreateObject(ctx, "CallerCaptureObject", objIDOnNode2); err != nil {
		t.Fatalf("CreateObject on node2: %v", err)
	}
	waitForObjectCreated(t, node2, objIDOnNode2, 10*time.Second)

	// Call from cluster1 with CallerIdentity. cluster1.CallObject will:
	//   1. Call InjectCallerToOutgoing → write identity to outgoing gRPC metadata
	//   2. Route the call to node2 (cross-node gRPC hop)
	//   3. node2's MockGoverseServer calls ExtractCallerFromIncoming → restores identity
	//   4. node2.CallObject dispatches to CallerCaptureObject.Capture with the restored ctx
	identity := &callcontext.CallerIdentity{UserID: "bob", Roles: []string{"editor"}}
	callCtx := callcontext.WithCallerIdentity(ctx, identity)

	resp, err := cluster1.CallObject(callCtx, "CallerCaptureObject", objIDOnNode2, "Capture", &structpb.Struct{})
	if err != nil {
		t.Fatalf("CrossNode CallObject: %v", err)
	}

	assertCallerCapture(t, resp, "bob", []string{"editor"})
}

// TestReliableCallerIdentity_GateToNode mirrors TestCallerIdentity_GateToNode
// but exercises the ReliableCallObject path, which had a missing
// InjectCallerToOutgoing call in cluster.go before this fix.
func TestReliableCallerIdentity_GateToNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Gate cluster
	gateAddr := testutil.GetFreeAddress()
	gw, err := gate.NewGate(&gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	})
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	gateCluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	if err := gateCluster.Start(ctx, nil); err != nil {
		t.Fatalf("gateCluster.Start: %v", err)
	}
	t.Cleanup(func() { gateCluster.Stop(ctx); gw.Stop() })

	// Node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	testNode := nodeCluster.GetThisNode()
	testNode.RegisterObjectType((*CallerCaptureObject)(nil))

	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(testNode)
	testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
	if err := testServer.Start(ctx); err != nil {
		t.Fatalf("testServer.Start: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	testutil.WaitForClustersReadyWithoutGateConnections(t, nodeCluster, gateCluster)

	objID := "reliable-calleridentity-gate-to-node-1"
	if _, err := gateCluster.CreateObject(ctx, "CallerCaptureObject", objID); err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	waitForObjectCreatedOnNode(t, testNode, objID, 10*time.Second)

	identity := &callcontext.CallerIdentity{UserID: "carol", Roles: []string{"player"}}
	callCtx := callcontext.WithCallerIdentity(ctx, identity)

	resp, _, err := gateCluster.ReliableCallObject(callCtx, "gate-to-node-reliable-call-1", "CallerCaptureObject", objID, "Capture", &structpb.Struct{})
	if err != nil {
		t.Fatalf("ReliableCallObject: %v", err)
	}

	assertCallerCapture(t, resp, "carol", []string{"player"})
}

// TestReliableCallerIdentity_CrossNode mirrors TestCallerIdentity_CrossNode
// but exercises the ReliableCallObject path, which had a missing
// InjectCallerToOutgoing call in cluster.go before this fix.
func TestReliableCallerIdentity_CrossNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	cluster1 := mustNewCluster(ctx, t, addr1, testPrefix)
	cluster2 := mustNewCluster(ctx, t, addr2, testPrefix)

	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.RegisterObjectType((*CallerCaptureObject)(nil))
	node2.RegisterObjectType((*CallerCaptureObject)(nil))

	for _, pair := range []struct {
		addr string
		node *node.Node
	}{
		{addr1, node1},
		{addr2, node2},
	} {
		ms := testutil.NewMockGoverseServer()
		ms.SetNode(pair.node)
		ts := testutil.NewTestServerHelper(pair.addr, ms)
		if err := ts.Start(ctx); err != nil {
			t.Fatalf("testServer.Start(%s): %v", pair.addr, err)
		}
		t.Cleanup(func() { ts.Stop() })
	}

	testutil.WaitForClustersReady(t, cluster1, cluster2)

	var objIDOnNode2 string
	for i := 0; i < 200; i++ {
		candidate := fmt.Sprintf("CallerCaptureObject-reliable-crossnode-%d", i)
		targetNode, err := cluster1.GetCurrentNodeForObject(ctx, candidate)
		if err != nil {
			continue
		}
		if targetNode == addr2 {
			objIDOnNode2 = candidate
			break
		}
	}
	if objIDOnNode2 == "" {
		t.Skip("could not find an object ID routed to node2 — shard distribution unexpected")
	}

	if _, err := cluster1.CreateObject(ctx, "CallerCaptureObject", objIDOnNode2); err != nil {
		t.Fatalf("CreateObject on node2: %v", err)
	}
	waitForObjectCreated(t, node2, objIDOnNode2, 10*time.Second)

	identity := &callcontext.CallerIdentity{UserID: "dave", Roles: []string{"admin"}}
	callCtx := callcontext.WithCallerIdentity(ctx, identity)

	resp, _, err := cluster1.ReliableCallObject(callCtx, "crossnode-reliable-call-1", "CallerCaptureObject", objIDOnNode2, "Capture", &structpb.Struct{})
	if err != nil {
		t.Fatalf("CrossNode ReliableCallObject: %v", err)
	}

	assertCallerCapture(t, resp, "dave", []string{"admin"})
}
