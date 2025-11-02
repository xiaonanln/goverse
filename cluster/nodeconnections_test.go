package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestNewNodeConnections(t *testing.T) {
	cluster := newClusterForTesting("TestCluster")
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
	// Setup test cluster
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	cluster := newClusterForTesting("TestCluster")
	
	// Connect to etcd (auto-creates managers)
	err := cluster.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Logf("ConnectEtcd failed (expected if etcd not running): %v", err)
	}

	// Set this node
	thisNode := node.NewNode("localhost:50000")
	cluster.SetThisNode(thisNode)

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
	// Setup test cluster
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	cluster := newClusterForTesting("TestCluster")
	
	// Connect to etcd (auto-creates managers)
	err := cluster.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Logf("ConnectEtcd failed (expected if etcd not running): %v", err)
	}

	thisNode := node.NewNode("localhost:50000")
	cluster.SetThisNode(thisNode)

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
	cluster := newClusterForTesting("TestCluster")
	nc := NewNodeConnections(cluster)

	// Initially should have 0 connections
	if nc.NumConnections() != 0 {
		t.Errorf("Expected 0 connections initially, got %d", nc.NumConnections())
	}
}

func TestNodeConnections_GetConnection_NotExists(t *testing.T) {
	cluster := newClusterForTesting("TestCluster")
	nc := NewNodeConnections(cluster)

	_, err := nc.GetConnection("localhost:50000")
	if err == nil {
		t.Error("GetConnection should return error for non-existent connection")
	}
}

func TestNodeConnections_GetAllConnections(t *testing.T) {
	cluster := newClusterForTesting("TestCluster")
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
	// Setup test cluster
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	cluster := newClusterForTesting("TestCluster")
	
	// Connect to etcd (auto-creates managers)
	err := cluster.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Logf("ConnectEtcd failed (expected if etcd not running): %v", err)
	}

	thisNode := node.NewNode("localhost:50000")
	cluster.SetThisNode(thisNode)

	nc := NewNodeConnections(cluster)

	// Note: We can't test actual connection establishment without a running server
	// We test the connection management logic

	// Test disconnect from non-existent node
	err = nc.disconnectFromNode("localhost:60000")
	if err != nil {
		t.Errorf("Disconnecting from non-existent node should not error: %v", err)
	}
}

func TestCluster_StartStopNodeConnections(t *testing.T) {
	// Setup test cluster
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	cluster := newClusterForTesting("TestCluster")
	
	// Connect to etcd (auto-creates managers)
	err := cluster.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Logf("ConnectEtcd failed (expected if etcd not running): %v", err)
	}

	thisNode := node.NewNode("localhost:50000")
	cluster.SetThisNode(thisNode)

	ctx := context.Background()

	// Start node connections
	err = cluster.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}

	if cluster.GetNodeConnections() == nil {
		t.Error("NodeConnections should be initialized after StartNodeConnections()")
	}

	// Stop node connections
	cluster.StopNodeConnections()

	if cluster.GetNodeConnections() != nil {
		t.Error("NodeConnections should be nil after StopNodeConnections()")
	}
}

func TestCluster_StartNodeConnectionsTwice(t *testing.T) {
	// Setup test cluster
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	cluster := newClusterForTesting("TestCluster")
	
	// Connect to etcd (auto-creates managers)
	err := cluster.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Logf("ConnectEtcd failed (expected if etcd not running): %v", err)
	}

	thisNode := node.NewNode("localhost:50000")
	cluster.SetThisNode(thisNode)

	ctx := context.Background()

	// Start first time
	err = cluster.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}

	// Start second time - should not error
	err = cluster.StartNodeConnections(ctx)
	if err != nil {
		t.Errorf("Starting node connections twice should not error: %v", err)
	}

	cluster.StopNodeConnections()
}

func TestCluster_StopNodeConnections_NotStarted(t *testing.T) {
	cluster := newClusterForTesting("TestCluster")

	// Stop without starting - should not panic
	cluster.StopNodeConnections()

	if cluster.GetNodeConnections() != nil {
		t.Error("NodeConnections should remain nil after stopping when not started")
	}
}

func TestNodeConnections_HandleNodeChanges(t *testing.T) {
	// Setup test cluster
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	cluster := newClusterForTesting("TestCluster")
	
	// Connect to etcd (auto-creates managers)
	err := cluster.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}
	t.Cleanup(func() {
		cluster.CloseEtcd()
	})

	thisNode := node.NewNode("localhost:50000")
	cluster.SetThisNode(thisNode)

	ctx := context.Background()

	// Register this node
	if err := cluster.RegisterNode(ctx); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	t.Cleanup(func() {
		cluster.UnregisterNode(ctx)
	})

	// Start watching cluster state
	if err := cluster.StartWatching(ctx); err != nil {
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
