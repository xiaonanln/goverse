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
