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
	if c.GetConsensusManagerForTesting().GetMinQuorum() != 3 {
		t.Errorf("Expected consensus manager minQuorum to be 3, got %d", c.GetConsensusManagerForTesting().GetMinQuorum())
	}
}

func TestConsensusManagerIsReadyWithMinQuorum(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start first cluster
	c1 := mustNewCluster(ctx, t, "localhost:47001", testPrefix)

	// Set minimum nodes to 3
	c1.SetMinQuorum(3)

	// Wait briefly for initialization
	time.Sleep(500 * time.Millisecond)

	// Consensus manager should NOT be ready with only 1 node (minQuorum=3)
	if c1.GetConsensusManagerForTesting().IsReady() {
		t.Error("ConsensusManager should not be ready with only 1 node when minQuorum=3")
	}

	// Create and start more clusters
	c2 := mustNewCluster(ctx, t, "localhost:47002", testPrefix)
	c2.SetMinQuorum(3)

	c3 := mustNewCluster(ctx, t, "localhost:47003", testPrefix)
	c3.SetMinQuorum(3)

	// Wait for node registrations to propagate and shard mapping to update
	// Need to wait for cluster stability duration + shard mapping check interval
	timeout := NodeStabilityDuration + ShardMappingCheckInterval + 5*time.Second
	time.Sleep(timeout)

	// Now consensus manager should be ready
	if !c1.GetConsensusManagerForTesting().IsReady() {
		t.Error("ConsensusManager should be ready with 3 nodes when minQuorum=3")
	}
}

func TestConsensusManagerIsStateStableWithMinQuorum(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start first cluster
	c1 := mustNewCluster(ctx, t, "localhost:47011", testPrefix)

	// Set minimum nodes to 2
	c1.SetMinQuorum(2)

	// Wait for initialization
	time.Sleep(200 * time.Millisecond)

	// State should NOT be stable with only 1 node (minQuorum=2)
	if c1.GetConsensusManagerForTesting().IsStateStable(100 * time.Millisecond) {
		t.Error("Cluster state should not be stable with only 1 node when minQuorum=2")
	}

	// Create and start second cluster
	c2 := mustNewCluster(ctx, t, "localhost:47012", testPrefix)
	c2.SetMinQuorum(2)

	// Wait for node registration to propagate and state to be stable
	time.Sleep(ShardMappingCheckInterval + 2*time.Second)

	// State should now be stable with 2 nodes (minQuorum=2)
	if !c1.GetConsensusManagerForTesting().IsStateStable(100 * time.Millisecond) {
		t.Error("Cluster state should be stable with 2 nodes when minQuorum=2")
	}
}
