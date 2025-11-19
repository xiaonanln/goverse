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

func TestMultiGatewayMultiNodeIntegration(t *testing.T) {

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start 3 nodes
	nodes := []*node.Node{
		node.NewNode("localhost:47201"),
		node.NewNode("localhost:47202"),
		node.NewNode("localhost:47203"),
	}

	clusters := make([]*cluster.Cluster, 3)

	for i, n := range nodes {
		err := n.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start node %d: %v", i+1, err)
		}

		clusterCfg := cluster.Config{
			EtcdAddress: "localhost:2379",
			EtcdPrefix:  testPrefix,
			MinQuorum:   1,
		}

		c, err := cluster.NewCluster(clusterCfg, n)
		if err != nil {
			n.Stop(ctx)
			t.Fatalf("Failed to create cluster %d: %v", i+1, err)
		}

		err = c.Start(ctx, n)
		if err != nil {
			n.Stop(ctx)
			t.Fatalf("Failed to start cluster %d: %v", i+1, err)
		}

		clusters[i] = c
	}

	// Register cleanup for all nodes
	t.Cleanup(func() {
		for i, c := range clusters {
			c.Stop(ctx)
			nodes[i].Stop(ctx)
		}
	})

	// Create and start 2 gateways
	gateways := make([]*Gateway, 2)

	for i := 0; i < 2; i++ {
		gatewayCfg := &GatewayConfig{
			EtcdAddress: "localhost:2379",
			EtcdPrefix:  testPrefix,
		}

		gw, err := NewGateway(gatewayCfg)
		if err != nil {
			t.Fatalf("Failed to create gateway %d: %v", i+1, err)
		}
		defer gw.Stop()

		err = gw.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start gateway %d: %v", i+1, err)
		}

		gateways[i] = gw
	}

	// Wait for node registration and gateway discovery to stabilize
	time.Sleep(2 * time.Second)

	// Verify both gateways can see all 3 nodes
	expectedNodes := []string{"localhost:47201", "localhost:47202", "localhost:47203"}

	for i, gw := range gateways {
		discoveredNodes := gw.consensusManager.GetNodes()

		if len(discoveredNodes) != 3 {
			t.Fatalf("Gateway %d should discover 3 nodes, but found %d: %v", i+1, len(discoveredNodes), discoveredNodes)
		}

		// Verify all expected nodes are present
		for _, expectedNode := range expectedNodes {
			found := false
			for _, discoveredNode := range discoveredNodes {
				if discoveredNode == expectedNode {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Gateway %d did not discover node %s, nodes found: %v", i+1, expectedNode, discoveredNodes)
			}
		}

		t.Logf("Gateway %d successfully discovered all nodes: %v", i+1, discoveredNodes)
	}
}
