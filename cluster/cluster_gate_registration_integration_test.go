package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// TestGateRegistrationWithEtcd tests that gates are properly registered in etcd under /gates/
func TestGateRegistrationWithEtcd(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a gate
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49001",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGate(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	// Create cluster with gate
	cluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	// Start the cluster (which registers the gate)
	err = cluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}
	defer cluster.Stop(ctx)

	// Wait for registration to complete
	time.Sleep(500 * time.Millisecond)

	// Verify gate is registered in etcd
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create etcd client: %v", err)
	}
	defer etcdClient.Close()

	gatesPrefix := testPrefix + "/gates/"
	key := gatesPrefix + "localhost:49001"

	resp, err := etcdClient.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to check gate key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Gate key %s should exist after cluster start", key)
	}
	t.Logf("Gate key %s exists as expected", key)

	// Verify gate is in the cluster's gate list
	gates := cluster.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}
	if gates[0] != "localhost:49001" {
		t.Fatalf("Expected gate localhost:49001, got %s", gates[0])
	}
}

// TestGateUnregistrationWithEtcd tests that gates are properly unregistered from etcd when stopped
func TestGateUnregistrationWithEtcd(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a gate
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49002",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGate(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	// Create cluster with gate
	cluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	// Start the cluster
	err = cluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Create etcd client for verification
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create etcd client: %v", err)
	}
	defer etcdClient.Close()

	gatesPrefix := testPrefix + "/gates/"
	key := gatesPrefix + "localhost:49002"

	// Verify gate is registered
	resp, err := etcdClient.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to check gate key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Gate key %s should exist after cluster start", key)
	}

	// Stop the cluster
	err = cluster.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop cluster: %v", err)
	}

	// Wait for unregistration
	time.Sleep(500 * time.Millisecond)

	// Verify gate is unregistered
	resp, err = etcdClient.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to check gate key after stop: %v", err)
	}
	if len(resp.Kvs) > 0 {
		t.Fatalf("Gate key %s should be removed after cluster stop", key)
	}
	t.Logf("Gate key %s successfully removed after stop", key)
}

// TestGateDiscoveryByNodes tests that nodes can discover gates in the cluster
func TestGateDiscoveryByNodes(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a gate cluster
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49003",
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

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	// Create a node cluster
	nodeCluster := mustNewCluster(ctx, t, "localhost:47020", testPrefix)
	defer nodeCluster.Stop(ctx)

	// Wait for discovery
	time.Sleep(1 * time.Second)

	// Node cluster should see the gate
	gates := nodeCluster.GetGates()
	t.Logf("Node cluster sees %d gates: %v", len(gates), gates)

	if len(gates) != 1 {
		t.Fatalf("Node cluster should see 1 gate, got %d", len(gates))
	}
	if gates[0] != "localhost:49003" {
		t.Fatalf("Expected gate localhost:49003, got %s", gates[0])
	}

	// Gate cluster should also see itself
	gatesFromGate := gateCluster.GetGates()
	if len(gatesFromGate) != 1 {
		t.Fatalf("Gate cluster should see 1 gate (itself), got %d", len(gatesFromGate))
	}
}

