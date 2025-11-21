package gate

import (
	"context"
	"testing"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestRegisterWithNodesCleanupRemovedNodes tests that when nodes are removed
// from the connections map, their cancel functions are invoked
func TestRegisterWithNodesCleanupRemovedNodes(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
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
	conn, err := grpc.NewClient("localhost:47000", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create mock connection: %v", err)
	}
	defer conn.Close()

	client2 := goverse_pb.NewGoverseClient(conn)

	// Manually add cancel functions to simulate active registrations
	// In real scenario these would be added by RegisterWithNodes when goroutines start
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	gateway.nodeCancelsMu.Lock()
	gateway.nodeCancels["node1"] = cancel1
	gateway.nodeCancels["node2"] = cancel2
	gateway.nodeCancelsMu.Unlock()

	// Verify both nodes have cancel functions
	gateway.nodeCancelsMu.RLock()
	initialCount := len(gateway.nodeCancels)
	gateway.nodeCancelsMu.RUnlock()

	if initialCount != 2 {
		t.Errorf("Expected 2 cancel functions initially, got %d", initialCount)
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

	// Verify node1 cancel function was removed from the map
	gateway.nodeCancelsMu.RLock()
	_, hasNode1 := gateway.nodeCancels["node1"]
	gateway.nodeCancelsMu.RUnlock()

	if hasNode1 {
		t.Error("Expected node1 cancel function to be removed from map")
	}
}

// TestRegisterWithNodesIdempotent tests that calling RegisterWithNodes multiple times
// with the same nodes doesn't cancel existing registrations
func TestRegisterWithNodesIdempotent(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
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
	conn, err := grpc.NewClient("localhost:47000", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create mock connection: %v", err)
	}
	defer conn.Close()

	client1 := goverse_pb.NewGoverseClient(conn)

	// Manually add a cancel function to simulate an active registration
	ctx1, cancel1 := context.WithCancel(ctx)
	gateway.nodeCancelsMu.Lock()
	gateway.nodeCancels["node1"] = cancel1
	gateway.nodeCancelsMu.Unlock()

	// Also add to registered nodes to simulate successful registration
	// This will make RegisterWithNodes skip re-registration
	mockStream := &mockGateStream{ctx: ctx1}
	gateway.registeredNodesMu.Lock()
	gateway.registeredNodes["node1"] = mockStream
	gateway.registeredNodesMu.Unlock()

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

	// Verify cancel function still exists
	gateway.nodeCancelsMu.RLock()
	_, hasNode1 := gateway.nodeCancels["node1"]
	gateway.nodeCancelsMu.RUnlock()

	if !hasNode1 {
		t.Error("Expected node1 cancel function to still exist after idempotent call")
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
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
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

	// Manually add cancel functions to simulate active registrations
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)
	ctx3, cancel3 := context.WithCancel(ctx)

	gateway.nodeCancelsMu.Lock()
	gateway.nodeCancels["node1"] = cancel1
	gateway.nodeCancels["node2"] = cancel2
	gateway.nodeCancels["node3"] = cancel3
	gateway.nodeCancelsMu.Unlock()

	// Verify registrations were created
	gateway.nodeCancelsMu.RLock()
	cancelCountBefore := len(gateway.nodeCancels)
	gateway.nodeCancelsMu.RUnlock()

	if cancelCountBefore != 3 {
		t.Errorf("Expected 3 cancel functions before stop, got %d", cancelCountBefore)
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

	// Verify all cancel functions were cleaned up
	gateway.nodeCancelsMu.RLock()
	cancelCountAfter := len(gateway.nodeCancels)
	gateway.nodeCancelsMu.RUnlock()

	if cancelCountAfter != 0 {
		t.Errorf("Expected 0 cancel functions after stop, got %d", cancelCountAfter)
	}
}

// TestRegisterWithNodesEmptyMap tests that calling RegisterWithNodes with an empty map
// cleans up all existing registrations
func TestRegisterWithNodesEmptyMap(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
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

	// Manually add cancel functions to simulate active registrations
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	gateway.nodeCancelsMu.Lock()
	gateway.nodeCancels["node1"] = cancel1
	gateway.nodeCancels["node2"] = cancel2
	gateway.nodeCancelsMu.Unlock()

	gateway.nodeCancelsMu.RLock()
	cancelCountBefore := len(gateway.nodeCancels)
	gateway.nodeCancelsMu.RUnlock()

	if cancelCountBefore != 2 {
		t.Errorf("Expected 2 cancel functions before empty map, got %d", cancelCountBefore)
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

	gateway.nodeCancelsMu.RLock()
	cancelCountAfter := len(gateway.nodeCancels)
	gateway.nodeCancelsMu.RUnlock()

	if cancelCountAfter != 0 {
		t.Errorf("Expected 0 cancel functions after empty map, got %d", cancelCountAfter)
	}
}
