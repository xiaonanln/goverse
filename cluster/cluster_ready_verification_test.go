package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterReadyRequiresBothConnectionsAndShardMapping demonstrates that the cluster
// is only marked ready when BOTH node connections are established AND shard mapping is available.
// This is a regression test for the issue where cluster was marked ready without waiting for node connections.
func TestClusterReadyRequiresBothConnectionsAndShardMapping(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Setup cluster
	n := node.NewNode("localhost:47201")
	c, err := newClusterWithEtcdForTesting("TestClusterReadyRequiresBothConnectionsAndShardMapping", n, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	c.setThisNode(n)

	err = n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	err = c.registerNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	defer c.unregisterNode(ctx)

	err = c.startWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching: %v", err)
	}

	// At this point: no node connections started, no shard mapping
	// Cluster should NOT be ready
	select {
	case <-c.ClusterReady():
		t.Error("Cluster should NOT be ready: node connections not started yet")
	case <-time.After(100 * time.Millisecond):
		// Expected: not ready
		t.Log("✓ Cluster correctly NOT ready without node connections")
	}

	// Start node connections
	err = c.startNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections: %v", err)
	}
	defer c.stopNodeConnections()

	// At this point: node connections started, but no shard mapping yet
	// Cluster should still NOT be ready
	select {
	case <-c.ClusterReady():
		t.Error("Cluster should NOT be ready: shard mapping not available yet")
	case <-time.After(100 * time.Millisecond):
		// Expected: not ready
		t.Log("✓ Cluster correctly NOT ready without shard mapping")
	}

	// Start shard mapping management
	err = c.startShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management: %v", err)
	}
	defer c.stopShardMappingManagement()

	// Now both conditions should be met after shard mapping is created
	// Wait for cluster to be ready
	timeout := NodeStabilityDuration + ShardMappingCheckInterval + 5*time.Second
	select {
	case <-c.ClusterReady():
		t.Log("✓ Cluster correctly became ready after BOTH node connections AND shard mapping are available")
	case <-time.After(timeout):
		t.Errorf("Cluster should be ready within %v after both prerequisites are met", timeout)
	}

	// Verify cluster is ready
	if !c.IsReady() {
		t.Error("Cluster.IsReady() should return true")
	}

	// Verify both prerequisites are met
	if c.GetNodeConnections() == nil {
		t.Error("Node connections should be established")
	}

	_, err = c.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("Shard mapping should be available: %v", err)
	}

	t.Log("✓ All prerequisites verified: node connections AND shard mapping both available")
}
