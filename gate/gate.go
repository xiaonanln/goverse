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

// nodeReg holds registration information for a single node
type nodeReg struct {
	stream goverse_pb.Goverse_RegisterGateClient // active stream to the node
	cancel context.CancelFunc                    // cancel function for the registration goroutine
	ctx    context.Context                       // context for the registration goroutine (used for comparison)
}

// Gateway handles the core gateway logic for routing requests to nodes
type Gateway struct {
	config           *GatewayConfig
	advertiseAddress string
	logger           *logger.Logger
	clients          map[string]*ClientProxy // Map of clientID -> ClientProxy
	clientsMu        sync.RWMutex            // Protects clients map
	nodeRegs         map[string]*nodeReg     // tracks node registrations by node address
	nodeRegMu        sync.RWMutex            // Protects nodeRegs map
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
		nodeRegs:         make(map[string]*nodeReg),
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

	g.cancelAllNodeRegistrations()

	g.logger.Infof("Gateway stopped")
	return nil
}

// cancelAllNodeRegistrations cancels all active node registration goroutines
func (g *Gateway) cancelAllNodeRegistrations() {
	g.nodeRegMu.Lock()
	defer g.nodeRegMu.Unlock()

	for nodeAddr, reg := range g.nodeRegs {
		g.logger.Infof("Cancelling registration for node %s during shutdown", nodeAddr)
		reg.cancel()
	}
	// Clear the map
	g.nodeRegs = make(map[string]*nodeReg)
}

// Register handles client registration and returns the client proxy
func (g *Gateway) Register(ctx context.Context) (*ClientProxy, error) {
	// Generate unique client ID using gateway address and unique ID
	clientID := g.advertiseAddress + "/" + uniqueid.UniqueId()

	// Create a new client proxy
	clientProxy := NewClientProxy(clientID)

	// Track the client connection
	g.clientsMu.Lock()
	g.clients[clientID] = clientProxy
	g.clientsMu.Unlock()

	g.logger.Infof("Registered new client: %s", clientID)
	return clientProxy, nil
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
// and cleans up registrations for nodes that are no longer in the provided connections
func (g *Gateway) RegisterWithNodes(ctx context.Context, nodeConnections map[string]goverse_pb.GoverseClient) {
	// Build set of desired node addresses
	desiredNodes := make(map[string]bool)
	for nodeAddr := range nodeConnections {
		desiredNodes[nodeAddr] = true
	}

	// Cancel registrations for nodes that are no longer in the connections
	g.nodeRegMu.Lock()
	for nodeAddr, reg := range g.nodeRegs {
		if !desiredNodes[nodeAddr] {
			g.logger.Infof("Cancelling registration for removed node %s", nodeAddr)
			reg.cancel()
			delete(g.nodeRegs, nodeAddr)
		}
	}
	g.nodeRegMu.Unlock()

	// Register with new or reconnected nodes
	for nodeAddr, client := range nodeConnections {
		// Check if already registered with this node (and stream is still active)
		g.nodeRegMu.RLock()
		existingReg, registered := g.nodeRegs[nodeAddr]
		g.nodeRegMu.RUnlock()

		if registered && existingReg.stream != nil && existingReg.stream.Context().Err() == nil {
			// Still have an active registration
			continue
		}

		// Register with this node
		g.logger.Infof("Registering gate with node %s", nodeAddr)
		nodeCtx, nodeCancel := context.WithCancel(ctx)

		// Store cancel function before starting goroutine
		// If there's an old registration (from a goroutine that's exiting), replace it
		g.nodeRegMu.Lock()
		if oldReg, exists := g.nodeRegs[nodeAddr]; exists {
			// Cancel the old goroutine if it hasn't already exited
			oldReg.cancel()
		}
		myReg := &nodeReg{
			ctx:    nodeCtx,
			cancel: nodeCancel,
		}
		g.nodeRegs[nodeAddr] = myReg
		g.nodeRegMu.Unlock()

		go g.registerWithNode(nodeCtx, nodeAddr, client, myReg)
	}
}

// registerWithNode registers this gate with a specific node and handles the message stream
func (g *Gateway) registerWithNode(ctx context.Context, nodeAddr string, client goverse_pb.GoverseClient, myReg *nodeReg) {
	// Ensure cleanup of registration on exit
	defer func() {
		g.nodeRegMu.Lock()
		// Only delete if we're still the current registration (not replaced by a newer one)
		if reg, exists := g.nodeRegs[nodeAddr]; exists && reg == myReg {
			delete(g.nodeRegs, nodeAddr)
			g.logger.Infof("Cleaned up registration for node %s", nodeAddr)
		}
		g.nodeRegMu.Unlock()
	}()

	stream, err := client.RegisterGate(ctx, &goverse_pb.RegisterGateRequest{
		GateAddr: g.advertiseAddress,
	})
	if err != nil {
		g.logger.Errorf("Failed to call RegisterGate on node %s: %v", nodeAddr, err)
		return
	}

	// Update registration with the stream
	g.nodeRegMu.Lock()
	if reg, exists := g.nodeRegs[nodeAddr]; exists && reg == myReg {
		reg.stream = stream
	}
	g.nodeRegMu.Unlock()
	g.logger.Infof("Successfully registered gate with node %s", nodeAddr)

	// Use stream's context to detect when stream is closed
	streamCtx := stream.Context()

	// Start receiving messages from the stream
	for {
		select {
		case <-streamCtx.Done():
			g.logger.Infof("Stream context cancelled for node %s: %v", nodeAddr, streamCtx.Err())
			return
		case <-ctx.Done():
			g.logger.Infof("Parent context cancelled, stopping registration with node %s", nodeAddr)
			return
		default:
			// Continue to receive messages
		}

		msg, err := stream.Recv()
		if err != nil {
			g.logger.Errorf("Stream from node %s closed: %v", nodeAddr, err)
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
		// Extract client ID and message from envelope
		envelope := m.ClientMessage
		clientID := envelope.GetClientId()

		g.logger.Debugf("Received client message from node %s for client %s", nodeAddr, clientID)

		// Find the target client
		client, exists := g.GetClient(clientID)
		if !exists {
			g.logger.Warnf("Client %s not found, dropping message from node %s", clientID, nodeAddr)
			return
		}

		// Unmarshal the message
		if envelope.Message != nil {
			msg, err := envelope.Message.UnmarshalNew()
			if err != nil {
				g.logger.Errorf("Failed to unmarshal message for client %s: %v", clientID, err)
				return
			}

			// Send to client's message channel
			client.HandleMessage(msg)
		}
	default:
		g.logger.Warnf("Received unknown message type from node %s", nodeAddr)
	}
}
