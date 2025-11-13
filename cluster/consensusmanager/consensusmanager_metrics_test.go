package consensusmanager

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/util/metrics"
)

func TestUpdateShardMetrics_EmptyState(t *testing.T) {
	// Reset metrics before test
	metrics.ShardsTotal.Reset()

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Test with no nodes and no shards
	cm.mu.Lock()
	cm.updateShardMetrics()
	cm.mu.Unlock()

	// No metrics should be set
	// Note: With no nodes, no metrics are reported
}

func TestUpdateShardMetrics_WithShards(t *testing.T) {
	// Reset metrics before test
	metrics.ShardsTotal.Reset()

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set up test state with nodes and shards
	cm.mu.Lock()
	cm.state.Nodes["localhost:47000"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Assign some shards to nodes (using CurrentNode for actual ownership)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"}
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"}
	cm.state.ShardMapping.Shards[3] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47001"}
	cm.state.ShardMapping.Shards[4] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47001"}

	// Update metrics
	cm.updateShardMetrics()
	cm.mu.Unlock()

	// Verify metrics
	count0 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 3.0 {
		t.Errorf("Expected shard count for localhost:47000 to be 3.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 2.0 {
		t.Errorf("Expected shard count for localhost:47001 to be 2.0, got %f", count1)
	}
}

func TestUpdateShardMetrics_NodeWithNoShards(t *testing.T) {
	// Reset metrics before test
	metrics.ShardsTotal.Reset()

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set up test state with nodes but no shards assigned to one node
	cm.mu.Lock()
	cm.state.Nodes["localhost:47000"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Assign all shards to only two nodes, leaving one with zero
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47001"}

	// Update metrics
	cm.updateShardMetrics()
	cm.mu.Unlock()

	// Verify metrics - node with no shards should have count of 0
	count0 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 1.0 {
		t.Errorf("Expected shard count for localhost:47000 to be 1.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 1.0 {
		t.Errorf("Expected shard count for localhost:47001 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47002"))
	if count2 != 0.0 {
		t.Errorf("Expected shard count for localhost:47002 to be 0.0, got %f", count2)
	}
}

func TestUpdateShardMetrics_UnclaimedShards(t *testing.T) {
	// Reset metrics before test
	metrics.ShardsTotal.Reset()

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set up test state with shards that have TargetNode but no CurrentNode (unclaimed)
	cm.mu.Lock()
	cm.state.Nodes["localhost:47000"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Shards with TargetNode but empty CurrentNode (not yet claimed)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: ""}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: ""}
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47001"}

	// Update metrics
	cm.updateShardMetrics()
	cm.mu.Unlock()

	// Verify metrics - only claimed shards (with CurrentNode) should be counted
	count0 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 0.0 {
		t.Errorf("Expected shard count for localhost:47000 to be 0.0 (shards not claimed), got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 1.0 {
		t.Errorf("Expected shard count for localhost:47001 to be 1.0, got %f", count1)
	}
}

func TestUpdateShardMetrics_ShardMigration(t *testing.T) {
	// Reset metrics before test
	metrics.ShardsTotal.Reset()

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set up test state simulating a shard migration scenario
	// Shard is being migrated from node 47000 to 47001
	cm.mu.Lock()
	cm.state.Nodes["localhost:47000"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Shard 0: TargetNode is 47001, but CurrentNode is still 47000 (migration in progress)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47000"}

	// Update metrics
	cm.updateShardMetrics()
	cm.mu.Unlock()

	// Verify metrics - should count based on CurrentNode (actual ownership)
	count0 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 1.0 {
		t.Errorf("Expected shard count for localhost:47000 (current) to be 1.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.ShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 0.0 {
		t.Errorf("Expected shard count for localhost:47001 (target) to be 0.0, got %f", count1)
	}
}
