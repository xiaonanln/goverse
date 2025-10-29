package cluster

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
	running       bool
	cluster       *Cluster
}

// NewNodeConnections creates a new NodeConnections manager
func NewNodeConnections(cluster *Cluster) *NodeConnections {
	return &NodeConnections{
		connections: make(map[string]*NodeConnection),
		logger:      logger.NewLogger("NodeConnections"),
		cluster:     cluster,
	}
}

// Start begins managing connections to cluster nodes
// It watches for node changes and maintains connections accordingly
func (nc *NodeConnections) Start(ctx context.Context) error {
	if nc.running {
		nc.logger.Warnf("NodeConnections already running")
		return nil
	}

	nc.ctx, nc.cancel = context.WithCancel(ctx)
	nc.running = true

	// Connect to all existing nodes
	nodes := nc.cluster.GetNodes()
	for _, nodeAddr := range nodes {
		// Don't connect to ourselves
		if nc.cluster.thisNode != nil && nodeAddr == nc.cluster.thisNode.GetAdvertiseAddress() {
			continue
		}
		if err := nc.connectToNode(nodeAddr); err != nil {
			nc.logger.Errorf("Failed to connect to node %s: %v", nodeAddr, err)
		}
	}

	// Start watching for node changes
	go nc.watchNodeChanges()
	nc.logger.Infof("Started NodeConnections manager with %d initial connections", len(nc.connections))

	return nil
}

// Stop stops the NodeConnections manager and closes all connections
func (nc *NodeConnections) Stop() {
	if !nc.running {
		return
	}

	if nc.cancel != nil {
		nc.cancel()
	}

	nc.running = false

	// Close all connections
	nc.connectionsMu.Lock()
	for addr, conn := range nc.connections {
		if err := nc.closeConnection(conn); err != nil {
			nc.logger.Errorf("Error closing connection to %s: %v", addr, err)
		}
		delete(nc.connections, addr)
	}
	nc.connectionsMu.Unlock()

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

// watchNodeChanges watches for changes in the cluster nodes and updates connections
func (nc *NodeConnections) watchNodeChanges() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	previousNodes := make(map[string]bool)
	// Initialize with current nodes
	for _, nodeAddr := range nc.cluster.GetNodes() {
		previousNodes[nodeAddr] = true
	}

	for {
		select {
		case <-nc.ctx.Done():
			nc.logger.Debugf("Node watch loop stopped")
			return
		case <-ticker.C:
			nc.handleNodeChanges(previousNodes)
		}
	}
}

// handleNodeChanges processes node additions and removals
func (nc *NodeConnections) handleNodeChanges(previousNodes map[string]bool) {
	currentNodes := nc.cluster.GetNodes()
	currentNodesMap := make(map[string]bool)

	for _, nodeAddr := range currentNodes {
		currentNodesMap[nodeAddr] = true

		// Skip our own node
		if nc.cluster.thisNode != nil && nodeAddr == nc.cluster.thisNode.GetAdvertiseAddress() {
			continue
		}

		// New node detected - connect to it
		if !previousNodes[nodeAddr] {
			nc.logger.Infof("New node detected: %s", nodeAddr)
			if err := nc.connectToNode(nodeAddr); err != nil {
				nc.logger.Errorf("Failed to connect to new node %s: %v", nodeAddr, err)
			}
		}
	}

	// Check for removed nodes
	for nodeAddr := range previousNodes {
		if !currentNodesMap[nodeAddr] {
			nc.logger.Infof("Node removed: %s", nodeAddr)
			if err := nc.disconnectFromNode(nodeAddr); err != nil {
				nc.logger.Errorf("Failed to disconnect from removed node %s: %v", nodeAddr, err)
			}
		}
	}

	// Update previousNodes for next iteration
	for addr := range previousNodes {
		delete(previousNodes, addr)
	}
	for addr := range currentNodesMap {
		previousNodes[addr] = true
	}
}

// NumConnections returns the number of active connections
func (nc *NodeConnections) NumConnections() int {
	nc.connectionsMu.RLock()
	defer nc.connectionsMu.RUnlock()
	return len(nc.connections)
}
