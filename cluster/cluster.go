package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/consensusmanager"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/nodeconnections"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/uniqueid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	thisCluster *Cluster
)

const (
	// ShardMappingCheckInterval is how often to check if shard mapping needs updating
	ShardMappingCheckInterval = 5 * time.Second
	// DefaultNodeStabilityDuration is how long the node list must be stable before updating shard mapping
	DefaultNodeStabilityDuration = 10 * time.Second
)

type Cluster struct {
	thisNode                  *node.Node
	etcdManager               *etcdmanager.EtcdManager
	consensusManager          *consensusmanager.ConsensusManager
	nodeConnections           *nodeconnections.NodeConnections
	shardLock                 *shardlock.ShardLock
	logger                    *logger.Logger
	etcdAddress               string        // etcd server address (e.g., "localhost:2379")
	etcdPrefix                string        // etcd key prefix for this cluster
	minQuorum                 int           // minimal number of nodes required for cluster to be considered stable
	nodeStabilityDuration     time.Duration // duration to wait for cluster state to stabilize before updating shard mapping
	shardMappingCheckInterval time.Duration // how often to check if shard mapping needs updating
	shardMappingCtx           context.Context
	shardMappingCancel        context.CancelFunc
	shardMappingRunning       bool
	clusterReadyChan          chan bool
	clusterReadyOnce          sync.Once
}

func SetThis(c *Cluster) {
	thisCluster = c
}

func This() *Cluster {
	return thisCluster
}

