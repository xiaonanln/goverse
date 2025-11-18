package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterEtcdIntegration tests cluster node registration and discovery through etcd
// This test requires a running etcd instance at localhost:2379
func TestClusterEtcdIntegration(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start both clusters
	cluster1 := mustNewCluster(ctx, t, "localhost:47001", testPrefix)
	cluster2 := mustNewCluster(ctx, t, "localhost:47002", testPrefix)

	// Wait for watches to sync
	time.Sleep(500 * time.Millisecond)

	// Both clusters should see each other's nodes
	nodes1 := cluster1.GetNodes()
	nodes2 := cluster2.GetNodes()

	t.Logf("Cluster1 sees %d nodes: %v", len(nodes1), nodes1)
	t.Logf("Cluster2 sees %d nodes: %v", len(nodes2), nodes2)

	// Each cluster should see at least 2 nodes (including itself)
	if len(nodes1) < 2 {
		t.Fatalf("Cluster1 should see at least 2 nodes, got %d", len(nodes1))
	}

	if len(nodes2) < 2 {
		t.Fatalf("Cluster2 should see at least 2 nodes, got %d", len(nodes2))
	}

	// Verify cluster1 sees node2
	found := false
	for _, nodeAddr := range nodes1 {
		if nodeAddr == "localhost:47002" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Cluster1 should see node2 in its node list")
	}

	// Verify cluster2 sees node1
	found = false
	for _, nodeAddr := range nodes2 {
		if nodeAddr == "localhost:47001" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Cluster2 should see node1 in its node list")
	}
}

// TestClusterEtcdDynamicDiscovery tests that clusters dynamically discover new nodes
func TestClusterEtcdDynamicDiscovery(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster1
	cluster1 := mustNewCluster(ctx, t, "localhost:47003", testPrefix)

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Record initial node count
	initialNodes := cluster1.GetNodes()
	t.Logf("Cluster1 initially sees %d nodes: %v", len(initialNodes), initialNodes)
	if len(initialNodes) != 1 || initialNodes[0] != "localhost:47003" {
		t.Fatalf("Cluster1 should initially see only itself ('localhost:47003'), got %d nodes: %v", len(initialNodes), initialNodes)
	}

	// Create and start cluster2 (dynamic discovery)
	_ = mustNewCluster(ctx, t, "localhost:47004", testPrefix)

	// Wait for cluster1 to discover cluster2
	time.Sleep(1 * time.Second)

	// Cluster1 should now see node2
	updatedNodes := cluster1.GetNodes()
	t.Logf("Cluster1 now sees %d nodes: %v", len(updatedNodes), updatedNodes)

	if len(updatedNodes) <= len(initialNodes) {
		t.Fatalf("Cluster1 should see more nodes after cluster2 joined, had %d, now has %d", len(initialNodes), len(updatedNodes))
	}

	// Verify node2 is in the list
	found := false
	for _, nodeAddr := range updatedNodes {
		if nodeAddr == "localhost:47004" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Cluster1 should discover node2 dynamically")
	}
}

// TestClusterEtcdLeaveDetection tests that clusters detect when other nodes leave
func TestClusterEtcdLeaveDetection(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster1
	cluster1 := mustNewCluster(ctx, t, "localhost:47005", testPrefix)

	// Create cluster2 (we'll stop it manually later to test leave detection)
	// For this test, we need manual control over cluster2's lifecycle
	node2 := node.NewNode("localhost:47006")
	err := node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", node2, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}

	err = cluster2.Start(ctx, node2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for both clusters to discover each other
	time.Sleep(1 * time.Second)

	// Verify cluster1 sees node2
	nodes := cluster1.GetNodes()
	t.Logf("Cluster1 sees %d nodes: %v", len(nodes), nodes)

	found := false
	for _, nodeAddr := range nodes {
		if nodeAddr == "localhost:47006" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Cluster1 should see node2 before it stops")
	}

	// Stop cluster2 (calls unregister and close internally)
	err = cluster2.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop cluster2: %v", err)
	}
	err = node2.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node2: %v", err)
	}

	// Wait for watch to detect the removal
	time.Sleep(1 * time.Second)

	// Cluster1 should no longer see node2
	nodes = cluster1.GetNodes()
	t.Logf("After node2 stopped, cluster1 sees %d nodes: %v", len(nodes), nodes)

	for _, nodeAddr := range nodes {
		if nodeAddr == "localhost:47006" {
			t.Fatal("Cluster1 should no longer see node2 after it stopped")
		}
	}
}