// TestMultipleGatesDiscovery tests that multiple gates can discover each other
func TestMultipleGatesDiscovery(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create first gate
	gw1Config := &gate.GateConfig{
		AdvertiseAddress: "localhost:49004",
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
	defer gate1Cluster.Stop(ctx)

	// Create second gate
	gw2Config := &gate.GateConfig{
		AdvertiseAddress: "localhost:49005",
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

	// Wait for discovery
	time.Sleep(1 * time.Second)

	// Both gates should see each other
	gates1 := gate1Cluster.GetGates()
	t.Logf("Gate 1 sees %d gates: %v", len(gates1), gates1)

	if len(gates1) != 2 {
		t.Fatalf("Gate 1 should see 2 gates, got %d", len(gates1))
	}

	gates2 := gate2Cluster.GetGates()
	t.Logf("Gate 2 sees %d gates: %v", len(gates2), gates2)

	if len(gates2) != 2 {
		t.Fatalf("Gate 2 should see 2 gates, got %d", len(gates2))
	}

	// Verify each gate sees the other
	found1 := false
	found2 := false
	for _, g := range gates1 {
		if g == "localhost:49005" {
			found1 = true
		}
	}
	for _, g := range gates2 {
		if g == "localhost:49004" {
			found2 = true
		}
	}

	if !found1 {
		t.Fatal("Gate 1 should see Gate 2")
	}
	if !found2 {
		t.Fatal("Gate 2 should see Gate 1")
	}
}

// TestGateDynamicDiscovery tests that existing clusters discover new gates dynamically
func TestGateDynamicDiscovery(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster first
	nodeCluster := mustNewCluster(ctx, t, "localhost:47030", testPrefix)
	defer nodeCluster.Stop(ctx)

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Node should not see any gates initially
	initialGates := nodeCluster.GetGates()
	if len(initialGates) != 0 {
		t.Fatalf("Node should not see any gates initially, got %d", len(initialGates))
	}

	// Create a gate
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49006",
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

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	// Wait for discovery
	time.Sleep(1 * time.Second)

	// Node should now see the gate
	updatedGates := nodeCluster.GetGates()
	t.Logf("Node now sees %d gates: %v", len(updatedGates), updatedGates)

	if len(updatedGates) != 1 {
		t.Fatalf("Node should see 1 gate after it joined, got %d", len(updatedGates))
	}
	if updatedGates[0] != "localhost:49006" {
		t.Fatalf("Expected gate localhost:49006, got %s", updatedGates[0])
	}
}

// TestGateLeaveDetection tests that clusters detect when gates leave
func TestGateLeaveDetection(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a node cluster
	nodeCluster := mustNewCluster(ctx, t, "localhost:47040", testPrefix)
	defer nodeCluster.Stop(ctx)

	// Create a gate
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49007",
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

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Wait for discovery
	time.Sleep(1 * time.Second)

	// Verify node sees the gate
	gates := nodeCluster.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Node should see 1 gate, got %d", len(gates))
	}

	// Stop the gate cluster
	err = gateCluster.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop gate cluster: %v", err)
	}

	// Wait for detection
	time.Sleep(1 * time.Second)

	// Node should no longer see the gate
	updatedGates := nodeCluster.GetGates()
	t.Logf("After gate stopped, node sees %d gates: %v", len(updatedGates), updatedGates)

	if len(updatedGates) != 0 {
		t.Fatalf("Node should not see any gates after gate stopped, got %d", len(updatedGates))
	}
}

