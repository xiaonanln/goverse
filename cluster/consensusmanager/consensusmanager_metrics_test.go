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
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

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

func TestUpdateShardMetrics_WithShards(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up test state with nodes and shards
	cm.mu.Lock()
	cm.state.Nodes[addr1] = true
	cm.state.Nodes[addr2] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Assign some shards to nodes (using CurrentNode for actual ownership)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: addr1, CurrentNode: addr1}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: addr1, CurrentNode: addr1}
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: addr1, CurrentNode: addr1}
	cm.state.ShardMapping.Shards[3] = ShardInfo{TargetNode: addr2, CurrentNode: addr2}
	cm.state.ShardMapping.Shards[4] = ShardInfo{TargetNode: addr2, CurrentNode: addr2}
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr1))
	if count0 != 3.0 {
		t.Fatalf("Expected shard count for localhost:47000 to be 3.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr2))
	if count1 != 2.0 {
		t.Fatalf("Expected shard count for localhost:47001 to be 2.0, got %f", count1)
	}
}

func TestUpdateShardMetrics_NodeWithNoShards(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	addr3 := testutil.GetFreeAddress()
	

	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up test state with nodes but no shards assigned to one node
	cm.mu.Lock()
	cm.state.Nodes[addr1] = true
	cm.state.Nodes[addr2] = true
	cm.state.Nodes[addr1] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Assign all shards to only two nodes, leaving one with zero
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: addr1, CurrentNode: addr1}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: addr2, CurrentNode: addr2}
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics - node with no shards should have count of 0
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr1))
	if count0 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47000 to be 1.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr2))
	if count1 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47001 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr1))
	if count2 != 0.0 {
		t.Fatalf("Expected shard count for localhost:47002 to be 0.0, got %f", count2)
	}
}

func TestUpdateShardMetrics_UnclaimedShards(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up test state with shards that have TargetNode but no CurrentNode (unclaimed)
	cm.mu.Lock()
	cm.state.Nodes[addr1] = true
	cm.state.Nodes[addr2] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Shards with TargetNode but empty CurrentNode (not yet claimed)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: addr1, CurrentNode: ""}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: addr1, CurrentNode: ""}
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: addr2, CurrentNode: addr2}
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics - only claimed shards (with CurrentNode) should be counted
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr1))
	if count0 != 0.0 {
		t.Fatalf("Expected shard count for localhost:47000 to be 0.0 (shards not claimed), got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr2))
	if count1 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47001 to be 1.0, got %f", count1)
	}
}

func TestUpdateShardMetrics_ShardMigration(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up test state simulating a shard migration scenario
	// Shard is being migrated from node 47000 to 47001
	cm.mu.Lock()
	cm.state.Nodes[addr1] = true
	cm.state.Nodes[addr2] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Shard 0: TargetNode is 47001, but CurrentNode is still 47000 (migration in progress)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: addr2, CurrentNode: addr1}
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify metrics - should count based on CurrentNode (actual ownership)
	count0 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr1))
	if count0 != 1.0 {
		t.Fatalf("Expected shard count for localhost:47000 (current) to be 1.0, got %f", count0)
	}

	count1 := testutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues(addr2))
	if count1 != 0.0 {
		t.Fatalf("Expected shard count for localhost:47001 (target) to be 0.0, got %f", count1)
	}

	// Verify migrating shards count
	migratingCount := testutil.ToFloat64(metrics.ShardsMigrating)
	if migratingCount != 1.0 {
		t.Fatalf("Expected migrating shards count to be 1.0, got %f", migratingCount)
	}
}

func TestUpdateShardMetrics_NoMigrations(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up test state with shards where TargetNode == CurrentNode (no migrations)
	cm.mu.Lock()
	cm.state.Nodes[addr1] = true
	cm.state.Nodes[addr2] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// All shards have matching TargetNode and CurrentNode (stable state)
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: addr1, CurrentNode: addr1}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: addr1, CurrentNode: addr1}
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: addr2, CurrentNode: addr2}
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify no shards are migrating
	migratingCount := testutil.ToFloat64(metrics.ShardsMigrating)
	if migratingCount != 0.0 {
		t.Fatalf("Expected migrating shards count to be 0.0, got %f", migratingCount)
	}
}

func TestUpdateShardMetrics_MultipleMigrations(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	addr3 := testutil.GetFreeAddress()
	

	// Lock metrics to prevent parallel execution with other metrics tests
	// This also resets all metrics to ensure clean state
	testutilpkg.LockMetrics(t)

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up test state with multiple shards in migration
	cm.mu.Lock()
	cm.state.Nodes[addr1] = true
	cm.state.Nodes[addr2] = true
	cm.state.Nodes[addr1] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Multiple shards in migration state
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: addr2, CurrentNode: addr1} // migrating
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: addr1, CurrentNode: addr1} // migrating
	cm.state.ShardMapping.Shards[2] = ShardInfo{TargetNode: addr1, CurrentNode: addr2} // migrating
	cm.state.ShardMapping.Shards[3] = ShardInfo{TargetNode: addr1, CurrentNode: addr1} // stable
	cm.state.ShardMapping.Shards[4] = ShardInfo{TargetNode: addr2, CurrentNode: addr2} // stable
	cm.mu.Unlock()

	// Update metrics
	cm.UpdateShardMetrics()

	// Verify 3 shards are migrating
	migratingCount := testutil.ToFloat64(metrics.ShardsMigrating)
	if migratingCount != 3.0 {
		t.Fatalf("Expected migrating shards count to be 3.0, got %f", migratingCount)
	}
}
