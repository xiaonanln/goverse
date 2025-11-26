package testutil

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockClusterReady is a mock implementation of ClusterReadyWaiter for testing
type mockClusterReady struct {
	readyChan chan bool
}

func (m *mockClusterReady) ClusterReady() <-chan bool {
	return m.readyChan
}

func (m *mockClusterReady) String() string {
	return "mockClusterReady"
}

func newMockClusterReady() *mockClusterReady {
	return &mockClusterReady{
		readyChan: make(chan bool),
	}
}

func newMockClusterAlreadyReady() *mockClusterReady {
	m := &mockClusterReady{
		readyChan: make(chan bool),
	}
	close(m.readyChan)
	return m
}

func TestWaitForClusterReady_AlreadyReady(t *testing.T) {
	// Create a mock cluster that is already ready
	cluster := newMockClusterAlreadyReady()

	// This should return immediately without timeout
	start := time.Now()
	WaitForClusterReady(t, cluster)
	elapsed := time.Since(start)

	// Should complete in well under 1 second (typically microseconds)
	if elapsed > 1*time.Second {
		t.Fatalf("WaitForClusterReady took too long for already-ready cluster: %v", elapsed)
	}
}

func TestWaitForClusterReady_BecomesReady(t *testing.T) {
	cluster := newMockClusterReady()

	// Start a goroutine that marks cluster ready after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(cluster.readyChan)
	}()

	// This should wait for the cluster to become ready
	start := time.Now()
	WaitForClusterReady(t, cluster)
	elapsed := time.Since(start)

	// Should complete in around 100ms
	if elapsed < 50*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("WaitForClusterReady took unexpected time: %v (expected ~100ms)", elapsed)
	}
}

// Note: We cannot easily test the timeout case because:
// 1. WaitForShardMappingTimeout is a const and cannot be modified
// 2. Waiting for the full 10 seconds would make tests too slow
// 3. The timeout behavior is straightforward (select with time.After)
// The timeout case will be implicitly tested when used in actual cluster tests.

// mockFullCluster is a mock implementation of FullClusterChecker for testing
type mockFullCluster struct {
	mu                   sync.RWMutex
	readyChan            chan bool
	name                 string
	advertiseAddr        string
	isNode               bool
	isGate               bool
	isReady              bool
	nodes                []string
	gates                []string
	connectedGates       map[string]bool
	shardMappingComplete bool
}

func (m *mockFullCluster) ClusterReady() <-chan bool {
	return m.readyChan
}

func (m *mockFullCluster) String() string {
	return m.name
}

func (m *mockFullCluster) GetNodes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodes
}

func (m *mockFullCluster) GetGates() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gates
}

func (m *mockFullCluster) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isReady
}

func (m *mockFullCluster) IsGateConnected(gateAddr string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectedGates[gateAddr]
}

func (m *mockFullCluster) GetAdvertiseAddress() string {
	return m.advertiseAddr
}

func (m *mockFullCluster) IsNode() bool {
	return m.isNode
}

func (m *mockFullCluster) IsGate() bool {
	return m.isGate
}

func (m *mockFullCluster) IsShardMappingComplete(ctx context.Context, numShards int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shardMappingComplete
}

// setReady sets the mock cluster state to ready with proper synchronization
func (m *mockFullCluster) setReady(nodes, gates []string, connectedGates map[string]bool, shardMappingComplete bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isReady = true
	m.nodes = nodes
	m.gates = gates
	m.connectedGates = connectedGates
	m.shardMappingComplete = shardMappingComplete
}

func newMockFullClusterNode(name, addr string) *mockFullCluster {
	m := &mockFullCluster{
		readyChan:            make(chan bool),
		name:                 name,
		advertiseAddr:        addr,
		isNode:               true,
		isGate:               false,
		isReady:              false,
		nodes:                []string{},
		gates:                []string{},
		connectedGates:       make(map[string]bool),
		shardMappingComplete: false,
	}
	close(m.readyChan) // Close by default so WaitFor doesn't block on ClusterReady
	return m
}

func newMockFullClusterGate(name, addr string) *mockFullCluster {
	m := &mockFullCluster{
		readyChan:            make(chan bool),
		name:                 name,
		advertiseAddr:        addr,
		isNode:               false,
		isGate:               true,
		isReady:              false,
		nodes:                []string{},
		gates:                []string{},
		connectedGates:       make(map[string]bool),
		shardMappingComplete: false,
	}
	close(m.readyChan) // Close by default so WaitFor doesn't block on ClusterReady
	return m
}

