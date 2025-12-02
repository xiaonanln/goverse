package clusterinfo_test

import (
	"testing"

	"github.com/xiaonanln/goverse/util/clusterinfo"
)

// mockClusterInfoProvider is a test implementation of ClusterInfoProvider
type mockClusterInfoProvider struct {
	connectedNodes  []string
	registeredGates []string
}

func (m *mockClusterInfoProvider) GetConnectedNodes() []string {
	return m.connectedNodes
}

func (m *mockClusterInfoProvider) GetRegisteredGates() []string {
	return m.registeredGates
}

func TestClusterInfoProvider_Interface(t *testing.T) {
	// Verify that mockClusterInfoProvider implements ClusterInfoProvider
	var _ clusterinfo.ClusterInfoProvider = (*mockClusterInfoProvider)(nil)
}

func TestClusterInfoProvider_GetConnectedNodes(t *testing.T) {
	provider := &mockClusterInfoProvider{
		connectedNodes: []string{"node1:8001", "node2:8002", "node3:8003"},
	}

	nodes := provider.GetConnectedNodes()
	if len(nodes) != 3 {
		t.Errorf("Expected 3 connected nodes, got %d", len(nodes))
	}

	expectedNodes := map[string]bool{
		"node1:8001": true,
		"node2:8002": true,
		"node3:8003": true,
	}
	for _, node := range nodes {
		if !expectedNodes[node] {
			t.Errorf("Unexpected node: %s", node)
		}
	}
}

func TestClusterInfoProvider_GetRegisteredGates(t *testing.T) {
	provider := &mockClusterInfoProvider{
		registeredGates: []string{"gate1:9001", "gate2:9002"},
	}

	gates := provider.GetRegisteredGates()
	if len(gates) != 2 {
		t.Errorf("Expected 2 registered gates, got %d", len(gates))
	}

	expectedGates := map[string]bool{
		"gate1:9001": true,
		"gate2:9002": true,
	}
	for _, gate := range gates {
		if !expectedGates[gate] {
			t.Errorf("Unexpected gate: %s", gate)
		}
	}
}

func TestClusterInfoProvider_EmptyValues(t *testing.T) {
	provider := &mockClusterInfoProvider{}

	nodes := provider.GetConnectedNodes()
	if nodes != nil {
		t.Errorf("Expected nil nodes, got %v", nodes)
	}

	gates := provider.GetRegisteredGates()
	if gates != nil {
		t.Errorf("Expected nil gates, got %v", gates)
	}
}

func TestClusterInfoProvider_GateOnlyProvider(t *testing.T) {
	// For gates, GetRegisteredGates should return nil since gates don't have registered gates
	provider := &mockClusterInfoProvider{
		connectedNodes:  []string{"node1:8001"},
		registeredGates: nil, // Gates don't track registered gates
	}

	nodes := provider.GetConnectedNodes()
	if len(nodes) != 1 {
		t.Errorf("Expected 1 connected node, got %d", len(nodes))
	}

	gates := provider.GetRegisteredGates()
	if gates != nil {
		t.Errorf("Expected nil gates for gate provider, got %v", gates)
	}
}
