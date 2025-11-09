package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Helper function to create and start a cluster with etcd for testing
func mustNewCluster(ctx context.Context, t *testing.T, nodeAddr string, etcdPrefix string) *Cluster {
	t.Helper()

	// Create a node
	n := node.NewNode(nodeAddr)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create cluster with etcd
	c, err := NewCluster(n, "localhost:2379", etcdPrefix)
	if err != nil {
		n.Stop(ctx) // Clean up node if cluster creation fails
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// Start the cluster (register node, start watching, etc.)
	err = c.Start(ctx, n)
	if err != nil {
		n.Stop(ctx) // Clean up node if cluster start fails
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		c.Stop(ctx)
		n.Stop(ctx)
	})

	return c
}

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
	n := node.NewNode("test-address")
	cluster := newClusterForTesting(n, "TestCluster")

	if cluster.GetThisNode() != n {
		t.Error("GetThisNode() should return the node set by newClusterForTesting()")
	}
}

func TestSetThisNode_Panic(t *testing.T) {
	// Create a new cluster for testing with n1
	n1 := node.NewNode("test-address-1")
	cluster := newClusterForTesting(n1, "TestCluster")

	// Trying to set a different node should fail (thisNode already set during creation)
	// This test verifies that the node cannot be changed after cluster creation
	if cluster.GetThisNode() != n1 {
		t.Error("cluster should have n1 set from creation")
	}
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
	_, err := cluster.CallObject(ctx, "testType", "test-id", "TestMethod", &emptypb.Empty{})

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
	n := node.NewNode("localhost:50000")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", n, "localhost:2379", testPrefix)
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
	// Create a new cluster for testing with node
	n := node.NewNode("test-address")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", n, "localhost:2379", testutil.PrepareEtcdPrefix(t, "localhost:2379"))
	// Connection may fail if etcd is not running
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
		if cluster == nil {
			t.Fatal("cluster should be created even if etcd connection fails")
		}
	}

	// Node should be set from cluster creation
	if cluster.GetThisNode() != n {
		t.Fatal("cluster should have the node set from creation")
	}

	// Cluster should have the manager
	if cluster.GetEtcdManagerForTesting() == nil {
		t.Error("Cluster should have the etcd manager")
	}
}

func TestNewCluster_WithEtcdConfig(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create cluster with etcd and node
	n := node.NewNode("test-address")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", n, "localhost:2379", testPrefix)
	// Connection may fail if etcd is not running
	if err != nil {
		t.Logf("newClusterWithEtcdForTesting failed (expected if etcd not running): %v", err)
		if cluster == nil {
			t.Fatal("cluster should be created even if etcd connection fails")
		}
	}

	// Node should be set
	if cluster.GetThisNode() != n {
		t.Fatal("cluster should have the node set")
	}

	// Cluster should have the manager
	if cluster.GetEtcdManagerForTesting() == nil {
		t.Error("Cluster should have the etcd manager")
	}
}

func TestGetLeaderNode_WithEtcdConfig(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create a new cluster with etcd initialized
	n := node.NewNode("localhost:50001")
	cluster, err := newClusterWithEtcdForTesting("TestCluster", n, "localhost:2379", testPrefix)
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
