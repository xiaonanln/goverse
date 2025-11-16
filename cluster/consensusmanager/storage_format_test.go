package consensusmanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// TestStorageFormat verifies that shards are stored in individual keys
func TestStorageFormat(t *testing.T) {
	// Create etcd manager
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	// Create consensus manager
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up some nodes
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.mu.Unlock()

	// Create a small shard mapping for testing (not all 8192)
	mapping := &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			mapping.Shards[i] = ShardInfo{
				TargetNode:  "node1",
				CurrentNode: "",
			}
		} else {
			mapping.Shards[i] = ShardInfo{
				TargetNode:  "node2",
				CurrentNode: "",
			}
		}
	}

	// Store the mapping
	ctx := context.Background()
	_, err = cm.storeShardMapping(ctx, mapping.Shards)
	if err != nil {
		t.Fatalf("Failed to store shard mapping: %v", err)
	}

	// Verify that individual shard keys exist in etcd
	client := mgr.GetClient()
	resp, err := client.Get(ctx, prefix+"/shard/", clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("Failed to read from etcd: %v", err)
	}

	// Should have 10 shard keys
	if len(resp.Kvs) != 10 {
		t.Errorf("Expected 10 shard keys, got %d", len(resp.Kvs))
	}

	// Verify each key has the correct format and value
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		value := string(kv.Value)

		// Check key format
		if key[:len(prefix+"/shard/")] != prefix+"/shard/" {
			t.Errorf("Unexpected key format: %s", key)
		}

		// Check value has the format "targetNode,currentNode"
		// For now, currentNode is empty, so it should be "node1," or "node2,"
		if value != "node1," && value != "node2," {
			t.Errorf("Unexpected value format: %s (expected 'node1,' or 'node2,')", value)
		}
	}

	// Load the mapping back and verify
	state, err := cm.loadClusterStateFromEtcd(ctx)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if state.ShardMapping == nil {
		t.Fatal("ShardMapping should not be nil")
	}

	if len(state.ShardMapping.Shards) != 10 {
		t.Errorf("Expected 10 shards in loaded mapping, got %d", len(state.ShardMapping.Shards))
	}

	// Verify all shards match
	for shardID := 0; shardID < 10; shardID++ {
		expectedInfo := mapping.Shards[shardID]
		actualInfo := state.ShardMapping.Shards[shardID]
		if actualInfo.TargetNode != expectedInfo.TargetNode {
			t.Errorf("Shard %d: expected target node %s, got %s", shardID, expectedInfo.TargetNode, actualInfo.TargetNode)
		}
		if actualInfo.CurrentNode != expectedInfo.CurrentNode {
			t.Errorf("Shard %d: expected current node %s, got %s", shardID, expectedInfo.CurrentNode, actualInfo.CurrentNode)
		}
	}

	t.Log("Storage format test passed - shards are stored as individual keys")
}

// TestStorageFormatFullMapping verifies storage works with all 8192 shards
func TestStorageFormatFullMapping(t *testing.T) {
	// Create etcd manager
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	// Create consensus manager
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Set up nodes
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.mu.Unlock()

	// Create full shard mapping
	ctx := context.Background()
	n, err := cm.ReassignShardTargetNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to create shard mapping: %v", err)
	}
	if n == 0 {
		t.Error("Expected shards to be reassigned")
	}

	// Verify that 8192 shard keys exist
	client := mgr.GetClient()
	resp, err := client.Get(ctx, prefix+"/shard/", clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("Failed to read from etcd: %v", err)
	}

	if len(resp.Kvs) != sharding.NumShards {
		t.Errorf("Expected %d shard keys, got %d", sharding.NumShards, len(resp.Kvs))
	}

	// Load back and verify count
	state, err := cm.loadClusterStateFromEtcd(ctx)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if len(state.ShardMapping.Shards) != sharding.NumShards {
		t.Errorf("Expected %d shards in loaded mapping, got %d", sharding.NumShards, len(state.ShardMapping.Shards))
	}

	t.Logf("Successfully stored and loaded all %d shards as individual keys", sharding.NumShards)
}

