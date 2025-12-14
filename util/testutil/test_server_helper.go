package testutil

import (
	"context"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/callcontext"
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

	// Update address with the actual bound address (important for :0 port allocation)
	tsh.address = listener.Addr().String()

	// Capture logger and server before starting goroutine to avoid race
	logger := tsh.logger
	server := tsh.server

	// Start serving in background
	go func() {
		if err := server.Serve(listener); err != nil {
			logger.Errorf("Server error: %v", err)
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

// GetAddress returns the address the server is listening on.
// This is particularly useful when using :0 for dynamic port allocation.
func (tsh *TestServerHelper) GetAddress() string {
	tsh.mu.Lock()
	defer tsh.mu.Unlock()
	return tsh.address
}

type nodeInterface interface {
	CreateObject(ctx context.Context, typ string, id string) (string, error)
	CallObject(ctx context.Context, typ string, id string, method string, request proto.Message) (proto.Message, error)
	DeleteObject(ctx context.Context, id string) error
	ReliableCallObject(ctx context.Context, seq int64, objectType string, objectID string) (*anypb.Any, error)
}

type clusterInterface interface {
	RegisterGateConnection(gateAddr string) (chan proto.Message, error)
	UnregisterGateConnection(gateAddr string, ch chan proto.Message)
}

// MockGoverseServer is a minimal implementation of the Goverse gRPC service for testing
// It delegates actual operations to a provided Node instance
type MockGoverseServer struct {
	goverse_pb.UnimplementedGoverseServer
	logger  *logger.Logger
	node    nodeInterface
	cluster clusterInterface
	mu      sync.Mutex
}

// NewMockGoverseServer creates a new mock Goverse server
func NewMockGoverseServer() *MockGoverseServer {
	return &MockGoverseServer{
		logger: logger.NewLogger("MockGoverseServer"),
	}
}

// SetNode sets the node instance for the server to delegate to
func (m *MockGoverseServer) SetNode(node nodeInterface) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node = node
}

// SetCluster sets the cluster instance for the server to delegate to
func (m *MockGoverseServer) SetCluster(cluster clusterInterface) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cluster = cluster
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

// CallObject handles remote object calls by delegating to the node
func (m *MockGoverseServer) CallObject(ctx context.Context, req *goverse_pb.CallObjectRequest) (*goverse_pb.CallObjectResponse, error) {
	m.mu.Lock()
	node := m.node
	m.mu.Unlock()

	if node == nil {
		return nil, fmt.Errorf("no node assigned to mock server")
	}

	// Inject client_id into context if present in the request
	if req.ClientId != "" {
		ctx = callcontext.WithClientID(ctx, req.ClientId)
	}

	// Unmarshal request
	var requestMsg proto.Message
	var err error
	if req.Request != nil {
		requestMsg, err = anypb.UnmarshalNew(req.Request, proto.UnmarshalOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %v", err)
		}
	}

	// Call the object on the node
	resp, err := node.CallObject(ctx, req.GetType(), req.GetId(), req.GetMethod(), requestMsg)
	if err != nil {
		return nil, err
	}

	// Marshal response
	var respAny anypb.Any
	if resp != nil {
		if err := respAny.MarshalFrom(resp); err != nil {
			return nil, fmt.Errorf("failed to marshal response: %v", err)
		}
	}

	return &goverse_pb.CallObjectResponse{
		Response: &respAny,
	}, nil
}

// CreateObject handles remote object creation by delegating to the node
func (m *MockGoverseServer) CreateObject(ctx context.Context, req *goverse_pb.CreateObjectRequest) (*goverse_pb.CreateObjectResponse, error) {
	m.mu.Lock()
	node := m.node
	m.mu.Unlock()

	if node == nil {
		return nil, fmt.Errorf("no node assigned to mock server")
	}

	// Call CreateObject on the actual node
	createdID, err := node.CreateObject(ctx, req.Type, req.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to create object: %v", err)
	}

	return &goverse_pb.CreateObjectResponse{
		Id: createdID,
	}, nil
}

// DeleteObject handles remote object deletion by delegating to the node
func (m *MockGoverseServer) DeleteObject(ctx context.Context, req *goverse_pb.DeleteObjectRequest) (*goverse_pb.DeleteObjectResponse, error) {
	m.mu.Lock()
	node := m.node
	m.mu.Unlock()

	if node == nil {
		return nil, fmt.Errorf("no node assigned to mock server")
	}

	// Call DeleteObject on the actual node
	err := node.DeleteObject(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete object: %v", err)
	}

	return &goverse_pb.DeleteObjectResponse{}, nil
}

// ReliableCallObject handles remote reliable object calls by delegating to the node
func (m *MockGoverseServer) ReliableCallObject(ctx context.Context, req *goverse_pb.ReliableCallObjectRequest) (*goverse_pb.ReliableCallObjectResponse, error) {
	m.mu.Lock()
	node := m.node
	m.mu.Unlock()

	if node == nil {
		return nil, fmt.Errorf("no node assigned to mock server")
	}

	// Validate request parameters
	if req.GetSeq() <= 0 {
		return nil, fmt.Errorf("seq must be positive in ReliableCallObject request")
	}
	if req.GetObjectType() == "" {
		return nil, fmt.Errorf("object_type must be specified in ReliableCallObject request")
	}
	if req.GetObjectId() == "" {
		return nil, fmt.Errorf("object_id must be specified in ReliableCallObject request")
	}

	// Call ReliableCallObject on the actual node
	resultAny, err := node.ReliableCallObject(ctx, req.GetSeq(), req.GetObjectType(), req.GetObjectId())
	if err != nil {
		return &goverse_pb.ReliableCallObjectResponse{
			Error: err.Error(),
		}, nil
	}

	return &goverse_pb.ReliableCallObjectResponse{
		Result: resultAny,
	}, nil
}

// ListObjects returns an empty list
func (m *MockGoverseServer) ListObjects(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.ListObjectsResponse, error) {
	return &goverse_pb.ListObjectsResponse{
		Objects: []*goverse_pb.ObjectInfo{},
	}, nil
}

// RegisterGate handles gate registration by delegating to the cluster
func (m *MockGoverseServer) RegisterGate(req *goverse_pb.RegisterGateRequest, stream goverse_pb.Goverse_RegisterGateServer) error {
	m.mu.Lock()
	cluster := m.cluster
	m.mu.Unlock()

	gateAddr := req.GetGateAddr()
	if gateAddr == "" {
		return fmt.Errorf("gate_addr must be specified in RegisterGate request")
	}

	if cluster == nil {
		return fmt.Errorf("no cluster assigned to mock server")
	}

	m.logger.Infof("Gate %s registered, starting message stream", gateAddr)

	// Send RegisterGateResponse as the first message
	registerResponse := &goverse_pb.GateMessage{
		Message: &goverse_pb.GateMessage_RegisterGateResponse{
			RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
		},
	}
	if err := stream.Send(registerResponse); err != nil {
		m.logger.Errorf("Failed to send RegisterGateResponse to gate %s: %v", gateAddr, err)
		return err
	}

	// Acquire message channel from cluster
	msgChan, err := cluster.RegisterGateConnection(gateAddr)
	if err != nil {
		m.logger.Errorf("Failed to register gate connection for %s: %v", gateAddr, err)
		return err
	}
	defer cluster.UnregisterGateConnection(gateAddr, msgChan)

	// Loop to forward messages from channel to stream
	for {
		select {
		case <-stream.Context().Done():
			m.logger.Infof("Gate %s stream closed: %v", gateAddr, stream.Context().Err())
			return nil
		case msg, ok := <-msgChan:
			if !ok {
				m.logger.Infof("Gate %s message channel closed, stopping stream", gateAddr)
				return nil
			}
			// The message should be a ClientMessageEnvelope
			envelope, ok := msg.(*goverse_pb.ClientMessageEnvelope)
			if !ok {
				m.logger.Errorf("Received non-envelope message for gate %s: %v", gateAddr, msg)
				continue
			}

			gateMsg := &goverse_pb.GateMessage{
				Message: &goverse_pb.GateMessage_ClientMessage{
					ClientMessage: envelope,
				},
			}

			if err := stream.Send(gateMsg); err != nil {
				m.logger.Errorf("Failed to send message to gate %s: %v", gateAddr, err)
				return err
			}
		}
	}
}
