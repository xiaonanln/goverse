package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
)

func TestCluster_IsLeader(t *testing.T) {
	// Create a test cluster
	c := &Cluster{
		logger: logger.NewLogger("TestCluster"),
	}

	// Test when etcd manager is not set
	if c.IsLeader() {
		t.Errorf("IsLeader() should return false when etcd manager is not set")
	}

	// Create mock etcd manager (without connecting)
	etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("NewEtcdManager() error: %v", err)
	}
	c.SetEtcdManager(etcdMgr)

	// Test when this node is not set
	if c.IsLeader() {
		t.Errorf("IsLeader() should return false when this node is not set")
	}

	// Create and set this node
	testNode := node.NewNode("node1")
	c.SetThisNode(testNode)

	// Mock nodes list
	etcdMgr.SetNodesForTesting([]string{"node1", "node2", "node3"})

	// node1 should be the leader (smallest address)
	if !c.IsLeader() {
		t.Errorf("IsLeader() should return true for node1")
	}

	// Change to node2
	c.thisNode = node.NewNode("node2")
	if c.IsLeader() {
		t.Errorf("IsLeader() should return false for node2")
	}
}

func TestCluster_InitializeShardMapping(t *testing.T) {
	ctx := context.Background()

	// Create a test cluster
	c := &Cluster{
		logger: logger.NewLogger("TestCluster"),
	}

	// Create mock etcd manager (without connecting)
	etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("NewEtcdManager() error: %v", err)
	}
	c.SetEtcdManager(etcdMgr)

	// Create and set this node as leader
	testNode := node.NewNode("node1")
	c.SetThisNode(testNode)

	// Mock nodes list
	etcdMgr.SetNodesForTesting([]string{"node1", "node2"})

	// Test initialization without being leader
	c.thisNode = node.NewNode("node2")
	err = c.InitializeShardMapping(ctx)
	if err == nil {
		t.Errorf("InitializeShardMapping() should fail when not leader")
	}

	// Make this node the leader
	c.thisNode = node.NewNode("node1")

	// For unit tests without etcd, we can't actually initialize
	// Just verify the cluster has correct methods and structure
	if c.shardMapper == nil {
		t.Errorf("shardMapper should be initialized when SetEtcdManager is called")
	}

	// Test creating a shard mapping directly
	nodes := c.GetNodes()
	mapping, err := c.shardMapper.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	if len(mapping.Shards) != sharding.NumShards {
		t.Errorf("Shard mapping has %d shards, want %d", len(mapping.Shards), sharding.NumShards)
	}

	if mapping.Version != 1 {
		t.Errorf("Shard mapping version = %d, want 1", mapping.Version)
	}
	
	// Verify we can cache the mapping
	c.shardMapper.SetMappingForTesting(mapping)
	
	// Now we can get the mapping from cache
	retrievedMapping, err := c.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("GetShardMapping() error: %v", err)
	}
	
	if retrievedMapping.Version != mapping.Version {
		t.Errorf("Retrieved mapping version = %d, want %d", retrievedMapping.Version, mapping.Version)
	}
}

func TestCluster_UpdateShardMapping(t *testing.T) {
	ctx := context.Background()

	// Create a test cluster
	c := &Cluster{
		logger: logger.NewLogger("TestCluster"),
	}

	// Create mock etcd manager (without connecting)
	etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("NewEtcdManager() error: %v", err)
	}
	c.SetEtcdManager(etcdMgr)

	// Create and set this node as leader
	testNode := node.NewNode("node1")
	c.SetThisNode(testNode)

	// Mock nodes list
	etcdMgr.SetNodesForTesting([]string{"node1", "node2"})

	// Create initial mapping directly (without etcd)
	nodes := c.GetNodes()
	initialMapping, err := c.shardMapper.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}
	
	c.shardMapper.SetMappingForTesting(initialMapping)

	// Add a new node
	etcdMgr.SetNodesForTesting([]string{"node1", "node2", "node3"})

	// Update shard mapping directly (without etcd)
	nodes = c.GetNodes()
	updatedMapping, err := c.shardMapper.UpdateShardMapping(ctx, nodes)
	if err != nil {
		t.Errorf("UpdateShardMapping() error: %v", err)
	}
	
	if updatedMapping.Version != 2 {
		t.Errorf("Shard mapping version = %d, want 2", updatedMapping.Version)
	}
	
	// Cache the updated mapping
	c.shardMapper.SetMappingForTesting(updatedMapping)

	// Verify mapping was updated
	mapping, err := c.GetShardMapping(ctx)
	if err != nil {
		t.Errorf("GetShardMapping() error: %v", err)
	}

	if mapping.Version != 2 {
		t.Errorf("Shard mapping version = %d, want 2", mapping.Version)
	}
}