// createAndConnectEtcdManager creates an etcd manager and connects to etcd
// This is a shared helper for cluster initialization
func createAndConnectEtcdManager(etcdAddress string, etcdPrefix string) (*etcdmanager.EtcdManager, error) {
	// Create etcd manager
	mgr, err := etcdmanager.NewEtcdManager(etcdAddress, etcdPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd manager: %w", err)
	}

	// Connect to etcd
	if err := mgr.Connect(); err != nil {
		// Clean up the manager if connection fails
		_ = mgr.Close()
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	return mgr, nil
}

// NewCluster creates a new cluster instance with the given configuration.
// It automatically connects to etcd and initializes the etcd manager and consensus manager.
// This function assigns the created cluster to the singleton and should be called once during application initialization.
// If the cluster singleton is already initialized, this function will return an error.
// This function is thread-safe.
func NewCluster(cfg Config, thisNode *node.Node) (*Cluster, error) {
	// Create a new cluster instance with its own ShardLock to ensure per-cluster isolation
	c := &Cluster{
		thisNode:                  thisNode,
		logger:                    logger.NewLogger("Cluster"),
		clusterReadyChan:          make(chan bool),
		etcdAddress:               cfg.EtcdAddress,
		etcdPrefix:                cfg.EtcdPrefix,
		minQuorum:                 cfg.MinQuorum,
		nodeStabilityDuration:     cfg.NodeStabilityDuration,
		shardMappingCheckInterval: cfg.ShardMappingCheckInterval,
		nodeConnections:           nodeconnections.New(),
		shardLock:                 shardlock.NewShardLock(),
	}

	// Set the cluster's ShardLock on the node immediately for per-cluster isolation
	if thisNode != nil {
		thisNode.SetShardLock(c.shardLock)
	}

	mgr, err := createAndConnectEtcdManager(cfg.EtcdAddress, cfg.EtcdPrefix)
	if err != nil {
		return nil, err
	}

	c.etcdManager = mgr
	c.consensusManager = consensusmanager.NewConsensusManager(mgr, c.shardLock)

	// Set minQuorum on consensus manager if specified
	if cfg.MinQuorum > 0 {
		c.consensusManager.SetMinQuorum(cfg.MinQuorum)
	}

	return c, nil
}

// newClusterForTesting creates a new cluster instance for testing with an initialized logger
// Uses test-appropriate configuration values
func newClusterForTesting(node *node.Node, name string) *Cluster {
	// Use test-appropriate durations (faster than production defaults)
	return &Cluster{
		thisNode:                  node,
		logger:                    logger.NewLogger(name),
		clusterReadyChan:          make(chan bool),
		nodeConnections:           nodeconnections.New(),
		minQuorum:                 1,
		nodeStabilityDuration:     3 * time.Second,
		shardMappingCheckInterval: 1 * time.Second,
		shardLock:                 shardlock.NewShardLock(),
	}
}

// newClusterWithEtcdForTesting creates a new cluster instance for testing and initializes it with etcd
// Uses test-appropriate configuration values (shorter durations)
func newClusterWithEtcdForTesting(name string, node *node.Node, etcdAddress string, etcdPrefix string) (*Cluster, error) {
	// Create config with test values
	cfg := Config{
		EtcdAddress:               etcdAddress,
		EtcdPrefix:                etcdPrefix,
		MinQuorum:                 1,
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}

	c, err := NewCluster(cfg, node)
	if err != nil {
		return nil, err
	}

	// Override logger with custom name for testing
	c.logger = logger.NewLogger(name)

	return c, nil
}

// Start initializes and starts the cluster with the given node.
// It performs the following operations in sequence:
// 1. Sets the node on the cluster
// 2. Sets the cluster's ShardLock on the node
// 3. Registers the node with etcd
// 4. Starts watching cluster state changes
// 5. Starts node connections
// 6. Starts shard mapping management
//
// This function should be called once during cluster initialization.
// Use Stop() to cleanly shutdown the cluster.
func (c *Cluster) Start(ctx context.Context, n *node.Node) error {
	// Set the cluster's ShardLock on the node for per-cluster isolation
	if c.thisNode != nil {
		c.thisNode.SetShardLock(c.shardLock)
	}

	// Register this node with etcd
	if err := c.registerNode(ctx); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	// Start watching for cluster state changes
	if err := c.startWatching(ctx); err != nil {
		return fmt.Errorf("failed to start watching: %w", err)
	}

	// Start shard mapping management
	if err := c.startShardMappingManagement(ctx); err != nil {
		return fmt.Errorf("failed to start shard mapping management: %w", err)
	}

	// Start node connections
	if err := c.startNodeConnections(ctx); err != nil {
		return fmt.Errorf("failed to start node connections: %w", err)
	}

	c.logger.Infof("Cluster started successfully")
	return nil
}

// Stop cleanly stops the cluster and releases all resources.
// It performs the following operations in reverse order of Start:
// 1. Stops shard mapping management
// 2. Stops node connections
// 3. Stops watching cluster state
// 4. Unregisters the node from etcd
// 5. Closes the etcd connection
func (c *Cluster) Stop(ctx context.Context) error {
	c.logger.Infof("Stopping cluster...")

	// Stop node connections
	c.stopNodeConnections()

	// Stop shard mapping management
	c.stopShardMappingManagement()

	// Stop watching cluster state (must stop before closing etcd)
	if c.consensusManager != nil {
		c.consensusManager.StopWatch()
	}

	// Unregister from etcd
	if err := c.unregisterNode(ctx); err != nil {
		c.logger.Errorf("Failed to unregister node: %v", err)
		// Continue with cleanup even if unregister fails
	}

	// Close etcd connection
	if err := c.closeEtcd(); err != nil {
		c.logger.Errorf("Failed to close etcd: %v", err)
		// Continue with cleanup even if close fails
	}

	c.logger.Infof("Cluster stopped")
	return nil
}

// GetMinQuorum returns the minimal number of nodes required for cluster stability
// If not set, returns 1 as the default
func (c *Cluster) GetMinQuorum() int {
	return c.getEffectiveMinQuorum()
}

// getEffectiveMinQuorum returns the effective minimum quorum value (with default of 1)
func (c *Cluster) getEffectiveMinQuorum() int {
	if c.minQuorum <= 0 {
		return 1
	}
	return c.minQuorum
}

// GetNodeStabilityDuration returns the duration to wait for cluster state to stabilize
// If not set, returns the default DefaultNodeStabilityDuration (10s)
func (c *Cluster) GetNodeStabilityDuration() time.Duration {
	return c.getEffectiveNodeStabilityDuration()
}

// getEffectiveNodeStabilityDuration returns the effective node stability duration (with default of 10s)
func (c *Cluster) getEffectiveNodeStabilityDuration() time.Duration {
	if c.nodeStabilityDuration <= 0 {
		return DefaultNodeStabilityDuration
	}
	return c.nodeStabilityDuration
}

// GetShardMappingCheckInterval returns how often to check if shard mapping needs updating
// If not set, returns the default ShardMappingCheckInterval (5s)
func (c *Cluster) GetShardMappingCheckInterval() time.Duration {
	return c.getEffectiveShardMappingCheckInterval()
}

// getEffectiveShardMappingCheckInterval returns the effective shard mapping check interval (with default of 5s)
func (c *Cluster) getEffectiveShardMappingCheckInterval() time.Duration {
	if c.shardMappingCheckInterval <= 0 {
		return ShardMappingCheckInterval
	}
	return c.shardMappingCheckInterval
}

// ResetForTesting resets the cluster state for testing purposes
// WARNING: This should only be used in tests
func (c *Cluster) ResetForTesting() {
	if c == nil {
		return
	}
	c.thisNode = nil
	c.etcdManager = nil
	c.consensusManager = nil
	c.etcdAddress = ""
	c.etcdPrefix = ""
	c.minQuorum = 0
	c.nodeStabilityDuration = 0
	c.shardMappingCheckInterval = 0
	if c.nodeConnections != nil {
		c.nodeConnections.Stop()
		c.nodeConnections = nil
	}
	if c.shardMappingCancel != nil {
		c.shardMappingCancel()
	}
	c.shardMappingCtx = nil
	c.shardMappingCancel = nil
	c.shardMappingRunning = false
	c.clusterReadyChan = make(chan bool)
	c.clusterReadyOnce = sync.Once{}

	// Reset the singleton to a fresh instance
	if c == thisCluster {
		thisCluster = nil
	}
}

func (c *Cluster) GetThisNode() *node.Node {
	return c.thisNode
}

// GetShardLock returns the cluster's ShardLock instance
func (c *Cluster) GetShardLock() *shardlock.ShardLock {
	return c.shardLock
}

// ClusterReady returns a channel that will be closed when the cluster is ready.
// The cluster is considered ready when:
// - Nodes are connected
// - Shard mapping has been successfully generated and loaded
//
// Usage:
//
//	<-cluster.This().ClusterReady()  // blocks until cluster is ready
//
//	// or with select:
//	select {
//	case <-cluster.This().ClusterReady():
//	    // cluster is ready
//	case <-ctx.Done():
//	    // timeout or cancel
//	}
func (c *Cluster) ClusterReady() <-chan bool {
	return c.clusterReadyChan
}

// IsReady returns true if the cluster is ready (shard mapping is loaded)
func (c *Cluster) IsReady() bool {
	select {
	case <-c.clusterReadyChan:
		return true
	default:
		return false
	}
}

// markClusterReady marks the cluster as ready by closing the clusterReadyChan
// This is called when shard mapping is successfully loaded or created
func (c *Cluster) markClusterReady() {
	c.clusterReadyOnce.Do(func() {
		c.logger.Infof("Cluster is now ready")
		close(c.clusterReadyChan)
	})
}

// checkAndMarkReady checks if all conditions are met to mark the cluster as ready:
// - Node connections are running
// - Shard mapping is available
// If both conditions are met, it marks the cluster as ready
func (c *Cluster) checkAndMarkReady() {
	if c.IsReady() {
		return
	}

	// Check if shard mapping is available
	if c.consensusManager == nil {
		c.logger.Infof("Cannot mark cluster ready: consensus manager not initialized")
		return
	}

	if !c.consensusManager.IsReady() {
		c.logger.Infof("Cannot mark cluster ready: consensus manager not ready")
		return
	}

	if !c.consensusManager.IsStateStable(c.getEffectiveNodeStabilityDuration()) {
		c.logger.Infof("Cannot mark cluster ready: cluster state not stable yet, will check again later")
		return
	}

	// Both conditions met, mark cluster as ready
	c.logger.Infof("All conditions met, marking cluster as READY for the first time!")
	c.markClusterReady()
}

func (c *Cluster) CallObject(ctx context.Context, objType string, id string, method string, request proto.Message) (proto.Message, error) {
	if c.thisNode == nil {
		return nil, fmt.Errorf("ThisNode is not set")
	}

	// Determine which node hosts this object
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("cannot determine node for object %s: %w", id, err)
	}

	// Check if the object is on this node
	if nodeAddr == c.thisNode.GetAdvertiseAddress() {
		// Call locally
		c.logger.Infof("Calling object %s.%s locally (type: %s)", id, method, objType)
		return c.thisNode.CallObject(ctx, objType, id, method, request)
	}

	// Route to the appropriate node
	c.logger.Infof("Routing CallObject for %s.%s to node %s (type: %s)", id, method, nodeAddr, objType)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Marshal request to Any
	var requestAny *anypb.Any
	if request != nil {
		requestAny = &anypb.Any{}
		if err := requestAny.MarshalFrom(request); err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
	}

	// Call CallObject on the remote node
	req := &goverse_pb.CallObjectRequest{
		Id:      id,
		Method:  method,
		Type:    objType,
		Request: requestAny,
	}

	resp, err := client.CallObject(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote CallObject failed on node %s: %w", nodeAddr, err)
	}

	// Unmarshal the response
	if resp.Response == nil {
		return nil, nil
	}

	response, err := resp.Response.UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response, nil
}

