package gateserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/anypb"
)

// GateServerConfig holds configuration for the gate server
type GateServerConfig struct {
	ListenAddress        string        // Address to listen on for client connections (e.g., ":49000")
	AdvertiseAddress     string        // Address to advertise to the cluster (e.g., "localhost:49000")
	HTTPListenAddress    string        // Optional: HTTP address for REST API and metrics (e.g., ":8080")
	EtcdAddress          string        // Address of etcd for cluster state
	EtcdPrefix           string        // Optional: etcd key prefix (default: "/goverse")
	NumShards            int           // Optional: number of shards in the cluster (default: 8192)
	DefaultCallTimeout   time.Duration // Optional: default timeout for CallObject operations (default: 30s)
	DefaultDeleteTimeout time.Duration // Optional: default timeout for DeleteObject operations (default: 10s)
}

// GateServer handles gRPC requests and delegates to the gate
type GateServer struct {
	gate_pb.UnimplementedGateServiceServer
	config     *GateServerConfig
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *logger.Logger
	grpcServer *grpc.Server
	httpServer *http.Server
	gate       *gate.Gate
	cluster    *cluster.Cluster
}

// NewGateServer creates a new gate server instance
func NewGateServer(config *GateServerConfig) (*GateServer, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gate configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create gate instance
	gateConfig := &gate.GateConfig{
		AdvertiseAddress: config.AdvertiseAddress,
		EtcdAddress:      config.EtcdAddress,
		EtcdPrefix:       config.EtcdPrefix,
	}
	gw, err := gate.NewGate(gateConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create gate: %w", err)
	}

	// Create cluster for this gate
	clusterCfg := cluster.Config{
		EtcdAddress: config.EtcdAddress,
		EtcdPrefix:  config.EtcdPrefix,
		NumShards:   config.NumShards,
	}
	c, err := cluster.NewClusterWithGate(clusterCfg, gw)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	server := &GateServer{
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger.NewLogger("GateServer"),
		gate:    gw,
		cluster: c,
	}

	return server, nil
}

// validateConfig validates the gate server configuration
func validateConfig(config *GateServerConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if config.ListenAddress == "" {
		return fmt.Errorf("ListenAddress cannot be empty")
	}
	if config.AdvertiseAddress == "" {
		return fmt.Errorf("AdvertiseAddress cannot be empty")
	}
	if config.EtcdAddress == "" {
		return fmt.Errorf("EtcdAddress cannot be empty")
	}

	// Set defaults
	if config.EtcdPrefix == "" {
		config.EtcdPrefix = "/goverse"
	}
	if config.DefaultCallTimeout <= 0 {
		config.DefaultCallTimeout = 30 * time.Second // Default 30 seconds
	}
	if config.DefaultDeleteTimeout <= 0 {
		config.DefaultDeleteTimeout = 10 * time.Second // Default 10 seconds
	}

	return nil
}

// Start starts the gate server
func (s *GateServer) Start(ctx context.Context) error {
	s.logger.Infof("Starting gate server on %s", s.config.ListenAddress)

	// Start the cluster
	if err := s.cluster.Start(ctx, nil); err != nil {
		return fmt.Errorf("failed to start cluster: %w", err)
	}

	// Start the gate
	if err := s.gate.Start(ctx); err != nil {
		return fmt.Errorf("failed to start gate: %w", err)
	}

	// Create gRPC server
	s.grpcServer = grpc.NewServer()
	gate_pb.RegisterGateServiceServer(s.grpcServer, s)
	reflection.Register(s.grpcServer)

	// Start listening
	listener, err := net.Listen("tcp", s.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.config.ListenAddress, err)
	}

	s.logger.Infof("Gate gRPC server listening on %s", s.config.ListenAddress)

	// Start serving in a goroutine
	go func() {
		if err := s.grpcServer.Serve(listener); err != nil {
			s.logger.Errorf("gRPC server error: %v", err)
		}
	}()

	// Start HTTP server if configured (includes REST API and metrics)
	if err := s.startHTTPServer(ctx); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()
	s.logger.Infof("Gate server context cancelled, initiating shutdown")

	return nil
}

