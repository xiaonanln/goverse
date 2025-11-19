package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGateNodeCreateObjectIntegration tests creating an object via a gateway cluster
// and verifying it's created on the node cluster.
// This test requires a running etcd instance at localhost:2379
func TestGateNodeCreateObjectIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster
	nodeAddr := "localhost:47100"
	n := node.NewNode(nodeAddr)
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	t.Cleanup(func() { n.Stop(ctx) })

	// Register a simple test object type
	n.RegisterObjectType((*TestGateNodeObject)(nil))

	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
	}

	nodeCluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = nodeCluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start node cluster: %v", err)
	}
	t.Cleanup(func() { nodeCluster.Stop(ctx) })

	// Start mock gRPC server for the node
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(n)
	testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
	err = testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	// Wait for server to be ready
	time.Sleep(500 * time.Millisecond)

	// Create a gateway cluster
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49100",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	gateCfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
	}

	gateCluster, err := NewClusterWithGate(gateCfg, gw)
	if err != nil {
		t.Fatalf("Failed to create gate cluster: %v", err)
	}

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	t.Cleanup(func() { gateCluster.Stop(ctx) })

	// Wait for clusters to discover each other and shard mapping to initialize
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create an object via the gateway cluster
	objID := "test-gate-obj-1"
	t.Logf("Creating object %s via gateway cluster", objID)

	// Get the target node for this object
	targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
	if err != nil {
		t.Fatalf("GetCurrentNodeForObject failed: %v", err)
	}
	t.Logf("Target node for object %s is %s", objID, targetNode)

	// Create the object from the gateway
	createdID, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	if createdID != objID {
		t.Fatalf("Expected object ID %s, got %s", objID, createdID)
	}

	// Wait for async object creation to complete
	t.Logf("Waiting for object to be created on node...")
	deadline := time.Now().Add(5 * time.Second)
	objectExists := false
	for {
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				objectExists = true
				break
			}
		}
		if objectExists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not created on node within 5 seconds", objID)
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Logf("Successfully verified object %s exists on node %s", objID, targetNode)

	// Verify the object is of the correct type
	foundObject := false
	for _, obj := range n.ListObjects() {
		if obj.Id == objID && obj.Type == "TestGateNodeObject" {
			foundObject = true
			break
		}
	}

	if !foundObject {
		t.Fatalf("Object %s with type TestGateNodeObject not found on node", objID)
	}

	t.Logf("Successfully created object %s via gateway and verified on node", objID)
}

// TestGateNodeObject is a simple test object for testing gateway-node integration
type TestGateNodeObject struct {
	object.BaseObject
}

func (o *TestGateNodeObject) OnCreated() {}