// CreateObject creates a distributed object on the appropriate node based on sharding
// The object ID is determined by the type and optional custom ID
// This method routes the creation request to the correct node in the cluster
func (c *Cluster) CreateObject(ctx context.Context, objType, objID string) (string, error) {
	if c.thisNode == nil {
		return "", fmt.Errorf("ThisNode is not set")
	}

	// If objID is not provided, generate one locally
	// We need to know the ID to determine which node should create it
	if objID == "" {
		objID = objType + "-" + uniqueid.UniqueId()
	}

	// Determine which node should host this object
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, objID)
	if err != nil {
		return "", fmt.Errorf("cannot determine node for object %s: %w", objID, err)
	}

	// Check if the object should be created on this node
	if nodeAddr == c.thisNode.GetAdvertiseAddress() {
		// Create locally
		c.logger.Infof("Creating object %s locally (type: %s)", objID, objType)
		return c.thisNode.CreateObject(ctx, objType, objID)
	}

	// Route to the appropriate node
	c.logger.Infof("Routing CreateObject for %s to node %s", objID, nodeAddr)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		c.logger.Warnf("CreateObject failed: %v", err)
		return "", fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Call CreateObject on the remote node
	req := &goverse_pb.CreateObjectRequest{
		Type: objType,
		Id:   objID,
	}

	resp, err := client.CreateObject(ctx, req)
	if err != nil {
		c.logger.Warnf("CreateObject failed: %v", err)
		return "", fmt.Errorf("remote CreateObject failed on node %s: %w", nodeAddr, err)
	}

	c.logger.Infof("Successfully created object %s on node %s", resp.Id, nodeAddr)
	return resp.Id, nil
}