// Stop gracefully stops the gate server
func (s *GateServer) Stop() error {
	s.logger.Infof("Stopping gate server")

	if s.grpcServer != nil {
		// Create a timeout context for graceful shutdown (5 seconds)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		// Graceful stop in goroutine
		done := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(done)
		}()

		// Wait for graceful stop or timeout
		select {
		case <-done:
			s.logger.Infof("gRPC server stopped gracefully")
		case <-shutdownCtx.Done():
			s.logger.Warnf("gRPC server shutdown timed out, forcing stop")
			s.grpcServer.Stop()
		}
	}

	// Shutdown HTTP server if running
	if err := s.stopHTTPServer(); err != nil {
		s.logger.Errorf("HTTP server shutdown error: %v", err)
	}

	// Stop the gate
	if s.gate != nil {
		if err := s.gate.Stop(); err != nil {
			s.logger.Errorf("Error stopping gate: %v", err)
		}
	}

	// Stop the cluster
	if s.cluster != nil {
		if err := s.cluster.Stop(context.Background()); err != nil {
			s.logger.Errorf("Error stopping cluster: %v", err)
		}
	}

	// Cancel context
	s.cancel()

	s.logger.Infof("Gate server stopped")
	return nil
}

// Register implements the Register RPC
func (s *GateServer) Register(req *gate_pb.Empty, stream grpc.ServerStreamingServer[anypb.Any]) error {
	ctx := stream.Context()
	clientProxy := s.gate.Register(ctx)
	clientID := clientProxy.GetID()

	// Make sure to unregister when done
	defer s.gate.Unregister(clientID)

	// Send RegisterResponse
	regResp := &gate_pb.RegisterResponse{ClientId: clientID}
	anyResp, err := anypb.New(regResp)
	if err != nil {
		return fmt.Errorf("failed to marshal RegisterResponse: %w", err)
	}

	if err := stream.Send(anyResp); err != nil {
		return fmt.Errorf("failed to send RegisterResponse: %w", err)
	}

	// Stream messages to the client using the returned clientProxy
	for {
		select {
		case <-ctx.Done():
			s.logger.Infof("Client %s stream closed: %v", clientID, ctx.Err())
			return nil
		case anyMsg, ok := <-clientProxy.MessageChan():
			if !ok {
				s.logger.Infof("Client %s message channel closed", clientID)
				return nil
			}

			// Send the Any message directly (already wrapped by the node)
			if err := stream.Send(anyMsg); err != nil {
				s.logger.Errorf("Failed to send message to client %s: %v", clientID, err)
				return err
			}

			s.logger.Debugf("Sent message to client %s", clientID)
		}
	}
}

// CallObject implements the CallObject RPC
func (s *GateServer) CallObject(ctx context.Context, req *gate_pb.CallObjectRequest) (*gate_pb.CallObjectResponse, error) {
	// Apply default timeout if context has no deadline
	ctx, cancel := callcontext.WithDefaultTimeout(ctx, s.config.DefaultCallTimeout)
	defer cancel()

	// Inject client_id into the context so it can be passed to the node
	if req.ClientId != "" {
		ctx = callcontext.WithClientID(ctx, req.ClientId)
	}

	// Use CallObjectAnyRequest to pass Any directly without unmarshaling (optimization)
	// The cluster will unmarshal only if needed (for local calls)
	response, err := s.cluster.CallObjectAnyRequest(ctx, req.Type, req.Id, req.Method, req.Request)
	if err != nil {
		return nil, err
	}

	return &gate_pb.CallObjectResponse{Response: response}, nil
}

// CreateObject implements the CreateObject RPC
func (s *GateServer) CreateObject(ctx context.Context, req *gate_pb.CreateObjectRequest) (*gate_pb.CreateObjectResponse, error) {
	// Call the cluster to create the object
	objID, err := s.cluster.CreateObject(ctx, req.Type, req.Id)
	if err != nil {
		return nil, err
	}

	return &gate_pb.CreateObjectResponse{Id: objID}, nil
}

// DeleteObject implements the DeleteObject RPC
func (s *GateServer) DeleteObject(ctx context.Context, req *gate_pb.DeleteObjectRequest) (*gate_pb.DeleteObjectResponse, error) {
	// Apply default timeout if context has no deadline
	ctx, cancel := callcontext.WithDefaultTimeout(ctx, s.config.DefaultDeleteTimeout)
	defer cancel()

	// Call the cluster to delete the object
	err := s.cluster.DeleteObject(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return &gate_pb.DeleteObjectResponse{}, nil
}
