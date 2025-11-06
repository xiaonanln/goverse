package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterReadyAfterNodeConnections verifies that the cluster is only marked ready
// after node connections are established AND shard mapping is available
func TestClusterReadyAfterNodeConnections(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster using the helper
	cluster1 := mustNewCluster(ctx, t, "localhost:47101", testPrefix)

	// Wait for shard mapping to be created and cluster to be marked ready
	// This should happen within NodeStabilityDuration + ShardMappingCheckInterval + some buffer
	timeout := NodeStabilityDuration + ShardMappingCheckInterval + 5*time.Second
	select {
	case <-cluster1.ClusterReady():
		t.Log("Cluster is now ready")
		// Success: cluster became ready after both node connections and shard mapping are available
	case <-time.After(timeout):
		t.Errorf("Cluster should be ready within %v after Start()", timeout)
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
	_, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Shard mapping should be available after cluster is ready: %v", err)
	}
}

// TestClusterReadyMultiNode verifies cluster readiness in a multi-node setup
func TestClusterReadyMultiNode(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start first cluster using the helper (will be leader)
	cluster1 := mustNewCluster(ctx, t, "localhost:47111", testPrefix)

	// Create and start second cluster using the helper (will be non-leader)
	cluster2 := mustNewCluster(ctx, t, "localhost:47112", testPrefix)

	// Wait for both clusters to be ready (cluster.Start already handles all initialization)
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
	_, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Cluster1 shard mapping should be available: %v", err)
	}

	_, err = cluster2.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Cluster2 shard mapping should be available: %v", err)
	}
}