// DeleteObject deletes an object from the cluster.
// It determines which node hosts the object and routes the deletion request accordingly.
func (c *Cluster) DeleteObject(ctx context.Context, objID string) error {
	if c.thisNode == nil {
		return fmt.Errorf("ThisNode is not set")
	}

	if objID == "" {
		return fmt.Errorf("object ID must be specified")
	}

	// Determine which node hosts this object
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, objID)
	if err != nil {
		return fmt.Errorf("cannot determine node for object %s: %w", objID, err)
	}

	// Check if the object should be deleted on this node
	if nodeAddr == c.thisNode.GetAdvertiseAddress() {
		// Delete locally
		c.logger.Infof("Deleting object %s locally", objID)
		return c.thisNode.DeleteObject(ctx, objID)
	}

	// Route to the appropriate node
	c.logger.Infof("Routing DeleteObject for %s to node %s", objID, nodeAddr)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		c.logger.Warnf("DeleteObject failed: %v", err)
		return fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Call DeleteObject on the remote node
	req := &goverse_pb.DeleteObjectRequest{
		Id: objID,
	}

	_, err = client.DeleteObject(ctx, req)
	if err != nil {
		c.logger.Warnf("DeleteObject failed: %v", err)
		return fmt.Errorf("remote DeleteObject failed on node %s: %w", nodeAddr, err)
	}

	c.logger.Infof("Successfully deleted object %s on node %s", objID, nodeAddr)
	return nil
}

