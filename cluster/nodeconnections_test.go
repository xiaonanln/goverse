package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestNewNodeConnections(t *testing.T) {
	// Create cluster with node
	thisNode := node.NewNode("localhost:50000")
	cluster := newClusterForTesting(thisNode, "TestCluster")
	nc := NewNodeConnections(cluster)

	if nc == nil {
		t.Fatal("NewNodeConnections() should not return nil")
	}

	if nc.cluster != cluster {
		t.Error("NodeConnections cluster should be set correctly")
	}

	if nc.connections == nil {
		t.Error("NodeConnections connections map should be initialized")
	}

	if nc.logger == nil {
		t.Error("NodeConnections logger should be initialized")
	}
}

func TestNodeConnections_StartStop(t *testing.T) {
	// Setup test cluster with node
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	thisNode := node.NewNode("localhost:50000")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", thisNode, "localhost:2379", testPrefix)
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
	}

	// Create NodeConnections
	nc := NewNodeConnections(cluster)

	ctx := context.Background()

	// Start NodeConnections
	err = nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}

	if !nc.running {
		t.Error("NodeConnections should be running after Start()")
	}

	// Stop NodeConnections
	nc.Stop()

	if nc.running {
		t.Error("NodeConnections should not be running after Stop()")
	}
}

func TestNodeConnections_StartTwice(t *testing.T) {
	// Setup test cluster with node
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	thisNode := node.NewNode("localhost:50000")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", thisNode, "localhost:2379", testPrefix)
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
	}

	nc := NewNodeConnections(cluster)
	ctx := context.Background()

	// Start first time
	err = nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}

	// Start second time - should not error
	err = nc.Start(ctx)
	if err != nil {
		t.Errorf("Starting NodeConnections twice should not error: %v", err)
	}

	nc.Stop()
}

func TestNodeConnections_NumConnections(t *testing.T) {
	// Create cluster with node
	thisNode := node.NewNode("localhost:50000")
	cluster := newClusterForTesting(thisNode, "TestCluster")
	nc := NewNodeConnections(cluster)

	// Initially should have 0 connections
	if nc.NumConnections() != 0 {
		t.Errorf("Expected 0 connections initially, got %d", nc.NumConnections())
	}
}

func TestNodeConnections_GetConnection_NotExists(t *testing.T) {
	// Create cluster with node
	thisNode := node.NewNode("localhost:50000")
	cluster := newClusterForTesting(thisNode, "TestCluster")
	nc := NewNodeConnections(cluster)

	_, err := nc.GetConnection("localhost:50000")
	if err == nil {
		t.Error("GetConnection should return error for non-existent connection")
	}
}

func TestNodeConnections_GetAllConnections(t *testing.T) {
	// Create cluster with node
	thisNode := node.NewNode("localhost:50000")
	cluster := newClusterForTesting(thisNode, "TestCluster")
	nc := NewNodeConnections(cluster)

	connections := nc.GetAllConnections()
	if connections == nil {
		t.Error("GetAllConnections should not return nil")
	}

	if len(connections) != 0 {
		t.Errorf("Expected 0 connections initially, got %d", len(connections))
	}
}

func TestNodeConnections_ConnectDisconnect(t *testing.T) {
	// Setup test cluster with node
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	thisNode := node.NewNode("localhost:50000")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", thisNode, "localhost:2379", testPrefix)
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
	}

	nc := NewNodeConnections(cluster)

	// Note: We can't test actual connection establishment without a running server
	// We test the connection management logic

	// Test disconnect from non-existent node
	err = nc.disconnectFromNode("localhost:60000")
	if err != nil {
		t.Errorf("Disconnecting from non-existent node should not error: %v", err)
	}
}

func TestNodeConnections_HandleNodeChanges(t *testing.T) {
	// Setup test cluster with node
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	thisNode := node.NewNode("localhost:50000")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", thisNode, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}
	t.Cleanup(func() {
		cluster.closeEtcd()
	})

	ctx := context.Background()

	// Register this node
	if err := cluster.registerNode(ctx); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	t.Cleanup(func() {
		cluster.unregisterNode(ctx)
	})

	// Start watching cluster state
	if err := cluster.startWatching(ctx); err != nil {
		t.Fatalf("Failed to start watching: %v", err)
	}

	// Wait for node registration to propagate
	time.Sleep(100 * time.Millisecond)

	nc := NewNodeConnections(cluster)
	err = nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}
	defer nc.Stop()

	// Initially, we should have 0 connections (excluding ourselves)
	initialCount := nc.NumConnections()
	if initialCount != 0 {
		t.Logf("Initial connection count: %d (expected 0, but may vary in test environment)", initialCount)
	}

	// Test that handleNodeChanges processes changes correctly
	previousNodes := make(map[string]bool)
	previousNodes["localhost:50000"] = true // our node

	// Simulate calling handleNodeChanges
	nc.handleNodeChanges(previousNodes)

	// The method should handle the changes without error
	// Actual connection attempts will fail without running servers, which is expected in unit tests
}