// TestMixedClusterWithNodesAndGates tests a mixed cluster with both nodes and gates
func TestMixedClusterWithNodesAndGates(t *testing.T) {
	t.Parallel()
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create two node clusters
	node1 := mustNewCluster(ctx, t, "localhost:47050", testPrefix)
	defer node1.Stop(ctx)

	node2 := mustNewCluster(ctx, t, "localhost:47051", testPrefix)
	defer node2.Stop(ctx)

	// Create two gate clusters
	gw1Config := &gate.GateConfig{
		AdvertiseAddress: "localhost:49010",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw1, err := gate.NewGate(gw1Config)
	if err != nil {
		t.Fatalf("Failed to create gate 1: %v", err)
	}
	gate1, err := newClusterWithEtcdForTestingGate("gate1", gw1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gate1.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster 1: %v", err)
	}
	defer gate1.Stop(ctx)

	gw2Config := &gate.GateConfig{
		AdvertiseAddress: "localhost:49011",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw2, err := gate.NewGate(gw2Config)
	if err != nil {
		t.Fatalf("Failed to create gate 2: %v", err)
	}
	gate2, err := newClusterWithEtcdForTestingGate("gate2", gw2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gate2.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster 2: %v", err)
	}
	defer gate2.Stop(ctx)

	// Wait for discovery
	time.Sleep(1 * time.Second)

	// All clusters should see 2 nodes
	for i, cluster := range []*Cluster{node1, node2, gate1, gate2} {
		nodes := cluster.GetNodes()
		if len(nodes) != 2 {
			t.Fatalf("Cluster %d should see 2 nodes, got %d", i+1, len(nodes))
		}
	}

	// All clusters should see 2 gates
	for i, cluster := range []*Cluster{node1, node2, gate1, gate2} {
		gates := cluster.GetGates()
		if len(gates) != 2 {
			t.Fatalf("Cluster %d should see 2 gates, got %d: %v", i+1, len(gates), gates)
		}
	}

	t.Logf("Node1 sees: %d nodes, %d gates", len(node1.GetNodes()), len(node1.GetGates()))
	t.Logf("Node2 sees: %d nodes, %d gates", len(node2.GetNodes()), len(node2.GetGates()))
	t.Logf("Gate1 sees: %d nodes, %d gates", len(gate1.GetNodes()), len(gate1.GetGates()))
	t.Logf("Gate2 sees: %d nodes, %d gates", len(gate2.GetNodes()), len(gate2.GetGates()))
}

// TestClusterWithTwoGatesAndThreeNodes tests a cluster with 2 gates and 3 nodes
// This verifies that all clusters can discover each other properly via etcd
func TestClusterWithTwoGatesAndThreeNodes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create three node clusters
	node1 := mustNewCluster(ctx, t, "localhost:47060", testPrefix)
	defer node1.Stop(ctx)

	node2 := mustNewCluster(ctx, t, "localhost:47061", testPrefix)
	defer node2.Stop(ctx)

	node3 := mustNewCluster(ctx, t, "localhost:47062", testPrefix)
	defer node3.Stop(ctx)

	// Create two gate clusters
	gw1Config := &gate.GateConfig{
		AdvertiseAddress: "localhost:49020",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw1, err := gate.NewGate(gw1Config)
	if err != nil {
		t.Fatalf("Failed to create gate 1: %v", err)
	}
	gate1, err := newClusterWithEtcdForTestingGate("gate1", gw1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gate1.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster 1: %v", err)
	}
	defer gate1.Stop(ctx)

	gw2Config := &gate.GateConfig{
		AdvertiseAddress: "localhost:49021",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw2, err := gate.NewGate(gw2Config)
	if err != nil {
		t.Fatalf("Failed to create gate 2: %v", err)
	}
	gate2, err := newClusterWithEtcdForTestingGate("gate2", gw2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gate2.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster 2: %v", err)
	}
	defer gate2.Stop(ctx)

	// Wait for discovery - all clusters should see each other
	time.Sleep(1 * time.Second)

	// All clusters should see 3 nodes
	for i, cluster := range []*Cluster{node1, node2, node3, gate1, gate2} {
		nodes := cluster.GetNodes()
		if len(nodes) != 3 {
			t.Fatalf("Cluster %d should see 3 nodes, got %d: %v", i+1, len(nodes), nodes)
		}
	}

	// All clusters should see 2 gates
	for i, cluster := range []*Cluster{node1, node2, node3, gate1, gate2} {
		gates := cluster.GetGates()
		if len(gates) != 2 {
			t.Fatalf("Cluster %d should see 2 gates, got %d: %v", i+1, len(gates), gates)
		}
	}

	t.Logf("Node1 sees: %d nodes, %d gates", len(node1.GetNodes()), len(node1.GetGates()))
	t.Logf("Node2 sees: %d nodes, %d gates", len(node2.GetNodes()), len(node2.GetGates()))
	t.Logf("Node3 sees: %d nodes, %d gates", len(node3.GetNodes()), len(node3.GetGates()))
	t.Logf("Gate1 sees: %d nodes, %d gates", len(gate1.GetNodes()), len(gate1.GetGates()))
	t.Logf("Gate2 sees: %d nodes, %d gates", len(gate2.GetNodes()), len(gate2.GetGates()))
}