// PushMessageToClient sends a message to a client by its ID
// Client IDs have the format: {nodeAddress}/{uniqueId} (e.g., "localhost:7001/abc123")
// This method parses the client ID to determine the target node and routes the message accordingly
func (c *Cluster) PushMessageToClient(ctx context.Context, clientID string, message proto.Message) error {
	if c.thisNode == nil {
		return fmt.Errorf("ThisNode is not set")
	}

	// Parse client ID to extract node address
	// Client ID format: nodeAddress/uniqueId
	parts := strings.SplitN(clientID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid client ID format: %s (expected format: nodeAddress/uniqueId)", clientID)
	}

	nodeAddr := parts[0]

	// Check if the client is on this node
	if nodeAddr == c.thisNode.GetAdvertiseAddress() {
		// Push locally
		c.logger.Infof("Pushing message to client %s locally", clientID)
		return c.thisNode.PushMessageToClient(clientID, message)
	}

	// Route to the appropriate node
	c.logger.Infof("Routing PushMessageToClient for %s to node %s", clientID, nodeAddr)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		return fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Marshal message to Any
	var messageAny *anypb.Any
	if message != nil {
		messageAny = &anypb.Any{}
		if err := messageAny.MarshalFrom(message); err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
	}

	// Call PushMessageToClient on the remote node
	req := &goverse_pb.PushMessageToClientRequest{
		ClientId: clientID,
		Message:  messageAny,
	}

	_, err = client.PushMessageToClient(ctx, req)
	if err != nil {
		return fmt.Errorf("remote PushMessageToClient failed on node %s: %w", nodeAddr, err)
	}

	c.logger.Infof("Successfully pushed message to client %s on node %s", clientID, nodeAddr)
	return nil
}

// GetEtcdManagerForTesting returns the cluster's etcd manager
// WARNING: This should only be used in tests
func (c *Cluster) GetEtcdManagerForTesting() *etcdmanager.EtcdManager {
	return c.etcdManager
}

// GetConsensusManagerForTesting returns the cluster's consensus manager
// WARNING: This should only be used in tests
func (c *Cluster) GetConsensusManagerForTesting() *consensusmanager.ConsensusManager {
	return c.consensusManager
}

// registerNode registers this node with etcd using the shared lease API
func (c *Cluster) registerNode(ctx context.Context) error {
	if c.thisNode == nil {
		return fmt.Errorf("thisNode not set")
	}

	nodesPrefix := c.etcdManager.GetPrefix() + "/nodes/"
	key := nodesPrefix + c.thisNode.GetAdvertiseAddress()
	value := c.thisNode.GetAdvertiseAddress()

	_, err := c.etcdManager.RegisterKeyLease(ctx, key, value, etcdmanager.NodeLeaseTTL)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	return nil
}

// unregisterNode unregisters this node from etcd using the shared lease API
func (c *Cluster) unregisterNode(ctx context.Context) error {
	if c.etcdManager == nil {
		// No-op if etcd manager is not set
		return nil
	}
	if c.thisNode == nil {
		return fmt.Errorf("thisNode not set")
	}

	nodesPrefix := c.etcdManager.GetPrefix() + "/nodes/"
	key := nodesPrefix + c.thisNode.GetAdvertiseAddress()
	return c.etcdManager.UnregisterKeyLease(ctx, key)
}

// closeEtcd closes the etcd connection
func (c *Cluster) closeEtcd() error {
	if c.etcdManager == nil {
		return nil
	}
	return c.etcdManager.Close()
}

