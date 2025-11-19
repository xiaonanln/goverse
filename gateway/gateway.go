package gateway

import (
	"context"
	"fmt"

	gateway_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/util/logger"
)

// GatewayConfig holds configuration for the gateway
type GatewayConfig struct {
	EtcdAddress string // Address of etcd for cluster state
	EtcdPrefix  string // etcd key prefix (default: "/goverse")
}

// Gateway handles the core gateway logic for routing requests to nodes
type Gateway struct {
	config *GatewayConfig
	logger *logger.Logger
}

// NewGateway creates a new gateway instance
func NewGateway(config *GatewayConfig) (*Gateway, error) {
	if err := validateGatewayConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gateway configuration: %w", err)
	}

	gateway := &Gateway{
		config: config,
		logger: logger.NewLogger("Gateway"),
	}

	return gateway, nil
}

// validateGatewayConfig validates the gateway configuration
func validateGatewayConfig(config *GatewayConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
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

// Start initializes the gateway and connects to the cluster
func (g *Gateway) Start(ctx context.Context) error {
	g.logger.Infof("Starting gateway")

	// TODO: Set up NodeConnections for routing to nodes

	g.logger.Infof("Gateway started")
	return nil
}

// Stop stops the gateway and cleans up resources
func (g *Gateway) Stop() error {
	g.logger.Infof("Stopping gateway")

	// TODO: Stop NodeConnections

	g.logger.Infof("Gateway stopped")
	return nil
}

// Register handles client registration and returns a client ID
func (g *Gateway) Register(ctx context.Context) (string, error) {
	g.logger.Infof("Register called (not yet implemented)")
	// TODO: Generate unique client ID
	// TODO: Track client connection
	return "", fmt.Errorf("not implemented")
}

// CallObject routes a method call to the appropriate node
func (g *Gateway) CallObject(ctx context.Context, req *gateway_pb.CallObjectRequest) (*gateway_pb.CallObjectResponse, error) {
	g.logger.Infof("CallObject called: objectId=%s, method=%s (not yet implemented)", req.Id, req.Method)
	// TODO: Determine which node owns this object based on shard mapping
	// TODO: Route to appropriate node using NodeConnections
	// TODO: Call object method via GoVerse service
	return &gateway_pb.CallObjectResponse{}, fmt.Errorf("not implemented")
}

// CreateObject routes an object creation request to the appropriate node
func (g *Gateway) CreateObject(ctx context.Context, req *gateway_pb.CreateObjectRequest) (*gateway_pb.CreateObjectResponse, error) {
	g.logger.Infof("CreateObject called: type=%s, objectId=%s (not yet implemented)", req.Type, req.Id)
	// TODO: Determine which node should own this object based on shard mapping
	// TODO: Route to appropriate node using NodeConnections
	// TODO: Create object via GoVerse service
	return &gateway_pb.CreateObjectResponse{Id: req.Id}, fmt.Errorf("not implemented")
}

// DeleteObject routes an object deletion request to the appropriate node
func (g *Gateway) DeleteObject(ctx context.Context, req *gateway_pb.DeleteObjectRequest) (*gateway_pb.DeleteObjectResponse, error) {
	g.logger.Infof("DeleteObject called: objectId=%s (not yet implemented)", req.Id)
	// TODO: Determine which node owns this object based on shard mapping
	// TODO: Route to appropriate node using NodeConnections
	// TODO: Delete object via GoVerse service
	return &gateway_pb.DeleteObjectResponse{}, fmt.Errorf("not implemented")
}
