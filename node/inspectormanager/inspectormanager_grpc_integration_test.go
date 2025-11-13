package inspectormanager

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/inspector"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// startTestInspectorServer starts a test inspector gRPC server using the original inspector service
// Returns the gRPC server, graph, and the actual address it's listening on
func startTestInspectorServer(t *testing.T) (*grpc.Server, *graph.GoverseGraph, string) {
	t.Helper()

	// Use :0 to get a random available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen on random port: %v", err)
	}

	// Get the actual address assigned
	address := listener.Addr().String()

	pg := graph.NewGoverseGraph()
	grpcServer := grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(grpcServer, inspector.NewService(pg))

	// Start server in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Inspector server stopped: %v", err)
		}
	}()

	// Register cleanup to gracefully stop the server
	t.Cleanup(func() {
		grpcServer.GracefulStop()
	})

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	return grpcServer, pg, address
}

// TestInspectorManager_ActualConnection tests actual gRPC connection between Node and Inspector
func TestInspectorManager_ActualConnection(t *testing.T) {
	// Start test inspector server on a random port
	_, pg, inspectorAddr := startTestInspectorServer(t)

	// Get a random port for the node
	nodeListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node: %v", err)
	}
	nodeAddr := nodeListener.Addr().String()
	nodeListener.Close()

	// Create and start inspector manager
	mgr := NewInspectorManager(nodeAddr)
	mgr.inspectorAddress = inspectorAddr // Use the randomly assigned inspector address
	mgr.SetHealthCheckInterval(1 * time.Second)
	ctx := context.Background()

	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start inspector manager: %v", err)
	}
	defer mgr.Stop()

	// Wait for connection to establish
	time.Sleep(200 * time.Millisecond)

	// Verify connection is established
	mgr.mu.RLock()
	connected := mgr.connected
	mgr.mu.RUnlock()

	if !connected {
		t.Error("Inspector manager should be connected")
	}

	// Verify node was registered in the graph
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node registered, got %d", len(nodes))
	}
	if len(nodes) > 0 && nodes[0].AdvertiseAddr != nodeAddr {
		t.Errorf("Expected node address %s, got %s", nodeAddr, nodes[0].AdvertiseAddr)
	}

	// Test direct ping
	conn, err := grpc.NewClient(inspectorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to inspector: %v", err)
	}
	defer conn.Close()

	client := inspector_pb.NewInspectorServiceClient(conn)
	_, err = client.Ping(ctx, &inspector_pb.Empty{})
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

// TestInspectorManager_ObjectNotifications tests object add/remove notifications over actual connection
func TestInspectorManager_ObjectNotifications(t *testing.T) {
	// Start test inspector server on a random port
	_, pg, inspectorAddr := startTestInspectorServer(t)

	// Get a random port for the node
	nodeListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node: %v", err)
	}
	nodeAddr := nodeListener.Addr().String()
	nodeListener.Close()

	// Create and start inspector manager
	mgr := NewInspectorManager(nodeAddr)
	mgr.inspectorAddress = inspectorAddr
	mgr.SetHealthCheckInterval(1 * time.Second)
	ctx := context.Background()

	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start inspector manager: %v", err)
	}
	defer mgr.Stop()

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	// Add an object
	mgr.NotifyObjectAdded("test-obj-1", "TestObjectType")

	// Wait for notification to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify object was registered in the graph
	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Errorf("Expected 1 object registered, got %d", len(objects))
	}
	if len(objects) > 0 && objects[0].ID != "test-obj-1" {
		t.Errorf("Expected object ID test-obj-1, got %s", objects[0].ID)
	}

	// Add more objects
	mgr.NotifyObjectAdded("test-obj-2", "TestObjectType")
	mgr.NotifyObjectAdded("test-obj-3", "AnotherType")

	time.Sleep(100 * time.Millisecond)

	// Verify all objects are registered
	objects = pg.GetObjects()
	if len(objects) != 3 {
		t.Errorf("Expected 3 objects registered, got %d", len(objects))
	}

	// Remove an object (note: removal tracking is local only)
	mgr.NotifyObjectRemoved("test-obj-2")

	// Verify it's removed from manager's tracking
	mgr.mu.RLock()
	_, tracked := mgr.objects["test-obj-2"]
	mgr.mu.RUnlock()

	if tracked {
		t.Error("Object test-obj-2 should not be tracked after removal")
	}
}

// TestInspectorManager_NodeUnregistration tests proper node unregistration
func TestInspectorManager_NodeUnregistration(t *testing.T) {
	// Start test inspector server on a random port
	_, pg, inspectorAddr := startTestInspectorServer(t)

	// Get a random port for the node
	nodeListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node: %v", err)
	}
	nodeAddr := nodeListener.Addr().String()
	nodeListener.Close()

	// Create and start inspector manager
	mgr := NewInspectorManager(nodeAddr)
	mgr.inspectorAddress = inspectorAddr
	mgr.SetHealthCheckInterval(1 * time.Second)
	ctx := context.Background()

	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start inspector manager: %v", err)
	}

	// Wait for connection and registration
	time.Sleep(200 * time.Millisecond)

	// Verify node is registered
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node registered before stop, got %d", len(nodes))
	}

	// Stop the manager (should unregister)
	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Failed to stop inspector manager: %v", err)
	}

	// Wait for unregistration to complete
	time.Sleep(100 * time.Millisecond)

	// Verify node is unregistered
	nodes = pg.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes after unregister, got %d", len(nodes))
	}
}

