package nodeconnections

import (
	"context"
	"fmt"
	"sync"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	initialRetryDelay = 1 * time.Second
	maxRetryDelay     = 30 * time.Second
)

// NodeConnection represents a gRPC connection to a single node
type NodeConnection struct {
	address string
	conn    *grpc.ClientConn
	client  goverse_pb.GoverseClient
}

// NodeConnections manages gRPC connections to all cluster nodes
type NodeConnections struct {
	connections   map[string]*NodeConnection // map of node address to connection
	connectionsMu sync.RWMutex
	logger        *logger.Logger
	ctx           context.Context
	cancel        context.CancelFunc

	// Retry state tracking
	retryingNodes   map[string]context.CancelFunc // map of node address to retry cancel func
	retryingNodesMu sync.Mutex
}

// New creates a new NodeConnections manager
func New() *NodeConnections {
	return &NodeConnections{
		connections:   make(map[string]*NodeConnection),
		retryingNodes: make(map[string]context.CancelFunc),
		logger:        logger.NewLogger("NodeConnections"),
	}
}

// Start begins managing connections to cluster nodes
func (nc *NodeConnections) Start(ctx context.Context) error {
	nc.connectionsMu.Lock()
	defer nc.connectionsMu.Unlock()

	nc.ctx, nc.cancel = context.WithCancel(ctx)

	nc.logger.Infof("Started NodeConnections manager")
	return nil
}

// Stop stops the NodeConnections manager and closes all connections
func (nc *NodeConnections) Stop() {
	nc.connectionsMu.Lock()
	defer nc.connectionsMu.Unlock()

	if nc.cancel != nil {
		nc.cancel()
	}

	// Cancel all retry goroutines
	nc.retryingNodesMu.Lock()
	for addr, cancel := range nc.retryingNodes {
		nc.logger.Debugf("Cancelling retry goroutine for node %s", addr)
		cancel()
		// Delete immediately to avoid goroutines trying to delete from map in defer
		delete(nc.retryingNodes, addr)
	}
	nc.retryingNodesMu.Unlock()

	// Close all connections
	for addr, conn := range nc.connections {
		if err := nc.closeConnection(conn); err != nil {
			nc.logger.Errorf("Error closing connection to %s: %v", addr, err)
		}
		delete(nc.connections, addr)
	}

	nc.logger.Infof("Stopped NodeConnections manager")
}

// connectToNode establishes a gRPC connection to a specific node
func (nc *NodeConnections) connectToNode(nodeAddr string) error {
	nc.connectionsMu.Lock()
	defer nc.connectionsMu.Unlock()

	// Check if already connected
	if _, exists := nc.connections[nodeAddr]; exists {
		nc.logger.Debugf("Already connected to node %s", nodeAddr)
		return nil
	}

	nc.logger.Infof("Connecting to node %s", nodeAddr)

	// Establish gRPC connection
	conn, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err)
	}

	// Create Goverse client
	client := goverse_pb.NewGoverseClient(conn)

	// Store the connection
	nc.connections[nodeAddr] = &NodeConnection{
		address: nodeAddr,
		conn:    conn,
		client:  client,
	}

	nc.logger.Infof("Successfully connected to node %s", nodeAddr)
	return nil
}

// retryConnection retries connection to a node with exponential backoff
func (nc *NodeConnections) retryConnection(nodeAddr string) {
	// Create a cancellable context for this retry goroutine
	retryCtx, retryCancel := context.WithCancel(nc.ctx)

	// Ensure cleanup on exit
	defer func() {
		retryCancel() // Ensure context is cancelled
		nc.retryingNodesMu.Lock()
		delete(nc.retryingNodes, nodeAddr)
		nc.retryingNodesMu.Unlock()
	}()

	// Check if parent context is already cancelled before starting
	select {
	case <-retryCtx.Done():
		nc.logger.Infof("Retry loop cancelled before start for node %s", nodeAddr)
		return
	default:
	}

	// Register the cancel function
	nc.retryingNodesMu.Lock()
	nc.retryingNodes[nodeAddr] = retryCancel
	nc.retryingNodesMu.Unlock()

	retryDelay := initialRetryDelay

	for {
		// Check if context is cancelled
		select {
		case <-retryCtx.Done():
			nc.logger.Infof("Retry loop cancelled for node %s", nodeAddr)
			return
		default:
		}

		// Wait before retrying
		nc.logger.Infof("Will retry connecting to node %s in %v", nodeAddr, retryDelay)
		select {
		case <-time.After(retryDelay):
			// Continue with retry
		case <-retryCtx.Done():
			nc.logger.Infof("Retry loop cancelled for node %s during wait", nodeAddr)
			return
		}

		// Attempt to connect
		err := nc.connectToNode(nodeAddr)
		if err == nil {
			// Connection successful, stop retrying
			nc.logger.Infof("Successfully connected to node %s on retry", nodeAddr)
			return
		}

		nc.logger.Warnf("Failed to connect to node %s: %v, will retry with backoff", nodeAddr, err)

		// Exponential backoff
		retryDelay = retryDelay * 2
		if retryDelay > maxRetryDelay {
			retryDelay = maxRetryDelay
		}
	}
}