// TestConditionalPutWithModRevision verifies that shards are stored conditionally based on ModRevision
func TestConditionalPutWithModRevision(t *testing.T) {
	// Create etcd manager
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	// Create consensus manager
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")
	ctx := context.Background()

	// Test 1: Store new shards with ModRevision=0 (should succeed)
	t.Run("NewShardsWithRevisionZero", func(t *testing.T) {
		newShards := map[int]ShardInfo{
			0: {TargetNode: "node1", CurrentNode: "", ModRevision: 0},
			1: {TargetNode: "node2", CurrentNode: "", ModRevision: 0},
		}

		count, err := cm.storeShardMapping(ctx, newShards)
		if err != nil {
			t.Fatalf("Failed to store new shards: %v", err)
		}

		if count != len(newShards) {
			t.Errorf("Expected to store %d shards, got %d", len(newShards), count)
		}

		// Verify shards were created
		client := mgr.GetClient()
		for shardID := range newShards {
			key := fmt.Sprintf("%s/shard/%d", prefix, shardID)
			resp, err := client.Get(ctx, key)
			if err != nil {
				t.Fatalf("Failed to read shard %d: %v", shardID, err)
			}
			if len(resp.Kvs) == 0 {
				t.Errorf("Shard %d was not created", shardID)
			}
		}
	})

	// Test 2: Try to overwrite with ModRevision=0 (should fail)
	t.Run("OverwriteWithRevisionZeroFails", func(t *testing.T) {
		duplicateShards := map[int]ShardInfo{
			0: {TargetNode: "node3", CurrentNode: "", ModRevision: 0},
		}

		count, err := cm.storeShardMapping(ctx, duplicateShards)
		if err == nil {
			t.Error("Expected error when trying to overwrite with ModRevision=0, got nil")
		}
		if count != 0 {
			t.Errorf("Expected 0 successful writes, got %d", count)
		}
	})

	// Test 3: Update with correct ModRevision (should succeed)
	t.Run("UpdateWithCorrectRevision", func(t *testing.T) {
		// First, load the current state to get the actual ModRevision
		state, err := cm.loadClusterStateFromEtcd(ctx)
		if err != nil {
			t.Fatalf("Failed to load state: %v", err)
		}

		// Get the current shard info with its ModRevision
		shard0 := state.ShardMapping.Shards[0]
		if shard0.ModRevision == 0 {
			t.Fatal("Shard 0 should have a non-zero ModRevision after being created")
		}

		// Update with the correct ModRevision
		updateShards := map[int]ShardInfo{
			0: {TargetNode: "node4", CurrentNode: "", ModRevision: shard0.ModRevision},
		}

		count, err := cm.storeShardMapping(ctx, updateShards)
		if err != nil {
			t.Fatalf("Failed to update shard with correct ModRevision: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 successful write, got %d", count)
		}

		// Verify the update
		client := mgr.GetClient()
		resp, err := client.Get(ctx, fmt.Sprintf("%s/shard/0", prefix))
		if err != nil {
			t.Fatalf("Failed to read updated shard: %v", err)
		}
		if len(resp.Kvs) == 0 {
			t.Fatal("Shard 0 should exist")
		}
		value := string(resp.Kvs[0].Value)
		if value != "node4," {
			t.Errorf("Expected value 'node4,', got '%s'", value)
		}
	})

	// Test 4: Update with incorrect ModRevision (should fail)
	t.Run("UpdateWithIncorrectRevisionFails", func(t *testing.T) {
		// Try to update with an old/incorrect ModRevision
		wrongRevisionShards := map[int]ShardInfo{
			0: {TargetNode: "node5", CurrentNode: "", ModRevision: 999},
		}

		count, err := cm.storeShardMapping(ctx, wrongRevisionShards)
		if err == nil {
			t.Error("Expected error when updating with incorrect ModRevision, got nil")
		}
		if count != 0 {
			t.Errorf("Expected 0 successful writes, got %d", count)
		}

		// Verify the shard was not updated
		client := mgr.GetClient()
		resp, err := client.Get(ctx, fmt.Sprintf("%s/shard/0", prefix))
		if err != nil {
			t.Fatalf("Failed to read shard: %v", err)
		}
		value := string(resp.Kvs[0].Value)
		if value == "node5," {
			t.Error("Shard should not have been updated with incorrect ModRevision")
		}
	})
}
