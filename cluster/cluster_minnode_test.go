package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/consensusmanager"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestClusterSetMinNodes(t *testing.T) {
	c := newClusterForTesting("TestClusterSetMinNodes")

	// Test default value
	if c.GetMinNodes() != 1 {
		t.Errorf("Expected default minNodes to be 1, got %d", c.GetMinNodes())
	}

	// Set minNodes
	c.SetMinNodes(3)
	if c.GetMinNodes() != 3 {
		t.Errorf("Expected minNodes to be 3, got %d", c.GetMinNodes())
	}
}

func TestConsensusManagerMinNodes(t *testing.T) {
	// Create a mock etcd manager
	mgr := &etcdmanager.EtcdManager{}
	cm := consensusmanager.NewConsensusManager(mgr)

	// Test default value
	if cm.GetMinNodes() != 1 {
		t.Errorf("Expected default minNodes to be 1, got %d", cm.GetMinNodes())
	}

	// Set minNodes
	cm.SetMinNodes(3)
	if cm.GetMinNodes() != 3 {
		t.Errorf("Expected minNodes to be 3, got %d", cm.GetMinNodes())
	}
}

func TestClusterMinNodesPropagation(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	c, err := newClusterWithEtcdForTesting("TestClusterMinNodesPropagation", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer c.CloseEtcd()

	// Set minNodes on cluster
	c.SetMinNodes(3)

	// Verify it propagated to consensus manager
	if c.consensusManager.GetMinNodes() != 3 {
		t.Errorf("Expected consensus manager minNodes to be 3, got %d", c.consensusManager.GetMinNodes())
	}
}

func TestConsensusManagerIsReadyWithMinNodes(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create cluster and register nodes
	c1, err := newClusterWithEtcdForTesting("Node1", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer c1.CloseEtcd()

	n1 := node.NewNode("localhost:47001")
	c1.SetThisNode(n1)

	// Set minimum nodes to 3
	c1.SetMinNodes(3)

	// Register first node
	err = c1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 1: %v", err)
	}

	// Initialize consensus manager
	err = c1.consensusManager.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize consensus manager: %v", err)
	}

	// Create shard mapping with only 1 node (should not be ready due to minNodes)
	err = c1.consensusManager.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}

	// Consensus manager should NOT be ready with only 1 node (minNodes=3)
	if c1.consensusManager.IsReady() {
		t.Error("ConsensusManager should not be ready with only 1 node when minNodes=3")
	}

	// Register more nodes
	c2, err := newClusterWithEtcdForTesting("Node2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster 2: %v", err)
	}
	defer c2.CloseEtcd()

	n2 := node.NewNode("localhost:47002")
	c2.SetThisNode(n2)
	c2.SetMinNodes(3)

	err = c2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 2: %v", err)
	}

	c3, err := newClusterWithEtcdForTesting("Node3", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster 3: %v", err)
	}
	defer c3.CloseEtcd()

	n3 := node.NewNode("localhost:47003")
	c3.SetThisNode(n3)
	c3.SetMinNodes(3)

	err = c3.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 3: %v", err)
	}

	// Wait a bit for node registrations to propagate
	time.Sleep(500 * time.Millisecond)

	// Re-initialize to pick up all nodes
	err = c1.consensusManager.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to re-initialize consensus manager: %v", err)
	}

	// Update shard mapping with 3 nodes
	err = c1.consensusManager.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to update shard mapping with 3 nodes: %v", err)
	}

	// Now consensus manager should be ready
	if !c1.consensusManager.IsReady() {
		t.Error("ConsensusManager should be ready with 3 nodes when minNodes=3")
	}
}

func TestConsensusManagerIsStateStableWithMinNodes(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create cluster and register nodes
	c1, err := newClusterWithEtcdForTesting("Node1", "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer c1.CloseEtcd()

	n1 := node.NewNode("localhost:47001")
	c1.SetThisNode(n1)

	// Set minimum nodes to 2
	c1.SetMinNodes(2)

	// Register first node
	err = c1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 1: %v", err)
	}

	// Initialize consensus manager
	err = c1.consensusManager.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize consensus manager: %v", err)
	}

	// Wait for state to be stable (in terms of time)
	time.Sleep(200 * time.Millisecond)

	// State should NOT be stable with only 1 node (minNodes=2)
	if c1.consensusManager.IsStateStable(100 * time.Millisecond) {
		t.Error("Cluster state should not be stable with only 1 node when minNodes=2")
	}

	// Register second node
	c2, err := newClusterWithEtcdForTesting("Node2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster 2: %v", err)
	}
	defer c2.CloseEtcd()

	n2 := node.NewNode("localhost:47002")
	c2.SetThisNode(n2)

	err = c2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 2: %v", err)
	}

	// Wait for node registration to propagate and state to be stable
	time.Sleep(500 * time.Millisecond)

	// Re-initialize to pick up both nodes
	err = c1.consensusManager.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to re-initialize consensus manager: %v", err)
	}

	// Wait again for stability
	time.Sleep(200 * time.Millisecond)

	// State should now be stable with 2 nodes (minNodes=2)
	if !c1.consensusManager.IsStateStable(100 * time.Millisecond) {
		t.Error("Cluster state should be stable with 2 nodes when minNodes=2")
	}
}
