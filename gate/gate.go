package gate

import (
	"context"
	"fmt"
	"sync"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/uniqueid"
)

const (
	// shutdownTimeout is the maximum time to wait for goroutines to finish during shutdown
	shutdownTimeout = 5 * time.Second
	// recvGoroutineCleanupTimeout is the time to wait for stream receive goroutine to finish
	recvGoroutineCleanupTimeout = 100 * time.Millisecond
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
	clients           map[string]*ClientProxy                          // Map of clientID -> ClientProxy
	clientsMu         sync.RWMutex                                     // Protects clients map
	registeredNodes   map[string]goverse_pb.Goverse_RegisterGateClient // tracks active streams by node address
	registeredNodesMu sync.RWMutex
	ctx               context.Context    // Gateway context for lifecycle management
	cancel            context.CancelFunc // Cancel function for gateway context
	wg                sync.WaitGroup     // WaitGroup to track goroutines
	stopOnce          sync.Once          // Ensures Stop is only executed once
}

// NewGateway creates a new gateway instance
func NewGateway(config *GatewayConfig) (*Gateway, error) {
	if err := validateGatewayConfig(config); err != nil {
		return nil, fmt.Errorf("invalid gateway configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	gateway := &Gateway{
		config:           config,
		advertiseAddress: config.AdvertiseAddress,
		logger:           logger.NewLogger("Gateway"),
		clients:          make(map[string]*ClientProxy),
		registeredNodes:  make(map[string]goverse_pb.Goverse_RegisterGateClient),
		ctx:              ctx,
		cancel:           cancel,
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
// Safe to call multiple times
func (g *Gateway) Stop() error {
	var err error
	g.stopOnce.Do(func() {
		g.logger.Infof("Stopping gateway")

		// Cancel context to signal all goroutines to stop
		if g.cancel != nil {
			g.cancel()
		}

		// Wait for all goroutines to finish with timeout
		done := make(chan struct{})
		go func() {
			g.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			g.logger.Infof("All gateway goroutines stopped")
		case <-time.After(shutdownTimeout):
			g.logger.Warnf("Timeout waiting for gateway goroutines to stop")
		}

		// Close all client connections
		g.clientsMu.Lock()
		for clientID, clientProxy := range g.clients {
			clientProxy.Close()
			g.logger.Debugf("Closed client proxy for %s", clientID)
		}
		g.clients = make(map[string]*ClientProxy)
		g.clientsMu.Unlock()

		g.logger.Infof("Gateway stopped")
	})
	return err
}

// Register handles client registration and returns a client ID and the client proxy
func (g *Gateway) Register(ctx context.Context) (string, *ClientProxy, error) {
	// Generate unique client ID using gateway address and unique ID
	clientID := g.advertiseAddress + "/" + uniqueid.UniqueId()

	// Create a new client proxy
	clientProxy := NewClientProxy(clientID)

	// Track the client connection
	g.clientsMu.Lock()
	g.clients[clientID] = clientProxy
	g.clientsMu.Unlock()

	g.logger.Infof("Registered new client: %s", clientID)
	return clientID, clientProxy, nil
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
		// Check if already registered with this node (and stream is still active)
		g.registeredNodesMu.RLock()
		existingStream, registered := g.registeredNodes[nodeAddr]
		g.registeredNodesMu.RUnlock()

		if registered && existingStream.Context().Err() == nil {
			// Still have an active registration
			continue
		}

		// Register with this node
		g.logger.Infof("Registering gate with node %s", nodeAddr)
		g.wg.Add(1)
		go g.registerWithNode(ctx, nodeAddr, client)
	}
}

// registerWithNode registers this gate with a specific node and handles the message stream
func (g *Gateway) registerWithNode(ctx context.Context, nodeAddr string, client goverse_pb.GoverseClient) {
	defer g.wg.Done()

	stream, err := client.RegisterGate(ctx, &goverse_pb.RegisterGateRequest{
		GateAddr: g.advertiseAddress,
	})
	if err != nil {
		g.logger.Errorf("Failed to call RegisterGate on node %s: %v", nodeAddr, err)
		return
	}

	// Mark as registered with this specific stream
	g.registeredNodesMu.Lock()
	g.registeredNodes[nodeAddr] = stream
	g.registeredNodesMu.Unlock()
	g.logger.Infof("Successfully registered gate with node %s", nodeAddr)

	// Ensure cleanup on exit - only delete if we're still the registered stream
	defer func() {
		g.registeredNodesMu.Lock()
		// Only delete if the stored stream is still ours (not replaced by a newer connection)
		if g.registeredNodes[nodeAddr] == stream {
			delete(g.registeredNodes, nodeAddr)
			g.logger.Infof("Unregistered from node %s", nodeAddr)
		} else {
			g.logger.Infof("Not unregistering from node %s - a newer connection exists", nodeAddr)
		}
		g.registeredNodesMu.Unlock()
	}()

	// Use stream's context to detect when stream is closed
	streamCtx := stream.Context()

	// Start receiving messages from the stream
	// Use a goroutine to receive messages so we can select on context cancellation
	msgCh := make(chan *goverse_pb.GateMessage, 10)
	errCh := make(chan error, 1)
	recvDone := make(chan struct{})

	// Track the recv goroutine with WaitGroup
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		defer close(recvDone)
		for {
			msg, err := stream.Recv()
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			select {
			case msgCh <- msg:
			case <-streamCtx.Done():
				return
			case <-g.ctx.Done():
				return
			}
		}
	}()

	defer func() {
		// Wait for receive goroutine to finish with timeout
		select {
		case <-recvDone:
		case <-time.After(recvGoroutineCleanupTimeout):
			g.logger.Warnf("Recv goroutine for node %s did not finish in time", nodeAddr)
		}
	}()

	for {
		select {
		case <-g.ctx.Done():
			g.logger.Infof("Gateway context cancelled, stopping registration with node %s", nodeAddr)
			return
		case <-streamCtx.Done():
			g.logger.Infof("Stream context cancelled for node %s: %v", nodeAddr, streamCtx.Err())
			return
		case <-ctx.Done():
			g.logger.Infof("Parent context cancelled, stopping registration with node %s", nodeAddr)
			return
		case err := <-errCh:
			g.logger.Errorf("Stream from node %s closed: %v", nodeAddr, err)
			return
		case msg := <-msgCh:
			g.handleGateMessage(nodeAddr, msg)
		}
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