// TestInspectorManager_ReconnectionLogic tests the reconnection behavior
func TestInspectorManager_ReconnectionLogic(t *testing.T) {
	// Start test inspector server on a random port
	grpcServer, pg, inspectorAddr := startTestInspectorServer(t)

	// Get a random port for the node
	nodeListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node: %v", err)
	}
	nodeAddr := nodeListener.Addr().String()
	nodeListener.Close()

	// Create and start inspector manager
	mgr := NewInspectorManager(nodeAddr)
	mgr.inspectorAddress = inspectorAddr
	mgr.SetHealthCheckInterval(1 * time.Second)
	ctx := context.Background()

	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start inspector manager: %v", err)
	}
	defer mgr.Stop()

	// Wait for initial connection
	time.Sleep(200 * time.Millisecond)

	// Verify connected
	mgr.mu.RLock()
	connected := mgr.connected
	mgr.mu.RUnlock()

	if !connected {
		t.Fatal("Inspector manager should be connected initially")
	}

	// Add an object before disconnection
	mgr.NotifyObjectAdded("pre-disconnect-obj", "PreType")
	time.Sleep(100 * time.Millisecond)

	// Verify object is registered
	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object before disconnect, got %d", len(objects))
	}

	// Stop the inspector server (simulate disconnection)
	grpcServer.GracefulStop()
	time.Sleep(100 * time.Millisecond)

	// Verify manager detects disconnection (after next health check)
	// This may take up to healthCheckInterval (1 second), so we wait a bit
	time.Sleep(2 * time.Second)

	mgr.mu.RLock()
	connected = mgr.connected
	mgr.mu.RUnlock()

	if connected {
		t.Error("Inspector manager should detect disconnection")
	}

	// Add object while disconnected (should be queued)
	mgr.NotifyObjectAdded("during-disconnect-obj", "DisconnectType")

	// Restart the inspector server on the same address
	listener, err := net.Listen("tcp", inspectorAddr)
	if err != nil {
		t.Fatalf("Failed to restart listener on %s: %v", inspectorAddr, err)
	}

	pg = graph.NewGoverseGraph()
	grpcServer = grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(grpcServer, inspector.NewService(pg))

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Inspector server stopped: %v", err)
		}
	}()
	defer grpcServer.GracefulStop()

	time.Sleep(100 * time.Millisecond)

	// Wait for reconnection (manager tries every healthCheckInterval = 1 second)
	time.Sleep(2 * time.Second)

	// Verify manager reconnected
	mgr.mu.RLock()
	connected = mgr.connected
	mgr.mu.RUnlock()

	if !connected {
		t.Error("Inspector manager should reconnect after server restart")
	}

	// Wait for re-registration
	time.Sleep(100 * time.Millisecond)

	// Verify node is re-registered
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node after reconnection, got %d", len(nodes))
	}

	// Verify objects are re-registered (both pre and during disconnect)
	objects = pg.GetObjects()
	if len(objects) != 2 {
		t.Errorf("Expected 2 objects after reconnection, got %d", len(objects))
	}

	// Check both objects are present
	objectIDs := make(map[string]bool)
	for _, obj := range objects {
		objectIDs[obj.ID] = true
	}

	if !objectIDs["pre-disconnect-obj"] {
		t.Error("pre-disconnect-obj should be re-registered")
	}
	if !objectIDs["during-disconnect-obj"] {
		t.Error("during-disconnect-obj should be registered after reconnection")
	}
}

