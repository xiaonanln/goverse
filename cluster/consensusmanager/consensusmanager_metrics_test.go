package consensusmanager

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/metrics"
	testutilpkg "github.com/xiaonanln/goverse/util/testutil"
)

func TestUpdateShardMetrics_EmptyState(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	// Initialize empty ShardMapping to prevent nil pointer
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.mu.Unlock()

	// Test with no nodes and no shards
	cm.UpdateShardMetrics()

	// No metrics should be set
	// Note: With no nodes, no metrics are reported
}

func TestUpdateShardMetrics_WithShards(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

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
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 3.0 {
		t.Fatalf("Expected shard count for localhost:47000 to be 3.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 2.0 {
		t.Fatalf("Expected shard count for localhost:47001 to be 2.0, got %f", count1)
	}
}

func TestUpdateShardMetrics_NodeWithNoShards(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

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
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics - node with no shards should have count of 0
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47000 to be 1.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47001 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47002"))
	if count2 != 0.0 {
		t.Fatalf("Expected shard count for localhost:47002 to be 0.0, got %f", count2)
	}
}

func TestUpdateShardMetrics_UnclaimedShards(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

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
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics - only claimed shards (with CurrentNode) should be counted
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 0.0 {
		t.Fatalf("Expected shard count for localhost:47000 to be 0.0 (shards not claimed), got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47001 to be 1.0, got %f", count1)
	}
}

func TestUpdateShardMetrics_ShardMigration(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

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
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics - should count based on CurrentNode (actual ownership)
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count0 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47000 (current) to be 1.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:47001"))
	if count1 != 0.0 {
		t.Fatalf("Expected shard count for localhost:47001 (target) to be 0.0, got %f", count1)
	}

	// Verify migrating shards count
	migratingCount := testutil.ToFloat64(metrics.ShardsMigrating)
	if migratingCount != 1.0 {
		t.Fatalf("Expected migrating shards count to be 1.0, got %f", migratingCount)
	}
}

func TestUpdateShardMetrics_NoMigrations(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	// Set up test state with shards where TargetNode == CurrentNode (no migrations)
	cm.mu.Lock()
	cm.state.Nodes["localhost:47000"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// All shards have matching TargetNode and CurrentNode (stable state)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"}
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47001"}
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify no shards are migrating
	migratingCount := testutil.ToFloat64(metrics.ShardsMigrating)
	if migratingCount != 0.0 {
		t.Fatalf("Expected migrating shards count to be 0.0, got %f", migratingCount)
	}
}

func TestUpdateShardMetrics_MultipleMigrations(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	// Set up test state with multiple shards in migration
	cm.mu.Lock()
	cm.state.Nodes["localhost:47000"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Multiple shards in migration state
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47000"} // migrating
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: "localhost:47002", CurrentNode: "localhost:47000"} // migrating
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: "localhost:47002", CurrentNode: "localhost:47001"} // migrating
	cm.state.ShardMapping.Shards[3] = ShardInfo{TargetNode: "localhost:47000", CurrentNode: "localhost:47000"} // stable
	cm.state.ShardMapping.Shards[4] = ShardInfo{TargetNode: "localhost:47001", CurrentNode: "localhost:47001"} // stable
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify 3 shards are migrating
	migratingCount := testutil.ToFloat64(metrics.ShardsMigrating)
	if migratingCount != 3.0 {
		t.Fatalf("Expected migrating shards count to be 3.0, got %f", migratingCount)
	}
}

func TestShardMappingWriteFailureMetrics_ModRevisionConflict(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	testutilpkg.LockMetrics(t)

	// Record a ModRevision conflict
	metrics.RecordShardMappingWriteFailure("modrevision_conflict")

	// Verify the metric was incremented
	count := testutil.ToFloat64(metrics.ShardMappingWriteFailuresTotal.WithLabelValues("modrevision_conflict"))
	if count != 1.0 {
		t.Fatalf("Expected modrevision_conflict count to be 1.0, got %f", count)
	}
}

func TestShardMappingWriteFailureMetrics_ConnectionError(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	testutilpkg.LockMetrics(t)

	// Record connection errors
	metrics.RecordShardMappingWriteFailure("connection_error")
	metrics.RecordShardMappingWriteFailure("connection_error")

	// Verify the metric was incremented twice
	count := testutil.ToFloat64(metrics.ShardMappingWriteFailuresTotal.WithLabelValues("connection_error"))
	if count != 2.0 {
		t.Fatalf("Expected connection_error count to be 2.0, got %f", count)
	}
}

func TestShardMappingWriteFailureMetrics_Timeout(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	testutilpkg.LockMetrics(t)

	// Record timeout errors
	metrics.RecordShardMappingWriteFailure("timeout")

	// Verify the metric was incremented
	count := testutil.ToFloat64(metrics.ShardMappingWriteFailuresTotal.WithLabelValues("timeout"))
	if count != 1.0 {
		t.Fatalf("Expected timeout count to be 1.0, got %f", count)
	}
}

func TestShardMappingWriteFailureMetrics_Other(t *testing.T) {
	// Lock metrics to prevent parallel execution with other metrics tests
	testutilpkg.LockMetrics(t)

	// Record other errors
	metrics.RecordShardMappingWriteFailure("other")

	// Verify the metric was incremented
	count := testutil.ToFloat64(metrics.ShardMappingWriteFailuresTotal.WithLabelValues("other"))
	if count != 1.0 {
		t.Fatalf("Expected other count to be 1.0, got %f", count)
	}
}
