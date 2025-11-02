package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterReadyAfterNodeConnections verifies that the cluster is only marked ready
// after node connections are established AND shard mapping is available
func TestClusterReadyAfterNodeConnections(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create cluster
	cluster1 := newClusterForTesting("TestCluster1")

	// Create etcd manager
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	// Create node
	node1 := node.NewNode("localhost:47101")
	cluster1.SetThisNode(node1)

	// Start node
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Connect to etcd
	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd: %v", err)
	}
	defer cluster1.CloseEtcd()

	// Register node
	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	// Start watching nodes
	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes: %v", err)
	}

	// At this point, cluster should NOT be ready (no node connections yet)
	select {
	case <-cluster1.ClusterReady():
		t.Error("Cluster should not be ready before node connections are established")
	case <-time.After(100 * time.Millisecond):
		// Expected: cluster is not ready yet
	}

	// Start NodeConnections
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}
	defer cluster1.StopNodeConnections()

	// At this point, cluster still might not be ready (no shard mapping yet)
	// But let's check the state
	if cluster1.IsReady() {
		t.Log("Cluster became ready immediately after node connections (shard mapping might be available)")
	} else {
		t.Log("Cluster not ready yet (waiting for shard mapping)")
	}

	// Start shard mapping management
	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	// Wait for shard mapping to be created and cluster to be marked ready
	// This should happen within NodeStabilityDuration + ShardMappingCheckInterval + some buffer
	timeout := NodeStabilityDuration + ShardMappingCheckInterval + 5*time.Second
	select {
	case <-cluster1.ClusterReady():
		t.Log("Cluster is now ready")
		// Success: cluster became ready after both node connections and shard mapping are available
	case <-time.After(timeout):
		t.Errorf("Cluster should be ready within %v after starting shard mapping management", timeout)
	}

	// Verify cluster is ready
	if !cluster1.IsReady() {
		t.Error("Cluster.IsReady() should return true after cluster is ready")
	}

	// Verify node connections are established
	nc := cluster1.GetNodeConnections()
	if nc == nil {
		t.Error("NodeConnections should not be nil after cluster is ready")
	}

	// Verify shard mapping is available
	_, err = cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Shard mapping should be available after cluster is ready: %v", err)
	}
}

// TestClusterReadyMultiNode verifies cluster readiness in a multi-node setup
func TestClusterReadyMultiNode(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create first cluster (will be leader)
	cluster1 := newClusterForTesting("TestCluster1")
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	node1 := node.NewNode("localhost:47111")
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

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching for cluster1: %v", err)
	}

	// Create second cluster (will be non-leader)
	cluster2 := newClusterForTesting("TestCluster2")
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}
	cluster2.SetEtcdManager(etcdMgr2)

	node2 := node.NewNode("localhost:47112")
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

	err = cluster2.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching for cluster2: %v", err)
	}

	// Start node connections on both clusters
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster1: %v", err)
	}
	defer cluster1.StopNodeConnections()

	err = cluster2.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster2: %v", err)
	}
	defer cluster2.StopNodeConnections()

	// Start shard mapping management on both clusters
	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster1: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	err = cluster2.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster2: %v", err)
	}
	defer cluster2.StopShardMappingManagement()

	// Wait for both clusters to be ready
	timeout := NodeStabilityDuration + ShardMappingCheckInterval*2 + 5*time.Second

	// Wait for cluster1 (leader) to be ready
	select {
	case <-cluster1.ClusterReady():
		t.Log("Cluster1 (leader) is ready")
	case <-time.After(timeout):
		t.Error("Cluster1 should be ready within timeout")
	}

	// Wait for cluster2 (non-leader) to be ready
	select {
	case <-cluster2.ClusterReady():
		t.Log("Cluster2 (non-leader) is ready")
	case <-time.After(timeout):
		t.Error("Cluster2 should be ready within timeout")
	}

	// Verify both clusters are ready
	if !cluster1.IsReady() {
		t.Error("Cluster1 should be ready")
	}
	if !cluster2.IsReady() {
		t.Error("Cluster2 should be ready")
	}

	// Verify shard mapping is available on both
	_, err = cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Cluster1 shard mapping should be available: %v", err)
	}

	_, err = cluster2.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Cluster2 shard mapping should be available: %v", err)
	}
}
