package inspectormanager

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

const (
	defaultInspectorAddress    = "localhost:8081"
	defaultHealthCheckInterval = 5 * time.Second
)

// InspectorManager manages the connection and communication with the Inspector service.
// It runs in its own goroutine for active connection management and reconnection.
type InspectorManager struct {
	nodeAddress         string
	inspectorAddress    string
	healthCheckInterval time.Duration
	logger              *logger.Logger

	mu        sync.RWMutex
	client    inspector_pb.InspectorServiceClient
	conn      *grpc.ClientConn
	connected bool
	objects   map[string]*inspector_pb.Object // Track objects for re-registration on reconnect

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewInspectorManager creates a new InspectorManager instance.
func NewInspectorManager(nodeAddress string) *InspectorManager {
	return &InspectorManager{
		nodeAddress:         nodeAddress,
		inspectorAddress:    defaultInspectorAddress,
		healthCheckInterval: defaultHealthCheckInterval,
		logger:              logger.NewLogger("InspectorManager"),
		objects:             make(map[string]*inspector_pb.Object),
	}
}

// SetHealthCheckInterval sets the health check interval for the InspectorManager.
// This must be called before Start() to take effect.
func (im *InspectorManager) SetHealthCheckInterval(interval time.Duration) {
	im.healthCheckInterval = interval
}

// Start initializes the connection to the Inspector and starts background management.
func (im *InspectorManager) Start(ctx context.Context) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	// Create a cancellable context for the manager
	im.ctx, im.cancel = context.WithCancel(ctx)

	// Attempt initial connection
	if err := im.connectLocked(); err != nil {
		im.logger.Warnf("Failed initial connection to inspector: %v (will retry in background)", err)
	}

	// Start background goroutine for health checks and reconnection
	im.wg.Add(1)
	go im.managementLoop()

	return nil
}

// Stop gracefully shuts down the InspectorManager and unregisters from Inspector.
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
		if err := im.unregisterNodeLocked(); err != nil {
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

	return nil
}

// NotifyObjectAdded notifies the Inspector that a new object has been created.
func (im *InspectorManager) NotifyObjectAdded(objectID, objectType string, shardID int) {
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
func (im *InspectorManager) NotifyObjectRemoved(objectID string) {
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
	conn, err := grpc.NewClient(im.inspectorAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
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

	// Register the node with all current objects
	if err := im.registerNodeLocked(); err != nil {
		im.logger.Warnf("Failed to register node with inspector: %v", err)
		// Don't fail the connection, just log the warning
	}

	return nil
}

// registerNodeLocked registers this node with the Inspector.
// Must be called with im.mu held.
func (im *InspectorManager) registerNodeLocked() error {
	if im.client == nil {
		return nil
	}

	objects := make([]*inspector_pb.Object, 0, len(im.objects))
	for _, obj := range im.objects {
		objects = append(objects, obj)
	}

	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: im.nodeAddress,
		Objects:          objects,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := im.client.RegisterNode(ctx, registerReq)
	if err != nil {
		return err
	}

	im.logger.Infof("Successfully registered node %s with inspector (%d objects)", im.nodeAddress, len(objects))
	return nil
}

// unregisterNodeLocked unregisters this node from the Inspector.
// Must be called with im.mu held.
func (im *InspectorManager) unregisterNodeLocked() error {
	if im.client == nil {
		return nil
	}

	unregisterReq := &inspector_pb.UnregisterNodeRequest{
		AdvertiseAddress: im.nodeAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := im.client.UnregisterNode(ctx, unregisterReq)
	if err != nil {
		return err
	}

	im.logger.Infof("Successfully unregistered node %s from inspector", im.nodeAddress)
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
		NodeAddress: im.nodeAddress,
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

// removeObjectLocked sends a RemoveObject RPC to the Inspector.
// Must be called with im.mu held.
func (im *InspectorManager) removeObjectLocked(objectID string) {
	if im.client == nil {
		return
	}

	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    objectID,
		NodeAddress: im.nodeAddress,
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

	for {
		select {
		case <-im.ctx.Done():
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
