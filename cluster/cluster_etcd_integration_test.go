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

	// Create two clusters
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	// Create etcd managers for both clusters with same test prefix


	// Create nodes for both clusters
	node1 := node.NewNode("localhost:47001")
	node2 := node.NewNode("localhost:47002")

	cluster1.SetThisNode(node1)
	cluster2.SetThisNode(node2)

	// Start and register node1
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Wait a bit for registration to complete
	time.Sleep(500 * time.Millisecond)

	// Start and register node2
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

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

	// Create and setup cluster1
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	node1 := node.NewNode("localhost:47003")
	cluster1.SetThisNode(node1)

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Record initial node count
	initialNodes := cluster1.GetNodes()
	t.Logf("Cluster1 initially sees %d nodes: %v", len(initialNodes), initialNodes)
	if len(initialNodes) != 1 || initialNodes[0] != "localhost:47003" {
		t.Fatalf("Cluster1 should initially see only itself ('localhost:47003'), got %d nodes: %v", len(initialNodes), initialNodes)
	}

	// Create and setup cluster2
	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	node2 := node.NewNode("localhost:47004")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	// Wait for watch to detect the new node
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

	// Create and setup cluster1
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	node1 := node.NewNode("localhost:47005")
	cluster1.SetThisNode(node1)

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Create and setup cluster2
	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}

	node2 := node.NewNode("localhost:47006")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}

	err = cluster2.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
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

	// Stop cluster2 (unregister and close)
	err = cluster2.UnregisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to unregister node2: %v", err)
	}
	err = cluster2.CloseEtcd()
	if err != nil {
		t.Fatalf("Failed to close etcd for cluster2: %v", err)
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
		var err error
		clusters[i], err = newClusterWithEtcdForTesting(fmt.Sprintf("TestCluster%d", i+1), "localhost:2379", testPrefix)
		if err != nil {
			t.Skipf("Skipping test: etcd not available: %v", err)
			return
		}
		defer clusters[i].CloseEtcd()


		nodes[i] = node.NewNode(addresses[i])
		clusters[i].SetThisNode(nodes[i])

		err = nodes[i].Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start node%d: %v", i+1, err)
		}
		defer nodes[i].Stop(ctx)

		err = clusters[i].RegisterNode(ctx)
		if err != nil {
			t.Fatalf("Failed to register node%d: %v", i+1, err)
		}
		defer clusters[i].UnregisterNode(ctx)

		err = clusters[i].StartWatching(ctx)
		if err != nil {
			t.Fatalf("Failed to start watching nodes for cluster%d: %v", i+1, err)
		}
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

	// Create two clusters - cluster2 has smaller address (will be initial leader)
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}
	defer cluster1.CloseEtcd()

	node1 := node.NewNode("localhost:47300")
	cluster1.SetThisNode(node1)

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}
	defer cluster2.CloseEtcd()

	node2 := node.NewNode("localhost:47200") // Smaller address - will be leader
	cluster2.SetThisNode(node2)

	// Start and register both nodes
	for i, n := range []*node.Node{node1, node2} {
		err := n.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start node%d: %v", i+1, err)
		}
		defer n.Stop(ctx)
	}

	for i, c := range []*Cluster{cluster1, cluster2} {
		err = c.RegisterNode(ctx)
		if err != nil {
			t.Fatalf("Failed to register node%d: %v", i+1, err)
		}

		err = c.StartWatching(ctx)
		if err != nil {
			t.Fatalf("Failed to start watching nodes for cluster%d: %v", i+1, err)
		}
	}

	// Wait for all nodes to be registered and watches to sync
	time.Sleep(1 * time.Second)

	// Initially, node2 should be the leader (smaller address)
	initialLeader := cluster1.GetLeaderNode()
	t.Logf("Initial leader: %s", initialLeader)
	if initialLeader != "localhost:47200" {
		t.Errorf("Initial leader should be localhost:47200, got %s", initialLeader)
	}

	// Unregister node2 (current leader)
	err = cluster2.UnregisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to unregister node2: %v", err)
	}

	// Wait for watch to detect the removal
	time.Sleep(1 * time.Second)

	// Now node1 should be the leader (only remaining node)
	newLeader := cluster1.GetLeaderNode()
	t.Logf("New leader after node2 left: %s", newLeader)
	if newLeader != "localhost:47300" {
		t.Errorf("After node2 left, leader should be localhost:47300, got %s", newLeader)
	}

	// Cleanup
	cluster1.UnregisterNode(ctx)
}