// startWatching initializes and starts watching all cluster state changes in etcd
// This includes node changes and shard mapping updates
func (c *Cluster) startWatching(ctx context.Context) error {
	// Initialize consensus manager state from etcd
	err := c.consensusManager.Initialize(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize consensus manager: %w", err)
	}

	// Start watching for changes
	err = c.consensusManager.StartWatch(ctx)
	if err != nil {
		return fmt.Errorf("failed to start consensus manager watch: %w", err)
	}

	return nil
}

// GetNodes returns a list of all registered nodes
func (c *Cluster) GetNodes() []string {
	return c.consensusManager.GetNodes()
}

// GetLeaderNode returns the leader node address.
// The leader is the node with the smallest advertised address in lexicographic order.
// Returns an empty string if there are no registered nodes or if consensus manager is not set.
func (c *Cluster) GetLeaderNode() string {
	return c.consensusManager.GetLeaderNode()
}

// IsLeader returns true if this node is the cluster leader
func (c *Cluster) IsLeader() bool {
	if c.thisNode == nil {
		return false
	}
	leaderNode := c.GetLeaderNode()
	return leaderNode != "" && leaderNode == c.thisNode.GetAdvertiseAddress()
}

// GetShardMapping retrieves the current shard mapping
func (c *Cluster) GetShardMapping(ctx context.Context) *consensusmanager.ShardMapping {
	return c.consensusManager.GetShardMapping()
}

// GetCurrentNodeForObject returns the node address that should handle the given object ID
// If the object ID contains a "/" separator (e.g., "localhost:7001/object-123"),
// the part before the first "/" is treated as a fixed node address and returned directly.
// Otherwise, the object is assigned to a node based on shard mapping.
func (c *Cluster) GetCurrentNodeForObject(ctx context.Context, objectID string) (string, error) {
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		// Fixed node address specified (first part before /)
		nodeAddr := parts[0]
		return nodeAddr, nil
	}

	return c.consensusManager.GetCurrentNodeForObject(objectID)
}

// GetNodeForShard returns the node address that owns the given shard
func (c *Cluster) GetNodeForShard(ctx context.Context, shardID int) (string, error) {
	return c.consensusManager.GetNodeForShard(shardID)
}

// startShardMappingManagement starts a background goroutine that periodically manages shard mapping
// If this node is the leader and the node list has been stable for the configured node stability duration,
// it will update/initialize the shard mapping.
// If this node is not the leader, it will periodically refresh the shard mapping from etcd.
func (c *Cluster) startShardMappingManagement(ctx context.Context) error {
	if c.shardMappingRunning {
		c.logger.Warnf("Shard mapping management already running")
		return nil
	}

	c.shardMappingCtx, c.shardMappingCancel = context.WithCancel(ctx)
	c.shardMappingRunning = true

	go c.shardMappingManagementLoop()
	c.logger.Infof("Started shard mapping management (check interval: %v, stability duration: %v)",
		c.getEffectiveShardMappingCheckInterval(), c.getEffectiveNodeStabilityDuration())

	return nil
}

// stopShardMappingManagement stops the shard mapping management background task
func (c *Cluster) stopShardMappingManagement() {
	if c.shardMappingCancel != nil {
		c.shardMappingCancel()
		c.shardMappingCancel = nil
	}
	c.shardMappingRunning = false
	c.logger.Infof("Stopped shard mapping management")
}

// shardMappingManagementLoop is the background loop that manages shard mapping
func (c *Cluster) shardMappingManagementLoop() {
	ticker := time.NewTicker(c.getEffectiveShardMappingCheckInterval())
	defer ticker.Stop()

	for {
		select {
		case <-c.shardMappingCtx.Done():
			c.logger.Debugf("Shard mapping management loop stopped")
			return
		case <-ticker.C:
			c.handleShardMappingCheck()
			c.updateMetrics()
		}
	}
}

