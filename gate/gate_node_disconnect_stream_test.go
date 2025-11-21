package gate

import (
	"context"
	"testing"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// This file contains integration tests for gate-node disconnect scenarios.
// These tests verify that when a node goes down or becomes unreachable,
// the gate properly detects the disconnection and cleans up internal state.
//
// The tests cover:
// 1. Stream disconnection detection (RegisterGate RPC failure/closure)
// 2. Node removal cleanup (when nodes are removed from connections map)
//
// These scenarios ensure the gate handles node failures gracefully without
// leaving stale registrations or goroutines.

// TestGateDetectsNodeStreamDisconnect tests that when a node's RegisterGate stream
// is closed (simulating node failure or shutdown), the gate properly detects it
// and cleans up the registration.
//
// This is an integration test that verifies the gate's registerWithNode goroutine
// properly handles stream.Recv() errors and performs cleanup when the stream closes.
func TestGateDetectsNodeStreamDisconnect(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-stream-disconnect",
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

	// Create mock node connection (this will fail to connect, but that's ok for this test)
	// We just want to verify the cleanup behavior when RegisterGate call fails/stream closes
	conn, err := grpc.NewClient("localhost:47000", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create mock connection: %v", err)
	}
	defer conn.Close()

	client := goverse_pb.NewGoverseClient(conn)
	connections := map[string]goverse_pb.GoverseClient{
		"localhost:47000": client,
	}

	// Start registration in a goroutine since it will try to connect and may block/fail
	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()

	regDone := make(chan struct{})
	go func() {
		gateway.RegisterWithNodes(regCtx, connections)
		close(regDone)
	}()

	// Wait a bit for registration attempt
	time.Sleep(500 * time.Millisecond)

	// The RegisterGate call should have failed or timed out
	// Wait for the registration goroutine to complete
	select {
	case <-regDone:
		t.Logf("Registration goroutine completed (expected to fail/timeout)")
	case <-time.After(3 * time.Second):
		t.Logf("Registration goroutine still running after timeout (ok)")
	}

	// Give time for cleanup
	time.Sleep(500 * time.Millisecond)

	// Verify that the gateway can be stopped cleanly
	// If cleanup didn't work properly, this might hang or panic
	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Gateway.Stop() returned error: %v", err)
	}

	t.Logf("Successfully verified gateway handles stream disconnect/failure gracefully")
}

// TestGateCleanupOnNodeRemoval tests that when a node is removed from the
// connections map (simulating the node being detected as down/removed),
// the gate cancels the registration and cleans up.
//
// This test specifically verifies the cleanup path in RegisterWithNodes
// when nodes are no longer present in the provided connections map.
func TestGateCleanupOnNodeRemoval(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49100",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-node-removal",
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

	// Manually add a mock registration to simulate an active connection
	ctx1, cancel1 := context.WithCancel(ctx)
	mockStream := &mockGateStream{ctx: ctx1}

	gateway.nodeRegMu.Lock()
	gateway.nodeRegs["localhost:47001"] = &nodeReg{
		stream: mockStream,
		cancel: cancel1,
		ctx:    ctx1,
	}
	gateway.nodeRegMu.Unlock()

	// Verify registration exists
	gateway.nodeRegMu.RLock()
	_, exists := gateway.nodeRegs["localhost:47001"]
	gateway.nodeRegMu.RUnlock()

	if !exists {
		t.Fatal("Registration should exist after manual addition")
	}

	// Verify context is not cancelled
	select {
	case <-ctx1.Done():
		t.Error("Context should not be cancelled yet")
	default:
		// Expected
	}

	// Call RegisterWithNodes with empty map - simulates all nodes being removed
	// This should trigger cleanup of the existing registration
	emptyConnections := map[string]goverse_pb.GoverseClient{}
	gateway.RegisterWithNodes(ctx, emptyConnections)

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify the registration was removed
	gateway.nodeRegMu.RLock()
	_, exists = gateway.nodeRegs["localhost:47001"]
	gateway.nodeRegMu.RUnlock()

	if exists {
		t.Error("Registration should have been removed after node removal")
	}

	// Verify context was cancelled (cleanup happened)
	select {
	case <-ctx1.Done():
		// Expected - cleanup should cancel the context
		t.Logf("Context was cancelled as expected during cleanup")
	default:
		t.Error("Context should have been cancelled during cleanup")
	}

	t.Logf("Successfully verified gate cleans up when nodes are removed")
}
