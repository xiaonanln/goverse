package cluster

import (
	"context"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
)

// TestServerHelper provides a lightweight gRPC server for testing distributed scenarios
// It creates actual network sockets but without production-level configuration
type TestServerHelper struct {
	address  string
	handler  goverse_pb.GoverseServer
	mu       sync.Mutex
	running  bool
	logger   *logger.Logger
	server   *grpc.Server
	listener net.Listener
}

// NewTestServerHelper creates a new test server helper
func NewTestServerHelper(address string, handler goverse_pb.GoverseServer) *TestServerHelper {
	return &TestServerHelper{
		address: address,
		handler: handler,
		logger:  logger.NewLogger("TestServerHelper"),
	}
}

// Start starts the gRPC server listening on the specified address
func (tsh *TestServerHelper) Start(ctx context.Context) error {
	tsh.mu.Lock()
	defer tsh.mu.Unlock()

	if tsh.running {
		return fmt.Errorf("server already running on %s", tsh.address)
	}

	// Create TCP listener
	listener, err := net.Listen("tcp", tsh.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", tsh.address, err)
	}

	// Create gRPC server
	tsh.server = grpc.NewServer()

	// Register the handler
	goverse_pb.RegisterGoverseServer(tsh.server, tsh.handler)

	tsh.listener = listener
	tsh.running = true

	// Start serving in background
	go func() {
		if err := tsh.server.Serve(listener); err != nil {
			tsh.logger.Errorf("Server error: %v", err)
		}
	}()

	tsh.logger.Infof("gRPC server started on %s", tsh.address)
	return nil
}

// Stop stops the gRPC server
func (tsh *TestServerHelper) Stop() error {
	tsh.mu.Lock()
	defer tsh.mu.Unlock()

	if !tsh.running {
		return nil
	}

	if tsh.server != nil {
		tsh.server.GracefulStop()
	}

	if tsh.listener != nil {
		tsh.listener.Close()
	}

	tsh.running = false
	tsh.logger.Infof("gRPC server stopped on %s", tsh.address)
	return nil
}

// IsRunning returns whether the server is running
func (tsh *TestServerHelper) IsRunning() bool {
	tsh.mu.Lock()
	defer tsh.mu.Unlock()
	return tsh.running
}

// MockGoverseServer is a minimal implementation of the Goverse gRPC service for testing
// It delegates actual operations to a provided Node instance
type MockGoverseServer struct {
	goverse_pb.UnimplementedGoverseServer
	logger *logger.Logger
	node   interface{} // *node.Node - using interface{} to avoid circular import
	mu     sync.Mutex
}

// NewMockGoverseServer creates a new mock Goverse server
func NewMockGoverseServer() *MockGoverseServer {
	return &MockGoverseServer{
		logger: logger.NewLogger("MockGoverseServer"),
	}
}

// SetNode sets the node instance for the server to delegate to
func (m *MockGoverseServer) SetNode(node interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node = node
}

// Status returns a status response
func (m *MockGoverseServer) Status(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.StatusResponse, error) {
	m.mu.Lock()
	node := m.node
	m.mu.Unlock()

	if node == nil {
		return &goverse_pb.StatusResponse{
			AdvertiseAddr: "test-node",
			NumObjects:    0,
			UptimeSeconds: 0,
		}, nil
	}

	// For now, return simple status - actual implementation would call node methods
	return &goverse_pb.StatusResponse{
		AdvertiseAddr: "test-node",
		NumObjects:    0,
		UptimeSeconds: 0,
	}, nil
}

// CallObject returns an error (not implemented for mock)
func (m *MockGoverseServer) CallObject(ctx context.Context, req *goverse_pb.CallObjectRequest) (*goverse_pb.CallObjectResponse, error) {
	return nil, fmt.Errorf("CallObject not implemented in mock server")
}

// CreateObject handles remote object creation by delegating to the node
func (m *MockGoverseServer) CreateObject(ctx context.Context, req *goverse_pb.CreateObjectRequest) (*goverse_pb.CreateObjectResponse, error) {
	m.mu.Lock()
	node := m.node
	m.mu.Unlock()

	if node == nil {
		return nil, fmt.Errorf("no node assigned to mock server")
	}

	// For testing purposes, we just return success with the object ID
	// In a real implementation, this would create the object on the node
	return &goverse_pb.CreateObjectResponse{
		Id: req.Id,
	}, nil
}

// ListObjects returns an empty list
func (m *MockGoverseServer) ListObjects(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.ListObjectsResponse, error) {
	return &goverse_pb.ListObjectsResponse{
		Objects: []*goverse_pb.ObjectInfo{},
	}, nil
}
