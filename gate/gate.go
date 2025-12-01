package gate

import (
	"context"
	"fmt"
	"sync"

	"github.com/xiaonanln/goverse/gate/inspectormanager"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/uniqueid"
)

// GateConfig holds configuration for the gate
type GateConfig struct {
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

// Gate handles the core gate logic for routing requests to nodes
type Gate struct {
	config           *GateConfig
	advertiseAddress string
	logger           *logger.Logger
	clients          map[string]*ClientProxy // Map of clientID -> ClientProxy
	clientsMu        sync.RWMutex            // Protects clients map
	nodeRegs         map[string]*nodeReg     // tracks node registrations by node address
	nodeRegMu        sync.RWMutex            // Protects nodeRegs map
	inspectorManager *inspectormanager.GateInspectorManager
}

// NewGate creates a new gate instance
func NewGate(config *GateConfig) (*Gate, error) {
	if err := validateGateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gate configuration: %w", err)
	}

	gate := &Gate{
		config:           config,
		advertiseAddress: config.AdvertiseAddress,
		logger:           logger.NewLogger("Gate"),
		clients:          make(map[string]*ClientProxy),
		nodeRegs:         make(map[string]*nodeReg),
		inspectorManager: inspectormanager.NewGateInspectorManager(config.AdvertiseAddress),
	}

	return gate, nil
}

// validateGateConfig validates the gate configuration
func validateGateConfig(config *GateConfig) error {
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

// Start initializes the gate and connects to the cluster
func (g *Gate) Start(ctx context.Context) error {
	g.logger.Infof("Starting gate")

	// Start the inspector manager
	if err := g.inspectorManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start inspector manager: %w", err)
	}

	g.logger.Infof("Gate started")
	return nil
}

// Stop stops the gate and cleans up resources
func (g *Gate) Stop() error {
	g.logger.Infof("Stopping gate")

	g.cleanupClientProxies()
	g.cancelAllNodeRegistrations()

	// Stop the inspector manager and unregister from inspector
	if err := g.inspectorManager.Stop(); err != nil {
		g.logger.Errorf("Error stopping inspector manager: %v", err)
	}

	g.logger.Infof("Gate stopped")
	return nil
}

// cancelAllNodeRegistrations cancels all active node registration goroutines
func (g *Gate) cancelAllNodeRegistrations() {
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
func (g *Gate) Register(ctx context.Context) *ClientProxy {
	// Generate unique client ID using gate address and unique ID
	clientID := g.advertiseAddress + "/" + uniqueid.UniqueId()

	// Create a new client proxy
	clientProxy := NewClientProxy(clientID)

	// Track the client connection
	g.clientsMu.Lock()
	g.clients[clientID] = clientProxy
	clientCount := len(g.clients)
	g.clientsMu.Unlock()

	// Set metrics to current count
	metrics.SetGateActiveClients(g.advertiseAddress, clientCount)

	g.logger.Infof("Registered new client: %s", clientID)
	return clientProxy
}

// Unregister removes a client by its ID
func (g *Gate) Unregister(clientID string) {
	g.clientsMu.Lock()
	defer g.clientsMu.Unlock()

	if clientProxy, exists := g.clients[clientID]; exists {
		clientProxy.Close()
		delete(g.clients, clientID)
		// Set metrics to current count
		metrics.SetGateActiveClients(g.advertiseAddress, len(g.clients))
		g.logger.Infof("Unregistered client: %s", clientID)
	}
}

// GetClient returns the client proxy for the given client ID
func (g *Gate) GetClient(clientID string) (*ClientProxy, bool) {
	g.clientsMu.RLock()
	defer g.clientsMu.RUnlock()
	client, exists := g.clients[clientID]
	return client, exists
}

func (g *Gate) cleanupClientProxies() {
	g.clientsMu.Lock()
	defer g.clientsMu.Unlock()

	for clientID, clientProxy := range g.clients {
		clientProxy.Close()
		delete(g.clients, clientID)
		g.logger.Infof("Cleaned up client proxy: %s", clientID)
	}
	// Set metrics to current count (should be 0 after cleanup)
	metrics.SetGateActiveClients(g.advertiseAddress, len(g.clients))
}

// GetAdvertiseAddress returns the advertise address of this gate
func (g *Gate) GetAdvertiseAddress() string {
	return g.advertiseAddress
}

// RegisterWithNodes registers this gate with all provided node connections that haven't been registered yet
// and cleans up registrations for nodes that are no longer in the provided connections
func (g *Gate) RegisterWithNodes(ctx context.Context, nodeConnections map[string]goverse_pb.GoverseClient) {
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
func (g *Gate) registerWithNode(ctx context.Context, nodeAddr string, client goverse_pb.GoverseClient, myReg *nodeReg) {
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
func (g *Gate) handleGateMessage(nodeAddr string, msg *goverse_pb.GateMessage) {
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
			// Record dropped message metric
			metrics.RecordGateDroppedMessage(g.advertiseAddress)
			return
		}

		// Forward the Any message directly without unmarshaling
		// The client is responsible for unmarshaling based on the application proto types
		if envelope.Message != nil {
			// Send the google.protobuf.Any directly to client's message channel
			client.PushMessageAny(envelope.Message)
			// Record metric for pushed message
			metrics.RecordGatePushedMessage(g.advertiseAddress)
		} else {
			g.logger.Warnf("Received nil message for client %s from node %s", clientID, nodeAddr)
			// Record dropped message metric for nil message
			metrics.RecordGateDroppedMessage(g.advertiseAddress)
		}
	default:
		g.logger.Warnf("Received unknown message type %v from node %s", msg.Message, nodeAddr)
	}
}
