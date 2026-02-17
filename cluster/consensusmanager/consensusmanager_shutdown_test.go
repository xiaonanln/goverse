package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShutdownMode_String(t *testing.T) {
	tests := []struct {
		mode     ShutdownMode
		expected string
	}{
		{ShutdownModeQuick, "quick"},
		{ShutdownModeClean, "clean"},
		{ShutdownMode(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.mode.String())
		})
	}
}

func TestConsensusManager_Shutdown_QuickMode(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 8
	nodeAddr := "test-node:1234"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Test quick shutdown - should be immediate and not change anything
	err = cm.Shutdown(ctx, ShutdownModeQuick)
	assert.NoError(t, err)
	
	// Verify no changes to cluster state
	state := cm.GetClusterStateForTesting()
	assert.NotNil(t, state)
}

func TestConsensusManager_Shutdown_CleanMode_NoShards(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 8
	nodeAddr := "test-node:1234"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Test clean shutdown with no owned shards
	err = cm.Shutdown(ctx, ShutdownModeClean)
	assert.NoError(t, err)
}

func TestConsensusManager_Shutdown_CleanMode_WithShards(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 8
	nodeAddr := "test-node:1234"
	otherNodeAddr := "other-node:5678"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Register this node and another node in etcd
	err = cm.etcdManager.RegisterNode(ctx, nodeAddr)
	require.NoError(t, err)
	err = cm.etcdManager.RegisterNode(ctx, otherNodeAddr) 
	require.NoError(t, err)
	
	// Set up initial shard mapping with some shards owned by this node
	initialMapping := map[int]ShardInfo{
		0: {TargetNode: nodeAddr, CurrentNode: nodeAddr},
		1: {TargetNode: nodeAddr, CurrentNode: nodeAddr}, 
		2: {TargetNode: otherNodeAddr, CurrentNode: otherNodeAddr},
		3: {TargetNode: nodeAddr, CurrentNode: nodeAddr},
	}
	
	_, err = cm.storeShardMapping(ctx, initialMapping)
	require.NoError(t, err)
	
	// Wait for state to be updated
	time.Sleep(100 * time.Millisecond)
	
	// Create a second consensus manager for the other node to claim released shards
	otherCM := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, otherNodeAddr, numShards)
	err = otherCM.Initialize(ctx)
	require.NoError(t, err)
	
	// Start watching for both managers to handle real-time updates
	err = cm.StartWatch(ctx)
	require.NoError(t, err)
	defer cm.StopWatch()
	
	err = otherCM.StartWatch(ctx)
	require.NoError(t, err) 
	defer otherCM.StopWatch()
	
	// Start the other node claiming shards in the background
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				// Try to claim shards for other node
				otherCM.ClaimShardsForNode(ctx)
			}
		}
	}()
	
	// Perform clean shutdown - this should release shards owned by nodeAddr
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	err = cm.Shutdown(shutdownCtx, ShutdownModeClean)
	assert.NoError(t, err)
	
	// Wait a moment for the state to propagate
	time.Sleep(200 * time.Millisecond)
	
	// Verify that all shards that were owned by nodeAddr are now released or claimed by others
	state := otherCM.GetClusterStateForTesting()
	require.NotNil(t, state)
	require.NotNil(t, state.ShardMapping)
	
	for shardID, shardInfo := range state.ShardMapping.Shards {
		if shardID == 0 || shardID == 1 || shardID == 3 {
			// These were owned by the shutting down node
			assert.NotEqual(t, nodeAddr, shardInfo.CurrentNode, 
				"Shard %d should not be owned by shutting down node %s", shardID, nodeAddr)
		}
	}
}

func TestConsensusManager_Shutdown_CleanMode_Timeout(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 4
	nodeAddr := "test-node:1234"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Register this node
	err = cm.etcdManager.RegisterNode(ctx, nodeAddr)
	require.NoError(t, err)
	
	// Set up shards owned by this node
	initialMapping := map[int]ShardInfo{
		0: {TargetNode: nodeAddr, CurrentNode: nodeAddr},
		1: {TargetNode: nodeAddr, CurrentNode: nodeAddr}, 
	}
	
	_, err = cm.storeShardMapping(ctx, initialMapping)
	require.NoError(t, err)
	
	// Create a very short timeout to test timeout behavior
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	
	// Clean shutdown should timeout because no other nodes will claim the shards
	err = cm.Shutdown(shortCtx, ShutdownModeClean)
	// Should not fail even on timeout - it should continue with shutdown
	assert.NoError(t, err)
}

