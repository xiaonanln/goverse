package node

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// cleanupEtcdNodes removes all node registrations from etcd to ensure test isolation
func cleanupEtcdNodes(t *testing.T) {
	// Connect to etcd and clean up all nodes
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Logf("Warning: failed to create etcd manager for cleanup: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Logf("Warning: failed to connect to etcd for cleanup: %v", err)
		return
	}
	defer mgr.Close()

	ctx := context.Background()

	// Delete all keys under /goverse/nodes/ prefix
	_, err = mgr.GetClient().Delete(ctx, etcdmanager.NodesPrefix, clientv3.WithPrefix())
	if err != nil {
		t.Logf("Warning: failed to cleanup etcd nodes: %v", err)
	}
}

// TestNodeEtcdIntegration tests node registration and discovery through etcd
// This test requires a running etcd instance at localhost:2379
func TestNodeEtcdIntegration(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Clean up etcd before test
	cleanupEtcdNodes(t)
	t.Cleanup(func() {
		cleanupEtcdNodes(t)
	})

	ctx := context.Background()

	// Create two nodes
	node1 := NewNodeWithEtcd("localhost:47001", "localhost:2379")
	node2 := NewNodeWithEtcd("localhost:47002", "localhost:2379")

	// Start node1
	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Start watching nodes for node1
	err = node1.GetEtcdManager().WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for node1: %v", err)
	}

	// Wait a bit for registration to complete
	time.Sleep(500 * time.Millisecond)

	// Start node2
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	// Start watching nodes for node2
	err = node2.GetEtcdManager().WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for node2: %v", err)
	}

	// Wait for watches to sync
	time.Sleep(500 * time.Millisecond)

	// Both nodes should see each other
	nodes1 := node1.GetNodes()
	nodes2 := node2.GetNodes()

	t.Logf("Node1 sees %d nodes: %v", len(nodes1), nodes1)
	t.Logf("Node2 sees %d nodes: %v", len(nodes2), nodes2)

	// Each node should see at least 2 nodes (including itself)
	if len(nodes1) < 2 {
		t.Fatalf("Node1 should see at least 2 nodes, got %d", len(nodes1))
	}

	if len(nodes2) < 2 {
		t.Fatalf("Node2 should see at least 2 nodes, got %d", len(nodes2))
	}

	// Verify node1 sees node2
	found := false
	for _, node := range nodes1 {
		if node == "localhost:47002" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Node1 should see node2 in its node list")
	}

	// Verify node2 sees node1
	found = false
	for _, node := range nodes2 {
		if node == "localhost:47001" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Node2 should see node1 in its node list")
	}
}

// TestNodeEtcdDynamicDiscovery tests that nodes dynamically discover new nodes
func TestNodeEtcdDynamicDiscovery(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Clean up etcd before test
	cleanupEtcdNodes(t)
	t.Cleanup(func() {
		cleanupEtcdNodes(t)
	})

	ctx := context.Background()

	// Create and start node1
	node1 := NewNodeWithEtcd("localhost:47003", "localhost:2379")
	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Start watching nodes for node1
	err = node1.GetEtcdManager().WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for node1: %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Record initial node count
	initialNodes := node1.GetNodes()
	t.Logf("Node1 initially sees %d nodes: %v", len(initialNodes), initialNodes)
	if len(initialNodes) != 1 || initialNodes[0] != "localhost:47003" {
		t.Fatalf("Node1 should initially see only itself ('localhost:47003'), got %d nodes: %v", len(initialNodes), initialNodes)
	}

	// Create and start node2
	node2 := NewNodeWithEtcd("localhost:47004", "localhost:2379")
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	// Start watching nodes for node2
	err = node2.GetEtcdManager().WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for node2: %v", err)
	}

	// Wait for watch to detect the new node
	time.Sleep(1 * time.Second)

	// Node1 should now see node2
	updatedNodes := node1.GetNodes()
	t.Logf("Node1 now sees %d nodes: %v", len(updatedNodes), updatedNodes)

	if len(updatedNodes) <= len(initialNodes) {
		t.Fatalf("Node1 should see more nodes after node2 joined, had %d, now has %d", len(initialNodes), len(updatedNodes))
	}

	// Verify node2 is in the list
	found := false
	for _, node := range updatedNodes {
		if node == "localhost:47004" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Node1 should discover node2 dynamically")
	}
}

// TestNodeEtcdLeaveDetection tests that nodes detect when other nodes leave
func TestNodeEtcdLeaveDetection(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Clean up etcd before test
	cleanupEtcdNodes(t)
	t.Cleanup(func() {
		cleanupEtcdNodes(t)
	})

	ctx := context.Background()

	// Create and start node1
	node1 := NewNodeWithEtcd("localhost:47005", "localhost:2379")
	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Start watching nodes for node1
	err = node1.GetEtcdManager().WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for node1: %v", err)
	}

	// Create and start node2
	node2 := NewNodeWithEtcd("localhost:47006", "localhost:2379")
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}

	// Start watching nodes for node2
	err = node2.GetEtcdManager().WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for node2: %v", err)
	}

	// Wait for both nodes to discover each other
	time.Sleep(1 * time.Second)

	// Verify node1 sees node2
	nodes := node1.GetNodes()
	t.Logf("Node1 sees %d nodes: %v", len(nodes), nodes)

	found := false
	for _, node := range nodes {
		if node == "localhost:47006" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Node1 should see node2 before it stops")
	}

	// Stop node2
	err = node2.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node2: %v", err)
	}

	// Wait for watch to detect the removal
	time.Sleep(1 * time.Second)

	// Node1 should no longer see node2
	nodes = node1.GetNodes()
	t.Logf("After node2 stopped, node1 sees %d nodes: %v", len(nodes), nodes)

	for _, node := range nodes {
		if node == "localhost:47006" {
			t.Fatal("Node1 should no longer see node2 after it stopped")
		}
	}
}
