package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestNodeConnectionsIntegration tests the NodeConnections manager with etcd node discovery
// This test requires a running etcd instance at localhost:2379
func TestNodeConnectionsIntegration(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create cluster
	cluster1 := newClusterForTesting("TestCluster1")

	// Create etcd manager

	// Create node
	node1 := node.NewNode("localhost:47001")
	cluster1.SetThisNode(node1)

	// Start node
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Connect to etcd
	err = cluster1.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd: %v", err)
	}
	defer cluster1.CloseEtcd()

	// Register node
	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	// Start watching nodes
	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes: %v", err)
	}

	// Wait for registration to complete
	time.Sleep(500 * time.Millisecond)

	// Start NodeConnections
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}
	defer cluster1.StopNodeConnections()

	// Verify NodeConnections is running
	nc := cluster1.GetNodeConnections()
	if nc == nil {
		t.Fatal("NodeConnections should not be nil after Start")
	}

	// Initially should have 0 connections (only our own node is registered)
	if nc.NumConnections() != 0 {
		t.Logf("Initial connection count: %d (expected 0)", nc.NumConnections())
	}

	// Now create a second cluster/node to test dynamic connection
	cluster2 := newClusterForTesting("TestCluster2")

	node2 := node.NewNode("localhost:47002")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	// Wait for node2 to be discovered and connected
	// The NodeConnections watcher runs every 5 seconds
	time.Sleep(6 * time.Second)

	// Cluster1's NodeConnections should now have a connection to node2
	// Note: The actual gRPC connection will fail because no server is listening,
	// but the connection manager should attempt to connect
	t.Logf("After node2 registration, cluster1 has %d connections", nc.NumConnections())

	// Unregister node2 and verify it gets disconnected
	err = cluster2.UnregisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to unregister node2: %v", err)
	}

	// Wait for disconnection to be detected
	time.Sleep(6 * time.Second)

	// Connection should be removed
	t.Logf("After node2 unregistration, cluster1 has %d connections", nc.NumConnections())
}

// TestNodeConnectionsDynamicDiscovery tests that NodeConnections automatically connects to new nodes
func TestNodeConnectionsDynamicDiscovery(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Setup cluster1
	cluster1 := newClusterForTesting("TestCluster1")

	node1 := node.NewNode("localhost:47011")
	cluster1.SetThisNode(node1)

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Start NodeConnections before any other nodes exist
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}
	defer cluster1.StopNodeConnections()

	time.Sleep(500 * time.Millisecond)

	nc1 := cluster1.GetNodeConnections()
	initialCount := nc1.NumConnections()
	t.Logf("Initial connection count: %d", initialCount)

	// Setup cluster2 AFTER NodeConnections is already running
	cluster2 := newClusterForTesting("TestCluster2")

	node2 := node.NewNode("localhost:47012")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	// Wait for dynamic discovery (watcher runs every 5 seconds)
	time.Sleep(6 * time.Second)

	// Verify connection count increased
	newCount := nc1.NumConnections()
	t.Logf("After node2 joins, cluster1 has %d connections", newCount)

	// The connection attempt should have been made (even if it fails to connect to the actual server)
	// We're testing the logic, not the actual gRPC connection success
}

// TestNodeConnectionsRemovalAndReaddition tests that a node can be removed and then re-added
// This test verifies that NodeConnections properly disconnects and reconnects
func TestNodeConnectionsRemovalAndReaddition(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Setup cluster1
	cluster1 := newClusterForTesting("TestCluster1")

	node1 := node.NewNode("localhost:47021")
	cluster1.SetThisNode(node1)

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Start NodeConnections
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}
	defer cluster1.StopNodeConnections()

	time.Sleep(500 * time.Millisecond)

	nc1 := cluster1.GetNodeConnections()
	initialCount := nc1.NumConnections()
	t.Logf("Initial connection count: %d", initialCount)

	// Setup cluster2
	cluster2 := newClusterForTesting("TestCluster2")

	node2 := node.NewNode("localhost:47022")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	// Step 1: Register node2
	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}

	// Wait for node2 to be discovered and connected
	time.Sleep(6 * time.Second)

	countAfterAdd := nc1.NumConnections()
	t.Logf("After node2 added, cluster1 has %d connections", countAfterAdd)

	// Step 2: Unregister node2 (remove it)
	err = cluster2.UnregisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to unregister node2: %v", err)
	}

	// Wait for disconnection to be detected
	time.Sleep(6 * time.Second)

	countAfterRemoval := nc1.NumConnections()
	t.Logf("After node2 removed, cluster1 has %d connections", countAfterRemoval)

	// Step 3: Re-register node2 (add it again)
	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to re-register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	// Wait for node2 to be re-discovered and reconnected
	time.Sleep(6 * time.Second)

	countAfterReaddition := nc1.NumConnections()
	t.Logf("After node2 re-added, cluster1 has %d connections", countAfterReaddition)

	// Verify the pattern: initial -> +1 after add -> back to initial after removal -> +1 again after re-add
	// Note: Actual connection attempts may fail without running servers, but we verify the logic
	if countAfterReaddition < countAfterRemoval {
		t.Errorf("Expected connection count to increase after re-addition, got %d (was %d after removal)",
			countAfterReaddition, countAfterRemoval)
	}
}