func TestCluster_GetNodeForObject(t *testing.T) {
	ctx := context.Background()

	// Create a test cluster
	c := &Cluster{
		logger: logger.NewLogger("TestCluster"),
	}

	// Create mock etcd manager (without connecting)
	etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("NewEtcdManager() error: %v", err)
	}
	c.SetEtcdManager(etcdMgr)

	// Create and set this node as leader
	testNode := node.NewNode("node1")
	c.SetThisNode(testNode)

	// Mock nodes list
	etcdMgr.SetNodesForTesting([]string{"node1", "node2"})

	// Create shard mapping directly (without etcd)
	nodes := c.GetNodes()
	mapping, err := c.shardMapper.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}
	
	c.shardMapper.SetMappingForTesting(mapping)

	// Test getting node for various object IDs
	testCases := []string{
		"object1",
		"object2",
		"user-12345",
		"session-abc",
	}

	for _, objectID := range testCases {
		node, err := c.GetNodeForObject(ctx, objectID)
		if err != nil {
			t.Errorf("GetNodeForObject(%s) error: %v", objectID, err)
			continue
		}

		if node != "node1" && node != "node2" {
			t.Errorf("GetNodeForObject(%s) = %s, expected node1 or node2", objectID, node)
		}

		// Verify consistency
		node2, err := c.GetNodeForObject(ctx, objectID)
		if err != nil {
			t.Errorf("GetNodeForObject(%s) second call error: %v", objectID, err)
			continue
		}

		if node != node2 {
			t.Errorf("GetNodeForObject(%s) not consistent: %s vs %s", objectID, node, node2)
		}
	}
}

func TestCluster_GetNodeForShard(t *testing.T) {
	ctx := context.Background()

	// Create a test cluster
	c := &Cluster{
		logger: logger.NewLogger("TestCluster"),
	}

	// Create mock etcd manager (without connecting)
	etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379")
	if err != nil {
		t.Fatalf("NewEtcdManager() error: %v", err)
	}
	c.SetEtcdManager(etcdMgr)

	// Create and set this node as leader
	testNode := node.NewNode("node1")
	c.SetThisNode(testNode)

	// Mock nodes list
	etcdMgr.SetNodesForTesting([]string{"node1", "node2"})

	// Create shard mapping directly (without etcd)
	nodes := c.GetNodes()
	mapping, err := c.shardMapper.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}
	
	c.shardMapper.SetMappingForTesting(mapping)

	// Test getting node for various shard IDs
	testCases := []int{0, 1, 100, 1000, sharding.NumShards - 1}

	for _, shardID := range testCases {
		node, err := c.GetNodeForShard(ctx, shardID)
		if err != nil {
			t.Errorf("GetNodeForShard(%d) error: %v", shardID, err)
			continue
		}

		if node != "node1" && node != "node2" {
			t.Errorf("GetNodeForShard(%d) = %s, expected node1 or node2", shardID, node)
		}
	}

	// Test invalid shard IDs
	invalidCases := []int{-1, sharding.NumShards, sharding.NumShards + 1}
	for _, shardID := range invalidCases {
		_, err := c.GetNodeForShard(ctx, shardID)
		if err == nil {
			t.Errorf("GetNodeForShard(%d) should return error for invalid shard ID", shardID)
		}
	}
}
