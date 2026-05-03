package consensusmanager

import (
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
)

// newTestCM creates a ConsensusManager suitable for unit tests (no etcd).
func newTestCM(nodes []string) *ConsensusManager {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, nodes[0], testNumShards)
	cm.mu.Lock()
	for _, n := range nodes {
		cm.state.Nodes[n] = true
	}
	cm.state.LastChange = time.Now().Add(-1 * time.Hour) // stable
	cm.mu.Unlock()
	return cm
}

// setShards writes a shard mapping directly into cm.state (no etcd).
func setShards(cm *ConsensusManager, shards map[int]ShardInfo) {
	cm.mu.Lock()
	for id, info := range shards {
		cm.state.ShardMapping.Shards[id] = info
	}
	cm.mu.Unlock()
}

// TestCalcReallocateStuckShards_NothingStuck verifies that fully-claimed shards
// are not candidates for reallocation.
func TestCalcReallocateStuckShards_NothingStuck(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: "node1"},
		1: {TargetNode: "node2", CurrentNode: "node2"},
	})

	result := cm.calcReallocateStuckShards(time.Now())
	if result != nil {
		t.Fatalf("expected nil (no stuck shards), got %v", result)
	}
}

// TestCalcReallocateStuckShards_NotYetTimedOut verifies that a shard assigned but
// not yet past the timeout is not reallocated.
func TestCalcReallocateStuckShards_NotYetTimedOut(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	cm.stuckShardTimeout = 30 * time.Second
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: ""}, // assigned, not claimed
	})

	now := time.Now()
	// First call seeds the tracking timestamp.
	result := cm.calcReallocateStuckShards(now)
	if result != nil {
		t.Fatal("first observation should not trigger reallocation")
	}

	// Second call before timeout — still no reallocation.
	result = cm.calcReallocateStuckShards(now.Add(15 * time.Second))
	if result != nil {
		t.Fatal("call before timeout should not trigger reallocation")
	}
}

// TestCalcReallocateStuckShards_TimedOut verifies that a shard stuck past the
// timeout is reassigned to a different live node.
func TestCalcReallocateStuckShards_TimedOut(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	cm.stuckShardTimeout = 30 * time.Second
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: ""},
	})

	now := time.Now()
	// Seed the tracking entry.
	cm.calcReallocateStuckShards(now)

	// Advance past the timeout.
	result := cm.calcReallocateStuckShards(now.Add(31 * time.Second))
	if result == nil {
		t.Fatal("expected reallocation after timeout, got nil")
	}
	info, ok := result[0]
	if !ok {
		t.Fatal("expected shard 0 in result")
	}
	if info.TargetNode == "node1" {
		t.Fatalf("expected TargetNode to change from node1, got %s", info.TargetNode)
	}
	if info.TargetNode != "node2" {
		t.Fatalf("expected TargetNode=node2, got %s", info.TargetNode)
	}
}

// TestCalcReallocateStuckShards_SingleNode verifies that with only one node,
// no reallocation is possible and no error is produced.
func TestCalcReallocateStuckShards_SingleNode(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1"})
	cm.stuckShardTimeout = 1 * time.Millisecond
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: ""},
	})

	now := time.Now()
	cm.calcReallocateStuckShards(now)
	result := cm.calcReallocateStuckShards(now.Add(1 * time.Second))
	if result != nil {
		t.Fatalf("single-node cluster must not reallocate, got %v", result)
	}
}

// TestCalcReallocateStuckShards_DeadTarget verifies that a shard whose TargetNode
// is dead is NOT treated as stuck (ReassignShardTargetNodes handles that path).
func TestCalcReallocateStuckShards_DeadTarget(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	cm.stuckShardTimeout = 1 * time.Millisecond
	// TargetNode "dead-node" is not in the active nodes map.
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "dead-node", CurrentNode: ""},
	})

	now := time.Now()
	cm.calcReallocateStuckShards(now)
	result := cm.calcReallocateStuckShards(now.Add(1 * time.Second))
	if result != nil {
		t.Fatal("dead target must not be treated as stuck; ReassignShardTargetNodes handles it")
	}
}

// TestCalcReallocateStuckShards_TrackingReset verifies that after a shard is
// reallocated, its tracking entry is removed so the new target gets a fresh window.
func TestCalcReallocateStuckShards_TrackingReset(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	cm.stuckShardTimeout = 10 * time.Millisecond
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: ""},
	})

	now := time.Now()
	cm.calcReallocateStuckShards(now)                            // seed
	cm.calcReallocateStuckShards(now.Add(20 * time.Millisecond)) // triggers reallocation

	// After reallocation, stuckShardFirstSeen must not contain the shard.
	if _, found := cm.stuckShardFirstSeen[0]; found {
		t.Fatal("tracking entry should be cleared after reallocation")
	}
}

// TestCalcReallocateStuckShards_AlreadyClaimed verifies that a shard that gets
// claimed between observations is removed from the tracking map.
func TestCalcReallocateStuckShards_AlreadyClaimed(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	cm.stuckShardTimeout = 30 * time.Second
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: ""},
	})

	now := time.Now()
	// Seed.
	cm.calcReallocateStuckShards(now)
	if _, found := cm.stuckShardFirstSeen[0]; !found {
		t.Fatal("shard should be tracked after first observation")
	}

	// Node claims the shard before timeout.
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: "node1"},
	})

	// Next call should clear the tracking entry.
	result := cm.calcReallocateStuckShards(now.Add(1 * time.Second))
	if result != nil {
		t.Fatal("claimed shard must not be reallocated")
	}
	if _, found := cm.stuckShardFirstSeen[0]; found {
		t.Fatal("tracking entry should be removed once shard is claimed")
	}
}

// TestCalcReallocateStuckShards_PinnedShard verifies that pinned shards are never
// reallocated, even when their target node does not claim them in time.
func TestCalcReallocateStuckShards_PinnedShard(t *testing.T) {
	t.Parallel()
	cm := newTestCM([]string{"node1", "node2"})
	cm.stuckShardTimeout = 1 * time.Millisecond
	setShards(cm, map[int]ShardInfo{
		0: {TargetNode: "node1", CurrentNode: "", Flags: []string{"pinned"}},
	})

	now := time.Now()
	cm.calcReallocateStuckShards(now) // seed (should not track pinned shard)
	result := cm.calcReallocateStuckShards(now.Add(1 * time.Second))
	if result != nil {
		t.Fatal("pinned shard must never be reallocated")
	}
	if _, found := cm.stuckShardFirstSeen[0]; found {
		t.Fatal("pinned shard must not be added to stuckShardFirstSeen tracking")
	}
}

// TestStuckShardDefaultTimeout verifies the built-in default is 1 minute.
func TestStuckShardDefaultTimeout(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "node1", testNumShards)
	if cm.stuckShardTimeout != defaultStuckShardTimeout {
		t.Fatalf("expected default %v, got %v", defaultStuckShardTimeout, cm.stuckShardTimeout)
	}
	if defaultStuckShardTimeout != 60*time.Second {
		t.Fatalf("defaultStuckShardTimeout should be 1 minute, got %v", defaultStuckShardTimeout)
	}
}
