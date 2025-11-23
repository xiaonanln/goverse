package gate

import (
	"context"
	"testing"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestRegisterWithNodesCleanupRemovedNodes tests that when nodes are removed
// from the connections map, their cancel functions are invoked
func TestRegisterWithNodesCleanupRemovedNodes(t *testing.T) {
	gateAddr := testutil.GetFreeAddress()
	nodeAddr := testutil.GetFreeAddress()
	
	config := &GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-cleanup",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Create mock node connections
	conn, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create mock connection: %v", err)
	}
	defer conn.Close()

	client2 := goverse_pb.NewGoverseClient(conn)

	// Manually add registrations to simulate active registrations
	// In real scenario these would be added by RegisterWithNodes when goroutines start
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	mockStream1 := &mockGateStream{ctx: ctx1}
	mockStream2 := &mockGateStream{ctx: ctx2}

	gateway.nodeRegMu.Lock()
	gateway.nodeRegs["node1"] = &nodeReg{
		stream: mockStream1,
		cancel: cancel1,
	}
	gateway.nodeRegs["node2"] = &nodeReg{
		stream: mockStream2,
		cancel: cancel2,
	}
	gateway.nodeRegMu.Unlock()

	// Verify both nodes have registrations
	gateway.nodeRegMu.RLock()
	initialCount := len(gateway.nodeRegs)
	gateway.nodeRegMu.RUnlock()

	if initialCount != 2 {
		t.Errorf("Expected 2 registrations initially, got %d", initialCount)
	}

	// Verify contexts are not cancelled yet
	select {
	case <-ctx1.Done():
		t.Error("ctx1 should not be cancelled yet")
	default:
	}
	select {
	case <-ctx2.Done():
		t.Error("ctx2 should not be cancelled yet")
	default:
	}

	// Call RegisterWithNodes with only node2 (node1 removed)
	connections := map[string]goverse_pb.GoverseClient{
		"node2": client2,
	}

	gateway.RegisterWithNodes(ctx, connections)

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	// Verify node1's context was cancelled (cleanup triggered)
	select {
	case <-ctx1.Done():
		// Expected - node1 was removed
	default:
		t.Error("Expected ctx1 to be cancelled after node1 removed")
	}

	// Verify node2's context is still active
	select {
	case <-ctx2.Done():
		t.Error("ctx2 should not be cancelled as node2 is still present")
	default:
		// Expected
	}

	// Verify node1 registration was removed from the map
	gateway.nodeRegMu.RLock()
	_, hasNode1 := gateway.nodeRegs["node1"]
	gateway.nodeRegMu.RUnlock()

	if hasNode1 {
		t.Error("Expected node1 registration to be removed from map")
	}
}

// TestRegisterWithNodesIdempotent tests that calling RegisterWithNodes multiple times
// with the same nodes doesn't cancel existing registrations
func TestRegisterWithNodesIdempotent(t *testing.T) {
	gateAddr := testutil.GetFreeAddress()
	nodeAddr := testutil.GetFreeAddress()
	
	config := &GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-idempotent",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Create mock connection
	conn, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create mock connection: %v", err)
	}
	defer conn.Close()

	client1 := goverse_pb.NewGoverseClient(conn)

	// Manually add a registration to simulate an active registration
	ctx1, cancel1 := context.WithCancel(ctx)
	mockStream := &mockGateStream{ctx: ctx1}

	gateway.nodeRegMu.Lock()
	gateway.nodeRegs["node1"] = &nodeReg{
		stream: mockStream,
		cancel: cancel1,
	}
	gateway.nodeRegMu.Unlock()

	connections := map[string]goverse_pb.GoverseClient{
		"node1": client1,
	}

	// Call RegisterWithNodes - should not cancel existing registration
	gateway.RegisterWithNodes(ctx, connections)
	time.Sleep(50 * time.Millisecond)

	// Verify context is still active (not cancelled)
	select {
	case <-ctx1.Done():
		t.Error("Expected ctx1 to remain active (not cancelled) on idempotent call")
	default:
		// Expected
	}

	// Verify registration still exists
	gateway.nodeRegMu.RLock()
	_, hasNode1 := gateway.nodeRegs["node1"]
	gateway.nodeRegMu.RUnlock()

	if !hasNode1 {
		t.Error("Expected node1 registration to still exist after idempotent call")
	}
}

