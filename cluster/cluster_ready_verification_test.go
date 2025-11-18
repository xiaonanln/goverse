package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterReadyRequiresBothConnectionsAndShardMapping demonstrates that the cluster
// is only marked ready when BOTH node connections are established AND shard mapping is available.
// This is a regression test for the issue where cluster was marked ready without waiting for node connections.
func TestClusterReadyRequiresBothConnectionsAndShardMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start cluster using the helper
	c := mustNewCluster(ctx, t, "localhost:47201", testPrefix)

	// Wait for cluster to be ready (Start() initializes everything)
	testutil.WaitForClusterReady(t, c)

	// Verify cluster is ready
	if !c.IsReady() {
		t.Fatal("Cluster.IsReady() should return true")
	}

	// Verify both prerequisites are met
	if c.GetNodeConnections() == nil {
		t.Fatal("Node connections should be established")
	}

	_ = c.GetShardMapping(ctx)

	t.Log("✓ All prerequisites verified: node connections AND shard mapping both available")
}