// TestInspectorManager_MultipleNodesConnection tests multiple nodes connecting to same inspector
func TestInspectorManager_MultipleNodesConnection(t *testing.T) {
	// Start test inspector server on a random port
	_, pg, inspectorAddr := startTestInspectorServer(t)

	ctx := context.Background()

	// Get random ports for all nodes
	nodeListener1, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node 1: %v", err)
	}
	nodeAddr1 := nodeListener1.Addr().String()
	nodeListener1.Close()

	nodeListener2, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node 2: %v", err)
	}
	nodeAddr2 := nodeListener2.Addr().String()
	nodeListener2.Close()

	nodeListener3, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node 3: %v", err)
	}
	nodeAddr3 := nodeListener3.Addr().String()
	nodeListener3.Close()

	// Create multiple managers for different nodes
	mgr1 := NewInspectorManager(nodeAddr1)
	mgr1.inspectorAddress = inspectorAddr
	mgr1.SetHealthCheckInterval(1 * time.Second)
	mgr2 := NewInspectorManager(nodeAddr2)
	mgr2.inspectorAddress = inspectorAddr
	mgr2.SetHealthCheckInterval(1 * time.Second)
	mgr3 := NewInspectorManager(nodeAddr3)
	mgr3.inspectorAddress = inspectorAddr
	mgr3.SetHealthCheckInterval(1 * time.Second)

	// Start all managers
	err = mgr1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager 1: %v", err)
	}
	defer mgr1.Stop()

	err = mgr2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager 2: %v", err)
	}
	defer mgr2.Stop()

	err = mgr3.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager 3: %v", err)
	}
	defer mgr3.Stop()

	// Wait for all connections
	time.Sleep(200 * time.Millisecond)

	// Verify all nodes are registered
	nodes := pg.GetNodes()
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes registered, got %d", len(nodes))
	}

	// Add objects from different managers
	mgr1.NotifyObjectAdded("node1-obj1", "Type1")
	mgr1.NotifyObjectAdded("node1-obj2", "Type1")
	mgr2.NotifyObjectAdded("node2-obj1", "Type2")
	mgr3.NotifyObjectAdded("node3-obj1", "Type3")

	time.Sleep(100 * time.Millisecond)

	// Verify all objects are registered
	objects := pg.GetObjects()
	if len(objects) != 4 {
		t.Errorf("Expected 4 objects registered, got %d", len(objects))
	}

	// Verify objects are associated with correct nodes
	objectsByNode := make(map[string][]string)
	for _, obj := range objects {
		objectsByNode[obj.GoverseNodeID] = append(objectsByNode[obj.GoverseNodeID], obj.ID)
	}

	if len(objectsByNode[nodeAddr1]) != 2 {
		t.Errorf("Expected 2 objects for node1, got %d", len(objectsByNode[nodeAddr1]))
	}
	if len(objectsByNode[nodeAddr2]) != 1 {
		t.Errorf("Expected 1 object for node2, got %d", len(objectsByNode[nodeAddr2]))
	}
	if len(objectsByNode[nodeAddr3]) != 1 {
		t.Errorf("Expected 1 object for node3, got %d", len(objectsByNode[nodeAddr3]))
	}
}

// TestInspectorManager_ConnectFailureAndRetry tests behavior when initial connection fails
// and manager retries in background
func TestInspectorManager_ConnectFailureAndRetry(t *testing.T) {
	// Get an unused port by listening and then closing immediately
	tempListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port: %v", err)
	}
	inspectorAddr := tempListener.Addr().String()
	tempListener.Close() // Close immediately so the port is free but nothing is listening

	// Get a random port for the node
	nodeListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get random port for node: %v", err)
	}
	nodeAddr := nodeListener.Addr().String()
	nodeListener.Close()

	// Create manager with inspector address pointing to unused port
	mgr := NewInspectorManager(nodeAddr)
	mgr.inspectorAddress = inspectorAddr
	ctx := context.Background()

	// Start should not return error even though connection fails
	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start should not return error even when initial connection fails, got: %v", err)
	}
	defer mgr.Stop()

	// Verify connected remains false after initial connection attempt
	mgr.mu.RLock()
	initialConnected := mgr.connected
	mgr.mu.RUnlock()

	if initialConnected {
		t.Error("Manager should not be connected when inspector is not available")
	}

	// Start test inspector server on that address
	listener, err := net.Listen("tcp", inspectorAddr)
	if err != nil {
		t.Fatalf("Failed to start listener on %s: %v", inspectorAddr, err)
	}

	pg := graph.NewGoverseGraph()
	grpcServer := grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(grpcServer, inspector.NewService(pg))

	// Start server in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Inspector server stopped: %v", err)
		}
	}()
	defer grpcServer.GracefulStop()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Poll with timeout to check if manager reconnects
	// The health check interval is 5 seconds, so we need to wait at least that long
	// We'll poll every 500ms for up to 10 seconds to give it enough time
	maxWait := 10 * time.Second
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(maxWait)

	connected := false
	for time.Now().Before(deadline) {
		mgr.mu.RLock()
		connected = mgr.connected
		mgr.mu.RUnlock()

		if connected {
			break
		}

		time.Sleep(pollInterval)
	}

	if !connected {
		t.Errorf("Manager should reconnect after server starts (waited %v)", maxWait)
	}

	// Poll to verify node is registered in the graph
	nodeRegistered := false
	deadline = time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		nodes := pg.GetNodes()
		if len(nodes) > 0 {
			for _, node := range nodes {
				if node.AdvertiseAddr == nodeAddr {
					nodeRegistered = true
					break
				}
			}
		}

		if nodeRegistered {
			break
		}

		time.Sleep(pollInterval)
	}

	if !nodeRegistered {
		nodes := pg.GetNodes()
		t.Errorf("Node should be registered after reconnection. Found %d nodes", len(nodes))
	}
}
