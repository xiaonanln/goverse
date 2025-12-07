package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGetNodesInfo_NoConfig tests GetNodesInfo when no config file is used (CLI mode)
func TestGetNodesInfo_NoConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create two nodes without config file
	nodeAddr1 := testutil.GetFreeAddress()
	nodeAddr2 := testutil.GetFreeAddress()

	node1 := node.NewNode(nodeAddr1, testutil.TestNumShards)
	node2 := node.NewNode(nodeAddr2, testutil.TestNumShards)

	cfg1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    nil, // No config file
	}

	cfg2 := cfg1

	cluster1, err := NewClusterWithNode(cfg1, node1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer cluster1.Stop(ctx)

	cluster2, err := NewClusterWithNode(cfg2, node2)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer cluster2.Stop(ctx)

	// Start both clusters
	if err := cluster1.Start(ctx, node1); err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}
	if err := cluster2.Start(ctx, node2); err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Get nodes info from cluster1
	nodesInfo := cluster1.GetNodesInfo()

	// Should have 2 nodes
	if len(nodesInfo) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodesInfo))
	}

	// Check node1
	info1, ok := nodesInfo[nodeAddr1]
	if !ok {
		t.Errorf("Node %s not found in nodes info", nodeAddr1)
	} else {
		if info1.Configured {
			t.Errorf("Node %s should not be configured (no config file)", nodeAddr1)
		}
		if !info1.FoundInClusterState {
			t.Errorf("Node %s should be found in cluster state", nodeAddr1)
		}
		if info1.Address != nodeAddr1 {
			t.Errorf("Node %s address mismatch: got %s", nodeAddr1, info1.Address)
		}
	}

	// Check node2
	info2, ok := nodesInfo[nodeAddr2]
	if !ok {
		t.Errorf("Node %s not found in nodes info", nodeAddr2)
	} else {
		if info2.Configured {
			t.Errorf("Node %s should not be configured (no config file)", nodeAddr2)
		}
		if !info2.FoundInClusterState {
			t.Errorf("Node %s should be found in cluster state", nodeAddr2)
		}
		if info2.Address != nodeAddr2 {
			t.Errorf("Node %s address mismatch: got %s", nodeAddr2, info2.Address)
		}
	}
}

// TestGetNodesInfo_WithConfig tests GetNodesInfo when using a config file
func TestGetNodesInfo_WithConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create addresses
	nodeAddr1 := testutil.GetFreeAddress()
	nodeAddr2 := testutil.GetFreeAddress()
	nodeAddr3 := testutil.GetFreeAddress() // Configured but not started

	// Create a config file
	configFile := &config.Config{
		Nodes: []config.NodeConfig{
			{ID: "node1", GRPCAddr: nodeAddr1, AdvertiseAddr: nodeAddr1},
			{ID: "node2", GRPCAddr: nodeAddr2, AdvertiseAddr: nodeAddr2},
			{ID: "node3", GRPCAddr: nodeAddr3, AdvertiseAddr: nodeAddr3},
		},
	}

	// Create two nodes with config file
	node1 := node.NewNode(nodeAddr1, testutil.TestNumShards)
	node2 := node.NewNode(nodeAddr2, testutil.TestNumShards)

	cfg1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    configFile,
	}

	cfg2 := cfg1

	cluster1, err := NewClusterWithNode(cfg1, node1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer cluster1.Stop(ctx)

	cluster2, err := NewClusterWithNode(cfg2, node2)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer cluster2.Stop(ctx)

	// Start both clusters
	if err := cluster1.Start(ctx, node1); err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}
	if err := cluster2.Start(ctx, node2); err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Get nodes info from cluster1
	nodesInfo := cluster1.GetNodesInfo()

	// Should have 3 nodes (2 active + 1 configured but not active)
	if len(nodesInfo) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(nodesInfo))
	}

	// Check node1 (configured and active)
	info1, ok := nodesInfo[nodeAddr1]
	if !ok {
		t.Errorf("Node %s not found in nodes info", nodeAddr1)
	} else {
		if !info1.Configured {
			t.Errorf("Node %s should be configured", nodeAddr1)
		}
		if !info1.FoundInClusterState {
			t.Errorf("Node %s should be found in cluster state", nodeAddr1)
		}
	}

	// Check node2 (configured and active)
	info2, ok := nodesInfo[nodeAddr2]
	if !ok {
		t.Errorf("Node %s not found in nodes info", nodeAddr2)
	} else {
		if !info2.Configured {
			t.Errorf("Node %s should be configured", nodeAddr2)
		}
		if !info2.FoundInClusterState {
			t.Errorf("Node %s should be found in cluster state", nodeAddr2)
		}
	}

	// Check node3 (configured but not active)
	info3, ok := nodesInfo[nodeAddr3]
	if !ok {
		t.Errorf("Node %s not found in nodes info", nodeAddr3)
	} else {
		if !info3.Configured {
			t.Errorf("Node %s should be configured", nodeAddr3)
		}
		if info3.FoundInClusterState {
			t.Errorf("Node %s should not be found in cluster state (not started)", nodeAddr3)
		}
	}
}

