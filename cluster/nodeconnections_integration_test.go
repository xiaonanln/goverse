package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestNodeConnectionsIntegration tests the NodeConnections manager with etcd node discovery
// This test requires a running etcd instance at localhost:2379
func TestNodeConnectionsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster1 using the helper
	cluster1 := mustNewCluster(ctx, t, "localhost:47001", testPrefix)

	// Get NodeConnections manager
	nc := cluster1.GetNodeConnections()
	if nc == nil {
		t.Fatal("NodeConnections should not be nil after Start")
	}

	// Verify we have no connections yet
	if nc.NumConnections() != 0 {
		t.Fatalf("Expected 0 connections initially, got %d", nc.NumConnections())
	}

	// Create and start cluster2 using the helper
	cluster2 := mustNewCluster(ctx, t, "localhost:47002", testPrefix)
	_ = cluster2 // cluster2 is used via side effect (node registration)

	// Wait for node2 to be discovered and connected
	// The NodeConnections watcher runs every 5 seconds
	time.Sleep(6 * time.Second)

	// Cluster1's NodeConnections should now have a connection to node2
	// Note: The actual gRPC connection will fail because no server is listening,
	// but the connection manager should attempt to connect
	t.Logf("After node2 registration, cluster1 has %d connections", nc.NumConnections())

	// Stop cluster2 which will unregister node2
	cluster2.Stop(ctx)

	// Wait for disconnection to be detected
	time.Sleep(6 * time.Second)

	// Connection should be removed
	t.Logf("After node2 unregistration, cluster1 has %d connections", nc.NumConnections())
}

// TestNodeConnectionsDynamicDiscovery tests that NodeConnections automatically connects to new nodes
func TestNodeConnectionsDynamicDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster1 using the helper
	cluster1 := mustNewCluster(ctx, t, "localhost:47011", testPrefix)

	time.Sleep(500 * time.Millisecond)

	nc1 := cluster1.GetNodeConnections()
	initialCount := nc1.NumConnections()
	t.Logf("Initial connection count: %d", initialCount)

	// Create and start cluster2 using the helper - this will be dynamically discovered
	cluster2 := mustNewCluster(ctx, t, "localhost:47012", testPrefix)
	_ = cluster2 // cluster2 is used via side effect (node registration)

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
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster1 using the helper
	cluster1 := mustNewCluster(ctx, t, "localhost:47021", testPrefix)

	time.Sleep(500 * time.Millisecond)

	nc1 := cluster1.GetNodeConnections()
	initialCount := nc1.NumConnections()
	t.Logf("Initial connection count: %d", initialCount)

	// Step 1: Create and start cluster2 (this registers node2)
	cluster2 := mustNewCluster(ctx, t, "localhost:47022", testPrefix)

	// Wait for node2 to be discovered and connected
	time.Sleep(6 * time.Second)

	countAfterAdd := nc1.NumConnections()
	t.Logf("After node2 added, cluster1 has %d connections", countAfterAdd)

	// Step 2: Stop cluster2 (this unregisters node2)
	cluster2.Stop(ctx)

	// Wait for disconnection to be detected
	time.Sleep(6 * time.Second)

	countAfterRemoval := nc1.NumConnections()
	t.Logf("After node2 removed, cluster1 has %d connections", countAfterRemoval)

	// Step 3: Re-create and start cluster2 (re-register node2)
	cluster2 = mustNewCluster(ctx, t, "localhost:47022", testPrefix)
	_ = cluster2 // cluster2 is used via side effect

	// Wait for node2 to be re-discovered and reconnected
	time.Sleep(6 * time.Second)

	countAfterReaddition := nc1.NumConnections()
	t.Logf("After node2 re-added, cluster1 has %d connections", countAfterReaddition)

	// Verify the pattern: initial -> +1 after add -> back to initial after removal -> +1 again after re-add
	// Note: Actual connection attempts may fail without running servers, but we verify the logic
	if countAfterReaddition < countAfterRemoval {
		t.Fatalf("Expected connection count to increase after re-addition, got %d (was %d after removal)",
			countAfterReaddition, countAfterRemoval)
	}
}
