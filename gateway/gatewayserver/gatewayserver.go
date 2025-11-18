package gatewayserver

import (
	"context"
	"fmt"
	"net"
	"time"

	gateway_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/gateway"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/anypb"
)

// GatewayServerConfig holds configuration for the gateway server
type GatewayServerConfig struct {
	ListenAddress string        // Address to listen on for client connections (e.g., ":8082")
	EtcdAddress   string        // Address of etcd for cluster state
	EtcdPrefix    string        // Optional: etcd key prefix (default: "/goverse")
	ShutdownGrace time.Duration // Optional: graceful shutdown timeout (default: 5s)
}

// GatewayServer handles client connections and routes requests to nodes
type GatewayServer struct {
	gateway_pb.UnimplementedGatewayServiceServer
	config     *GatewayServerConfig
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *logger.Logger
	grpcServer *grpc.Server
	gateway    *gateway.Gateway
}

// NewGatewayServer creates a new gateway server instance
func NewGatewayServer(config *GatewayServerConfig) (*GatewayServer, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gateway configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create gateway instance
	gatewayConfig := &gateway.GatewayConfig{
		EtcdAddress: config.EtcdAddress,
		EtcdPrefix:  config.EtcdPrefix,
	}
	gw, err := gateway.NewGateway(gatewayConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	server := &GatewayServer{
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger.NewLogger("GatewayServer"),
		gateway: gw,
	}

	return server, nil
}

// validateConfig validates the gateway server configuration
func validateConfig(config *GatewayServerConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if config.ListenAddress == "" {
		return fmt.Errorf("ListenAddress cannot be empty")
	}
	if config.EtcdAddress == "" {
		return fmt.Errorf("EtcdAddress cannot be empty")
	}

	// Set defaults
	if config.EtcdPrefix == "" {
		config.EtcdPrefix = "/goverse"
	}
	if config.ShutdownGrace == 0 {
		config.ShutdownGrace = 5 * time.Second
	}

	return nil
}

// Start starts the gateway server
func (s *GatewayServer) Start(ctx context.Context) error {
	s.logger.Infof("Starting gateway server on %s", s.config.ListenAddress)

	// Start the gateway
	if err := s.gateway.Start(ctx); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
	}

	// Create gRPC server
	s.grpcServer = grpc.NewServer()
	gateway_pb.RegisterGatewayServiceServer(s.grpcServer, s)
	reflection.Register(s.grpcServer)

	// Start listening
	listener, err := net.Listen("tcp", s.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.config.ListenAddress, err)
	}

	s.logger.Infof("Gateway gRPC server listening on %s", s.config.ListenAddress)

	// Start serving in a goroutine
	go func() {
		if err := s.grpcServer.Serve(listener); err != nil {
			s.logger.Errorf("gRPC server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	s.logger.Infof("Gateway server context cancelled, initiating shutdown")

	return nil
}

// Stop gracefully stops the gateway server
func (s *GatewayServer) Stop() error {
	s.logger.Infof("Stopping gateway server")

	if s.grpcServer != nil {
		// Create a timeout context for graceful shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.config.ShutdownGrace)
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

	// Stop the gateway
	if s.gateway != nil {
		if err := s.gateway.Stop(); err != nil {
			s.logger.Errorf("Error stopping gateway: %v", err)
		}
	}

	// Cancel context
	s.cancel()

	s.logger.Infof("Gateway server stopped")
	return nil
}

// Register implements the Register RPC
func (s *GatewayServer) Register(req *gateway_pb.Empty, stream grpc.ServerStreamingServer[anypb.Any]) error {
	ctx := stream.Context()
	clientID, err := s.gateway.Register(ctx)
	if err != nil {
		s.logger.Errorf("Register failed: %v", err)
		return err
	}

	// Send RegisterResponse
	regResp := &gateway_pb.RegisterResponse{ClientId: clientID}
	anyResp, err := anypb.New(regResp)
	if err != nil {
		return fmt.Errorf("failed to marshal RegisterResponse: %w", err)
	}

	if err := stream.Send(anyResp); err != nil {
		return fmt.Errorf("failed to send RegisterResponse: %w", err)
	}

	// Keep stream open for push messages
	<-ctx.Done()
	return nil
}

// CallObject implements the CallObject RPC
func (s *GatewayServer) CallObject(ctx context.Context, req *gateway_pb.CallObjectRequest) (*gateway_pb.CallObjectResponse, error) {
	return s.gateway.CallObject(ctx, req)
}

// CreateObject implements the CreateObject RPC
func (s *GatewayServer) CreateObject(ctx context.Context, req *gateway_pb.CreateObjectRequest) (*gateway_pb.CreateObjectResponse, error) {
	return s.gateway.CreateObject(ctx, req)
}

// DeleteObject implements the DeleteObject RPC
func (s *GatewayServer) DeleteObject(ctx context.Context, req *gateway_pb.DeleteObjectRequest) (*gateway_pb.DeleteObjectResponse, error) {
	return s.gateway.DeleteObject(ctx, req)
}
