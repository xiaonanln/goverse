package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGateRegistersToAllNodesAutomatically tests that a gate automatically registers
// with all available nodes using the RegisterGate RPC, and tracks which nodes it has
// registered with
func TestGateRegistersToAllNodesAutomatically(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create three node clusters first
	node1 := mustNewCluster(ctx, t, "localhost:47070", testPrefix)
	node2 := mustNewCluster(ctx, t, "localhost:47071", testPrefix)
	node3 := mustNewCluster(ctx, t, "localhost:47072", testPrefix)

	// Start gRPC servers for each node so gates can register with them
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1.GetThisNode())
	mockServer1.SetCluster(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47070", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2.GetThisNode())
	mockServer2.SetCluster(node2)
	testServer2 := testutil.NewTestServerHelper("localhost:47071", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}

	mockServer3 := testutil.NewMockGoverseServer()
	mockServer3.SetNode(node3.GetThisNode())
	mockServer3.SetCluster(node3)
	testServer3 := testutil.NewTestServerHelper("localhost:47072", mockServer3)
	err = testServer3.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 3: %v", err)
	}

	// Wait for nodes to discover each other and servers to be ready
	time.Sleep(1 * time.Second)

	// Verify all nodes see each other
	if len(node1.GetNodes()) != 3 {
		t.Fatalf("Node1 should see 3 nodes, got %d", len(node1.GetNodes()))
	}

	// Create a gateway
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49030",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	gateCluster, err := newClusterWithEtcdForTestingGate("gateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Wait for gate registration to complete
	// The gate should automatically register with all nodes via the cluster management loop
	time.Sleep(2 * time.Second)

	// Verify that the gate has registered with all three nodes
	// We can verify this by checking if each node's cluster has the gate connection registered
	gateAddr := "localhost:49030"

	// Check node1
	if !hasGateConnection(node1, gateAddr) {
		t.Errorf("Node1 should have gate %s registered", gateAddr)
	}

	// Check node2
	if !hasGateConnection(node2, gateAddr) {
		t.Errorf("Node2 should have gate %s registered", gateAddr)
	}

	// Check node3
	if !hasGateConnection(node3, gateAddr) {
		t.Errorf("Node3 should have gate %s registered", gateAddr)
	}

	t.Logf("Gate %s successfully registered with all 3 nodes", gateAddr)

	// Verify all clusters see the correct number of nodes
	for i, node := range []*Cluster{node1, node2, node3, gateCluster} {
		nodes := node.GetNodes()
		if len(nodes) != 3 {
			t.Errorf("Cluster %d should see 3 nodes, got %d: %v", i+1, len(nodes), nodes)
		}
	}

	// Clean up properly
	gateCluster.Stop(ctx)
	testServer1.Stop()
	testServer2.Stop()
	testServer3.Stop()
}

// hasGateConnection checks if a node cluster has a gate connection registered
func hasGateConnection(c *Cluster, gateAddr string) bool {
	c.gateChannelsMu.RLock()
	defer c.gateChannelsMu.RUnlock()
	_, exists := c.gateChannels[gateAddr]
	return exists
}

// TestGateRegistersToMultipleNodesSimultaneously tests that a gate can register
// with multiple nodes at the same time
func TestGateRegistersToMultipleNodesSimultaneously(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create two node clusters
	node1 := mustNewCluster(ctx, t, "localhost:47080", testPrefix)
	defer node1.Stop(ctx)

	node2 := mustNewCluster(ctx, t, "localhost:47081", testPrefix)
	defer node2.Stop(ctx)

	// Start gRPC servers for the nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1.GetThisNode())
	mockServer1.SetCluster(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47080", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	defer testServer1.Stop()

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2.GetThisNode())
	mockServer2.SetCluster(node2)
	testServer2 := testutil.NewTestServerHelper("localhost:47081", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	defer testServer2.Stop()

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Create a gateway
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49040",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	gateCluster, err := newClusterWithEtcdForTestingGate("gateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	// Wait for registration
	time.Sleep(2 * time.Second)

	// Verify gate registered with both nodes
	gateAddr := "localhost:49040"
	if !hasGateConnection(node1, gateAddr) {
		t.Errorf("Node1 should have gate %s registered", gateAddr)
	}
	if !hasGateConnection(node2, gateAddr) {
		t.Errorf("Node2 should have gate %s registered", gateAddr)
	}

	t.Logf("Gate %s successfully registered with both nodes simultaneously", gateAddr)
}

// TestMultipleGatesRegisterToSameNodes tests that multiple gates can register
// to the same set of nodes
func TestMultipleGatesRegisterToSameNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create two node clusters
	node1 := mustNewCluster(ctx, t, "localhost:47090", testPrefix)
	defer node1.Stop(ctx)

	node2 := mustNewCluster(ctx, t, "localhost:47091", testPrefix)
	defer node2.Stop(ctx)

	// Start gRPC servers for the nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1.GetThisNode())
	mockServer1.SetCluster(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47090", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	defer testServer1.Stop()

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2.GetThisNode())
	mockServer2.SetCluster(node2)
	testServer2 := testutil.NewTestServerHelper("localhost:47091", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	defer testServer2.Stop()

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Create first gateway
	gw1Config := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49050",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw1, err := gate.NewGateway(gw1Config)
	if err != nil {
		t.Fatalf("Failed to create gateway 1: %v", err)
	}
	gate1Cluster, err := newClusterWithEtcdForTestingGate("gate1Cluster", gw1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gate1Cluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster 1: %v", err)
	}
	defer gate1Cluster.Stop(ctx)

	// Create second gateway
	gw2Config := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49051",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw2, err := gate.NewGateway(gw2Config)
	if err != nil {
		t.Fatalf("Failed to create gateway 2: %v", err)
	}
	gate2Cluster, err := newClusterWithEtcdForTestingGate("gate2Cluster", gw2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gate2Cluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster 2: %v", err)
	}
	defer gate2Cluster.Stop(ctx)

	// Wait for registration
	time.Sleep(3 * time.Second)

	// Verify both gates registered with both nodes
	gate1Addr := "localhost:49050"
	gate2Addr := "localhost:49051"

	if !hasGateConnection(node1, gate1Addr) {
		t.Errorf("Node1 should have gate1 %s registered", gate1Addr)
	}
	if !hasGateConnection(node1, gate2Addr) {
		t.Errorf("Node1 should have gate2 %s registered", gate2Addr)
	}
	if !hasGateConnection(node2, gate1Addr) {
		t.Errorf("Node2 should have gate1 %s registered", gate1Addr)
	}
	if !hasGateConnection(node2, gate2Addr) {
		t.Errorf("Node2 should have gate2 %s registered", gate2Addr)
	}

	t.Logf("Both gates successfully registered with both nodes")
}
