package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/consensusmanager"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestClusterSetMinQuorum(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	c := newClusterForTesting(testNode, "TestClusterSetMinQuorum")

	// Test default value
	if c.GetMinQuorum() != 1 {
		t.Errorf("Expected default minQuorum to be 1, got %d", c.GetMinQuorum())
	}

	// Set minQuorum
	c.SetMinQuorum(3)
	if c.GetMinQuorum() != 3 {
		t.Errorf("Expected minQuorum to be 3, got %d", c.GetMinQuorum())
	}
}

func TestConsensusManagerMinQuorum(t *testing.T) {
	// Create a mock etcd manager
	mgr := &etcdmanager.EtcdManager{}
	cm := consensusmanager.NewConsensusManager(mgr)

	// Test default value
	if cm.GetMinQuorum() != 1 {
		t.Errorf("Expected default minQuorum to be 1, got %d", cm.GetMinQuorum())
	}

	// Set minQuorum
	cm.SetMinQuorum(3)
	if cm.GetMinQuorum() != 3 {
		t.Errorf("Expected minQuorum to be 3, got %d", cm.GetMinQuorum())
	}
}

func TestClusterMinQuorumPropagation(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()
	n := testutil.MustNewNode(ctx, t, "localhost:47001")
	c, err := newClusterWithEtcdForTesting("TestClusterMinQuorumPropagation", n, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	// Set minQuorum on cluster
	c.SetMinQuorum(3)

	// Verify it propagated to consensus manager
	if c.consensusManager.GetMinQuorum() != 3 {
		t.Errorf("Expected consensus manager minQuorum to be 3, got %d", c.consensusManager.GetMinQuorum())
	}
}

func TestConsensusManagerIsReadyWithMinQuorum(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create cluster and register nodes
	n1 := testutil.MustNewNode(ctx, t, "localhost:47001")
	c1, err := newClusterWithEtcdForTesting("Node1", n1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer c1.closeEtcd()

	// Set minimum nodes to 3
	c1.SetMinQuorum(3)

	err = c1.startWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching cluster state: %v", err)
	}

	// Register first node
	err = c1.registerNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 1: %v", err)
	}

	// Wait briefly for the etcd registration to be written by the keep-alive goroutine
	time.Sleep(500 * time.Millisecond)

	// Initialize consensus manager
	err = c1.consensusManager.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize consensus manager: %v", err)
	}

	time.Sleep(1 * time.Second) // Wait a bit for initialization

	// Create shard mapping with only 1 node (should not be ready due to minQuorum)
	err = c1.consensusManager.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	} else {
		t.Logf("Shard mapping with 1 node created")
	}

	// Consensus manager should NOT be ready with only 1 node (minQuorum=3)
	if c1.consensusManager.IsReady() {
		t.Error("ConsensusManager should not be ready with only 1 node when minQuorum=3")
	}

	// Register more nodes
	n2 := testutil.MustNewNode(ctx, t, "localhost:47002")
	c2, err := newClusterWithEtcdForTesting("Node2", n2, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster 2: %v", err)
	}
	defer c2.closeEtcd()

	c2.SetMinQuorum(3)

	err = c2.registerNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node 2: %v", err)
	}

	n3 := testutil.MustNewNode(ctx, t, "localhost:47003")
	c3, err := newClusterWithEtcdForTesting("Node3", n3, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster 3: %v", err)
	}
	defer c3.closeEtcd()

	c3.SetMinQuorum(3)

	err = c3.registerNode(ctx)
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
		t.Error("ConsensusManager should be ready with 3 nodes when minQuorum=3")
	}
}

func TestConsensusManagerIsStateStableWithMinQuorum(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create cluster and register nodes
	n1 := testutil.MustNewNode(ctx, t, "localhost:47001")
	c1, err := newClusterWithEtcdForTesting("Node1", n1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer c1.closeEtcd()

	// Set minimum nodes to 2
	c1.SetMinQuorum(2)

	// Register first node
	err = c1.registerNode(ctx)
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

	// State should NOT be stable with only 1 node (minQuorum=2)
	if c1.consensusManager.IsStateStable(100 * time.Millisecond) {
		t.Error("Cluster state should not be stable with only 1 node when minQuorum=2")
	}

	// Register second node
	n2 := testutil.MustNewNode(ctx, t, "localhost:47002")
	c2, err := newClusterWithEtcdForTesting("Node2", n2, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster 2: %v", err)
	}
	defer c2.closeEtcd()

	err = c2.registerNode(ctx)
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

	// State should now be stable with 2 nodes (minQuorum=2)
	if !c1.consensusManager.IsStateStable(100 * time.Millisecond) {
		t.Error("Cluster state should be stable with 2 nodes when minQuorum=2")
	}
}
