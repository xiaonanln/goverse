package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// waitForObjectCreatedOnNode waits for an object to be created on the specified node
func waitForObjectCreatedOnNode(t *testing.T, n *node.Node, objID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not created on node within %v", objID, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestGateNodeObject is a simple test object
type TestGateNodeObject struct {
	object.BaseObject
}

func (o *TestGateNodeObject) OnCreated() {}

// TestGateNodeIntegration tests creating an object via a gate cluster
// and verifying it is created on a node cluster
// This test requires a running etcd instance at localhost:2379
func TestGateNodeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create gate cluster
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49001",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	// Create gate cluster with test values
	gateCluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	// Start the gate cluster
	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	t.Cleanup(func() {
		gateCluster.Stop(ctx)
		gw.Stop()
	})
	t.Logf("Created gate cluster at localhost:49001")

	// Create node cluster using mustNewCluster
	nodeCluster := mustNewCluster(ctx, t, "localhost:47001", testPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at localhost:47001")

	// Register test object type on the node
	testNode.RegisterObjectType((*TestGateNodeObject)(nil))

	// Wait for clusters to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC server for the node to handle inter-cluster communication
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(testNode) // Assign the actual node to the mock server
	testServer := testutil.NewTestServerHelper("localhost:47001", mockServer)
	err = testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Wait for shard mapping to be initialized
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Verify shard mapping is ready
	_ = gateCluster.GetShardMapping(ctx)

	t.Run("CreateObjectViaGate", func(t *testing.T) {
		// Test creating an object through the gate cluster
		objID := "test-gate-object-1"

		// Get the target node for this object
		targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
		}
		t.Logf("Creating object %s from gate, expect target node %s", objID, targetNode)

		// Create the object via the gate cluster - it will be routed to the node
		createdID, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		if createdID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID)
		}

		// Verify the object was created on the node
		if targetNode != "localhost:47001" {
			t.Fatalf("Expected target node localhost:47001, got %s", targetNode)
		}

		// Wait for async object creation to complete
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully created and verified object %s on node %s", objID, targetNode)
	})

	t.Run("CreateMultipleObjectsViaGate", func(t *testing.T) {
		// Test creating multiple objects through the gate cluster
		for i := 1; i <= 5; i++ {
			objID := fmt.Sprintf("test-gate-object-%d", i+1)

			// Get the target node for this object
			targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
			if err != nil {
				t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
			}
			t.Logf("Creating object %s from gate, expect target node %s", objID, targetNode)

			// Create the object via the gate cluster
			createdID, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
			if err != nil {
				t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
			}

			if createdID != objID {
				t.Fatalf("Expected object ID %s, got %s", objID, createdID)
			}

			// Verify the object was created on the correct node
			if targetNode != "localhost:47001" {
				t.Fatalf("Expected target node localhost:47001, got %s", targetNode)
			}

			// Wait for async object creation to complete
			waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

			t.Logf("Successfully created and verified object %s on node %s", objID, targetNode)
		}
	})
}
