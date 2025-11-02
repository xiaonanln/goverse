package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGet(t *testing.T) {
	// Test that Get returns a singleton
	cluster1 := Get()
	cluster2 := Get()

	if cluster1 != cluster2 {
		t.Error("Get() should return the same cluster instance")
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

func TestSetEtcdManager(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestCluster")

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create an etcd manager (without connecting)
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	cluster.SetEtcdManager(mgr)

	if cluster.GetEtcdManager() != mgr {
		t.Error("GetEtcdManager() should return the manager set by SetEtcdManager()")
	}
}

func TestSetEtcdManager_WithNode(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestCluster")

	// Set a node first
	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Then set the etcd manager
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	cluster.SetEtcdManager(mgr)

	// Cluster should have the manager
	if cluster.GetEtcdManager() != mgr {
		t.Error("Cluster should have the etcd manager")
	}
}

func TestSetThisNode_WithEtcdManager(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestCluster")

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Set etcd manager first
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	cluster.SetEtcdManager(mgr)

	// Then set the node
	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Cluster should have the manager
	if cluster.GetEtcdManager() != mgr {
		t.Error("Cluster should have the etcd manager")
	}
}

func TestGetNodes_NoEtcdManager(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	nodes := cluster.GetNodes()

	if len(nodes) != 0 {
		t.Error("GetNodes() should return empty list when etcd manager is not set")
	}
}

func TestStartWatching_NoConsensusManager(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	ctx := context.Background()
	err := cluster.StartWatching(ctx)

	if err == nil {
		t.Error("StartWatching should return error when consensus manager is not set")
	}

	expectedErr := "consensus manager not initialized"
	if err.Error() != expectedErr {
		t.Errorf("StartWatching error = %v; want %v", err.Error(), expectedErr)
	}
}

func TestGetLeaderNode_NoEtcdManager(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	leader := cluster.GetLeaderNode()

	if leader != "" {
		t.Error("GetLeaderNode() should return empty string when etcd manager is not set")
	}
}

func TestGetLeaderNode_WithEtcdManager(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestCluster")

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create an etcd manager (without connecting)
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	cluster.SetEtcdManager(mgr)

	// When there are no nodes, leader should be empty
	leader := cluster.GetLeaderNode()
	if leader != "" {
		t.Errorf("GetLeaderNode() should return empty string when no nodes, got %s", leader)
	}
}
