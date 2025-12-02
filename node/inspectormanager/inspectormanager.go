package inspectormanager

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/util/clusterinfo"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"

	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

const (
	defaultHealthCheckInterval = 5 * time.Second

	// DefaultConnectionTimeout is the default timeout for gRPC connection establishment.
	// Per TIMEOUT_DESIGN.md, gRPC connection operations should have a 30 second timeout.
	DefaultConnectionTimeout = 30 * time.Second
)

// Mode represents the type of component using the InspectorManager
type Mode int

const (
	// ModeNode indicates the InspectorManager is used by a node
	ModeNode Mode = iota
	// ModeGate indicates the InspectorManager is used by a gate
	ModeGate
)

// InspectorManager manages the connection and communication with the Inspector service.
// It runs in its own goroutine for active connection management and reconnection.
// It can operate in node mode or gate mode, using the appropriate registration RPCs.
type InspectorManager struct {
	address             string // advertise address (node or gate)
	mode                Mode   // operating mode (node or gate)
	inspectorAddress    string
	healthCheckInterval time.Duration
	logger              *logger.Logger
	clusterInfoProvider clusterinfo.ClusterInfoProvider // consolidated provider for cluster info (preferred)

	mu        sync.RWMutex
	client    inspector_pb.InspectorServiceClient
	conn      *grpc.ClientConn
	connected bool
	started   bool                            // Track if Start() has been called
	objects   map[string]*inspector_pb.Object // Track objects for re-registration on reconnect (node mode only)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewInspectorManager creates a new InspectorManager instance for a node.
// By default, the inspector is disabled. Call SetInspectorAddress before Start() to enable it.
func NewInspectorManager(nodeAddress string) *InspectorManager {
	return &InspectorManager{
		address:             nodeAddress,
		mode:                ModeNode,
		inspectorAddress:    "", // disabled by default
		healthCheckInterval: defaultHealthCheckInterval,
		logger:              logger.NewLogger("InspectorManager"),
		objects:             make(map[string]*inspector_pb.Object),
	}
}

// NewGateInspectorManager creates a new InspectorManager instance for a gate.
// Gates don't track objects, so the objects map is not initialized.
// By default, the inspector is disabled. Call SetInspectorAddress before Start() to enable it.
func NewGateInspectorManager(gateAddress string) *InspectorManager {
	return &InspectorManager{
		address:             gateAddress,
		mode:                ModeGate,
		inspectorAddress:    "", // disabled by default
		healthCheckInterval: defaultHealthCheckInterval,
		logger:              logger.NewLogger("GateInspectorManager"),
	}
}

// SetInspectorAddress sets the inspector service address.
// This must be called before Start() to take effect.
// If address is empty, the inspector manager will be disabled.
func (im *InspectorManager) SetInspectorAddress(address string) {
	im.inspectorAddress = address
}

// SetHealthCheckInterval sets the health check interval for the InspectorManager.
// This must be called before Start() to take effect.
func (im *InspectorManager) SetHealthCheckInterval(interval time.Duration) {
	im.healthCheckInterval = interval
}

// IsEnabled returns true if the inspector manager has an inspector address configured.
// When disabled, Start() is a no-op and all notification methods do nothing.
func (im *InspectorManager) IsEnabled() bool {
	return im.inspectorAddress != ""
}

// SetConnectedNodesProvider sets the provider function for getting connected node addresses.
// This is used when registering with the inspector to report which nodes this component is connected to.
// Works for both node mode (node-to-node connections) and gate mode (gate-to-node connections).
// Must be called before Start() to take effect.
func (im *InspectorManager) SetClusterInfoProvider(provider clusterinfo.ClusterInfoProvider) {
	im.clusterInfoProvider = provider
}

// Start initializes the connection to the Inspector and starts background management.
// If inspectorAddress is empty, this method is a no-op and returns nil immediately.
func (im *InspectorManager) Start(ctx context.Context) error {
	im.mu.Lock()

	// If inspector address is not configured, do nothing
	if im.inspectorAddress == "" {
		im.mu.Unlock()
		return nil
	}

	// Make Start() idempotent - if already started, return immediately
	if im.started {
		im.mu.Unlock()
		return nil
	}
	im.started = true

	// Create a cancellable context for the manager
	im.ctx, im.cancel = context.WithCancel(ctx)

	// Attempt initial connection
	if err := im.connectLocked(); err != nil {
		im.logger.Warnf("Failed initial connection to inspector: %v (will retry in background)", err)
	}

	im.mu.Unlock()

	// Start background goroutine for health checks and reconnection
	// Started after releasing lock to allow the goroutine to acquire it
	im.wg.Add(1)
	go im.managementLoop()

	return nil
} // Stop gracefully shuts down the InspectorManager and unregisters from Inspector.
func (im *InspectorManager) Stop() error {
	im.mu.Lock()

	// Cancel the context to signal shutdown
	if im.cancel != nil {
		im.cancel()
	}
	im.mu.Unlock()

	// Wait for the management loop to finish
	im.wg.Wait()

	// Unregister from inspector before closing
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.connected && im.client != nil {
		if err := im.unregisterLocked(); err != nil {
			im.logger.Warnf("Failed to unregister from inspector: %v", err)
		}
	}

	// Close the connection
	if im.conn != nil {
		im.conn.Close()
		im.conn = nil
	}

	im.connected = false
	im.client = nil
	im.started = false // Reset so Start() can be called again

	return nil
}

// NotifyObjectAdded notifies the Inspector that a new object has been created.
// If the inspector is disabled (empty address), this is a no-op.
func (im *InspectorManager) NotifyObjectAdded(objectID, objectType string, shardID int) {
	// If inspector is disabled, skip all work
	if im.inspectorAddress == "" {
		return
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	// Store object info for re-registration on reconnect
	im.objects[objectID] = &inspector_pb.Object{
		Id:      objectID,
		Class:   objectType,
		ShardId: int32(shardID),
	}

	// If connected, send the notification immediately
	if im.connected && im.client != nil {
		im.addOrUpdateObjectLocked(objectID, objectType, shardID)
	}
}

// NotifyObjectRemoved notifies the Inspector that an object has been removed.
// If the inspector is disabled (empty address), this is a no-op.
func (im *InspectorManager) NotifyObjectRemoved(objectID string) {
	// If inspector is disabled, skip all work
	if im.inspectorAddress == "" {
		return
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	// Remove from local tracking
	delete(im.objects, objectID)

	// If connected, send the removal notification immediately
	if im.connected && im.client != nil {
		im.removeObjectLocked(objectID)
	}
}

// connectLocked attempts to connect to the Inspector service.
// Must be called with im.mu held.
func (im *InspectorManager) connectLocked() error {
	// Configure connection parameters with timeout per TIMEOUT_DESIGN.md
	connectParams := grpc.ConnectParams{
		Backoff:           backoff.DefaultConfig,
		MinConnectTimeout: DefaultConnectionTimeout,
	}

	conn, err := grpc.NewClient(im.inspectorAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(connectParams),
	)
	if err != nil {
		return err
	}

	client := inspector_pb.NewInspectorServiceClient(conn)

	// Test the connection with a ping
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Ping(ctx, &inspector_pb.Empty{})
	if err != nil {
		conn.Close()
		return err
	}

	im.conn = conn
	im.client = client
	im.connected = true

	im.logger.Infof("Connected to inspector service at %s", im.inspectorAddress)

	// Register based on mode
	if err := im.registerLocked(); err != nil {
		im.logger.Warnf("Failed to register with inspector: %v", err)
		// Don't fail the connection, just log the warning
	}

	return nil
}

// registerLocked registers this component with the Inspector based on mode.
// Must be called with im.mu held.
func (im *InspectorManager) registerLocked() error {
	if im.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch im.mode {
	case ModeNode:
		objects := make([]*inspector_pb.Object, 0, len(im.objects))
		for _, obj := range im.objects {
			objects = append(objects, obj)
		}

		// Get connected nodes and registered gates from cluster info provider
		var connectedNodes []string
		var registeredGates []string
		if im.clusterInfoProvider != nil {
			connectedNodes = im.clusterInfoProvider.GetConnectedNodes()
			registeredGates = im.clusterInfoProvider.GetRegisteredGates()
		}

		registerReq := &inspector_pb.RegisterNodeRequest{
			AdvertiseAddress: im.address,
			Objects:          objects,
			ConnectedNodes:   connectedNodes,
			RegisteredGates:  registeredGates,
		}

		_, err := im.client.RegisterNode(ctx, registerReq)
		if err != nil {
			return err
		}

		im.logger.Infof("Successfully registered node %s with inspector (%d objects, %d connected nodes, %d registered gates)", im.address, len(objects), len(connectedNodes), len(registeredGates))

	case ModeGate:
		// Get connected nodes from cluster info provider
		var connectedNodes []string
		if im.clusterInfoProvider != nil {
			connectedNodes = im.clusterInfoProvider.GetConnectedNodes()
		}

		registerReq := &inspector_pb.RegisterGateRequest{
			AdvertiseAddress: im.address,
			ConnectedNodes:   connectedNodes,
		}

		_, err := im.client.RegisterGate(ctx, registerReq)
		if err != nil {
			return err
		}

		im.logger.Infof("Successfully registered gate %s with inspector (%d connected nodes)", im.address, len(connectedNodes))
	}

	return nil
}

// unregisterLocked unregisters this component from the Inspector based on mode.
// Must be called with im.mu held.
func (im *InspectorManager) unregisterLocked() error {
	if im.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch im.mode {
	case ModeNode:
		unregisterReq := &inspector_pb.UnregisterNodeRequest{
			AdvertiseAddress: im.address,
		}

		_, err := im.client.UnregisterNode(ctx, unregisterReq)
		if err != nil {
			return err
		}

		im.logger.Infof("Successfully unregistered node %s from inspector", im.address)

	case ModeGate:
		unregisterReq := &inspector_pb.UnregisterGateRequest{
			AdvertiseAddress: im.address,
		}

		_, err := im.client.UnregisterGate(ctx, unregisterReq)
		if err != nil {
			return err
		}

		im.logger.Infof("Successfully unregistered gate %s from inspector", im.address)
	}

	return nil
}

// addOrUpdateObjectLocked sends an AddOrUpdateObject RPC to the Inspector.
// Must be called with im.mu held.
func (im *InspectorManager) addOrUpdateObjectLocked(objectID, objectType string, shardID int) {
	if im.client == nil {
		return
	}

	req := &inspector_pb.AddOrUpdateObjectRequest{
		Object: &inspector_pb.Object{
			Id:      objectID,
			Class:   objectType,
			ShardId: int32(shardID),
		},
		NodeAddress: im.address,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := im.client.AddOrUpdateObject(ctx, req)
	if err != nil {
		im.logger.Warnf("Failed to register object %s with inspector: %v", objectID, err)
		return
	}

	im.logger.Infof("Registered object %s with inspector", objectID)
}

// UpdateConnectedNodes sends an UpdateConnectedNodes RPC to the Inspector.
// This is called when the component's connections change.
// The same RPC is used for both nodes and gates - the inspector determines the type based on the address.
// If the inspector is disabled (empty address), this is a no-op.
func (im *InspectorManager) UpdateConnectedNodes() {
	// If inspector is disabled, skip all work
	if im.inspectorAddress == "" {
		return
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	if im.client == nil {
		return
	}

	// Get current connected nodes from cluster info provider
	var connectedNodes []string
	if im.clusterInfoProvider != nil {
		connectedNodes = im.clusterInfoProvider.GetConnectedNodes()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: im.address,
		ConnectedNodes:   connectedNodes,
	}

	_, err := im.client.UpdateConnectedNodes(ctx, req)
	if err != nil {
		im.logger.Warnf("Failed to update connected nodes with inspector: %v", err)
		return
	}

	im.logger.Debugf("Updated connected nodes with inspector (%d nodes)", len(connectedNodes))
}

// UpdateRegisteredGates sends an UpdateRegisteredGates RPC to the Inspector.
// This is called when the node's registered gates change.
// This method is only applicable in node mode.
// If the inspector is disabled (empty address), this is a no-op.
func (im *InspectorManager) UpdateRegisteredGates() {
	// If inspector is disabled, skip all work
	if im.inspectorAddress == "" {
		return
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	if im.client == nil {
		return
	}

	// Only nodes can have registered gates
	if im.mode != ModeNode {
		return
	}

	// Get current registered gates from cluster info provider
	var registeredGates []string
	if im.clusterInfoProvider != nil {
		registeredGates = im.clusterInfoProvider.GetRegisteredGates()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &inspector_pb.UpdateRegisteredGatesRequest{
		AdvertiseAddress: im.address,
		RegisteredGates:  registeredGates,
	}

	_, err := im.client.UpdateRegisteredGates(ctx, req)
	if err != nil {
		im.logger.Warnf("Failed to update registered gates with inspector: %v", err)
		return
	}

	im.logger.Debugf("Updated registered gates with inspector (%d gates)", len(registeredGates))
}

// removeObjectLocked sends a RemoveObject RPC to the Inspector.
// Must be called with im.mu held.
func (im *InspectorManager) removeObjectLocked(objectID string) {
	if im.client == nil {
		return
	}

	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    objectID,
		NodeAddress: im.address,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := im.client.RemoveObject(ctx, req)
	if err != nil {
		im.logger.Warnf("Failed to remove object %s from inspector: %v", objectID, err)
		return
	}

	im.logger.Infof("Removed object %s from inspector", objectID)
}

// managementLoop runs in the background to handle health checks and reconnection.
func (im *InspectorManager) managementLoop() {
	defer im.wg.Done()

	ticker := time.NewTicker(im.healthCheckInterval)
	defer ticker.Stop()

	// Get context with proper synchronization
	im.mu.RLock()
	ctx := im.ctx
	im.mu.RUnlock()

	for {
		select {
		case <-ctx.Done():
			// Shutdown signal received
			return

		case <-ticker.C:
			// Perform health check
			im.healthCheck()
		}
	}
}

// healthCheck verifies the connection is alive and attempts reconnection if needed.
func (im *InspectorManager) healthCheck() {
	im.mu.Lock()
	defer im.mu.Unlock()

	// If not connected, try to reconnect
	if !im.connected {
		im.logger.Infof("Attempting to reconnect to inspector...")
		if err := im.connectLocked(); err != nil {
			im.logger.Warnf("Reconnection failed: %v (will retry)", err)
		}
		return
	}

	// If connected, verify with a ping
	if im.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := im.client.Ping(ctx, &inspector_pb.Empty{})
		if err != nil {
			im.logger.Warnf("Inspector health check failed: %v (will attempt reconnect)", err)

			// Connection lost, close and mark as disconnected
			if im.conn != nil {
				im.conn.Close()
				im.conn = nil
			}
			im.client = nil
			im.connected = false

			// Try immediate reconnection
			if err := im.connectLocked(); err != nil {
				im.logger.Warnf("Immediate reconnection failed: %v (will retry)", err)
			}
		}
	}
}

// GetContextForTesting returns the context for testing purposes.
func (im *InspectorManager) GetContextForTesting() context.Context {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.ctx
}

// IsObjectTracked returns true if the object is being tracked by the inspector manager.
func (im *InspectorManager) IsObjectTracked(objectID string) bool {
	im.mu.RLock()
	defer im.mu.RUnlock()
	_, exists := im.objects[objectID]
	return exists
}

// ObjectCount returns the number of objects being tracked.
func (im *InspectorManager) ObjectCount() int {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return len(im.objects)
}
