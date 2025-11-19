package gate

import (
	"context"
	"fmt"
	"sync"

	goverse_pb "github.com/xiaonanln/goverse/proto"
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
	config            *GatewayConfig
	advertiseAddress  string
	logger            *logger.Logger
	clients           map[string]*ClientProxy // Map of clientID -> ClientProxy
	clientsMu         sync.RWMutex            // Protects clients map
	registeredNodes   map[string]bool         // tracks which nodes this gate has registered with
	registeredNodesMu sync.RWMutex
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
		registeredNodes:  make(map[string]bool),
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

// GetAdvertiseAddress returns the advertise address of this gateway
func (g *Gateway) GetAdvertiseAddress() string {
	return g.advertiseAddress
}

// RegisterWithNodes registers this gate with all provided node connections that haven't been registered yet
func (g *Gateway) RegisterWithNodes(ctx context.Context, nodeConnections map[string]goverse_pb.GoverseClient) {
	for nodeAddr, client := range nodeConnections {
		// Check if already registered with this node
		g.registeredNodesMu.RLock()
		alreadyRegistered := g.registeredNodes[nodeAddr]
		g.registeredNodesMu.RUnlock()

		if alreadyRegistered {
			continue
		}

		// Register with this node
		g.logger.Infof("Registering gate with node %s", nodeAddr)
		go g.registerWithNode(ctx, nodeAddr, client)
	}
}

// registerWithNode registers this gate with a specific node and handles the message stream
func (g *Gateway) registerWithNode(ctx context.Context, nodeAddr string, client goverse_pb.GoverseClient) {
	stream, err := client.RegisterGate(ctx, &goverse_pb.RegisterGateRequest{
		GateAddr: g.advertiseAddress,
	})
	if err != nil {
		g.logger.Errorf("Failed to call RegisterGate on node %s: %v", nodeAddr, err)
		return
	}

	// Mark as registered
	g.registeredNodesMu.Lock()
	g.registeredNodes[nodeAddr] = true
	g.registeredNodesMu.Unlock()
	g.logger.Infof("Successfully registered gate with node %s", nodeAddr)

	// Start receiving messages from the stream
	for {
		msg, err := stream.Recv()
		if err != nil {
			g.logger.Errorf("Stream from node %s closed: %v", nodeAddr, err)
			// Mark as unregistered so we can retry
			g.registeredNodesMu.Lock()
			delete(g.registeredNodes, nodeAddr)
			g.registeredNodesMu.Unlock()
			return
		}
		g.handleGateMessage(nodeAddr, msg)
	}
}

// handleGateMessage handles messages received from a node via the gate stream
func (g *Gateway) handleGateMessage(nodeAddr string, msg *goverse_pb.GateMessage) {
	switch m := msg.Message.(type) {
	case *goverse_pb.GateMessage_RegisterGateResponse:
		g.logger.Debugf("Received RegisterGateResponse from node %s", nodeAddr)
	case *goverse_pb.GateMessage_ClientMessage:
		// Handle client message
		if m.ClientMessage != nil {
			clientID := m.ClientMessage.ClientId
			g.logger.Infof("Received client message for %s from node %s", clientID, nodeAddr)
			// Forward to client proxy
			if clientProxy, exists := g.GetClient(clientID); exists {
				clientProxy.HandleMessage(m.ClientMessage)
			} else {
				g.logger.Warnf("Received message for unknown client %s", clientID)
			}
		}
	default:
		g.logger.Warnf("Received unknown message type from node %s", nodeAddr)
	}
}
