package cluster

import (
	"context"
	"fmt"
	"net"
	"sync"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
)

// TestServerHelper provides a lightweight mock gRPC server for testing distributed scenarios
type TestServerHelper struct {
	address  string
	server   *grpc.Server
	listener net.Listener
	handler  goverse_pb.GoverseServer
	mu       sync.Mutex
	running  bool
	logger   *logger.Logger
}

// NewTestServerHelper creates a new test server helper
func NewTestServerHelper(address string, handler goverse_pb.GoverseServer) *TestServerHelper {
	return &TestServerHelper{
		address: address,
		handler: handler,
		logger:  logger.NewLogger("TestServerHelper"),
	}
}

// Start starts the mock gRPC server
func (tsh *TestServerHelper) Start(ctx context.Context) error {
	tsh.mu.Lock()
	defer tsh.mu.Unlock()

	if tsh.running {
		return fmt.Errorf("server already running on %s", tsh.address)
	}

	// Create listener
	listener, err := net.Listen("tcp", tsh.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", tsh.address, err)
	}

	tsh.listener = listener
	tsh.server = grpc.NewServer()

	// Register the Goverse service
	goverse_pb.RegisterGoverseServer(tsh.server, tsh.handler)

	// Start server in background
	go func() {
		tsh.logger.Infof("Starting test gRPC server on %s", tsh.address)
		if err := tsh.server.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			tsh.logger.Errorf("Server error: %v", err)
		}
	}()

	tsh.running = true
	tsh.logger.Infof("Test gRPC server started on %s", tsh.address)
	return nil
}

// Stop stops the mock gRPC server
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
	tsh.logger.Infof("Test gRPC server stopped on %s", tsh.address)
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
}

// NewMockGoverseServer creates a new mock Goverse server
func NewMockGoverseServer() *MockGoverseServer {
	return &MockGoverseServer{
		logger: logger.NewLogger("MockGoverseServer"),
	}
}

// SetNode sets the node instance for the server to delegate to
func (m *MockGoverseServer) SetNode(node interface{}) {
	m.node = node
}

// Status returns a status response
func (m *MockGoverseServer) Status(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.StatusResponse, error) {
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

// CreateObject returns an error (not implemented for mock)
func (m *MockGoverseServer) CreateObject(ctx context.Context, req *goverse_pb.CreateObjectRequest) (*goverse_pb.CreateObjectResponse, error) {
	return nil, fmt.Errorf("CreateObject not implemented in mock server")
}

// ListObjects returns an empty list
func (m *MockGoverseServer) ListObjects(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.ListObjectsResponse, error) {
	return &goverse_pb.ListObjectsResponse{
		Objects: []*goverse_pb.ObjectInfo{},
	}, nil
}
