package gate

import (
	"context"
	"fmt"
	"sync"

	gateway_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/uniqueid"
)

// GatewayConfig holds configuration for the gateway
type GatewayConfig struct {
	AdvertiseAddress string // Address to advertise to the cluster (e.g., "localhost:49000")
	EtcdAddress      string // Address of etcd for cluster state
	EtcdPrefix       string // etcd key prefix (default: "/goverse")
}

// Gateway handles the core gateway logic for routing requests to nodes
type Gateway struct {
	config           *GatewayConfig
	advertiseAddress string
	logger           *logger.Logger
	clients          map[string]*ClientProxy // Map of clientID -> ClientProxy
	clientsMu        sync.RWMutex            // Protects clients map
}

// NewGateway creates a new gateway instance
func NewGateway(config *GatewayConfig) (*Gateway, error) {
	if err := validateGatewayConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gateway configuration: %w", err)
	}

	gateway := &Gateway{
		config:           config,
		advertiseAddress: config.AdvertiseAddress,
		logger:           logger.NewLogger("Gateway"),
		clients:          make(map[string]*ClientProxy),
	}

	return gateway, nil
}

// validateGatewayConfig validates the gateway configuration
func validateGatewayConfig(config *GatewayConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
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
	// Generate unique client ID using gateway address and unique ID
	clientID := g.advertiseAddress + "/" + uniqueid.UniqueId()

	// Create a new client proxy
	clientProxy := NewClientProxy(clientID)

	// Track the client connection
	g.clientsMu.Lock()
	g.clients[clientID] = clientProxy
	g.clientsMu.Unlock()

	g.logger.Infof("Registered new client: %s", clientID)
	return clientID, nil
}

// Unregister removes a client by its ID
func (g *Gateway) Unregister(clientID string) {
	g.clientsMu.Lock()
	defer g.clientsMu.Unlock()

	if clientProxy, exists := g.clients[clientID]; exists {
		clientProxy.Close()
		delete(g.clients, clientID)
		g.logger.Infof("Unregistered client: %s", clientID)
	}
}

// GetClient returns the client proxy for the given client ID
func (g *Gateway) GetClient(clientID string) (*ClientProxy, bool) {
	g.clientsMu.RLock()
	defer g.clientsMu.RUnlock()
	client, exists := g.clients[clientID]
	return client, exists
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

// GetAdvertiseAddress returns the advertise address of this gateway
func (g *Gateway) GetAdvertiseAddress() string {
	return g.advertiseAddress
}
