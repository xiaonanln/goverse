package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/nodeconnections"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestClusterSetNodeStabilityDuration(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	
	// Create cluster with production defaults to test setter methods
	c := &Cluster{
		thisNode:         testNode,
		logger:           logger.NewLogger("TestClusterSetNodeStabilityDuration"),
		clusterReadyChan: make(chan bool),
		nodeConnections:  nodeconnections.New(),
	}

	// Test default value (when not set, should use production default)
	if c.GetNodeStabilityDuration() != NodeStabilityDuration {
		t.Errorf("Expected default NodeStabilityDuration to be %v, got %v", NodeStabilityDuration, c.GetNodeStabilityDuration())
	}

	// Set custom duration
	customDuration := 5 * time.Second
	c.SetNodeStabilityDuration(customDuration)
	if c.GetNodeStabilityDuration() != customDuration {
		t.Errorf("Expected NodeStabilityDuration to be %v, got %v", customDuration, c.GetNodeStabilityDuration())
	}
}

func TestClusterNodeStabilityDurationZeroValue(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	
	c := &Cluster{
		thisNode:         testNode,
		logger:           logger.NewLogger("TestClusterNodeStabilityDurationZeroValue"),
		clusterReadyChan: make(chan bool),
		nodeConnections:  nodeconnections.New(),
	}

	// Set to zero (should return default)
	c.SetNodeStabilityDuration(0)
	if c.GetNodeStabilityDuration() != NodeStabilityDuration {
		t.Errorf("Expected default NodeStabilityDuration to be %v when set to 0, got %v", NodeStabilityDuration, c.GetNodeStabilityDuration())
	}
}

func TestClusterNodeStabilityDurationNegativeValue(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	
	c := &Cluster{
		thisNode:         testNode,
		logger:           logger.NewLogger("TestClusterNodeStabilityDurationNegativeValue"),
		clusterReadyChan: make(chan bool),
		nodeConnections:  nodeconnections.New(),
	}

	// Set to negative (should return default)
	c.SetNodeStabilityDuration(-5 * time.Second)
	if c.GetNodeStabilityDuration() != NodeStabilityDuration {
		t.Errorf("Expected default NodeStabilityDuration to be %v when set to negative, got %v", NodeStabilityDuration, c.GetNodeStabilityDuration())
	}
}

func TestClusterStabilityWithCustomDuration(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create cluster with custom short stability duration
	c1 := mustNewCluster(ctx, t, "localhost:47021", testPrefix)
	
	// Set a short stability duration for faster testing
	shortDuration := 2 * time.Second
	c1.SetNodeStabilityDuration(shortDuration)

	// Verify the custom duration is set
	if c1.GetNodeStabilityDuration() != shortDuration {
		t.Errorf("Expected NodeStabilityDuration to be %v, got %v", shortDuration, c1.GetNodeStabilityDuration())
	}

	// Wait for the short stability duration + check interval
	// This should be enough for the cluster to become stable
	waitTime := shortDuration + ShardMappingCheckInterval + 5*time.Second
	time.Sleep(waitTime)

	// Verify cluster becomes ready within the custom duration
	if !c1.IsReady() {
		t.Error("Cluster should be ready after custom short stability duration")
	}

	// Verify consensus manager is ready
	if !c1.GetConsensusManagerForTesting().IsReady() {
		t.Error("ConsensusManager should be ready after custom short stability duration")
	}
}
