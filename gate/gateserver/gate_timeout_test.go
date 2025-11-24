package gateserver

import (
	"context"
	"sync"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// SlowObject is a test object that simulates slow operations
type SlowObject struct {
	object.BaseObject
	mu sync.Mutex
}

func (o *SlowObject) OnCreated() {}

// SlowMethod takes a configurable amount of time to execute
// Request format: {delay_ms: number}
// Response format: {completed: bool}
func (o *SlowObject) SlowMethod(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delayMs := 1000.0 // default 1 second
	if req != nil && req.Fields["delay_ms"] != nil {
		delayMs = req.Fields["delay_ms"].GetNumberValue()
	}

	// Use a ticker to check context periodically
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	deadline := time.Now().Add(time.Duration(delayMs) * time.Millisecond)
	
	for {
		select {
		case <-ctx.Done():
			// Context was cancelled or timed out
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				// Completed successfully
				return &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"completed": structpb.NewBoolValue(true),
					},
				}, nil
			}
		}
	}
}

// TestGateCallObjectTimeout tests that CallObject applies default timeout when context has no deadline
func TestGateCallObjectTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	if etcdPrefix == "" {
		t.Skip("Skipping test: etcd not available")
	}

	// Create and start a node with cluster (so it registers with etcd)
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register SlowObject type
	testNode.RegisterObjectType((*SlowObject)(nil))

	// Start mock gRPC server for the node to handle inter-node communication
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create and start gate server with a short timeout for testing
	gateAddr := testutil.GetFreeAddress()
	gateConfig := &GateServerConfig{
		ListenAddress:      gateAddr,
		AdvertiseAddress:   gateAddr,
		EtcdAddress:        "localhost:2379",
		EtcdPrefix:         etcdPrefix,
		NumShards:          testutil.TestNumShards,
		DefaultCallTimeout: 2 * time.Second, // Short timeout for testing
	}

	gateServer, err := NewGateServer(gateConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}

	gateCtx, gateCancel := context.WithCancel(ctx)
	t.Cleanup(gateCancel)
	t.Cleanup(func() { gateServer.Stop() })

	// Start the gate server in a goroutine
	gwStarted := make(chan error, 1)
	go func() {
		gwStarted <- gateServer.Start(gateCtx)
	}()

	// Wait for gate to be ready
	time.Sleep(1 * time.Second)
	select {
	case err := <-gwStarted:
		if err != nil {
			t.Fatalf("Gate server failed to start: %v", err)
		}
		t.Fatalf("Gate server exited prematurely")
	default:
		// Gate server is running
	}

	// Create gRPC client to gate
	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate: %v", err)
	}
	defer conn.Close()

	gateClient := gate_pb.NewGateServiceClient(conn)

	t.Run("DefaultTimeoutApplied", func(t *testing.T) {
		// Create object directly on the node (bypassing async creation for testing)
		objID := testutil.GetObjectIDForShard(0, "SlowObject")
		_, err := testNode.CreateObject(ctx, "SlowObject", objID)
		if err != nil {
			t.Fatalf("Failed to create SlowObject: %v", err)
		}

		// Wait for object to be created
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Create request that will take longer than the gate's default timeout (2s)
		// but shorter than what would be a reasonable timeout (3s delay vs 2s timeout)
		req := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"delay_ms": structpb.NewNumberValue(3000), // 3 second delay
			},
		}
		reqAny, err := anypb.New(req)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Call with context that has NO deadline - gate should apply default timeout
		callReq := &gate_pb.CallObjectRequest{
			Type:    "SlowObject",
			Id:      objID,
			Method:  "SlowMethod",
			Request: reqAny,
		}

		start := time.Now()
		_, err = gateClient.CallObject(context.Background(), callReq)
		elapsed := time.Since(start)

		// Should timeout after approximately 2 seconds (the default timeout)
		if err == nil {
			t.Fatalf("Expected timeout error, but call succeeded")
		}

		// Verify timeout occurred around the expected time (2s ± 1s tolerance)
		if elapsed < 1*time.Second || elapsed > 3*time.Second {
			t.Errorf("Timeout occurred at %v, expected around 2s", elapsed)
		}

		t.Logf("Call timed out after %v (expected ~2s)", elapsed)
	})

	t.Run("ExistingDeadlinePreserved", func(t *testing.T) {
		// Create object
		objID := testutil.GetObjectIDForShard(1, "SlowObject")
		_, err := testNode.CreateObject(ctx, "SlowObject", objID)
		if err != nil {
			t.Fatalf("Failed to create SlowObject: %v", err)
		}

		// Wait for object to be created
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Create request with short delay (500ms)
		req := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"delay_ms": structpb.NewNumberValue(500), // 0.5 second delay
			},
		}
		reqAny, err := anypb.New(req)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Call with context that HAS a deadline (1 second) - should use this instead of default 2s
		callCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		callReq := &gate_pb.CallObjectRequest{
			Type:    "SlowObject",
			Id:      objID,
			Method:  "SlowMethod",
			Request: reqAny,
		}

		// This should succeed because 500ms delay < 1s timeout
		_, err = gateClient.CallObject(callCtx, callReq)
		if err != nil {
			t.Fatalf("Expected call to succeed with custom timeout, got error: %v", err)
		}

		t.Logf("Call succeeded with custom deadline")
	})

	t.Run("ExistingDeadlineTimesOut", func(t *testing.T) {
		// Create object
		objID := testutil.GetObjectIDForShard(2, "SlowObject")
		_, err := testNode.CreateObject(ctx, "SlowObject", objID)
		if err != nil {
			t.Fatalf("Failed to create SlowObject: %v", err)
		}

		// Wait for object to be created
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Create request with longer delay (2s)
		req := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"delay_ms": structpb.NewNumberValue(2000), // 2 second delay
			},
		}
		reqAny, err := anypb.New(req)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		// Call with context that HAS a short deadline (500ms)
		callCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		callReq := &gate_pb.CallObjectRequest{
			Type:    "SlowObject",
			Id:      objID,
			Method:  "SlowMethod",
			Request: reqAny,
		}

		start := time.Now()
		_, err = gateClient.CallObject(callCtx, callReq)
		elapsed := time.Since(start)

		// Should timeout after approximately 500ms
		if err == nil {
			t.Fatalf("Expected timeout error, but call succeeded")
		}

		// Verify timeout occurred around the expected time (500ms ± 300ms tolerance)
		if elapsed < 200*time.Millisecond || elapsed > 800*time.Millisecond {
			t.Errorf("Timeout occurred at %v, expected around 500ms", elapsed)
		}

		t.Logf("Call timed out after %v (expected ~500ms)", elapsed)
	})
}
