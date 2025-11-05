package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/consensusmanager"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
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
	// NodeStabilityDuration is how long the node list must be stable before updating shard mapping
	NodeStabilityDuration = 10 * time.Second
)

type Cluster struct {
	thisNode            *node.Node
	etcdManager         *etcdmanager.EtcdManager
	consensusManager    *consensusmanager.ConsensusManager
	nodeConnections     *NodeConnections
	logger              *logger.Logger
	etcdAddress         string // etcd server address (e.g., "localhost:2379")
	etcdPrefix          string // etcd key prefix for this cluster
	shardMappingCtx     context.Context
	shardMappingCancel  context.CancelFunc
	shardMappingRunning bool
	clusterReadyChan    chan bool
	clusterReadyOnce    sync.Once
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

// NewCluster creates a new cluster instance with the given etcd address and prefix.
// It automatically connects to etcd and initializes the etcd manager and consensus manager.
// This function assigns the created cluster to the singleton and should be called once during application initialization.
// If the cluster singleton is already initialized, this function will return an error.
// This function is thread-safe.
func NewCluster(etcdAddress string, etcdPrefix string) (*Cluster, error) {
	// Create a new cluster instance
	c := &Cluster{
		logger:           logger.NewLogger("Cluster"),
		clusterReadyChan: make(chan bool),
		etcdAddress:      etcdAddress,
		etcdPrefix:       etcdPrefix,
	}

	mgr, err := createAndConnectEtcdManager(etcdAddress, etcdPrefix)
	if err != nil {
		return nil, err
	}

	c.etcdManager = mgr
	c.consensusManager = consensusmanager.NewConsensusManager(mgr)

	return c, nil
}

// newClusterForTesting creates a new cluster instance for testing with an initialized logger
func newClusterForTesting(name string) *Cluster {
	return &Cluster{
		logger:           logger.NewLogger(name),
		clusterReadyChan: make(chan bool),
	}
}

// newClusterWithEtcdForTesting creates a new cluster instance for testing and initializes it with etcd
func newClusterWithEtcdForTesting(name string, etcdAddress string, etcdPrefix string) (*Cluster, error) {
	c, err := NewCluster(etcdAddress, etcdPrefix)
	if err != nil {
		return nil, err
	}

	// Override logger with custom name for testing
	c.logger = logger.NewLogger(name)

	return c, nil
}

// initializeEtcdForTesting initializes the etcd manager for a test cluster instance
// WARNING: This should only be used in tests
func (c *Cluster) initializeEtcdForTesting(etcdAddress string, etcdPrefix string) error {
	if c.etcdManager != nil {
		return fmt.Errorf("etcd manager already initialized")
	}

	c.etcdAddress = etcdAddress
	c.etcdPrefix = etcdPrefix

	mgr, err := createAndConnectEtcdManager(etcdAddress, etcdPrefix)
	if err != nil {
		return err
	}

	c.etcdManager = mgr
	c.consensusManager = consensusmanager.NewConsensusManager(mgr)

	return nil
}

func (c *Cluster) SetThisNode(n *node.Node) {
	if c.thisNode != nil {
		panic("ThisNode is already set")
	}
	c.thisNode = n
	c.logger.Infof("This Node is %s", n)
	
	// Set this node's address in the consensus manager so it can claim shard ownership
	if c.consensusManager != nil {
		c.consensusManager.SetThisNode(n.GetAdvertiseAddress())
	}
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
// - Node connections are established (nodeConnections is not nil and running)
// - Shard mapping is available
// If both conditions are met, it marks the cluster as ready
func (c *Cluster) checkAndMarkReady() {
	if c.IsReady() {
		return
	}

	// Check if node connections are established
	if c.nodeConnections == nil {
		c.logger.Debugf("Cannot mark cluster ready: node connections not established")
		return
	}

	// Check if shard mapping is available
	if c.consensusManager == nil {
		c.logger.Debugf("Cannot mark cluster ready: consensus manager not initialized")
		return
	}

	if !c.consensusManager.IsReady() {
		c.logger.Debugf("Cannot mark cluster ready: consensus manager not ready")
		return
	}

	if !c.consensusManager.IsStateStable(5 * time.Second) {
		c.logger.Debugf("Cannot mark cluster ready: cluster state not stable yet, will check again later")
		return
	}

	// Both conditions met, mark cluster as ready
	c.logger.Infof("All conditions met, marking cluster as READY for the first time!")
	c.markClusterReady()
}

func (c *Cluster) CallObject(ctx context.Context, id string, method string, request proto.Message) (proto.Message, error) {
	if c.thisNode == nil {
		return nil, fmt.Errorf("ThisNode is not set")
	}

	// Determine which node hosts this object
	nodeAddr, err := c.GetNodeForObject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("cannot determine node for object %s: %w", id, err)
	}

	// Check if the object is on this node
	if nodeAddr == c.thisNode.GetAdvertiseAddress() {
		// Call locally
		c.logger.Infof("Calling object %s.%s locally", id, method)
		return c.thisNode.CallObject(ctx, id, method, request)
	}

	// Route to the appropriate node
	c.logger.Infof("Routing CallObject for %s.%s to node %s", id, method, nodeAddr)

	if c.nodeConnections == nil {
		return nil, fmt.Errorf("node connections not initialized")
	}

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
func (c *Cluster) CreateObject(ctx context.Context, objType, objID string, initData proto.Message) (string, error) {
	if c.thisNode == nil {
		return "", fmt.Errorf("ThisNode is not set")
	}

	// If objID is not provided, generate one locally
	// We need to know the ID to determine which node should create it
	if objID == "" {
		objID = objType + "-" + uniqueid.UniqueId()
	}

	// Determine which node should host this object
	nodeAddr, err := c.GetNodeForObject(ctx, objID)
	if err != nil {
		return "", fmt.Errorf("cannot determine node for object %s: %w", objID, err)
	}

	// Check if the object should be created on this node
	if nodeAddr == c.thisNode.GetAdvertiseAddress() {
		// Create locally
		c.logger.Infof("Creating object %s locally (type: %s)", objID, objType)
		return c.thisNode.CreateObject(ctx, objType, objID, initData)
	}

	// Route to the appropriate node
	c.logger.Infof("Routing CreateObject for %s to node %s", objID, nodeAddr)

	if c.nodeConnections == nil {
		c.logger.Warnf("CreateObject failed: Node connections not initialized")
		return "", fmt.Errorf("node connections not initialized")
	}

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		c.logger.Warnf("CreateObject failed: %v", err)
		return "", fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Marshal initData to Any
	var initDataAny *anypb.Any
	if initData != nil {
		initDataAny = &anypb.Any{}
		if err := initDataAny.MarshalFrom(initData); err != nil {
			c.logger.Warnf("CreateObject failed: %v", err)
			return "", fmt.Errorf("failed to marshal init data: %w", err)
		}
	}

	// Call CreateObject on the remote node
	req := &goverse_pb.CreateObjectRequest{
		Type:     objType,
		Id:       objID,
		InitData: initDataAny,
	}

	resp, err := client.CreateObject(ctx, req)
	if err != nil {
		c.logger.Warnf("CreateObject failed: %v", err)
		return "", fmt.Errorf("remote CreateObject failed on node %s: %w", nodeAddr, err)
	}

	c.logger.Infof("Successfully created object %s on node %s", resp.Id, nodeAddr)
	return resp.Id, nil
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

	if c.nodeConnections == nil {
		return fmt.Errorf("node connections not initialized")
	}

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

// RegisterNode registers this node with etcd
func (c *Cluster) RegisterNode(ctx context.Context) error {
	if c.thisNode == nil {
		return fmt.Errorf("thisNode not set")
	}
	return c.etcdManager.RegisterNode(ctx, c.thisNode.GetAdvertiseAddress())
}

// UnregisterNode unregisters this node from etcd
func (c *Cluster) UnregisterNode(ctx context.Context) error {
	if c.etcdManager == nil {
		// No-op if etcd manager is not set
		return nil
	}
	if c.thisNode == nil {
		return fmt.Errorf("thisNode not set")
	}
	return c.etcdManager.UnregisterNode(ctx, c.thisNode.GetAdvertiseAddress())
}

// CloseEtcd closes the etcd connection
func (c *Cluster) CloseEtcd() error {
	if c.etcdManager == nil {
		return nil
	}
	return c.etcdManager.Close()
}

// StartWatching initializes and starts watching all cluster state changes in etcd
// This includes node changes and shard mapping updates
func (c *Cluster) StartWatching(ctx context.Context) error {
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
func (c *Cluster) GetShardMapping(ctx context.Context) (*consensusmanager.ShardMapping, error) {
	return c.consensusManager.GetShardMapping()
}

// GetNodeForObject returns the node address that should handle the given object ID
// If the object ID contains a "/" separator (e.g., "localhost:7001/object-123"),
// the part before the first "/" is treated as a fixed node address and returned directly.
// Otherwise, the object is assigned to a node based on shard mapping.
func (c *Cluster) GetNodeForObject(ctx context.Context, objectID string) (string, error) {
	// Check if object ID specifies a fixed node address
	// Format: nodeAddress/actualObjectID (e.g., "localhost:7001/object-123")
	// This doesn't require consensus manager
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			// Fixed node address specified (first part before /)
			return parts[0], nil
		}
	}

	// For standard shard-based routing, use consensus manager
	return c.consensusManager.GetNodeForObject(objectID)
}

// GetNodeForShard returns the node address that owns the given shard
func (c *Cluster) GetNodeForShard(ctx context.Context, shardID int) (string, error) {
	return c.consensusManager.GetNodeForShard(shardID)
}

// InvalidateShardMappingCache clears the local shard mapping cache
// This method is deprecated and kept for backward compatibility
func (c *Cluster) InvalidateShardMappingCache() {
	// With ConsensusManager, the cache is automatically updated via watch
	// This is a no-op for backward compatibility
	c.logger.Debugf("InvalidateShardMappingCache is deprecated with ConsensusManager")
}

// StartShardMappingManagement starts a background goroutine that periodically manages shard mapping
// If this node is the leader and the node list has been stable for NodeStabilityDuration,
// it will update/initialize the shard mapping.
// If this node is not the leader, it will periodically refresh the shard mapping from etcd.
func (c *Cluster) StartShardMappingManagement(ctx context.Context) error {
	if c.shardMappingRunning {
		c.logger.Warnf("Shard mapping management already running")
		return nil
	}

	c.shardMappingCtx, c.shardMappingCancel = context.WithCancel(ctx)
	c.shardMappingRunning = true

	go c.shardMappingManagementLoop()
	c.logger.Infof("Started shard mapping management (check interval: %v, stability duration: %v)",
		ShardMappingCheckInterval, NodeStabilityDuration)

	return nil
}

// StopShardMappingManagement stops the shard mapping management background task
func (c *Cluster) StopShardMappingManagement() {
	if c.shardMappingCancel != nil {
		c.shardMappingCancel()
		c.shardMappingCancel = nil
	}
	c.shardMappingRunning = false
	c.logger.Infof("Stopped shard mapping management")
}

// shardMappingManagementLoop is the background loop that manages shard mapping
func (c *Cluster) shardMappingManagementLoop() {
	ticker := time.NewTicker(ShardMappingCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.shardMappingCtx.Done():
			c.logger.Debugf("Shard mapping management loop stopped")
			return
		case <-ticker.C:
			c.handleShardMappingCheck()
		}
	}
}

// handleShardMappingCheck checks and updates shard mapping based on leadership and node stability
func (c *Cluster) handleShardMappingCheck() {
	ctx := c.shardMappingCtx
	c.leaderShardMappingManagement(ctx)
	c.checkAndMarkReady()
}

func (c *Cluster) leaderShardMappingManagement(ctx context.Context) {
	if !c.IsLeader() {
		return
	}

	clusterState := c.consensusManager.GetClusterState()
	c.logger.Infof("Acting as leader: %s; nodes: %d, sharding map: %d, revision: %d",
		c.thisNode.GetAdvertiseAddress(), len(clusterState.Nodes), len(clusterState.ShardMapping.Shards), clusterState.Revision)

	if !clusterState.IsStable(NodeStabilityDuration) {
		c.logger.Warnf("Cluster state not yet stable, waiting before updating shard mapping")
		return
	}

	if len(clusterState.Nodes) == 0 {
		c.logger.Warnf("No nodes available, skipping shard mapping update")
		return
	}

	if c.thisNode == nil {
		c.logger.Warnf("ThisNode not set; leader cannot ensure self registration")
		return
	}

	localAddr := c.thisNode.GetAdvertiseAddress()
	if !clusterState.HasNode(localAddr) {
		c.logger.Infof("This node %s not present in cluster state", localAddr)
		return
	}

	err := c.consensusManager.UpdateShardMapping(ctx)
	if err != nil {
		c.logger.Errorf("Failed to update shard mapping: %v", err)
	} else {
		c.logger.Infof("Shard mapping updated successfully")
	}
}

// StartNodeConnections initializes and starts the node connections manager
// This should be called after StartWatching is started
func (c *Cluster) StartNodeConnections(ctx context.Context) error {
	if c.nodeConnections != nil {
		c.logger.Warnf("NodeConnections already started")
		return nil
	}

	c.nodeConnections = NewNodeConnections(c)
	err := c.nodeConnections.Start(ctx)
	if err != nil {
		return err
	}

	// Check if we can mark cluster as ready now that node connections are established
	c.checkAndMarkReady()
	return nil
}

// StopNodeConnections stops the node connections manager
func (c *Cluster) StopNodeConnections() {
	if c.nodeConnections != nil {
		c.nodeConnections.Stop()
		c.nodeConnections = nil
	}
}

// GetNodeConnections returns the node connections manager
func (c *Cluster) GetNodeConnections() *NodeConnections {
	return c.nodeConnections
}
