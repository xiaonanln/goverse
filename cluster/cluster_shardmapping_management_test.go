package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestStartShardMappingManagement_NoEtcdManager(t *testing.T) {
	t.Parallel()

	cluster := newClusterForTesting("TestCluster")
	ctx := context.Background()

	err := cluster.StartShardMappingManagement(ctx)
	if err == nil {
		t.Error("StartShardMappingManagement should return error when etcd manager is not set")
	}

	expectedErr := "etcd manager not set"
	if err.Error() != expectedErr {
		t.Errorf("StartShardMappingManagement error = %v; want %v", err.Error(), expectedErr)
	}
}

func TestStartShardMappingManagement_NoShardMapper(t *testing.T) {
	t.Parallel()

	cluster := newClusterForTesting("TestCluster")
	ctx := context.Background()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and set etcd manager but don't initialize shard mapper
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.etcdManager = mgr

	err = cluster.StartShardMappingManagement(ctx)
	if err == nil {
		t.Error("StartShardMappingManagement should return error when shard mapper is not initialized")
	}

	expectedErr := "shard mapper not initialized"
	if err.Error() != expectedErr {
		t.Errorf("StartShardMappingManagement error = %v; want %v", err.Error(), expectedErr)
	}
}

func TestStartAndStopShardMappingManagement(t *testing.T) {
	t.Parallel()

	cluster := newClusterForTesting("TestCluster")
	ctx := context.Background()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Set up the cluster with etcd manager and shard mapper
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.SetEtcdManager(mgr)

	// Set a node
	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Start shard mapping management
	err = cluster.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management: %v", err)
	}

	if !cluster.shardMappingRunning {
		t.Error("Shard mapping management should be running")
	}

	// Wait a moment to let the goroutine start
	time.Sleep(100 * time.Millisecond)

	// Stop shard mapping management
	cluster.StopShardMappingManagement()

	if cluster.shardMappingRunning {
		t.Error("Shard mapping management should be stopped")
	}
}

func TestStartShardMappingManagement_AlreadyRunning(t *testing.T) {
	t.Parallel()

	cluster := newClusterForTesting("TestCluster")
	ctx := context.Background()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Set up the cluster
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.SetEtcdManager(mgr)

	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Start shard mapping management
	err = cluster.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management: %v", err)
	}
	defer cluster.StopShardMappingManagement()

	// Try to start again - should not error but log a warning
	err = cluster.StartShardMappingManagement(ctx)
	if err != nil {
		t.Errorf("Starting shard mapping management twice should not error, got: %v", err)
	}
}

func TestEtcdManager_NodeStability(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	// Initially, nodes are not stable (lastNodeChange is zero)
	if mgr.IsNodeListStable(1 * time.Second) {
		t.Error("Node list should not be stable when lastNodeChange is zero")
	}

	// Simulate a node change by using the testing helper
	mgr.SetLastNodeChangeForTesting(time.Now())

	// Right after a change, should not be stable
	if mgr.IsNodeListStable(1 * time.Second) {
		t.Error("Node list should not be stable immediately after a change")
	}

	// Wait a bit
	time.Sleep(1100 * time.Millisecond)

	// Now it should be stable
	if !mgr.IsNodeListStable(1 * time.Second) {
		t.Error("Node list should be stable after waiting")
	}
}

func TestEtcdManager_GetLastNodeChangeTime(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	// Initially, should be zero time
	lastChange := mgr.GetLastNodeChangeTime()
	if !lastChange.IsZero() {
		t.Error("GetLastNodeChangeTime should return zero time initially")
	}

	// Set a time using the testing helper
	now := time.Now()
	mgr.SetLastNodeChangeForTesting(now)

	// Should return the set time
	lastChange = mgr.GetLastNodeChangeTime()
	if lastChange != now {
		t.Errorf("GetLastNodeChangeTime = %v; want %v", lastChange, now)
	}
}

func TestHandleShardMappingCheck_NoNodes(t *testing.T) {
	t.Parallel()

	cluster := newClusterForTesting("TestCluster")

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Set up the cluster
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.SetEtcdManager(mgr)

	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	// Create context for testing
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cluster.shardMappingCtx = ctx

	// When there are no nodes and we're leader, should skip update
	// This test just verifies the function doesn't panic
	cluster.handleShardMappingCheck()
}

func TestShardMappingConstants(t *testing.T) {
	t.Parallel()

	// Verify the constants are set to expected values
	if ShardMappingCheckInterval != 5*time.Second {
		t.Errorf("ShardMappingCheckInterval = %v; want %v", ShardMappingCheckInterval, 5*time.Second)
	}

	if NodeStabilityDuration != 10*time.Second {
		t.Errorf("NodeStabilityDuration = %v; want %v", NodeStabilityDuration, 10*time.Second)
	}
}

func TestCluster_ShardMappingCacheInvalidation(t *testing.T) {
	t.Parallel()

	cluster := newClusterForTesting("TestCluster")

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Set up the cluster
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.SetEtcdManager(mgr)

	// With ConsensusManager, there's no manual cache invalidation
	// The in-memory state is automatically updated via watch
	// This test now verifies that InvalidateShardMappingCache is a no-op
	
	ctx := context.Background()
	
	// Set up a test mapping in ConsensusManager
	testMapping := &sharding.ShardMapping{
		Shards:  make(map[int]string),
		Version: 1,
	}
	
	// Use testing helper to set the mapping
	cluster.consensusManager.SetMappingForTesting(testMapping)

	// Verify mapping is available
	mapping, err := cluster.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping: %v", err)
	}
	if mapping.Version != 1 {
		t.Errorf("Expected version 1, got %d", mapping.Version)
	}

	// Call InvalidateShardMappingCache (should be a no-op)
	cluster.InvalidateShardMappingCache()

	// Mapping should still be available (no-op means no change)
	mapping, err = cluster.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Mapping should still be available after InvalidateShardMappingCache: %v", err)
	}
	if mapping.Version != 1 {
		t.Errorf("Expected version 1 after no-op invalidation, got %d", mapping.Version)
	}
}
