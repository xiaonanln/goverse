package node

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InspectorServiceImpl is a test implementation of the Inspector gRPC service
type InspectorServiceImpl struct {
	inspector_pb.UnimplementedInspectorServiceServer
	pg *graph.GoverseGraph
}

// NewInspectorServiceImpl creates a new inspector service for testing
func NewInspectorServiceImpl(pg *graph.GoverseGraph) *InspectorServiceImpl {
	return &InspectorServiceImpl{pg: pg}
}

// Ping handles ping requests
func (s *InspectorServiceImpl) Ping(ctx context.Context, req *inspector_pb.Empty) (*inspector_pb.Empty, error) {
	return &inspector_pb.Empty{}, nil
}

// RegisterNode handles node registration
func (s *InspectorServiceImpl) RegisterNode(ctx context.Context, req *inspector_pb.RegisterNodeRequest) (*inspector_pb.RegisterNodeResponse, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		return nil, grpc.Errorf(3, "advertise address cannot be empty")
	}

	node := models.GoverseNode{
		ID:            addr,
		Label:         "Node " + addr,
		X:             0,
		Y:             0,
		Width:         120,
		Height:        80,
		Color:         "#4CAF50",
		Type:          "goverse_node",
		AdvertiseAddr: addr,
		RegisteredAt:  time.Now(),
	}
	s.pg.AddOrUpdateNode(node)

	// Register initial objects
	for _, o := range req.GetObjects() {
		if o == nil || o.Id == "" {
			continue
		}
		obj := models.GoverseObject{
			ID:            o.Id,
			Label:         o.GetClass() + " (" + o.GetId() + ")",
			X:             0,
			Y:             0,
			Size:          10,
			Color:         "#1f77b4",
			Type:          "object",
			GoverseNodeID: addr,
		}
		s.pg.AddOrUpdateObject(obj)
	}

	return &inspector_pb.RegisterNodeResponse{}, nil
}

// UnregisterNode handles node unregistration
func (s *InspectorServiceImpl) UnregisterNode(ctx context.Context, req *inspector_pb.UnregisterNodeRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()
	s.pg.RemoveNode(addr)
	return &inspector_pb.Empty{}, nil
}

// AddOrUpdateObject handles object addition/update
func (s *InspectorServiceImpl) AddOrUpdateObject(ctx context.Context, req *inspector_pb.AddOrUpdateObjectRequest) (*inspector_pb.Empty, error) {
	o := req.GetObject()
	if o == nil || o.Id == "" {
		return &inspector_pb.Empty{}, nil
	}

	nodeAddress := req.GetNodeAddress()
	if !s.pg.IsNodeRegistered(nodeAddress) {
		return nil, grpc.Errorf(5, "node not registered")
	}

	obj := models.GoverseObject{
		ID:            o.Id,
		Label:         o.GetClass() + " (" + o.GetId() + ")",
		X:             0,
		Y:             0,
		Size:          10,
		Color:         "#1f77b4",
		Type:          "object",
		GoverseNodeID: nodeAddress,
	}
	s.pg.AddOrUpdateObject(obj)
	return &inspector_pb.Empty{}, nil
}

// startTestInspectorServer starts a test inspector gRPC server
func startTestInspectorServer(t *testing.T, address string) (*grpc.Server, *graph.GoverseGraph) {
	t.Helper()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("Failed to listen on %s: %v", address, err)
	}

	pg := graph.NewGoverseGraph()
	grpcServer := grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(grpcServer, NewInspectorServiceImpl(pg))

	// Start server in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Inspector server stopped: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	return grpcServer, pg
}

// TestInspectorManager_ActualConnection tests actual gRPC connection between Node and Inspector
func TestInspectorManager_ActualConnection(t *testing.T) {
	// Start test inspector server on the expected port
	grpcServer, pg := startTestInspectorServer(t, "localhost:8081")
	defer grpcServer.GracefulStop()

	// Create and start inspector manager
	mgr := NewInspectorManager("localhost:47200")
	ctx := context.Background()

	err := mgr.Start(ctx)
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
	if len(nodes) > 0 && nodes[0].AdvertiseAddr != "localhost:47200" {
		t.Errorf("Expected node address localhost:47200, got %s", nodes[0].AdvertiseAddr)
	}

	// Test direct ping
	conn, err := grpc.NewClient("localhost:8081", grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	// Start test inspector server
	grpcServer, pg := startTestInspectorServer(t, "localhost:8081")
	defer grpcServer.GracefulStop()

	// Create and start inspector manager
	mgr := NewInspectorManager("localhost:47201")
	ctx := context.Background()

	err := mgr.Start(ctx)
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
	// Start test inspector server
	grpcServer, pg := startTestInspectorServer(t, "localhost:8081")
	defer grpcServer.GracefulStop()

	// Create and start inspector manager
	mgr := NewInspectorManager("localhost:47202")
	ctx := context.Background()

	err := mgr.Start(ctx)
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
	// Start test inspector server
	grpcServer, pg := startTestInspectorServer(t, "localhost:8081")

	// Create and start inspector manager
	mgr := NewInspectorManager("localhost:47203")
	ctx := context.Background()

	err := mgr.Start(ctx)
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
	// This may take up to healthCheckInterval (5 seconds), so we wait a bit
	time.Sleep(6 * time.Second)

	mgr.mu.RLock()
	connected = mgr.connected
	mgr.mu.RUnlock()

	if connected {
		t.Error("Inspector manager should detect disconnection")
	}

	// Add object while disconnected (should be queued)
	mgr.NotifyObjectAdded("during-disconnect-obj", "DisconnectType")

	// Restart the inspector server
	grpcServer, pg = startTestInspectorServer(t, "localhost:8081")
	defer grpcServer.GracefulStop()

	// Wait for reconnection (manager tries every reconnectRetryInterval = 5 seconds)
	time.Sleep(6 * time.Second)

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
	// Start test inspector server
	grpcServer, pg := startTestInspectorServer(t, "localhost:8081")
	defer grpcServer.GracefulStop()

	ctx := context.Background()

	// Create multiple managers for different nodes
	mgr1 := NewInspectorManager("localhost:47210")
	mgr2 := NewInspectorManager("localhost:47211")
	mgr3 := NewInspectorManager("localhost:47212")

	// Start all managers
	err := mgr1.Start(ctx)
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

	if len(objectsByNode["localhost:47210"]) != 2 {
		t.Errorf("Expected 2 objects for node1, got %d", len(objectsByNode["localhost:47210"]))
	}
	if len(objectsByNode["localhost:47211"]) != 1 {
		t.Errorf("Expected 1 object for node2, got %d", len(objectsByNode["localhost:47211"]))
	}
	if len(objectsByNode["localhost:47212"]) != 1 {
		t.Errorf("Expected 1 object for node3, got %d", len(objectsByNode["localhost:47212"]))
	}
}
