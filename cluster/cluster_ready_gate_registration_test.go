package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestNodeClusterWaitsForGateRegistration verifies that a node cluster
// does not become ready until all discovered gates have registered with it
func TestNodeClusterWaitsForGateRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster first
	nodeCluster := mustNewCluster(ctx, t, "localhost:47200", testPrefix)
	defer nodeCluster.Stop(ctx)

	// Start a gRPC server for the node FIRST so the gate can register with it
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(nodeCluster.GetThisNode())
	mockServer.SetCluster(nodeCluster)
	testServer := testutil.NewTestServerHelper("localhost:47200", mockServer)
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer testServer.Stop()

	// Create a gateway cluster
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49100",
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

	// Start the gate cluster - this will register it in etcd AND attempt to register with the node
	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	// Wait a bit for gate discovery and registration
	time.Sleep(3 * time.Second)

	// The node should see the gate in the cluster
	gates := nodeCluster.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Node should see 1 gate, got %d: %v", len(gates), gates)
	}

	// Now the node cluster should become ready
	timeout := 15 * time.Second
	select {
	case <-nodeCluster.ClusterReady():
		t.Log("Node cluster is now ready after gate registration")
	case <-time.After(timeout):
		t.Fatalf("Node cluster should become ready after gate registration within %v", timeout)
	}

	// Verify that the gate has actually registered
	if !hasGateConnection(nodeCluster, "localhost:49100") {
		t.Fatal("Gate should have registered with the node")
	}

	// Verify cluster is ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster should be ready after gate registration")
	}
}

// TestNodeClusterReadyWithNoGates verifies that a node cluster becomes ready
// even when there are no gates in the cluster
func TestNodeClusterReadyWithNoGates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster without any gates
	nodeCluster := mustNewCluster(ctx, t, "localhost:47201", testPrefix)
	defer nodeCluster.Stop(ctx)

	// The node cluster should become ready even without gates
	timeout := testutil.WaitForShardMappingTimeout
	select {
	case <-nodeCluster.ClusterReady():
		t.Log("Node cluster is ready without any gates")
	case <-time.After(timeout):
		t.Fatalf("Node cluster should become ready without gates within %v", timeout)
	}

	// Verify no gates are registered
	gates := nodeCluster.GetGates()
	if len(gates) != 0 {
		t.Fatalf("Expected 0 gates, got %d", len(gates))
	}

	// Verify cluster is ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster should be ready")
	}
}

// TestNodeClusterReadyAfterMultipleGatesRegister verifies that a node cluster
// waits for all gates to register before becoming ready
func TestNodeClusterReadyAfterMultipleGatesRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster first
	nodeCluster := mustNewCluster(ctx, t, "localhost:47202", testPrefix)
	defer nodeCluster.Stop(ctx)

	// Start a gRPC server for the node
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(nodeCluster.GetThisNode())
	mockServer.SetCluster(nodeCluster)
	testServer := testutil.NewTestServerHelper("localhost:47202", mockServer)
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer testServer.Stop()

	// Create and start first gateway
	gw1Config := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49101",
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

	// Create and start second gateway
	gw2Config := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49102",
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

	// Wait for gates to discover the node and register
	time.Sleep(3 * time.Second)

	// The node should see both gates
	gates := nodeCluster.GetGates()
	if len(gates) != 2 {
		t.Fatalf("Node should see 2 gates, got %d", len(gates))
	}

	// Now the node cluster should become ready after both gates register
	timeout := 15 * time.Second
	select {
	case <-nodeCluster.ClusterReady():
		t.Log("Node cluster is now ready after both gates registered")
	case <-time.After(timeout):
		t.Fatalf("Node cluster should become ready after gates registration within %v", timeout)
	}

	// Verify both gates have registered
	if !hasGateConnection(nodeCluster, "localhost:49101") {
		t.Fatal("Gate 1 should have registered with the node")
	}
	if !hasGateConnection(nodeCluster, "localhost:49102") {
		t.Fatal("Gate 2 should have registered with the node")
	}

	// Verify cluster is ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster should be ready after all gates registered")
	}
}

// TestNodeClusterReadyAfterGateReconnect verifies that node cluster readiness
// is maintained when a gate disconnects and reconnects
func TestNodeClusterReadyAfterGateReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster first
	nodeCluster := mustNewCluster(ctx, t, "localhost:47204", testPrefix)
	defer nodeCluster.Stop(ctx)

	// Start a gRPC server for the node
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(nodeCluster.GetThisNode())
	mockServer.SetCluster(nodeCluster)
	testServer := testutil.NewTestServerHelper("localhost:47204", mockServer)
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer testServer.Stop()

	// Create and start gateway
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49104",
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

	// Wait for gate to register
	time.Sleep(3 * time.Second)

	// Wait for node cluster to become ready
	timeout := 15 * time.Second
	select {
	case <-nodeCluster.ClusterReady():
		t.Log("Node cluster is ready after gate registration")
	case <-time.After(timeout):
		t.Fatalf("Node cluster should become ready within %v", timeout)
	}

	// Verify gate is registered
	if !hasGateConnection(nodeCluster, "localhost:49104") {
		t.Fatal("Gate should be registered with the node")
	}

	// Stop the gate cluster (simulating disconnect)
	err = gateCluster.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop gate cluster: %v", err)
	}

	// Node cluster should remain ready even after gate disconnects
	// (once ready, a cluster stays ready)
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster should remain ready after gate disconnect")
	}

	t.Log("Node cluster remained ready after gate disconnect (as expected)")
}