func TestWaitForClustersReady_SingleNodeReady(t *testing.T) {
	node := newMockFullClusterNode("node1", "localhost:7001")
	node.isReady = true
	node.nodes = []string{"localhost:7001"}
	node.shardMappingComplete = true

	start := time.Now()
	WaitForClustersReady(t, node)
	elapsed := time.Since(start)

	// Should complete quickly (within WaitFor polling interval)
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForClustersReady took too long: %v", elapsed)
	}
}

func TestWaitForClustersReady_TwoNodesReady(t *testing.T) {
	node1 := newMockFullClusterNode("node1", "localhost:7001")
	node2 := newMockFullClusterNode("node2", "localhost:7002")

	// Both nodes see each other and are ready
	node1.isReady = true
	node1.nodes = []string{"localhost:7001", "localhost:7002"}
	node1.shardMappingComplete = true

	node2.isReady = true
	node2.nodes = []string{"localhost:7001", "localhost:7002"}
	node2.shardMappingComplete = true

	start := time.Now()
	WaitForClustersReady(t, node1, node2)
	elapsed := time.Since(start)

	// Should complete quickly
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForClustersReady took too long: %v", elapsed)
	}
}

func TestWaitForClustersReady_NodeAndGateReady(t *testing.T) {
	node := newMockFullClusterNode("node1", "localhost:7001")
	gate := newMockFullClusterGate("gate1", "localhost:9001")

	// Node and gate see each other
	node.isReady = true
	node.nodes = []string{"localhost:7001"}
	node.gates = []string{"localhost:9001"}
	node.connectedGates["localhost:9001"] = true
	node.shardMappingComplete = true

	gate.isReady = true
	gate.nodes = []string{"localhost:7001"}
	gate.gates = []string{"localhost:9001"}
	gate.shardMappingComplete = true

	start := time.Now()
	WaitForClustersReady(t, node, gate)
	elapsed := time.Since(start)

	// Should complete quickly
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForClustersReady took too long: %v", elapsed)
	}
}

func TestWaitForClustersReady_BecomesReady(t *testing.T) {
	node := newMockFullClusterNode("node1", "localhost:7001")
	gate := newMockFullClusterGate("gate1", "localhost:9001")

	// Initially not ready (default state from constructor)

	// Make it ready after a short delay using thread-safe setReady method
	go func() {
		time.Sleep(200 * time.Millisecond)
		node.setReady(
			[]string{"localhost:7001"},
			[]string{"localhost:9001"},
			map[string]bool{"localhost:9001": true},
			true,
		)
		gate.setReady(
			[]string{"localhost:7001"},
			[]string{"localhost:9001"},
			nil,
			true,
		)
	}()

	start := time.Now()
	WaitForClustersReady(t, node, gate)
	elapsed := time.Since(start)

	// Should complete in around 200-400ms (initial delay + polling)
	if elapsed < 150*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("WaitForClustersReady took unexpected time: %v", elapsed)
	}
}

func TestWaitForClustersReady_MultipleNodesAndGates(t *testing.T) {
	node1 := newMockFullClusterNode("node1", "localhost:7001")
	node2 := newMockFullClusterNode("node2", "localhost:7002")
	gate1 := newMockFullClusterGate("gate1", "localhost:9001")
	gate2 := newMockFullClusterGate("gate2", "localhost:9002")

	allNodes := []string{"localhost:7001", "localhost:7002"}
	allGates := []string{"localhost:9001", "localhost:9002"}

	// Configure all clusters to be fully ready
	for _, c := range []*mockFullCluster{node1, node2, gate1, gate2} {
		c.isReady = true
		c.nodes = allNodes
		c.gates = allGates
		c.shardMappingComplete = true
	}

	// Nodes need to have gates connected
	node1.connectedGates["localhost:9001"] = true
	node1.connectedGates["localhost:9002"] = true
	node2.connectedGates["localhost:9001"] = true
	node2.connectedGates["localhost:9002"] = true

	start := time.Now()
	WaitForClustersReady(t, node1, node2, gate1, gate2)
	elapsed := time.Since(start)

	// Should complete quickly
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForClustersReady took too long: %v", elapsed)
	}
}
