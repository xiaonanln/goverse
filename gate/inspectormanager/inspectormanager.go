package inspectormanager

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"

	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

const (
	defaultInspectorAddress    = "localhost:8081"
	defaultHealthCheckInterval = 5 * time.Second

	// DefaultConnectionTimeout is the default timeout for gRPC connection establishment.
	// Per TIMEOUT_DESIGN.md, gRPC connection operations should have a 30 second timeout.
	DefaultConnectionTimeout = 30 * time.Second
)

// GateInspectorManager manages the connection and communication with the Inspector service for gates.
// It runs in its own goroutine for active connection management and reconnection.
type GateInspectorManager struct {
	gateAddress         string
	inspectorAddress    string
	healthCheckInterval time.Duration
	logger              *logger.Logger

	mu        sync.RWMutex
	client    inspector_pb.InspectorServiceClient
	conn      *grpc.ClientConn
	connected bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewGateInspectorManager creates a new GateInspectorManager instance.
func NewGateInspectorManager(gateAddress string) *GateInspectorManager {
	return &GateInspectorManager{
		gateAddress:         gateAddress,
		inspectorAddress:    defaultInspectorAddress,
		healthCheckInterval: defaultHealthCheckInterval,
		logger:              logger.NewLogger("GateInspectorManager"),
	}
}

// SetHealthCheckInterval sets the health check interval for the GateInspectorManager.
// This must be called before Start() to take effect.
func (gim *GateInspectorManager) SetHealthCheckInterval(interval time.Duration) {
	gim.healthCheckInterval = interval
}

// Start initializes the connection to the Inspector and starts background management.
func (gim *GateInspectorManager) Start(ctx context.Context) error {
	gim.mu.Lock()
	defer gim.mu.Unlock()

	// Create a cancellable context for the manager
	gim.ctx, gim.cancel = context.WithCancel(ctx)

	// Attempt initial connection
	if err := gim.connectLocked(); err != nil {
		gim.logger.Warnf("Failed initial connection to inspector: %v (will retry in background)", err)
	}

	// Start background goroutine for health checks and reconnection
	gim.wg.Add(1)
	go gim.managementLoop()

	return nil
}

// Stop gracefully shuts down the GateInspectorManager and unregisters from Inspector.
func (gim *GateInspectorManager) Stop() error {
	gim.mu.Lock()

	// Cancel the context to signal shutdown
	if gim.cancel != nil {
		gim.cancel()
	}
	gim.mu.Unlock()

	// Wait for the management loop to finish
	gim.wg.Wait()

	// Unregister from inspector before closing
	gim.mu.Lock()
	defer gim.mu.Unlock()

	if gim.connected && gim.client != nil {
		if err := gim.unregisterGateLocked(); err != nil {
			gim.logger.Warnf("Failed to unregister from inspector: %v", err)
		}
	}

	// Close the connection
	if gim.conn != nil {
		gim.conn.Close()
		gim.conn = nil
	}

	gim.connected = false
	gim.client = nil

	return nil
}

// connectLocked attempts to connect to the Inspector service.
// Must be called with gim.mu held.
func (gim *GateInspectorManager) connectLocked() error {
	// Configure connection parameters with timeout per TIMEOUT_DESIGN.md
	connectParams := grpc.ConnectParams{
		Backoff:           backoff.DefaultConfig,
		MinConnectTimeout: DefaultConnectionTimeout,
	}

	conn, err := grpc.NewClient(gim.inspectorAddress,
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

	gim.conn = conn
	gim.client = client
	gim.connected = true

	gim.logger.Infof("Connected to inspector service at %s", gim.inspectorAddress)

	// Register the gate with inspector
	if err := gim.registerGateLocked(); err != nil {
		gim.logger.Warnf("Failed to register gate with inspector: %v", err)
		// Don't fail the connection, just log the warning
	}

	return nil
}

// registerGateLocked registers this gate with the Inspector.
// Must be called with gim.mu held.
func (gim *GateInspectorManager) registerGateLocked() error {
	if gim.client == nil {
		return nil
	}

	registerReq := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: gim.gateAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gim.client.RegisterGate(ctx, registerReq)
	if err != nil {
		return err
	}

	gim.logger.Infof("Successfully registered gate %s with inspector", gim.gateAddress)
	return nil
}

// unregisterGateLocked unregisters this gate from the Inspector.
// Must be called with gim.mu held.
func (gim *GateInspectorManager) unregisterGateLocked() error {
	if gim.client == nil {
		return nil
	}

	unregisterReq := &inspector_pb.UnregisterGateRequest{
		AdvertiseAddress: gim.gateAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gim.client.UnregisterGate(ctx, unregisterReq)
	if err != nil {
		return err
	}

	gim.logger.Infof("Successfully unregistered gate %s from inspector", gim.gateAddress)
	return nil
}

// managementLoop runs in the background to handle health checks and reconnection.
func (gim *GateInspectorManager) managementLoop() {
	defer gim.wg.Done()

	ticker := time.NewTicker(gim.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gim.ctx.Done():
			// Shutdown signal received
			return

		case <-ticker.C:
			// Perform health check
			gim.healthCheck()
		}
	}
}

// healthCheck verifies the connection is alive and attempts reconnection if needed.
func (gim *GateInspectorManager) healthCheck() {
	gim.mu.Lock()
	defer gim.mu.Unlock()

	// If not connected, try to reconnect
	if !gim.connected {
		gim.logger.Infof("Attempting to reconnect to inspector...")
		if err := gim.connectLocked(); err != nil {
			gim.logger.Warnf("Reconnection failed: %v (will retry)", err)
		}
		return
	}

	// If connected, verify with a ping
	if gim.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := gim.client.Ping(ctx, &inspector_pb.Empty{})
		if err != nil {
			gim.logger.Warnf("Inspector health check failed: %v (will attempt reconnect)", err)

			// Connection lost, close and mark as disconnected
			if gim.conn != nil {
				gim.conn.Close()
				gim.conn = nil
			}
			gim.client = nil
			gim.connected = false

			// Try immediate reconnection
			if err := gim.connectLocked(); err != nil {
				gim.logger.Warnf("Immediate reconnection failed: %v (will retry)", err)
			}
		}
	}
}

// GetContextForTesting returns the context for testing purposes.
func (gim *GateInspectorManager) GetContextForTesting() context.Context {
	gim.mu.RLock()
	defer gim.mu.RUnlock()
	return gim.ctx
}