// disconnectFromNode closes the connection to a specific node
func (nc *NodeConnections) disconnectFromNode(nodeAddr string) error {
	nc.connectionsMu.Lock()
	defer nc.connectionsMu.Unlock()

	conn, exists := nc.connections[nodeAddr]
	if !exists {
		nc.logger.Debugf("No connection to node %s to disconnect", nodeAddr)
		return nil
	}

	nc.logger.Infof("Disconnecting from node %s", nodeAddr)

	if err := nc.closeConnection(conn); err != nil {
		nc.logger.Errorf("Error closing connection to %s: %v", nodeAddr, err)
	}

	delete(nc.connections, nodeAddr)
	nc.logger.Infof("Disconnected from node %s", nodeAddr)
	return nil
}

// closeConnection closes a single NodeConnection
func (nc *NodeConnections) closeConnection(conn *NodeConnection) error {
	if conn.conn != nil {
		return conn.conn.Close()
	}
	return nil
}

// GetConnection returns the gRPC client for a specific node
func (nc *NodeConnections) GetConnection(nodeAddr string) (goverse_pb.GoverseClient, error) {
	nc.connectionsMu.RLock()
	defer nc.connectionsMu.RUnlock()

	conn, exists := nc.connections[nodeAddr]
	if !exists {
		return nil, fmt.Errorf("no connection to node %s", nodeAddr)
	}

	return conn.client, nil
}

// GetAllConnections returns a map of all active connections
func (nc *NodeConnections) GetAllConnections() map[string]goverse_pb.GoverseClient {
	nc.connectionsMu.RLock()
	defer nc.connectionsMu.RUnlock()

	result := make(map[string]goverse_pb.GoverseClient, len(nc.connections))
	for addr, conn := range nc.connections {
		result[addr] = conn.client
	}

	return result
}

// SetNodes updates the list of nodes to maintain connections to
// It will connect to new nodes and disconnect from removed nodes
// The caller should exclude this node's address from the list
func (nc *NodeConnections) SetNodes(nodes []string) {
	// Build a map of desired nodes
	desiredNodes := make(map[string]bool)
	for _, nodeAddr := range nodes {
		desiredNodes[nodeAddr] = true
	}

	nc.connectionsMu.RLock()
	currentNodes := make(map[string]bool)
	for addr := range nc.connections {
		currentNodes[addr] = true
	}
	nc.connectionsMu.RUnlock()

	// Connect to new nodes
	for nodeAddr := range desiredNodes {
		if !currentNodes[nodeAddr] {
			nc.logger.Infof("Connecting to new node: %s", nodeAddr)
			if err := nc.connectToNode(nodeAddr); err != nil {
				nc.logger.Errorf("Failed to connect to node %s: %v, starting retry with exponential backoff", nodeAddr, err)

				// Check if already retrying this node
				nc.retryingNodesMu.Lock()
				_, alreadyRetrying := nc.retryingNodes[nodeAddr]
				nc.retryingNodesMu.Unlock()

				if !alreadyRetrying {
					// Start retry goroutine with exponential backoff
					go nc.retryConnection(nodeAddr)
				}
			}
		}
	}

	// Disconnect from removed nodes
	for nodeAddr := range currentNodes {
		if !desiredNodes[nodeAddr] {
			nc.logger.Infof("Disconnecting from removed node: %s", nodeAddr)

			// Cancel any ongoing retry for this node
			nc.retryingNodesMu.Lock()
			if cancel, exists := nc.retryingNodes[nodeAddr]; exists {
				nc.logger.Debugf("Cancelling retry goroutine for removed node %s", nodeAddr)
				cancel()
				delete(nc.retryingNodes, nodeAddr)
			}
			nc.retryingNodesMu.Unlock()

			if err := nc.disconnectFromNode(nodeAddr); err != nil {
				nc.logger.Errorf("Failed to disconnect from node %s: %v", nodeAddr, err)
			}
		}
	}
}

// NumConnections returns the number of active connections
func (nc *NodeConnections) NumConnections() int {
	nc.connectionsMu.RLock()
	defer nc.connectionsMu.RUnlock()
	return len(nc.connections)
}