// TestClusterGetLeaderNode tests leader election across multiple nodes
func TestClusterGetLeaderNode(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create three clusters with different addresses
	// node2 has the smallest address, so it should be the leader
	clusters := make([]*Cluster, 3)
	nodes := make([]*node.Node, 3)
	addresses := []string{"localhost:47100", "localhost:47050", "localhost:47200"}

	for i := 0; i < 3; i++ {
		nodes[i] = node.NewNode(addresses[i])
		err := nodes[i].Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start node%d: %v", i+1, err)
		}
		// Capture loop variable for closure
		idx := i
		defer nodes[idx].Stop(ctx)

		clusters[i], err = newClusterWithEtcdForTesting(fmt.Sprintf("TestCluster%d", i+1), nodes[i], "localhost:2379", testPrefix)
		if err != nil {
			t.Skipf("Skipping test: etcd not available: %v", err)
			return
		}

		err = clusters[i].Start(ctx, nodes[i])
		if err != nil {
			t.Fatalf("Failed to start cluster%d: %v", i+1, err)
		}
		defer clusters[idx].Stop(ctx)
	}

	// Wait for all nodes to be registered and watches to sync
	time.Sleep(1 * time.Second)

	// All clusters should see the same leader (the one with smallest address)
	expectedLeader := "localhost:47050" // This is the smallest address

	for i, cluster := range clusters {
		leader := cluster.GetLeaderNode()
		t.Logf("Cluster %d sees leader: %s", i+1, leader)
		if leader != expectedLeader {
			t.Errorf("Cluster %d: GetLeaderNode() = %s, want %s", i+1, leader, expectedLeader)
		}
	}

	// Verify all clusters see all 3 nodes
	for i, cluster := range clusters {
		nodeList := cluster.GetNodes()
		if len(nodeList) < 3 {
			t.Errorf("Cluster %d should see at least 3 nodes, got %d", i+1, len(nodeList))
		}
	}
}

// TestClusterGetLeaderNode_DynamicChange tests leader change when current leader leaves
func TestClusterGetLeaderNode_DynamicChange(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes - node2 has smaller address (will be initial leader)
	node1 := node.NewNode("localhost:47300")
	node2 := node.NewNode("localhost:47200") // Smaller address - will be leader

	// Start both nodes
	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	// Create two clusters
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", node1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = cluster1.Start(ctx, node1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}
	defer cluster1.Stop(ctx)

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", node2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = cluster2.Start(ctx, node2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}
	defer cluster2.Stop(ctx)

	// Wait for all nodes to be registered and watches to sync
	time.Sleep(1 * time.Second)

	// Initially, node2 should be the leader (smaller address)
	initialLeader := cluster1.GetLeaderNode()
	t.Logf("Initial leader: %s", initialLeader)
	if initialLeader != "localhost:47200" {
		t.Errorf("Initial leader should be localhost:47200, got %s", initialLeader)
	}

	// Stop cluster2 (current leader) - this will unregister the node
	err = cluster2.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop cluster2: %v", err)
	}

	// Wait for watch to detect the removal
	time.Sleep(1 * time.Second)

	// Now node1 should be the leader (only remaining node)
	newLeader := cluster1.GetLeaderNode()
	t.Logf("New leader after node2 left: %s", newLeader)
	if newLeader != "localhost:47300" {
		t.Errorf("After node2 left, leader should be localhost:47300, got %s", newLeader)
	}
}

func TestClusterStopUnregistersFromEtcd(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start cluster
	cluster := mustNewCluster(ctx, t, "localhost:47010", testPrefix)
	nodeAddr := cluster.GetThisNode().GetAdvertiseAddress()

	// Get etcd client to check key directly
	etcdMgr := cluster.GetEtcdManagerForTesting()
	client := etcdMgr.GetClient()
	nodesPrefix := etcdMgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + nodeAddr

	// Wait for etcd registration to complete
	time.Sleep(500 * time.Millisecond)

	// Verify key exists after start
	resp, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to check node key after start: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Node key %s should exist after cluster start", key)
	}
	t.Logf("Node key %s exists as expected after start", key)

	// Stop the cluster
	err = cluster.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop cluster: %v", err)
	}

	// Wait for etcd unregistration to complete
	time.Sleep(500 * time.Millisecond)

	// Verify key is gone after stop
	resp, err = client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to check node key after stop: %v", err)
	}
	if len(resp.Kvs) > 0 {
		t.Fatalf("Node key %s should be removed after cluster stop", key)
	}
	t.Logf("Node key %s successfully removed after stop", key)
}
