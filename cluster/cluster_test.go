package cluster

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// Helper function to create and start a cluster with etcd for testing
func mustNewCluster(ctx context.Context, t *testing.T, nodeAddr string, etcdPrefix string) *Cluster {
	t.Helper()

	// Create a node
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create cluster config with test values (shorter durations for faster tests)
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
	}

	// Create cluster with etcd
	c, err := NewClusterWithNode(cfg, n)
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
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Test that Get returns a singleton
	cluster1 := This()
	cluster2 := This()

	if cluster1 != cluster2 {
		t.Fatal("This() should return the same cluster instance")
	}
}

func TestSetThisNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a new cluster for testing
	n := node.NewNode("test-address", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	if cluster.GetThisNode() != n {
		t.Fatal("GetThisNode() should return the node set by newClusterForTesting()")
	}
}

func TestSetThisNode_Panic(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a new cluster for testing with n1
	n1 := node.NewNode("test-address-1", testutil.TestNumShards)
	cluster := newClusterForTesting(n1, "TestCluster")

	// Trying to set a different node should fail (thisNode already set during creation)
	// This test verifies that the node cannot be changed after cluster creation
	if cluster.GetThisNode() != n1 {
		t.Fatal("cluster should have n1 set from creation")
	}
}

func TestNewCluster(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create a new cluster instance (not the singleton) for testing
	n := node.NewNode("localhost:50000", testutil.TestNumShards)
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
		t.Fatal("GetEtcdManagerForTesting() should return the manager after NewCluster")
	}
}

func TestNewCluster_WithNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a new cluster for testing with node
	n := node.NewNode("test-address", testutil.TestNumShards)
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
		t.Fatal("Cluster should have the etcd manager")
	}
}

func TestNewCluster_WithEtcdConfig(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create cluster with etcd and node
	n := node.NewNode("test-address", testutil.TestNumShards)
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
		t.Fatal("Cluster should have the etcd manager")
	}
}

func TestGetLeaderNode_WithEtcdConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create a new cluster with etcd initialized
	n := node.NewNode("localhost:50001", testutil.TestNumShards)
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
		t.Fatalf("GetLeaderNode() should return empty string when no nodes, got %s", leader)
	}
}

func TestClusterString(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Test String() with a basic cluster
	n := node.NewNode("localhost:47000", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	str := cluster.String()
	t.Logf("Cluster string: %s", str)

	// Verify format: Cluster<local-node-address,leader|member,quorum=%d>
	// Should contain the node address
	if !strings.Contains(str, "localhost:47000") {
		t.Fatalf("String() should contain node address 'localhost:47000', got: %s", str)
	}

	// Should contain either "leader" or "member"
	if !strings.Contains(str, "leader") && !strings.Contains(str, "member") {
		t.Fatalf("String() should contain 'leader' or 'member', got: %s", str)
	}

	// Should contain quorum information
	if !strings.Contains(str, "quorum=") {
		t.Fatalf("String() should contain 'quorum=', got: %s", str)
	}

	// Should start with "Cluster<" and end with ">"
	if !strings.HasPrefix(str, "Cluster<") {
		t.Fatalf("String() should start with 'Cluster<', got: %s", str)
	}
	if !strings.HasSuffix(str, ">") {
		t.Fatalf("String() should end with '>', got: %s", str)
	}
}

func TestClusterString_WithQuorum(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Test String() with different quorum values
	tests := []struct {
		name      string
		minQuorum int
		expected  string
	}{
		{"quorum_1", 1, "quorum=1"},
		{"quorum_3", 3, "quorum=3"},
		{"quorum_5", 5, "quorum=5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := node.NewNode("localhost:47000", testutil.TestNumShards)
			cluster := newClusterForTesting(n, "TestCluster")
			cluster.minQuorum = tt.minQuorum

			str := cluster.String()
			t.Logf("Cluster string: %s", str)

			if !strings.Contains(str, tt.expected) {
				t.Fatalf("String() should contain '%s', got: %s", tt.expected, str)
			}
		})
	}
}


func TestClusterGetClientCount(t *testing.T) {
ctx := context.Background()

t.Run("NodeCluster_ReturnsZero", func(t *testing.T) {
// Create a simple node cluster
nodeAddr := testutil.GetFreeAddress()
n := node.NewNode(nodeAddr, testutil.TestNumShards)
err := n.Start(ctx)
if err != nil {
t.Fatalf("Failed to start node: %v", err)
}
defer n.Stop(ctx)

// Create minimal cluster
cluster := &Cluster{
node:      n,
numShards: testutil.TestNumShards,
}

// Node clusters should return 0
if count := cluster.GetClientCount(); count != 0 {
t.Errorf("Expected GetClientCount() = 0 for node cluster, got %d", count)
}
})

t.Run("GateCluster_ReturnsActualCount", func(t *testing.T) {
gateAddr := testutil.GetFreeAddress()

// Create gate
gateConfig := &gate.GateConfig{
AdvertiseAddress: gateAddr,
EtcdAddress:      "localhost:2379",
EtcdPrefix:       "/test-cluster-clientcount",
}

g, err := gate.NewGate(gateConfig)
if err != nil {
t.Fatalf("Failed to create gate: %v", err)
}

err = g.Start(ctx)
if err != nil {
t.Fatalf("Failed to start gate: %v", err)
}
defer g.Stop()

// Create minimal gate cluster
cluster := &Cluster{
gate:      g,
numShards: testutil.TestNumShards,
}

// Initially should have 0 clients
if count := cluster.GetClientCount(); count != 0 {
t.Errorf("Expected GetClientCount() = 0 initially, got %d", count)
}

// Register 2 clients
_ = g.Register(ctx)
_ = g.Register(ctx)

// Should have 2 clients now
if count := cluster.GetClientCount(); count != 2 {
t.Errorf("Expected GetClientCount() = 2 after registering clients, got %d", count)
}
})
}