// TestGetGatesInfo_NoConfig tests GetGatesInfo when no config file is used (CLI mode)
func TestGetGatesInfo_NoConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create node and gates without config file
	nodeAddr := testutil.GetFreeAddress()
	gateAddr1 := testutil.GetFreeAddress()
	gateAddr2 := testutil.GetFreeAddress()

	node1 := node.NewNode(nodeAddr, testutil.TestNumShards)

	nodeConfig := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    nil, // No config file
	}

	nodeCluster, err := NewClusterWithNode(nodeConfig, node1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer nodeCluster.Stop(ctx)

	// Create gates
	gateConfig1 := &gate.GateConfig{
		AdvertiseAddress: gateAddr1,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
	}
	gate1, err := gate.NewGate(gateConfig1)
	if err != nil {
		t.Fatalf("Failed to create gate1: %v", err)
	}

	gateClusterConfig1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    nil, // No config file
	}

	gateCluster1, err := NewClusterWithGate(gateClusterConfig1, gate1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer gateCluster1.Stop(ctx)

	gateConfig2 := &gate.GateConfig{
		AdvertiseAddress: gateAddr2,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
	}
	gate2, err := gate.NewGate(gateConfig2)
	if err != nil {
		t.Fatalf("Failed to create gate2: %v", err)
	}

	gateClusterConfig2 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    nil, // No config file
	}

	gateCluster2, err := NewClusterWithGate(gateClusterConfig2, gate2)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer gateCluster2.Stop(ctx)

	// Start clusters
	if err := nodeCluster.Start(ctx, node1); err != nil {
		t.Fatalf("Failed to start node cluster: %v", err)
	}
	if err := gateCluster1.Start(ctx, nil); err != nil {
		t.Fatalf("Failed to start gate cluster 1: %v", err)
	}
	if err := gateCluster2.Start(ctx, nil); err != nil {
		t.Fatalf("Failed to start gate cluster 2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, nodeCluster, gateCluster1, gateCluster2)

	// Get gates info from node cluster
	gatesInfo := nodeCluster.GetGatesInfo()

	// Should have 2 gates
	if len(gatesInfo) != 2 {
		t.Errorf("Expected 2 gates, got %d", len(gatesInfo))
	}

	// Check gate1
	info1, ok := gatesInfo[gateAddr1]
	if !ok {
		t.Errorf("Gate %s not found in gates info", gateAddr1)
	} else {
		if info1.Configured {
			t.Errorf("Gate %s should not be configured (no config file)", gateAddr1)
		}
		if !info1.FoundInClusterState {
			t.Errorf("Gate %s should be found in cluster state", gateAddr1)
		}
		if info1.Address != gateAddr1 {
			t.Errorf("Gate %s address mismatch: got %s", gateAddr1, info1.Address)
		}
	}

	// Check gate2
	info2, ok := gatesInfo[gateAddr2]
	if !ok {
		t.Errorf("Gate %s not found in gates info", gateAddr2)
	} else {
		if info2.Configured {
			t.Errorf("Gate %s should not be configured (no config file)", gateAddr2)
		}
		if !info2.FoundInClusterState {
			t.Errorf("Gate %s should be found in cluster state", gateAddr2)
		}
		if info2.Address != gateAddr2 {
			t.Errorf("Gate %s address mismatch: got %s", gateAddr2, info2.Address)
		}
	}
}

// TestGetGatesInfo_WithConfig tests GetGatesInfo when using a config file
func TestGetGatesInfo_WithConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create addresses
	nodeAddr := testutil.GetFreeAddress()
	gateAddr1 := testutil.GetFreeAddress()
	gateAddr2 := testutil.GetFreeAddress()
	gateAddr3 := testutil.GetFreeAddress() // Configured but not started

	// Create a config file
	configFile := &config.Config{
		Nodes: []config.NodeConfig{
			{ID: "node1", GRPCAddr: nodeAddr, AdvertiseAddr: nodeAddr},
		},
		Gates: []config.GateConfig{
			{ID: "gate1", GRPCAddr: gateAddr1, AdvertiseAddr: gateAddr1},
			{ID: "gate2", GRPCAddr: gateAddr2, AdvertiseAddr: gateAddr2},
			{ID: "gate3", GRPCAddr: gateAddr3, AdvertiseAddr: gateAddr3},
		},
	}

	// Create node with config file
	node1 := node.NewNode(nodeAddr, testutil.TestNumShards)

	nodeConfig := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    configFile,
	}

	nodeCluster, err := NewClusterWithNode(nodeConfig, node1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer nodeCluster.Stop(ctx)

	// Create gates with config file
	gateConfig1 := &gate.GateConfig{
		AdvertiseAddress: gateAddr1,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
	}
	gate1, err := gate.NewGate(gateConfig1)
	if err != nil {
		t.Fatalf("Failed to create gate1: %v", err)
	}

	gateClusterConfig1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    configFile,
	}

	gateCluster1, err := NewClusterWithGate(gateClusterConfig1, gate1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer gateCluster1.Stop(ctx)

	gateConfig2 := &gate.GateConfig{
		AdvertiseAddress: gateAddr2,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
	}
	gate2, err := gate.NewGate(gateConfig2)
	if err != nil {
		t.Fatalf("Failed to create gate2: %v", err)
	}

	gateClusterConfig2 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    configFile,
	}

	gateCluster2, err := NewClusterWithGate(gateClusterConfig2, gate2)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer gateCluster2.Stop(ctx)

	// Start clusters
	if err := nodeCluster.Start(ctx, node1); err != nil {
		t.Fatalf("Failed to start node cluster: %v", err)
	}
	if err := gateCluster1.Start(ctx, nil); err != nil {
		t.Fatalf("Failed to start gate cluster 1: %v", err)
	}
	if err := gateCluster2.Start(ctx, nil); err != nil {
		t.Fatalf("Failed to start gate cluster 2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, nodeCluster, gateCluster1, gateCluster2)

	// Get gates info from node cluster
	gatesInfo := nodeCluster.GetGatesInfo()

	// Should have 3 gates (2 active + 1 configured but not active)
	if len(gatesInfo) != 3 {
		t.Errorf("Expected 3 gates, got %d", len(gatesInfo))
	}

	// Check gate1 (configured and active)
	info1, ok := gatesInfo[gateAddr1]
	if !ok {
		t.Errorf("Gate %s not found in gates info", gateAddr1)
	} else {
		if !info1.Configured {
			t.Errorf("Gate %s should be configured", gateAddr1)
		}
		if !info1.FoundInClusterState {
			t.Errorf("Gate %s should be found in cluster state", gateAddr1)
		}
	}

	// Check gate2 (configured and active)
	info2, ok := gatesInfo[gateAddr2]
	if !ok {
		t.Errorf("Gate %s not found in gates info", gateAddr2)
	} else {
		if !info2.Configured {
			t.Errorf("Gate %s should be configured", gateAddr2)
		}
		if !info2.FoundInClusterState {
			t.Errorf("Gate %s should be found in cluster state", gateAddr2)
		}
	}

	// Check gate3 (configured but not active)
	info3, ok := gatesInfo[gateAddr3]
	if !ok {
		t.Errorf("Gate %s not found in gates info", gateAddr3)
	} else {
		if !info3.Configured {
			t.Errorf("Gate %s should be configured", gateAddr3)
		}
		if info3.FoundInClusterState {
			t.Errorf("Gate %s should not be found in cluster state (not started)", gateAddr3)
		}
	}
}

// TestGetNodesInfo_Empty tests GetNodesInfo with no nodes
func TestGetNodesInfo_Empty(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a gate (no nodes)
	gateAddr := testutil.GetFreeAddress()

	gateConfig := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
	}
	gate1, err := gate.NewGate(gateConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	gateClusterConfig := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    prefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
		ConfigFile:                    nil,
	}

	gateCluster, err := NewClusterWithGate(gateClusterConfig, gate1)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer gateCluster.Stop(ctx)

	if err := gateCluster.Start(ctx, nil); err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Small delay to let gate register
	time.Sleep(500 * time.Millisecond)

	// Get nodes info - should be empty
	nodesInfo := gateCluster.GetNodesInfo()

	if len(nodesInfo) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(nodesInfo))
	}
}
