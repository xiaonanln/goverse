package gateserver

import (
	"context"
	"fmt"
	"net"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// GatewayServerConfig holds configuration for the gateway server
type GatewayServerConfig struct {
	ListenAddress    string // Address to listen on for client connections (e.g., ":49000")
	AdvertiseAddress string // Address to advertise to the cluster (e.g., "localhost:49000")
	EtcdAddress      string // Address of etcd for cluster state
	EtcdPrefix       string // Optional: etcd key prefix (default: "/goverse")
}

// GatewayServer handles gRPC requests and delegates to the gateway
type GatewayServer struct {
	gate_pb.UnimplementedGateServiceServer
	config     *GatewayServerConfig
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *logger.Logger
	grpcServer *grpc.Server
	gate       *gate.Gateway
	cluster    *cluster.Cluster
}

// NewGatewayServer creates a new gateway server instance
func NewGatewayServer(config *GatewayServerConfig) (*GatewayServer, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gateway configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create gateway instance
	gatewayConfig := &gate.GatewayConfig{
		AdvertiseAddress: config.AdvertiseAddress,
		EtcdAddress:      config.EtcdAddress,
		EtcdPrefix:       config.EtcdPrefix,
	}
	gw, err := gate.NewGateway(gatewayConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	// Create cluster for this gateway
	clusterCfg := cluster.Config{
		EtcdAddress: config.EtcdAddress,
		EtcdPrefix:  config.EtcdPrefix,
	}
	c, err := cluster.NewClusterWithGate(clusterCfg, gw)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	server := &GatewayServer{
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger.NewLogger("GatewayServer"),
		gate:    gw,
		cluster: c,
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

	return nil
}

// Start starts the gateway server
func (s *GatewayServer) Start(ctx context.Context) error {
	s.logger.Infof("Starting gateway server on %s", s.config.ListenAddress)

	// Start the cluster first
	if err := s.cluster.Start(ctx, nil); err != nil {
		return fmt.Errorf("failed to start cluster: %w", err)
	}

	// Start the gateway
	if err := s.gate.Start(ctx); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
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

	// Stop the gateway
	if s.gate != nil {
		if err := s.gate.Stop(); err != nil {
			s.logger.Errorf("Error stopping gateway: %v", err)
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

	s.logger.Infof("Gateway server stopped")
	return nil
}

// Register implements the Register RPC
func (s *GatewayServer) Register(req *gate_pb.Empty, stream grpc.ServerStreamingServer[anypb.Any]) error {
	ctx := stream.Context()
	clientProxy, err := s.gate.Register(ctx)
	if err != nil {
		s.logger.Errorf("Register failed: %v", err)
		return err
	}

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
		case msg, ok := <-clientProxy.MessageChan():
			if !ok {
				s.logger.Infof("Client %s message channel closed", clientID)
				return nil
			}

			// Marshal message to Any and send
			anyMsg, err := anypb.New(msg)
			if err != nil {
				s.logger.Errorf("Failed to marshal message for client %s: %v", clientID, err)
				continue
			}

			if err := stream.Send(anyMsg); err != nil {
				s.logger.Errorf("Failed to send message to client %s: %v", clientID, err)
				return err
			}

			s.logger.Debugf("Sent message to client %s", clientID)
		}
	}
}

// CallObject implements the CallObject RPC
func (s *GatewayServer) CallObject(ctx context.Context, req *gate_pb.CallObjectRequest) (*gate_pb.CallObjectResponse, error) {
	// Unmarshal the request from Any if present
	var requestMsg proto.Message
	if req.Request != nil {
		var err error
		requestMsg, err = req.Request.UnmarshalNew()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	// Call the object via cluster routing
	responseMsg, err := s.cluster.CallObject(ctx, req.Type, req.Id, req.Method, requestMsg)
	if err != nil {
		return nil, err
	}

	// Marshal the response back to Any
	var responseAny *anypb.Any
	if responseMsg != nil {
		responseAny = &anypb.Any{}
		if err := responseAny.MarshalFrom(responseMsg); err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}
	}

	return &gate_pb.CallObjectResponse{Response: responseAny}, nil
}

// CreateObject implements the CreateObject RPC
func (s *GatewayServer) CreateObject(ctx context.Context, req *gate_pb.CreateObjectRequest) (*gate_pb.CreateObjectResponse, error) {
	// Call the cluster to create the object
	objID, err := s.cluster.CreateObject(ctx, req.Type, req.Id)
	if err != nil {
		return nil, err
	}

	return &gate_pb.CreateObjectResponse{Id: objID}, nil
}

// DeleteObject implements the DeleteObject RPC
func (s *GatewayServer) DeleteObject(ctx context.Context, req *gate_pb.DeleteObjectRequest) (*gate_pb.DeleteObjectResponse, error) {
	// Call the cluster to delete the object
	err := s.cluster.DeleteObject(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return &gate_pb.DeleteObjectResponse{}, nil
}