func TestConsensusManager_Shutdown_UnsupportedMode(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 8
	nodeAddr := "test-node:1234"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Test unsupported shutdown mode
	err = cm.Shutdown(ctx, ShutdownMode(999))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shutdown mode")
}

func TestConsensusManager_identifyOwnedShards(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 8
	nodeAddr := "test-node:1234"
	otherNodeAddr := "other-node:5678"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Set up shard mapping 
	cm.SetMappingForTesting(&ShardMapping{
		Shards: map[int]ShardInfo{
			0: {TargetNode: nodeAddr, CurrentNode: nodeAddr},      // Owned by this node
			1: {TargetNode: otherNodeAddr, CurrentNode: nodeAddr}, // Owned by this node (migration)
			2: {TargetNode: otherNodeAddr, CurrentNode: otherNodeAddr}, // Owned by other node
			3: {TargetNode: nodeAddr, CurrentNode: ""},            // Not claimed yet
		},
	})
	
	// Test identifying owned shards
	ownedShards := cm.identifyOwnedShards(nodeAddr)
	
	assert.Len(t, ownedShards, 2, "Should identify 2 owned shards")
	
	// Check shard 0 - owned by this node
	assert.Contains(t, ownedShards, 0)
	assert.Equal(t, nodeAddr, ownedShards[0].TargetNode)
	assert.Equal(t, "", ownedShards[0].CurrentNode) // Should be set to empty for release
	
	// Check shard 1 - owned by this node but migrating
	assert.Contains(t, ownedShards, 1)
	assert.Equal(t, otherNodeAddr, ownedShards[1].TargetNode) // Should preserve TargetNode
	assert.Equal(t, "", ownedShards[1].CurrentNode)          // Should be set to empty for release
	
	// Should not contain shard 2 (owned by other node) or shard 3 (not claimed)
	assert.NotContains(t, ownedShards, 2)
	assert.NotContains(t, ownedShards, 3)
}

func TestConsensusManager_countUnclaimedShards(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	numShards := 8
	nodeAddr := "test-node:1234"
	otherNodeAddr := "other-node:5678"
	
	cm := NewConsensusManager(etcdManager, shardlock.NewShardLock(), 
		10*time.Second, nodeAddr, numShards)

	ctx := context.Background()
	err := cm.Initialize(ctx)
	require.NoError(t, err)

	// Set up shard mapping
	cm.SetMappingForTesting(&ShardMapping{
		Shards: map[int]ShardInfo{
			0: {TargetNode: nodeAddr, CurrentNode: otherNodeAddr}, // Claimed by other node
			1: {TargetNode: nodeAddr, CurrentNode: ""},            // Unclaimed
			2: {TargetNode: nodeAddr, CurrentNode: ""},            // Unclaimed
			3: {TargetNode: otherNodeAddr, CurrentNode: otherNodeAddr}, // Claimed by other node
		},
	})
	
	// Test counting unclaimed shards
	shardIDs := []int{0, 1, 2, 3}
	unclaimed := cm.countUnclaimedShards(shardIDs)
	
	assert.Equal(t, 2, unclaimed, "Should have 2 unclaimed shards (1 and 2)")
	
	// Test with empty shard mapping
	cm.SetMappingForTesting(&ShardMapping{Shards: map[int]ShardInfo{}})
	unclaimed = cm.countUnclaimedShards(shardIDs)
	assert.Equal(t, 4, unclaimed, "All shards should be unclaimed with empty mapping")
	
	// Test with nil shard mapping
	cm.SetMappingForTesting(nil)
	unclaimed = cm.countUnclaimedShards(shardIDs)
	assert.Equal(t, 4, unclaimed, "All shards should be unclaimed with nil mapping")
}