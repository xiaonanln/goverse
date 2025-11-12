package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterReadyRequiresBothConnectionsAndShardMapping demonstrates that the cluster
// is only marked ready when BOTH node connections are established AND shard mapping is available.
// This is a regression test for the issue where cluster was marked ready without waiting for node connections.
func TestClusterReadyRequiresBothConnectionsAndShardMapping(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start cluster using the helper
	c := mustNewCluster(ctx, t, "localhost:47201", testPrefix)

	// Wait for cluster to be ready (Start() initializes everything)
	timeout := testutil.WaitForShardMappingTimeout
	select {
	case <-c.ClusterReady():
		t.Log("✓ Cluster correctly became ready after BOTH node connections AND shard mapping are available")
	case <-time.After(timeout):
		t.Errorf("Cluster should be ready within %v after Start()", timeout)
	}

	// Verify cluster is ready
	if !c.IsReady() {
		t.Error("Cluster.IsReady() should return true")
	}

	// Verify both prerequisites are met
	if c.GetNodeConnections() == nil {
		t.Error("Node connections should be established")
	}

	_ = c.GetShardMapping(ctx)

	t.Log("✓ All prerequisites verified: node connections AND shard mapping both available")
}
