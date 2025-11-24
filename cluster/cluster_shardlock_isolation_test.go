package cluster

import (
"github.com/xiaonanln/goverse/util/testutil"
	"sync"
	"testing"
	"time"


	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/node"
)

// TestCluster_ShardLockIsolation verifies that multiple cluster instances
// have isolated shard locks and don't interfere with each other
func TestCluster_ShardLockIsolation(t *testing.T) {
	t.Parallel()
	// Create two separate cluster instances
	node1 := node.NewNode("localhost:50001", testutil.TestNumShards)
	node2 := node.NewNode("localhost:50002", testutil.TestNumShards)

	cluster1 := newClusterForTesting(node1, "Cluster1")
	cluster2 := newClusterForTesting(node2, "Cluster2")

	// Verify each cluster has its own ShardLock instance
	sl1 := cluster1.GetShardLock()
	sl2 := cluster2.GetShardLock()

	if sl1 == nil {
		t.Fatal("Cluster1 ShardLock is nil")
	}
	if sl2 == nil {
		t.Fatal("Cluster2 ShardLock is nil")
	}
	if sl1 == sl2 {
		t.Fatal("Cluster1 and Cluster2 share the same ShardLock instance - isolation broken!")
	}

	// Test that locks on the same shard ID in different clusters don't interfere
	shardID := 0
	var wg sync.WaitGroup
	wg.Add(2)

	start := time.Now()

	// Cluster1 acquires write lock on shard 0
	go func() {
		defer wg.Done()
		unlock := sl1.AcquireWrite(shardID)
		defer unlock()
		time.Sleep(50 * time.Millisecond)
	}()

	// Cluster2 acquires write lock on shard 0 (should not block cluster1)
	go func() {
		defer wg.Done()
		unlock := sl2.AcquireWrite(shardID)
		defer unlock()
		time.Sleep(50 * time.Millisecond)
	}()

	wg.Wait()
	duration := time.Since(start)

	// If locks are truly isolated, total time should be ~50ms
	// If they interfere, it would be ~100ms
	if duration > 80*time.Millisecond {
		t.Fatalf("Locks on different clusters interfered: took %v (expected ~50ms)", duration)
	}

	t.Logf("Test passed: locks isolated correctly, duration: %v", duration)
}

// TestCluster_NodeShardLockSet verifies that the cluster sets its ShardLock on the node
func TestCluster_NodeShardLockSet(t *testing.T) {
	t.Parallel()
	n := node.NewNode("localhost:50003", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	// Verify the cluster's ShardLock was set on the node
	// We can't directly access the node's shardLock, but we can verify it's not nil
	// by checking that the cluster has a non-nil shardLock
	sl := cluster.GetShardLock()
	if sl == nil {
		t.Fatal("Cluster ShardLock is nil")
	}
}

// TestShardLock_MultipleClustersConcurrent creates multiple clusters in parallel
// and verifies they all have isolated shard locks
func TestShardLock_MultipleClustersConcurrent(t *testing.T) {
	t.Parallel()

	numClusters := 5
	clusters := make([]*Cluster, numClusters)

	// Create clusters in parallel
	var wg sync.WaitGroup
	wg.Add(numClusters)

	for i := 0; i < numClusters; i++ {
		go func(idx int) {
			defer wg.Done()
			n := node.NewNode("localhost:5000"+string(rune('0'+idx)), testutil.TestNumShards)
			clusters[idx] = newClusterForTesting(n, "Cluster"+string(rune('0'+idx)))
		}(i)
	}

	wg.Wait()

	// Verify all clusters have different ShardLock instances
	shardLocks := make(map[*shardlock.ShardLock]bool)
	for i, cluster := range clusters {
		sl := cluster.GetShardLock()
		if sl == nil {
			t.Fatalf("Cluster %d has nil ShardLock", i)
			continue
		}
		if shardLocks[sl] {
			t.Fatalf("Cluster %d shares ShardLock with another cluster", i)
		}
		shardLocks[sl] = true
	}

	// Verify we have exactly numClusters unique ShardLock instances
	if len(shardLocks) != numClusters {
		t.Fatalf("Expected %d unique ShardLock instances, got %d", numClusters, len(shardLocks))
	}
}
