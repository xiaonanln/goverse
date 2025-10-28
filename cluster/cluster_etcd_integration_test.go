package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
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

// TestClusterEtcdIntegration tests cluster node registration and discovery through etcd
// This test requires a running etcd instance at localhost:2379
func TestClusterEtcdIntegration(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Clean up etcd before test
	cleanupEtcdNodes(t)
	t.Cleanup(func() {
		cleanupEtcdNodes(t)
	})

	ctx := context.Background()

	// Create two clusters
	cluster1 := &Cluster{}
	cluster1.logger = cluster1.logger
	cluster2 := &Cluster{}
	cluster2.logger = cluster2.logger

	// Create etcd managers for both clusters
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}

	cluster1.SetEtcdManager(etcdMgr1)
	cluster2.SetEtcdManager(etcdMgr2)

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

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
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

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
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
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Clean up etcd before test
	cleanupEtcdNodes(t)
	t.Cleanup(func() {
		cleanupEtcdNodes(t)
	})

	ctx := context.Background()

	// Create and setup cluster1
	cluster1 := &Cluster{}
	cluster1.logger = cluster1.logger
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	node1 := node.NewNode("localhost:47003")
	cluster1.SetThisNode(node1)

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
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
	cluster2 := &Cluster{}
	cluster2.logger = cluster2.logger
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}
	cluster2.SetEtcdManager(etcdMgr2)

	node2 := node.NewNode("localhost:47004")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
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
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Clean up etcd before test
	cleanupEtcdNodes(t)
	t.Cleanup(func() {
		cleanupEtcdNodes(t)
	})

	ctx := context.Background()

	// Create and setup cluster1
	cluster1 := &Cluster{}
	cluster1.logger = cluster1.logger
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	node1 := node.NewNode("localhost:47005")
	cluster1.SetThisNode(node1)

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Create and setup cluster2
	cluster2 := &Cluster{}
	cluster2.logger = cluster2.logger
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}
	cluster2.SetEtcdManager(etcdMgr2)

	node2 := node.NewNode("localhost:47006")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}

	err = cluster2.WatchNodes(ctx)
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
