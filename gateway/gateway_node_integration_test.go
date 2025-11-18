package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestGatewayNodeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start a node
	n := node.NewNode("localhost:47100")
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create cluster config
	clusterCfg := cluster.Config{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  testPrefix,
		MinQuorum:   1,
	}

	c, err := cluster.NewCluster(clusterCfg, n)
	if err != nil {
		n.Stop(ctx)
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// Start the cluster (registers node and starts watching)
	err = c.Start(ctx, n)
	if err != nil {
		n.Stop(ctx)
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		c.Stop(ctx)
		n.Stop(ctx)
	})

	// Create and start gateway
	gatewayCfg := &GatewayConfig{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  testPrefix,
	}

	gw, err := NewGateway(gatewayCfg)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gw.Stop()

	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start gateway: %v", err)
	}

	// Wait for both node registration and gateway discovery to stabilize
	time.Sleep(2 * time.Second)

	// Verify gateway can see the node via consensus manager
	nodes := gw.consensusManager.GetNodes()

	if len(nodes) == 0 {
		t.Fatal("Gateway should discover at least one node")
	}

	// Verify the node address is in the list
	foundNode := false
	for _, nodeAddr := range nodes {
		if nodeAddr == "localhost:47100" {
			foundNode = true
			break
		}
	}

	if !foundNode {
		t.Fatalf("Gateway did not discover node localhost:47100, nodes found: %v", nodes)
	}

	t.Logf("Gateway successfully discovered node via ConsensusManager: %v", nodes)
}
