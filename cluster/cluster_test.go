package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGet(t *testing.T) {
	// Test that Get returns a singleton
	cluster1 := This()
	cluster2 := This()

	if cluster1 != cluster2 {
		t.Error("This() should return the same cluster instance")
	}
}

func TestSetThisNode(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestCluster")

	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	if cluster.GetThisNode() != n {
		t.Error("GetThisNode() should return the node set by SetThisNode()")
	}
}

func TestSetThisNode_Panic(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestCluster")

	n1 := node.NewNode("test-address-1")
	cluster.SetThisNode(n1)

	// Setting the node again should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetThisNode should panic when called twice")
		}
	}()

	n2 := node.NewNode("test-address-2")
	cluster.SetThisNode(n2)
}

func TestGetThisNode_NotSet(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	if cluster.GetThisNode() != nil {
		t.Error("GetThisNode() should return nil when node is not set")
	}
}

func TestCallObject_NodeNotSet(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	ctx := context.Background()
	_, err := cluster.CallObject(ctx, "test-id", "TestMethod", &emptypb.Empty{})

	if err == nil {
		t.Error("CallObject should return error when ThisNode is not set")
	}

	expectedErr := "ThisNode is not set"
	if err.Error() != expectedErr {
		t.Errorf("CallObject error = %v; want %v", err.Error(), expectedErr)
	}
}

func TestNewCluster(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create a new cluster instance (not the singleton) for testing
	cluster, err := newClusterWithEtcdForTesting("TestCluster", "localhost:2379", testPrefix)
	// Connection may fail if etcd is not running, but cluster and managers should be created
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
		// Even if connection failed, cluster should be created
		if cluster == nil {
			t.Fatal("cluster should be created even if etcd connection fails")
		}
	}

	if cluster.GetEtcdManagerForTesting() == nil {
		t.Error("GetEtcdManagerForTesting() should return the manager after NewCluster")
	}
}

func TestNewCluster_WithNode(t *testing.T) {
	// Create a new cluster for testing
	cluster, err := newClusterWithEtcdForTesting("TestCluster", "localhost:2379", testutil.PrepareEtcdPrefix(t, "localhost:2379"))
	// Connection may fail if etcd is not running
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
		if cluster == nil {
			t.Fatal("cluster should be created even if etcd connection fails")
		}
	}

	// Set a node
	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Cluster should have the manager
	if cluster.GetEtcdManagerForTesting() == nil {
		t.Error("Cluster should have the etcd manager")
	}
}

func TestNewCluster_WithEtcdConfig(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create cluster with etcd
	cluster, err := newClusterWithEtcdForTesting("TestCluster", "localhost:2379", testPrefix)
	// Connection may fail if etcd is not running
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
		if cluster == nil {
			t.Fatal("cluster should be created even if etcd connection fails")
		}
	}

	// Then set the node
	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Cluster should have the manager
	if cluster.GetEtcdManagerForTesting() == nil {
		t.Error("Cluster should have the etcd manager")
	}
}

func TestGetLeaderNode_WithEtcdConfig(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create a new cluster with etcd initialized
	cluster, err := newClusterWithEtcdForTesting("TestCluster", "localhost:2379", testPrefix)
	// Connection may fail if etcd is not running
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
		if cluster == nil {
			t.Fatal("cluster should be created even if etcd connection fails")
		}
	}

	// When there are no nodes, leader should be empty
	leader := cluster.GetLeaderNode()
	if leader != "" {
		t.Errorf("GetLeaderNode() should return empty string when no nodes, got %s", leader)
	}
}

func TestClusterStart(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a cluster
	cluster, err := newClusterWithEtcdForTesting("TestCluster", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	// Create a node
	n := node.NewNode("localhost:50001")

	// Start the node
	err = n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Start the cluster
	err = cluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}
	defer cluster.Stop(ctx)

	// Verify node is set
	if cluster.GetThisNode() != n {
		t.Error("GetThisNode() should return the node set by Start()")
	}

	// Verify node is registered
	nodes := cluster.GetNodes()
	found := false
	for _, nodeAddr := range nodes {
		if nodeAddr == n.GetAdvertiseAddress() {
			found = true
			break
		}
	}
	if !found {
		t.Error("Node should be registered after Start()")
	}
}

func TestClusterStop(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a cluster
	cluster, err := newClusterWithEtcdForTesting("TestCluster", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	// Create a node
	n := node.NewNode("localhost:50002")

	// Start the node
	err = n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Start the cluster
	err = cluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Stop the cluster
	err = cluster.Stop(ctx)
	if err != nil {
		t.Errorf("Failed to stop cluster: %v", err)
	}

	// Verify the cluster is stopped (node connections should be nil)
	if cluster.GetNodeConnections() != nil {
		t.Error("Node connections should be nil after Stop()")
	}
}
