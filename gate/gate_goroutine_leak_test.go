package gate

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"google.golang.org/grpc"
)

// mockGoverseClient is a mock implementation for testing
type mockGoverseClient struct {
	goverse_pb.GoverseClient
	registerGateFunc func(ctx context.Context, in *goverse_pb.RegisterGateRequest, opts ...grpc.CallOption) (goverse_pb.Goverse_RegisterGateClient, error)
}

func (m *mockGoverseClient) RegisterGate(ctx context.Context, in *goverse_pb.RegisterGateRequest, opts ...grpc.CallOption) (goverse_pb.Goverse_RegisterGateClient, error) {
	if m.registerGateFunc != nil {
		return m.registerGateFunc(ctx, in, opts...)
	}
	return nil, nil
}

// mockRegisterGateClient is a mock stream for testing
type mockRegisterGateClient struct {
	goverse_pb.Goverse_RegisterGateClient
	ctx    context.Context
	cancel context.CancelFunc
	msgCh  chan *goverse_pb.GateMessage
}

func newMockRegisterGateClient() *mockRegisterGateClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &mockRegisterGateClient{
		ctx:    ctx,
		cancel: cancel,
		msgCh:  make(chan *goverse_pb.GateMessage, 10),
	}
}

func (m *mockRegisterGateClient) Context() context.Context {
	return m.ctx
}

func (m *mockRegisterGateClient) Recv() (*goverse_pb.GateMessage, error) {
	select {
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	case msg := <-m.msgCh:
		return msg, nil
	}
}

func (m *mockRegisterGateClient) Close() {
	m.cancel()
	close(m.msgCh)
}

// TestGatewayGoroutineCleanup verifies that gateway properly cleans up goroutines on shutdown
func TestGatewayGoroutineCleanup(t *testing.T) {
	// Get baseline goroutine count
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	// Create gateway
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test",
	}
	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	// Create mock streams
	stream1 := newMockRegisterGateClient()
	stream2 := newMockRegisterGateClient()

	// Send initial registration response to both streams
	stream1.msgCh <- &goverse_pb.GateMessage{
		Message: &goverse_pb.GateMessage_RegisterGateResponse{
			RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
		},
	}
	stream2.msgCh <- &goverse_pb.GateMessage{
		Message: &goverse_pb.GateMessage_RegisterGateResponse{
			RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
		},
	}

	// Create mock clients
	client1 := &mockGoverseClient{
		registerGateFunc: func(ctx context.Context, in *goverse_pb.RegisterGateRequest, opts ...grpc.CallOption) (goverse_pb.Goverse_RegisterGateClient, error) {
			return stream1, nil
		},
	}
	client2 := &mockGoverseClient{
		registerGateFunc: func(ctx context.Context, in *goverse_pb.RegisterGateRequest, opts ...grpc.CallOption) (goverse_pb.Goverse_RegisterGateClient, error) {
			return stream2, nil
		},
	}

	// Register with nodes (spawns goroutines)
	nodeConnections := map[string]goverse_pb.GoverseClient{
		"node1:8001": client1,
		"node2:8002": client2,
	}
	gateway.RegisterWithNodes(context.Background(), nodeConnections)

	// Wait for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Verify goroutines are running (should have at least 2 more than baseline)
	currentGoroutines := runtime.NumGoroutine()
	if currentGoroutines < baselineGoroutines+2 {
		t.Logf("Warning: Expected at least 2 new goroutines, but only found %d (baseline: %d, current: %d)",
			currentGoroutines-baselineGoroutines, baselineGoroutines, currentGoroutines)
	}

	// Stop the gateway - this should clean up all goroutines
	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Gateway Stop failed: %v", err)
	}

	// Close the mock streams to unblock any Recv() calls
	stream1.Close()
	stream2.Close()

	// Give goroutines time to exit
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// Check goroutine count is back to baseline
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > baselineGoroutines+1 { // Allow 1 extra for test framework
		t.Errorf("Goroutine leak detected: baseline=%d, final=%d, leaked=%d",
			baselineGoroutines, finalGoroutines, finalGoroutines-baselineGoroutines)
	} else {
		t.Logf("No goroutine leak: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
	}
}