// mockGateStream is a mock implementation of Goverse_RegisterGateClient for testing
type mockGateStream struct {
	ctx context.Context
	goverse_pb.Goverse_RegisterGateClient
}

func (m *mockGateStream) Context() context.Context {
	return m.ctx
}

// TestGatewayStopCancelsAllRegistrations tests that stopping the gateway
// cancels all active node registrations
func TestGatewayStopCancelsAllRegistrations(t *testing.T) {
	gateAddr := testutil.GetFreeAddress()
	
	config := &GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-stop-cancels",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Manually add registrations to simulate active registrations
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)
	ctx3, cancel3 := context.WithCancel(ctx)

	gateway.nodeRegMu.Lock()
	gateway.nodeRegs["node1"] = &nodeReg{cancel: cancel1}
	gateway.nodeRegs["node2"] = &nodeReg{cancel: cancel2}
	gateway.nodeRegs["node3"] = &nodeReg{cancel: cancel3}
	gateway.nodeRegMu.Unlock()

	// Verify registrations were created
	gateway.nodeRegMu.RLock()
	regCountBefore := len(gateway.nodeRegs)
	gateway.nodeRegMu.RUnlock()

	if regCountBefore != 3 {
		t.Errorf("Expected 3 registrations before stop, got %d", regCountBefore)
	}

	// Verify contexts are active
	for i, c := range []context.Context{ctx1, ctx2, ctx3} {
		select {
		case <-c.Done():
			t.Errorf("Context %d should not be cancelled before stop", i+1)
		default:
		}
	}

	// Stop the gateway
	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Gateway.Stop() returned error: %v", err)
	}

	// Verify all contexts were cancelled
	for i, c := range []context.Context{ctx1, ctx2, ctx3} {
		select {
		case <-c.Done():
			// Expected
		default:
			t.Errorf("Context %d should be cancelled after stop", i+1)
		}
	}

	// Verify all registrations were cleaned up
	gateway.nodeRegMu.RLock()
	regCountAfter := len(gateway.nodeRegs)
	gateway.nodeRegMu.RUnlock()

	if regCountAfter != 0 {
		t.Errorf("Expected 0 registrations after stop, got %d", regCountAfter)
	}
}

// TestRegisterWithNodesEmptyMap tests that calling RegisterWithNodes with an empty map
// cleans up all existing registrations
func TestRegisterWithNodesEmptyMap(t *testing.T) {
	gateAddr := testutil.GetFreeAddress()
	
	config := &GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-empty-map",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Manually add registrations to simulate active registrations
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	gateway.nodeRegMu.Lock()
	gateway.nodeRegs["node1"] = &nodeReg{cancel: cancel1}
	gateway.nodeRegs["node2"] = &nodeReg{cancel: cancel2}
	gateway.nodeRegMu.Unlock()

	gateway.nodeRegMu.RLock()
	regCountBefore := len(gateway.nodeRegs)
	gateway.nodeRegMu.RUnlock()

	if regCountBefore != 2 {
		t.Errorf("Expected 2 registrations before empty map, got %d", regCountBefore)
	}

	// Verify contexts are active
	select {
	case <-ctx1.Done():
		t.Error("ctx1 should not be cancelled before empty map call")
	default:
	}
	select {
	case <-ctx2.Done():
		t.Error("ctx2 should not be cancelled before empty map call")
	default:
	}

	// Call with empty map - should clean up all registrations
	emptyConnections := map[string]goverse_pb.GoverseClient{}
	gateway.RegisterWithNodes(ctx, emptyConnections)
	time.Sleep(50 * time.Millisecond)

	// Verify contexts were cancelled
	select {
	case <-ctx1.Done():
		// Expected
	default:
		t.Error("Expected ctx1 to be cancelled after empty map call")
	}
	select {
	case <-ctx2.Done():
		// Expected
	default:
		t.Error("Expected ctx2 to be cancelled after empty map call")
	}

	gateway.nodeRegMu.RLock()
	regCountAfter := len(gateway.nodeRegs)
	gateway.nodeRegMu.RUnlock()

	if regCountAfter != 0 {
		t.Errorf("Expected 0 registrations after empty map, got %d", regCountAfter)
	}
}
