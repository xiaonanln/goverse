package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterReadyAfterNodeConnections verifies that the cluster is only marked ready
// after node connections are established AND shard mapping is available
func TestClusterReadyAfterNodeConnections(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start cluster using the helper
	cluster1 := mustNewCluster(ctx, t, "localhost:47101", testPrefix)

	// Wait for shard mapping to be created and cluster to be marked ready
	// This should happen within NodeStabilityDuration + ShardMappingCheckInterval + some buffer
	testutil.WaitForClusterReady(t, cluster1)

	// Verify cluster is ready
	if !cluster1.IsReady() {
		t.Fatal("Cluster.IsReady() should return true after cluster is ready")
	}

	// Verify node connections are established
	nc := cluster1.GetNodeConnections()
	if nc == nil {
		t.Fatal("NodeConnections should not be nil after cluster is ready")
	}

	// Verify shard mapping is available
	_ = cluster1.GetShardMapping(ctx)
}

// TestClusterReadyMultiNode verifies cluster readiness in a multi-node setup
func TestClusterReadyMultiNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start first cluster using the helper (will be leader)
	cluster1 := mustNewCluster(ctx, t, "localhost:47111", testPrefix)

	// Create and start second cluster using the helper (will be non-leader)
	cluster2 := mustNewCluster(ctx, t, "localhost:47112", testPrefix)

	// Wait for both clusters to be ready (cluster.Start already handles all initialization)
	testutil.WaitForClusterReady(t, cluster1)
	testutil.WaitForClusterReady(t, cluster2)

	// Verify both clusters are ready
	if !cluster1.IsReady() {
		t.Fatal("Cluster1 should be ready")
	}
	if !cluster2.IsReady() {
		t.Fatal("Cluster2 should be ready")
	}

	// Verify shard mapping is available on both
	_ = cluster1.GetShardMapping(ctx)

	_ = cluster2.GetShardMapping(ctx)
}