// handleShardMappingCheck checks and updates shard mapping based on leadership and node stability
func (c *Cluster) handleShardMappingCheck() {
	ctx := c.shardMappingCtx

	// If leader made changes to cluster state, skip other operations this cycle
	// to allow the cluster state to stabilize before proceeding. Always update
	// shard metrics so monitoring reflects the latest assignment even during
	// transient state changes.
	stateChanged := c.leaderShardManagementLogic(ctx)
	if stateChanged {
		c.logger.Debugf("Cluster state changed by leader, waiting for next cycle")
		// Ensure metrics are updated even when leader changed state to keep
		// Prometheus/monitoring in sync with the in-memory state.
		return
	}

	c.claimShardOwnership(ctx)
	c.removeObjectsNotBelongingToThisNode(ctx)
	c.releaseShardOwnership(ctx)
	c.updateNodeConnections()
	c.checkAndMarkReady()
}

func (c *Cluster) updateMetrics() {
	c.consensusManager.UpdateShardMetrics()
}

// claimShardOwnership claims ownership of shards when cluster state is stable
func (c *Cluster) claimShardOwnership(ctx context.Context) {
	if c.thisNode == nil {
		return
	}

	clusterState := c.consensusManager.GetClusterState()

	// Only claim shards when cluster state is stable
	if !clusterState.IsStable(c.getEffectiveNodeStabilityDuration()) {
		return
	}

	localAddr := c.thisNode.GetAdvertiseAddress()
	if !clusterState.HasNode(localAddr) {
		// This node is not yet in the cluster state
		return
	}

	// Claim ownership of shards where this node is the target
	err := c.consensusManager.ClaimShardsForNode(ctx, localAddr)
	if err != nil {
		c.logger.Warnf("Failed to claim shard ownership: %v", err)
	}
}

// releaseShardOwnership releases ownership of shards when:
// - CurrentNode is this node
// - TargetNode is another node (not empty and not this node)
// - No objects exist on this node for that shard
func (c *Cluster) releaseShardOwnership(ctx context.Context) {
	if c.thisNode == nil {
		return
	}

	clusterState := c.consensusManager.GetClusterState()

	// Only release shards when cluster state is stable
	if !clusterState.IsStable(c.getEffectiveNodeStabilityDuration()) {
		return
	}

	localAddr := c.thisNode.GetAdvertiseAddress()

	// Count objects per shard on this node
	objectIDs := c.thisNode.ListObjectIDs()
	localObjectsPerShard := make(map[int]int)
	for _, objectID := range objectIDs {
		// Skip client objects (those with "/" in ID)
		if strings.Contains(objectID, "/") {
			continue
		}
		shardID := sharding.GetShardID(objectID)
		localObjectsPerShard[shardID]++
	}

	// Release ownership of shards where this node is no longer needed
	err := c.consensusManager.ReleaseShardsForNode(ctx, localAddr, localObjectsPerShard)
	if err != nil {
		c.logger.Warnf("Failed to release shard ownership: %v", err)
	}
}

// removeObjectsNotBelongingToThisNode removes objects whose shards no longer belong to this node
func (c *Cluster) removeObjectsNotBelongingToThisNode(ctx context.Context) {
	if c.thisNode == nil {
		c.logger.Warnf("Cannot remove objects: thisNode is nil")
		return
	}

	// Check if cluster state is stable without cloning
	if !c.consensusManager.IsStateStable(c.getEffectiveNodeStabilityDuration()) {
		c.logger.Debugf("Skipping object removal: cluster state is not stable yet")
		return
	}

	localAddr := c.thisNode.GetAdvertiseAddress()

	// Get all object IDs on this node
	objectIDs := c.thisNode.ListObjectIDs()

	// Ask ConsensusManager to determine which objects should be evicted
	// This is more efficient than cloning the entire cluster state
	// ConsensusManager will also check if this node is in the cluster
	objectsToEvict := c.consensusManager.GetObjectsToEvict(localAddr, objectIDs)

	// Remove each object that should be evicted
	for _, objectID := range objectsToEvict {
		shardID := sharding.GetShardID(objectID)
		c.logger.Infof("Removing object %s (shard %d) as it no longer belongs to this node",
			objectID, shardID)

		err := c.thisNode.DeleteObject(ctx, objectID)
		if err != nil {
			c.logger.Errorf("Failed to delete object %s: %v", objectID, err)
		} else {
			c.logger.Infof("Successfully removed object %s", objectID)
		}
	}
}

