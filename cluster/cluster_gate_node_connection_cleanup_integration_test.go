package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGateNodeConnectionCleanupOnGateShutdown tests that when a gate connects and
// registers to a node, and then the gate is shutdown, the node automatically
// clears the connection.
//
// This integration test verifies:
// 1. Gate connects and registers to node via RegisterGate RPC
// 2. Node tracks the gate connection in its gateChannels map
// 3. When gate is shutdown, the node detects disconnection via stream.Context().Done()
// 4. Node automatically cleans up the gate connection (removes from gateChannels)
func TestGateNodeConnectionCleanupOnGateShutdown(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Step 1: Create and start a node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	t.Logf("Created node cluster at %s", nodeAddr)

	// Step 2: Start gRPC server for the node so gate can register with it
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	defer nodeServer.Stop()
	t.Logf("Started mock node server at %s", nodeAddr)

	// Wait for node to be ready
	time.Sleep(500 * time.Millisecond)

	// Step 3: Create a gate that will connect to the node
	gateAddr := testutil.GetFreeAddress()
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGate(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	gateCluster, err := newClusterWithEtcdForTestingGate("gateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	// Start the gate cluster which will trigger registration with nodes
	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	t.Logf("Started gate cluster at %s", gateAddr)

	// Step 4: Wait for gate to register with node
	// The gate should automatically register via the cluster management loop
	time.Sleep(2 * time.Second)

	// Step 5: Verify gate is registered on the node
	// Check that node has gate connection in gateChannels map
	if !hasGateConnection(nodeCluster, gateAddr) {
		t.Fatalf("Node should have gate %s registered after gate start", gateAddr)
	}
	t.Logf("✓ Verified: Gate %s is registered on node", gateAddr)

	// Also verify via GetGates() that node sees the gate in cluster
	gates := nodeCluster.GetGates()
	foundInGates := false
	for _, g := range gates {
		if g == gateAddr {
			foundInGates = true
			break
		}
	}
	if !foundInGates {
		t.Logf("Note: Gate %s not yet in node's gate list (may be etcd delay), but connection exists", gateAddr)
	}

	// Step 6: Get the initial count of gate connections
	initialGateCount := getGateConnectionCount(nodeCluster)
	if initialGateCount == 0 {
		t.Fatalf("Expected at least 1 gate connection, got %d", initialGateCount)
	}
	t.Logf("Node has %d gate connection(s) before shutdown", initialGateCount)

	// Step 7: Shutdown the gate
	t.Logf("Shutting down gate %s...", gateAddr)
	err = gateCluster.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop gate cluster: %v", err)
	}

	// Step 8: Wait for node to detect disconnection and clean up
	// The RegisterGate RPC should detect stream.Context().Done() and call defer UnregisterGateConnection
	time.Sleep(1 * time.Second)

	// Step 9: Verify node automatically cleaned up the gate connection
	if hasGateConnection(nodeCluster, gateAddr) {
		t.Errorf("Node should have cleaned up gate %s connection after gate shutdown", gateAddr)
	}
	t.Logf("✓ Verified: Node automatically cleaned up gate %s connection", gateAddr)

	// Verify gate connection count decreased
	finalGateCount := getGateConnectionCount(nodeCluster)
	if finalGateCount >= initialGateCount {
		t.Errorf("Expected gate connection count to decrease after shutdown, initial=%d, final=%d",
			initialGateCount, finalGateCount)
	}
	t.Logf("Node now has %d gate connection(s) after shutdown", finalGateCount)

	t.Logf("✓ Test passed: Node successfully detected gate shutdown and cleaned up connection")
}

// TestGateNodeConnectionCleanupWithMultipleGates tests that when multiple gates
// connect to a node and one is shutdown, only that gate's connection is cleaned up
func TestGateNodeConnectionCleanupWithMultipleGates(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start a node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	// Start gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	defer nodeServer.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create first gate
	gate1Addr := testutil.GetFreeAddress()
	gw1Config := &gate.GateConfig{
		AdvertiseAddress: gate1Addr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw1, err := gate.NewGate(gw1Config)
	if err != nil {
		t.Fatalf("Failed to create gate 1: %v", err)
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
	t.Logf("Started gate 1 at %s", gate1Addr)

	// Create second gate
	gate2Addr := testutil.GetFreeAddress()
	gw2Config := &gate.GateConfig{
		AdvertiseAddress: gate2Addr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw2, err := gate.NewGate(gw2Config)
	if err != nil {
		t.Fatalf("Failed to create gate 2: %v", err)
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
	t.Logf("Started gate 2 at %s", gate2Addr)

	// Wait for both gates to register
	time.Sleep(3 * time.Second)

	// Verify both gates are registered
	if !hasGateConnection(nodeCluster, gate1Addr) {
		t.Fatalf("Node should have gate 1 %s registered", gate1Addr)
	}
	if !hasGateConnection(nodeCluster, gate2Addr) {
		t.Fatalf("Node should have gate 2 %s registered", gate2Addr)
	}
	t.Logf("✓ Both gates registered on node")

	initialGateCount := getGateConnectionCount(nodeCluster)
	if initialGateCount < 2 {
		t.Fatalf("Expected at least 2 gate connections, got %d", initialGateCount)
	}
	t.Logf("Node has %d gate connections", initialGateCount)

	// Shutdown only gate 1
	t.Logf("Shutting down gate 1 %s...", gate1Addr)
	err = gate1Cluster.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop gate cluster 1: %v", err)
	}

	// Wait for cleanup
	time.Sleep(1 * time.Second)

	// Verify gate 1 is cleaned up but gate 2 is still registered
	if hasGateConnection(nodeCluster, gate1Addr) {
		t.Errorf("Node should have cleaned up gate 1 %s connection", gate1Addr)
	}
	if !hasGateConnection(nodeCluster, gate2Addr) {
		t.Errorf("Node should still have gate 2 %s connection", gate2Addr)
	}

	finalGateCount := getGateConnectionCount(nodeCluster)
	if finalGateCount >= initialGateCount {
		t.Errorf("Expected gate connection count to decrease, initial=%d, final=%d",
			initialGateCount, finalGateCount)
	}

	t.Logf("✓ Verified: Only gate 1 cleaned up, gate 2 still connected")
	t.Logf("Node now has %d gate connection(s)", finalGateCount)
}

// TestGateNodeReconnection tests that if a gate disconnects and reconnects,
// the node properly updates the connection
func TestGateNodeReconnection(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create and start a node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	// Start gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	defer nodeServer.Stop()

	time.Sleep(500 * time.Millisecond)

	gateAddr := testutil.GetFreeAddress()

	// First connection
	gwConfig1 := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw1, err := gate.NewGate(gwConfig1)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	gateCluster1, err := newClusterWithEtcdForTestingGate("gateCluster1", gw1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gateCluster1.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Wait for registration
	time.Sleep(2 * time.Second)

	// Verify first connection
	if !hasGateConnection(nodeCluster, gateAddr) {
		t.Fatalf("Node should have gate registered after first connection")
	}
	t.Logf("✓ First connection established")

	// Disconnect
	err = gateCluster1.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop gate cluster: %v", err)
	}

	// Wait for cleanup
	time.Sleep(1 * time.Second)

	// Verify cleanup
	if hasGateConnection(nodeCluster, gateAddr) {
		t.Errorf("Node should have cleaned up gate connection after disconnect")
	}
	t.Logf("✓ First connection cleaned up")

	// Reconnect with same address
	gwConfig2 := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw2, err := gate.NewGate(gwConfig2)
	if err != nil {
		t.Fatalf("Failed to create gate for reconnection: %v", err)
	}

	gateCluster2, err := newClusterWithEtcdForTestingGate("gateCluster2", gw2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gateCluster2.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster for reconnection: %v", err)
	}
	defer gateCluster2.Stop(ctx)

	// Wait for reconnection
	time.Sleep(2 * time.Second)

	// Verify reconnection
	if !hasGateConnection(nodeCluster, gateAddr) {
		t.Errorf("Node should have gate registered after reconnection")
	}
	t.Logf("✓ Reconnection successful")
}

// getGateConnectionCount returns the number of gate connections on a node
func getGateConnectionCount(c *Cluster) int {
	c.gateChannelsMu.RLock()
	defer c.gateChannelsMu.RUnlock()
	return len(c.gateChannels)
}
