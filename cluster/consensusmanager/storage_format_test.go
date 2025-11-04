package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const testPrefixTimeFormat = "20060102-150405"

// TestStorageFormat verifies that shards are stored in individual keys
func TestStorageFormat(t *testing.T) {
	// Create etcd manager
	prefix := "/test-storage-format-" + time.Now().Format(testPrefixTimeFormat)
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

	// Clean up at the end
	defer func() {
		ctx := context.Background()
		client := mgr.GetClient()
		if client != nil {
			client.Delete(ctx, prefix, clientv3.WithPrefix())
		}
	}()

	// Create consensus manager
	cm := NewConsensusManager(mgr)

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
	err = cm.storeShardMapping(ctx, mapping.Shards)
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
	prefix := "/test-storage-full-" + time.Now().Format(testPrefixTimeFormat)
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

	// Clean up at the end
	defer func() {
		ctx := context.Background()
		client := mgr.GetClient()
		if client != nil {
			client.Delete(ctx, prefix, clientv3.WithPrefix())
		}
	}()

	// Create consensus manager
	cm := NewConsensusManager(mgr)

	// Set up nodes
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.mu.Unlock()

	// Create full shard mapping
	ctx := context.Background()
	err = cm.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to create shard mapping: %v", err)
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