// leaderShardManagementLogic manages shard mapping as the leader node
// Returns true if the cluster state was changed (shards reassigned or rebalanced)
// Only performs one operation per call to allow cluster state to stabilize between changes
func (c *Cluster) leaderShardManagementLogic(ctx context.Context) bool {
	if !c.IsLeader() {
		return false
	}

	clusterState := c.consensusManager.GetClusterState()
	minQuorum := c.getEffectiveMinQuorum()

	c.logger.Infof("Acting as leader: %s; nodes: %d (min quorum: %d), sharding map: %d, revision: %d",
		c.thisNode.GetAdvertiseAddress(), len(clusterState.Nodes), minQuorum, len(clusterState.ShardMapping.Shards), clusterState.Revision)

	if !clusterState.IsStable(c.getEffectiveNodeStabilityDuration()) {
		c.logger.Warnf("Cluster state not yet stable, waiting before updating shard mapping")
		return false
	}

	if len(clusterState.Nodes) == 0 {
		c.logger.Warnf("No nodes available, skipping shard mapping update")
		return false
	}

	// Check if we have the minimum required nodes
	if len(clusterState.Nodes) < minQuorum {
		c.logger.Warnf("Cluster has %d nodes but requires minimum quorum of %d nodes - waiting for more nodes to join", len(clusterState.Nodes), minQuorum)
		return false
	}

	if c.thisNode == nil {
		c.logger.Warnf("ThisNode not set; leader cannot ensure self registration")
		return false
	}

	localAddr := c.thisNode.GetAdvertiseAddress()
	if !clusterState.HasNode(localAddr) {
		c.logger.Infof("This node %s not present in cluster state", localAddr)
		return false
	}

	// First priority: reassign shards whose target nodes are no longer in the cluster
	// This handles node failures and ensures all shards have valid target nodes
	reassignedCount, err := c.consensusManager.ReassignShardTargetNodes(ctx)
	if reassignedCount > 0 {
		c.logger.Infof("Reassigned %d shards to new target nodes", reassignedCount)
		return true // State changed, allow it to stabilize before next operation
	}
	if err != nil {
		c.logger.Errorf("Failed to reassign shard target nodes: %v", err)
		return false
	}

	// Second priority: rebalance shards to improve cluster balance
	// Only attempt if no reassignment was needed (cluster is stable)
	rebalanced, err := c.consensusManager.RebalanceShards(ctx)
	if err != nil {
		c.logger.Errorf("Failed to rebalance shards: %v", err)
		return false
	}

	if rebalanced {
		c.logger.Infof("Rebalanced one shard to improve cluster balance")
		return true // State changed
	}

	return false // No changes made
}

// startNodeConnections initializes and starts the node connections manager
// This should be called after StartWatching is started
func (c *Cluster) startNodeConnections(ctx context.Context) error {
	if c.nodeConnections == nil {
		return fmt.Errorf("nodeConnections is nil - cluster not properly initialized")
	}

	err := c.nodeConnections.Start(ctx)
	if err != nil {
		return err
	}

	// Set initial nodes
	c.updateNodeConnections()

	// Check if we can mark cluster as ready now that node connections are established
	c.checkAndMarkReady()
	return nil
}

// stopNodeConnections stops the node connections manager
func (c *Cluster) stopNodeConnections() {
	c.nodeConnections.Stop()
}

// updateNodeConnections updates the NodeConnections with the current list of cluster nodes
func (c *Cluster) updateNodeConnections() {
	if c.thisNode == nil {
		c.logger.Warnf("updateNodeConnections: thisNode is not initialized")
		return
	}

	allNodes := c.GetNodes()
	thisNodeAddr := c.thisNode.GetAdvertiseAddress()

	// Filter out this node's address
	otherNodes := make([]string, 0, len(allNodes))
	for _, nodeAddr := range allNodes {
		if nodeAddr != thisNodeAddr {
			otherNodes = append(otherNodes, nodeAddr)
		}
	}

	c.nodeConnections.SetNodes(otherNodes)
}

// GetNodeConnections returns the node connections manager
func (c *Cluster) GetNodeConnections() *nodeconnections.NodeConnections {
	return c.nodeConnections
}