// TestGatewayMultipleStopsNoLeak verifies multiple Stop calls don't cause issues
func TestGatewayMultipleStopsNoLeak(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test",
	}
	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	// Create a mock stream
	stream := newMockRegisterGateClient()
	stream.msgCh <- &goverse_pb.GateMessage{
		Message: &goverse_pb.GateMessage_RegisterGateResponse{
			RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
		},
	}

	client := &mockGoverseClient{
		registerGateFunc: func(ctx context.Context, in *goverse_pb.RegisterGateRequest, opts ...grpc.CallOption) (goverse_pb.Goverse_RegisterGateClient, error) {
			return stream, nil
		},
	}

	// Register with one node
	nodeConnections := map[string]goverse_pb.GoverseClient{
		"node1:8001": client,
	}
	gateway.RegisterWithNodes(context.Background(), nodeConnections)
	time.Sleep(50 * time.Millisecond)

	// Stop multiple times - should not panic or hang
	err = gateway.Stop()
	if err != nil {
		t.Fatalf("First Stop failed: %v", err)
	}
	stream.Close()

	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Second Stop failed: %v", err)
	}

	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Third Stop failed: %v", err)
	}
}

// TestGatewayConcurrentStops verifies concurrent Stop calls are safe
func TestGatewayConcurrentStops(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test",
	}
	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	// Create mock streams
	streams := make([]*mockRegisterGateClient, 3)
	for i := range streams {
		streams[i] = newMockRegisterGateClient()
		streams[i].msgCh <- &goverse_pb.GateMessage{
			Message: &goverse_pb.GateMessage_RegisterGateResponse{
				RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
			},
		}
	}

	// Register with multiple nodes
	nodeConnections := make(map[string]goverse_pb.GoverseClient)
	for i := range streams {
		stream := streams[i]
		client := &mockGoverseClient{
			registerGateFunc: func(ctx context.Context, in *goverse_pb.RegisterGateRequest, opts ...grpc.CallOption) (goverse_pb.Goverse_RegisterGateClient, error) {
				return stream, nil
			},
		}
		nodeConnections[fmt.Sprintf("node%d:800%d", i, i)] = client
	}
	gateway.RegisterWithNodes(context.Background(), nodeConnections)
	time.Sleep(50 * time.Millisecond)

	// Call Stop concurrently from multiple goroutines
	var wg sync.WaitGroup
	stopCount := 10
	wg.Add(stopCount)
	for i := 0; i < stopCount; i++ {
		go func() {
			defer wg.Done()
			err := gateway.Stop()
			if err != nil {
				t.Errorf("Concurrent Stop failed: %v", err)
			}
		}()
	}

	// Wait for all Stop calls to complete
	wg.Wait()

	// Close mock streams
	for _, stream := range streams {
		stream.Close()
	}

	// One more Stop should still be safe
	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Final Stop after concurrent stops failed: %v", err)
	}
}

// TestClientProxyClosedChannelSafety verifies HandleMessage doesn't panic when client is closed
func TestClientProxyClosedChannelSafety(t *testing.T) {
	proxy := NewClientProxy("test-client")

	// Close the proxy
	proxy.Close()

	// Verify MessageChan returns nil after close
	if ch := proxy.MessageChan(); ch != nil {
		t.Errorf("MessageChan() should return nil after Close(), got %v", ch)
	}

	// Try to handle a message - should not panic and should return false
	msg := &goverse_pb.RegisterGateResponse{}
	sent := proxy.HandleMessage(msg)
	if sent {
		t.Errorf("HandleMessage() should return false for closed proxy, got true")
	}

	// Multiple close calls should be safe
	proxy.Close()
	proxy.Close()
}
