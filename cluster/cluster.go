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
	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
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
	node                      *node.Node
	gate                      *gate.Gate
	etcdManager               *etcdmanager.EtcdManager
	consensusManager          *consensusmanager.ConsensusManager
	nodeConnections           *nodeconnections.NodeConnections
	shardLock                 *shardlock.ShardLock
	logger                    *logger.Logger
	config                    Config        // cluster configuration
	etcdAddress               string        // etcd server address (e.g., "localhost:2379")
	etcdPrefix                string        // etcd key prefix for this cluster
	minQuorum                 int           // minimal number of nodes required for cluster to be considered stable
	numShards                 int           // number of shards in this cluster
	shardMappingCheckInterval time.Duration // how often to check if shard mapping needs updating
	clusterManagementCtx      context.Context
	clusterManagementCancel   context.CancelFunc
	clusterManagementRunning  bool
	clusterReadyChan          chan bool
	clusterReadyOnce          sync.Once
	gateChannels              map[string]chan proto.Message
	gateChannelsMu            sync.RWMutex
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

// NewClusterWithNode creates a new cluster instance with the given configuration.
// It automatically connects to etcd and initializes the etcd manager and consensus manager.
// This function assigns the created cluster to the singleton and should be called once during application initialization.
// If the cluster singleton is already initialized, this function will return an error.
// This function is thread-safe.
func NewClusterWithNode(cfg Config, node *node.Node) (*Cluster, error) {
	// Determine number of shards
	numShards := cfg.NumShards
	if numShards <= 0 {
		numShards = sharding.NumShards
	}

	// Create a new cluster instance with its own ShardLock to ensure per-cluster isolation
	c := &Cluster{
		node:                      node,
		logger:                    logger.NewLogger("Cluster"),
		clusterReadyChan:          make(chan bool),
		config:                    cfg,
		etcdAddress:               cfg.EtcdAddress,
		etcdPrefix:                cfg.EtcdPrefix,
		minQuorum:                 cfg.MinQuorum,
		numShards:                 numShards,
		shardMappingCheckInterval: cfg.ShardMappingCheckInterval,
		nodeConnections:           nodeconnections.New(),
		shardLock:                 shardlock.NewShardLock(numShards),
		gateChannels:              make(map[string]chan proto.Message),
	}
	if err := c.init(cfg); err != nil {
		return nil, err
	}
	return c, nil
}

func NewClusterWithGate(cfg Config, g *gate.Gate) (*Cluster, error) {
	// Determine number of shards
	numShards := cfg.NumShards
	if numShards <= 0 {
		numShards = sharding.NumShards
	}

	c := &Cluster{
		gate:                      g,
		logger:                    logger.NewLogger("Cluster"),
		clusterReadyChan:          make(chan bool),
		config:                    cfg,
		etcdAddress:               cfg.EtcdAddress,
		etcdPrefix:                cfg.EtcdPrefix,
		minQuorum:                 cfg.MinQuorum,
		numShards:                 numShards,
		shardMappingCheckInterval: cfg.ShardMappingCheckInterval,
		nodeConnections:           nodeconnections.New(),
		shardLock:                 shardlock.NewShardLock(numShards),
		gateChannels:              make(map[string]chan proto.Message),
	}
	if err := c.init(cfg); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cluster) init(cfg Config) error {
	// Initialization logic for the cluster
	// Set the cluster's ShardLock on the node immediately for per-cluster isolation
	// Note: Gates don't need ShardLock as they don't host objects
	if c.isNode() {
		c.node.SetShardLock(c.shardLock)
		// Set the cluster info provider so the node's InspectorManager can report cluster info
		// The cluster implements clusterinfo.ClusterInfoProvider interface
		c.node.SetClusterInfoProvider(c)
	}

	// Set the cluster info provider for gates so the gate's InspectorManager can report cluster info
	if c.isGate() {
		c.gate.SetClusterInfoProvider(c)
	}

	mgr, err := createAndConnectEtcdManager(cfg.EtcdAddress, cfg.EtcdPrefix)
	if err != nil {
		return err
	}

	c.etcdManager = mgr

	// Use the provided cluster state stability duration or default
	stabilityDuration := cfg.ClusterStateStabilityDuration
	if stabilityDuration <= 0 {
		stabilityDuration = DefaultNodeStabilityDuration
	}
	// Use the cluster's numShards for consensus manager
	c.consensusManager = consensusmanager.NewConsensusManager(mgr, c.shardLock, stabilityDuration, c.getAdvertiseAddr(), c.numShards)

	// Set minQuorum on consensus manager if specified
	if cfg.MinQuorum > 0 {
		c.consensusManager.SetMinQuorum(cfg.MinQuorum)
	}

	// Set rebalance shards batch size on consensus manager if specified
	if cfg.RebalanceShardsBatchSize > 0 {
		c.consensusManager.SetRebalanceShardsBatchSize(cfg.RebalanceShardsBatchSize)
	}

	// Set imbalance threshold on consensus manager if specified
	if cfg.ImbalanceThreshold > 0 {
		c.consensusManager.SetImbalanceThreshold(cfg.ImbalanceThreshold)
	}
	return nil
}

// newClusterForTesting creates a new cluster instance for testing with an initialized logger
// Uses test-appropriate configuration values
func newClusterForTesting(node *node.Node, name string) *Cluster {
	// Use test-appropriate durations (faster than production defaults)
	// Use testutil.TestNumShards for consistency with test node configuration
	numShards := testutil.TestNumShards
	shardLock := shardlock.NewShardLock(numShards)
	return &Cluster{
		node:                      node,
		logger:                    logger.NewLogger(name),
		clusterReadyChan:          make(chan bool),
		nodeConnections:           nodeconnections.New(),
		consensusManager:          consensusmanager.NewConsensusManager(nil, shardLock, 3*time.Second, "", numShards),
		minQuorum:                 1,
		numShards:                 numShards,
		shardMappingCheckInterval: 1 * time.Second,
		shardLock:                 shardLock,
		gateChannels:              make(map[string]chan proto.Message),
	}
}

// newClusterWithEtcdForTesting creates a new cluster instance for testing and initializes it with etcd
// Uses test-appropriate configuration values (shorter durations)
func newClusterWithEtcdForTesting(name string, node *node.Node, etcdAddress string, etcdPrefix string) (*Cluster, error) {
	// Create config with test values
	cfg := Config{
		EtcdAddress:                   etcdAddress,
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
	}

	c, err := NewClusterWithNode(cfg, node)
	if err != nil {
		return nil, err
	}

	// Override logger with custom name for testing
	c.logger = logger.NewLogger(name)

	return c, nil
}

// newClusterWithEtcdForTestingGate creates a new cluster instance for testing with a gate and initializes it with etcd
// Uses test-appropriate configuration values (shorter durations)
func newClusterWithEtcdForTestingGate(name string, gate *gate.Gate, etcdAddress string, etcdPrefix string) (*Cluster, error) {
	// Create config with test values
	cfg := Config{
		EtcdAddress:                   etcdAddress,
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
	}

	c, err := NewClusterWithGate(cfg, gate)
	if err != nil {
		return nil, err
	}

	// Override logger with custom name for testing
	c.logger = logger.NewLogger(name)

	return c, nil
}

// Start initializes and starts the cluster with the given node.
// It performs the following operations in sequence:
// 3. Registers the node/gate with etcd
// 4. Starts watching cluster state changes
// 5. Starts node connections
// 6. Starts shard mapping management
//
// This function should be called once during cluster initialization.
// Use Stop() to cleanly shutdown the cluster.
func (c *Cluster) Start(ctx context.Context, n *node.Node) error {
	// Register this node or gate with etcd
	if c.isNode() {
		if err := c.registerNode(ctx); err != nil {
			return fmt.Errorf("failed to register node: %w", err)
		}
	} else if c.isGate() {
		if err := c.registerGate(ctx); err != nil {
			return fmt.Errorf("failed to register gate: %w", err)
		}
	}

	// Start watching for cluster state changes
	if err := c.startWatching(ctx); err != nil {
		return fmt.Errorf("failed to start watching: %w", err)
	}

	// Start shard mapping management
	if err := c.startClusterManagement(ctx); err != nil {
		return fmt.Errorf("failed to start shard mapping management: %w", err)
	}

	// Start node connections
	if err := c.startNodeConnections(ctx); err != nil {
		return fmt.Errorf("failed to start node connections: %w", err)
	}

	c.logger.Infof("%s - Cluster started successfully", c)
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
	c.logger.Infof("%s - Stopping cluster...", c)

	// Stop node connections
	c.stopNodeConnections()

	// Stop shard mapping management
	c.stopShardMappingManagement()

	// Stop watching cluster state (must stop before closing etcd)
	c.consensusManager.StopWatch()

	// Clean up gate channels
	c.cleanupGateChannels()

	// Unregister from etcd
	if c.isNode() {
		if err := c.unregisterNode(ctx); err != nil {
			c.logger.Errorf("%s - Failed to unregister node: %v", c, err)
			// Continue with cleanup even if unregister fails
		}
	} else if c.isGate() {
		if err := c.unregisterGate(ctx); err != nil {
			c.logger.Errorf("%s - Failed to unregister gate: %v", c, err)
			// Continue with cleanup even if unregister fails
		}
	}

	// Close etcd connection
	if err := c.closeEtcd(); err != nil {
		c.logger.Errorf("%s - Failed to close etcd: %v", c, err)
		// Continue with cleanup even if close fails
	}

	c.logger.Infof("%s - Cluster stopped", c)
	return nil
}

// getAdvertiseAddr returns the advertise address of this cluster (node or gate)
func (c *Cluster) getAdvertiseAddr() string {
	if c.node != nil {
		return c.node.GetAdvertiseAddress()
	}
	if c.gate != nil {
		return c.gate.GetAdvertiseAddress()
	}
	return ""
}

// GetAdvertiseAddress returns the advertise address of this cluster (node or gate)
func (c *Cluster) GetAdvertiseAddress() string {
	return c.getAdvertiseAddr()
}

// isNode returns true if this cluster is a node cluster
func (c *Cluster) isNode() bool {
	return c.node != nil
}

// IsNode returns true if this cluster is a node cluster
func (c *Cluster) IsNode() bool {
	return c.isNode()
}

// isGate returns true if this cluster is a gate cluster
func (c *Cluster) isGate() bool {
	return c.gate != nil
}

// IsGate returns true if this cluster is a gate cluster
func (c *Cluster) IsGate() bool {
	return c.isGate()
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

func (c *Cluster) GetThisNode() *node.Node {
	return c.node
}

// GetThisGate returns the gate instance if this cluster is a gate cluster
// Returns nil if this is not a gate cluster
func (c *Cluster) GetThisGate() *gate.Gate {
	return c.gate
}

// String returns a string representation of the cluster
// Format: Cluster<local-address,type,leader|member,quorum=%d>
func (c *Cluster) String() string {
	addr := c.getAdvertiseAddr()

	var clusterType string
	if c.isNode() {
		clusterType = "node"
	} else if c.isGate() {
		clusterType = "gate"
	} else {
		clusterType = "unknown"
	}

	var role string
	if c.IsLeader() {
		role = "leader"
	} else {
		role = "member"
	}

	quorum := c.getEffectiveMinQuorum()

	return fmt.Sprintf("Cluster<%s,%s,%s,quorum=%d>", addr, clusterType, role, quorum)
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
		c.logger.Infof("%s - Cluster is now ready", c)
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
	if !c.consensusManager.IsReady() {
		c.logger.Infof("%s - Cannot mark cluster ready: consensus manager not ready", c)
		return
	}

	if !c.consensusManager.IsStateStable() {
		c.logger.Infof("Cannot mark cluster ready: cluster state not stable yet, will check again later")
		return
	}

	// Both conditions met, mark cluster as ready
	c.logger.Infof("%s - All conditions met, marking cluster as READY for the first time!", c)
	c.markClusterReady()
}

func (c *Cluster) CallObject(ctx context.Context, objType string, id string, method string, request proto.Message) (proto.Message, error) {
	// Determine which node hosts this object
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("cannot determine node for object %s: %w", id, err)
	}

	// Check if the object is on this node (only for node clusters)
	if c.isNode() && nodeAddr == c.getAdvertiseAddr() {
		// Call locally on node
		c.logger.Infof("%s - Calling object %s.%s locally (type: %s)", c, id, method, objType)
		return c.node.CallObject(ctx, objType, id, method, request)
	}

	// Route to the appropriate node (both node and gate clusters can route)
	c.logger.Infof("%s - Routing CallObject for %s.%s to node %s (type: %s)", c, id, method, nodeAddr, objType)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Marshal the request to Any for transport
	requestAny, err := protohelper.MsgToAny(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Call CallObject on the remote node
	// Extract client_id from context if present and pass it in the request
	req := &goverse_pb.CallObjectRequest{
		Id:       id,
		Method:   method,
		Type:     objType,
		Request:  requestAny,
		ClientId: callcontext.ClientID(ctx),
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

// CallObjectAnyRequest calls a method on a distributed object, accepting an *anypb.Any request.
// This is an optimized version that avoids unnecessary marshal/unmarshal cycles when the request
// is already in Any format (e.g., from a gate that received it from a client).
func (c *Cluster) CallObjectAnyRequest(ctx context.Context, objType string, id string, method string, request *anypb.Any) (*anypb.Any, error) {
	if !c.isGate() {
		return nil, fmt.Errorf("CallObjectAnyRequest can only be called on gate clusters")
	}
	// Determine which node hosts this object
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("cannot determine node for object %s: %w", id, err)
	}

	// Route to the appropriate node (both node and gate clusters can route)
	c.logger.Infof("%s - Routing CallObject for %s.%s to node %s (type: %s)", c, id, method, nodeAddr, objType)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Call CallObject on the remote node - pass Any directly (optimization: no marshal needed)
	// Extract client_id from context if present and pass it in the request
	req := &goverse_pb.CallObjectRequest{
		Id:       id,
		Method:   method,
		Type:     objType,
		Request:  request,
		ClientId: callcontext.ClientID(ctx),
	}

	resp, err := client.CallObject(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote CallObject failed on node %s: %w", nodeAddr, err)
	}

	return resp.Response, nil
}

// CreateObject creates a distributed object on the appropriate node based on sharding
// The object ID is determined by the type and optional custom ID
// This method routes the creation request to the correct node in the cluster
func (c *Cluster) CreateObject(ctx context.Context, objType, objID string) (string, error) {
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

	// Check if the object should be created on this node (only for node clusters)
	if c.isNode() && nodeAddr == c.getAdvertiseAddr() {
		go func() {
			c.logger.Infof("%s - Async creating object %s locally (type: %s)", c, objID, objType)
			_, err := c.node.CreateObject(ctx, objType, objID)
			if err != nil {
				c.logger.Errorf("%s - Async CreateObject %s failed: %v", c, objID, err)
			} else {
				c.logger.Infof("%s - Async CreateObject %s completed successfully", c, objID)
				// Record metrics on success if numShards is configured
				if c.numShards > 0 {
					shardID := sharding.GetShardID(objID, c.numShards)
					metrics.RecordObjectCreated(c.getAdvertiseAddr(), objType, shardID)
				}
			}
		}()
		return objID, nil
	}

	// Route to the appropriate node
	c.logger.Infof("%s - Routing CreateObject for %s to node %s", c, objID, nodeAddr)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		c.logger.Errorf("%s - CreateObject %s failed to get connection: %v", c, objID, err)
		return "", fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Execute the actual creation
	// For gates, execute synchronously since they don't risk deadlocks
	// (gates don't host objects so they don't hold object locks)
	// For nodes, execute asynchronously to prevent deadlocks when called from within object methods
	req := &goverse_pb.CreateObjectRequest{
		Type: objType,
		Id:   objID,
	}
	if c.isGate() {
		// Synchronous execution for gates
		_, err = client.CreateObject(ctx, req)
		if err != nil {
			c.logger.Errorf("%s - CreateObject %s failed on remote node: %v", c, objID, err)
			return "", fmt.Errorf("remote CreateObject failed on node %s: %w", nodeAddr, err)
		}
		c.logger.Infof("%s - CreateObject %s completed successfully on node %s", c, objID, nodeAddr)
		return objID, nil
	} else {
		// Asynchronous execution for nodes to prevent deadlocks
		go func() {
			_, err = client.CreateObject(ctx, req)
			if err != nil {
				c.logger.Errorf("%s - Async CreateObject %s failed on remote node: %v", c, objID, err)
			} else {
				c.logger.Infof("%s - Async CreateObject %s completed successfully on node %s", c, objID, nodeAddr)
			}
		}()

		// Return immediately with the object ID
		c.logger.Infof("%s - CreateObject %s initiated asynchronously", c, objID)
		return objID, nil
	}
}

// DeleteObject deletes an object from the cluster.
// It determines which node hosts the object and routes the deletion request accordingly.
// For gates, the deletion is performed synchronously. For nodes, it is performed
// asynchronously to prevent deadlocks when called from within object methods.
func (c *Cluster) DeleteObject(ctx context.Context, objID string) error {
	if objID == "" {
		return fmt.Errorf("object ID must be specified")
	}

	// Determine which node hosts this object
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, objID)
	if err != nil {
		return fmt.Errorf("cannot determine node for object %s: %w", objID, err)
	}
	// Check if the object should be deleted on this node (only for node clusters)
	if c.isNode() && nodeAddr == c.getAdvertiseAddr() {
		go func() {
			// Delete locally
			c.logger.Infof("%s - Async deleting object %s locally", c, objID)
			err := c.node.DeleteObject(ctx, objID)
			if err != nil {
				c.logger.Errorf("%s - Async DeleteObject %s failed: %v", c, objID, err)
			} else {
				c.logger.Infof("%s - Async DeleteObject %s completed successfully", c, objID)
				// Metrics are not recorded here since we don't track object types
			}
		}()
		return nil
	}

	// Route to the appropriate node
	c.logger.Infof("%s - Routing DeleteObject for %s to node %s", c, objID, nodeAddr)

	client, err := c.nodeConnections.GetConnection(nodeAddr)
	if err != nil {
		c.logger.Errorf("%s - DeleteObject %s failed to get connection: %v", c, objID, err)
		return fmt.Errorf("failed to get connection to node %s: %w", nodeAddr, err)
	}

	// Execute the actual deletion
	// For gates, execute synchronously since they don't risk deadlocks
	// (gates don't host objects so they don't hold object locks)
	// For nodes, execute asynchronously to prevent deadlocks when called from within object methods
	req := &goverse_pb.DeleteObjectRequest{
		Id: objID,
	}
	if c.isGate() {
		// Synchronous execution for gates
		_, err = client.DeleteObject(ctx, req)
		if err != nil {
			c.logger.Errorf("%s - DeleteObject %s failed on remote node: %v", c, objID, err)
			return fmt.Errorf("remote DeleteObject failed on node %s: %w", nodeAddr, err)
		}
		c.logger.Infof("%s - DeleteObject %s completed successfully on node %s", c, objID, nodeAddr)
		return nil
	} else {
		// Asynchronous execution for nodes to prevent deadlocks
		go func() {
			_, err = client.DeleteObject(ctx, req)
			if err != nil {
				c.logger.Errorf("%s - Async DeleteObject %s failed on remote node: %v", c, objID, err)
			} else {
				c.logger.Infof("%s - Async DeleteObject %s completed successfully on node %s", c, objID, nodeAddr)
			}
		}()

		// Return immediately
		c.logger.Infof("%s - DeleteObject %s initiated asynchronously", c, objID)
		return nil
	}
}

// ReliableCallObject inserts a reliable call into the database for exactly-once execution semantics.
// This is part of PR 10: Cluster-Level ReliableCallObject (Insert to DB only).
//
// Parameters:
//   - ctx: Context for the operation
//   - callID: Unique identifier for deduplication (should be generated by caller using uniqueid.UniqueId())
//   - objectType: Type of the target object
//   - objectID: ID of the target object
//   - methodName: Name of the method to invoke on the object
//   - requestData: Serialized request data (proto.Message marshaled to bytes)
//
// Returns:
//   - *object.ReliableCall: The inserted or existing call record
//   - error: Any error that occurred during the operation
//
// Behavior:
//   - Inserts the call into the database via the persistence provider
//   - If a call with the same callID already exists:
//   - If the existing call status is NOT "pending", returns the call immediately (with cached result/error)
//   - If the existing call status is "pending", returns the call (waiting for processing)
//   - This method only handles database insertion; routing and execution are handled in later PRs
//
// Example:
//
//	callID := uniqueid.UniqueId()
//	requestData, _ := proto.Marshal(myRequest)
//	rc, err := cluster.ReliableCallObject(ctx, callID, "MyType", "obj-123", "MyMethod", requestData)
//	if err != nil {
//	    return err
//	}
//	// Check if call is already completed
//	if rc.Status == "success" {
//	    // Use rc.ResultData
//	} else if rc.Status == "failed" {
//	    // Handle rc.Error
//	}
func (c *Cluster) ReliableCallObject(ctx context.Context, callID string, objectType string, objectID string, methodName string, request proto.Message) (proto.Message, goverse_pb.ReliableCallStatus, error) {
	// Validate input parameters
	if callID == "" {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("callID cannot be empty")
	}
	if objectType == "" {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("objectType cannot be empty")
	}
	if objectID == "" {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("objectID cannot be empty")
	}
	if methodName == "" {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("methodName cannot be empty")
	}

	// Only node clusters should have persistence provider
	if !c.isNode() {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("ReliableCallObject can only be called on node clusters, not gate clusters")
	}

	// Serialize request data BEFORE routing
	requestData, err := protohelper.MsgToBytes(request)
	if err != nil {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Find the target node FIRST (before INSERT)
	// This ensures INSERT happens on the owner node with proper locking
	targetNodeAddr, err := c.GetCurrentNodeForObject(ctx, objectID)
	if err != nil {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("failed to determine target node for object %s: %w", objectID, err)
	}

	c.logger.Infof("%s - Routing reliable call %s to target node %s for object %s (type: %s, method: %s)",
		c, callID, targetNodeAddr, objectID, objectType, methodName)

	node := c.GetThisNode()

	// Check if the object is on this node (local call)
	if targetNodeAddr == c.getAdvertiseAddr() {
		// Local: INSERT + execute on this node
		c.logger.Infof("%s - Processing reliable call %s locally for object %s (type: %s)", c, callID, objectID, objectType)
		resultAny, status, err := node.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			return nil, status, fmt.Errorf("failed to process reliable call locally: %w", err)
		}
		msg, err := protohelper.AnyToMsg(resultAny)
		return msg, status, err
	}

	// Remote: send RPC with full call data to owner node
	c.logger.Infof("%s - Routing reliable call %s to remote node %s for object %s (type: %s)", c, callID, targetNodeAddr, objectID, objectType)

	client, err := c.nodeConnections.GetConnection(targetNodeAddr)
	if err != nil {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("failed to get connection to node %s: %w", targetNodeAddr, err)
	}

	// Issue ReliableCallObject RPC to target node with full call data
	req := &goverse_pb.ReliableCallObjectRequest{
		CallId:      callID,
		ObjectType:  objectType,
		ObjectId:    objectID,
		MethodName:  methodName,
		RequestData: requestData,
	}

	resp, err := client.ReliableCallObject(ctx, req)
	if err != nil {
		return nil, goverse_pb.ReliableCallStatus_UNKNOWN, fmt.Errorf("failed to call ReliableCallObject RPC on node %s: %w", targetNodeAddr, err)
	}

	// Check if the response contains an error
	if resp.Error != "" {
		return nil, resp.Status, fmt.Errorf("reliable call failed on remote node: %s", resp.Error)
	}

	c.logger.Infof("%s - Reliable call %s successfully processed on remote node %s", c, callID, targetNodeAddr)
	msg, err := protohelper.AnyToMsg(resp.Result)
	return msg, resp.Status, err
}

// RegisterGateConnection registers a gate connection and returns a channel for sending messages to it
func (c *Cluster) RegisterGateConnection(gateAddr string) (chan proto.Message, error) {
	if !c.isNode() {
		return nil, fmt.Errorf("RegisterGateConnection can only be called on node clusters, not gate clusters")
	}

	c.gateChannelsMu.Lock()
	defer c.gateChannelsMu.Unlock()

	if oldCh, ok := c.gateChannels[gateAddr]; ok {
		c.logger.Warnf("Gate %s already registered, replacing connection", gateAddr)
		close(oldCh)
	}

	// Create a buffered channel to prevent blocking
	ch := make(chan proto.Message, 1024)
	c.gateChannels[gateAddr] = ch
	gateCount := len(c.gateChannels)
	nodeAddr := c.node.GetAdvertiseAddress()
	c.logger.Infof("Registered gate connection for %s", gateAddr)

	// Set metric and notify inspector
	metrics.SetNodeConnectedGates(nodeAddr, gateCount)
	c.node.NotifyRegisteredGatesChanged()

	return ch, nil
}

// UnregisterGateConnection removes a gate connection
func (c *Cluster) UnregisterGateConnection(gateAddr string, ch chan proto.Message) {
	if !c.isNode() {
		c.logger.Warnf("UnregisterGateConnection can only be called on node clusters, not gate clusters")
		return
	}

	c.gateChannelsMu.Lock()
	defer c.gateChannelsMu.Unlock()

	if currentCh, ok := c.gateChannels[gateAddr]; ok {
		if currentCh == ch {
			close(currentCh)
			delete(c.gateChannels, gateAddr)
			gateCount := len(c.gateChannels)
			nodeAddr := c.node.GetAdvertiseAddress()
			c.logger.Infof("Unregistered gate connection for %s", gateAddr)

			// Set metric and notify inspector
			metrics.SetNodeConnectedGates(nodeAddr, gateCount)
			c.node.NotifyRegisteredGatesChanged()
		} else {
			c.logger.Warnf("Skipping unregister for gate %s: channel mismatch (connection replaced)", gateAddr)
		}
	}
}

// cleanupGateChannels closes all gate channels and clears the map
// Should be called during cluster shutdown
func (c *Cluster) cleanupGateChannels() {
	if !c.isNode() {
		return
	}

	c.gateChannelsMu.Lock()
	defer c.gateChannelsMu.Unlock()

	for gateAddr, ch := range c.gateChannels {
		// Use recover to handle potential double-close panics gracefully
		closeSucceeded := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Warnf("Gate channel for %s already closed or panic during close: %v", gateAddr, r)
				}
			}()
			close(ch)
			closeSucceeded = true
		}()
		if closeSucceeded {
			c.logger.Infof("Closed gate channel for %s during shutdown", gateAddr)
		}
	}
	c.gateChannels = make(map[string]chan proto.Message)
	// Set metric to current gate count (should be 0 after cleanup)
	metrics.SetNodeConnectedGates(c.node.GetAdvertiseAddress(), len(c.gateChannels))
}

// IsGateConnected returns true if the specified gate is connected to this node
// This is useful for tests to verify gate registration has completed
func (c *Cluster) IsGateConnected(gateAddr string) bool {
	if !c.isNode() {
		return false
	}

	c.gateChannelsMu.RLock()
	defer c.gateChannelsMu.RUnlock()

	_, exists := c.gateChannels[gateAddr]
	return exists
}

// getRegisteredGateAddresses returns the list of gate addresses registered to this node
// This is used by the InspectorManager to report registered gates to the inspector.
func (c *Cluster) getRegisteredGateAddresses() []string {
	if !c.isNode() {
		return nil
	}

	c.gateChannelsMu.RLock()
	defer c.gateChannelsMu.RUnlock()

	addresses := make([]string, 0, len(c.gateChannels))
	for addr := range c.gateChannels {
		addresses = append(addresses, addr)
	}
	return addresses
}

// GetConnectedNodes returns the list of node addresses that this cluster component
// is currently connected to. This implements the clusterinfo.ClusterInfoProvider interface.
// For both nodes and gates, this returns the addresses of other nodes in the cluster.
func (c *Cluster) GetConnectedNodes() []string {
	return c.nodeConnections.GetConnectedNodeAddresses()
}

// GetRegisteredGates returns the list of gate addresses that are registered to this node.
// This implements the clusterinfo.ClusterInfoProvider interface.
// This is only meaningful for nodes; for gates, it returns nil.
func (c *Cluster) GetRegisteredGates() []string {
	return c.getRegisteredGateAddresses()
}

// GetClientCount returns the number of registered clients connected to this gate.
// This implements the clusterinfo.ClusterInfoProvider interface.
// This is only meaningful for gates; for nodes, it returns 0.
func (c *Cluster) GetClientCount() int {
	if c.gate != nil {
		return c.gate.GetClientCount()
	}
	return 0
}

// PushMessageToClient sends a message to a client by its ID
// Client IDs have the format: {gateAddress}/{uniqueId} (e.g., "localhost:7001/abc123")
// This method parses the client ID to determine the target gate and routes the message accordingly
// This method can only be called on a node cluster, not a gate cluster
func (c *Cluster) PushMessageToClient(ctx context.Context, clientID string, message proto.Message) error {
	// This method can only be called on node clusters
	if c.isGate() {
		return fmt.Errorf("PushMessageToClient can only be called on node clusters, not gate clusters")
	}

	// Parse client ID to extract gate address
	// Client ID format: gateAddress/uniqueId
	parts := strings.SplitN(clientID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid client ID format: %s (expected format: gateAddress/uniqueId)", clientID)
	}

	gateAddr := parts[0]

	// Check if the client is on a connected gate
	c.gateChannelsMu.RLock()
	gateCh, isGateConnected := c.gateChannels[gateAddr]

	if isGateConnected {
		c.logger.Infof("%s - Pushing message to client %s via connected gate %s", c, clientID, gateAddr)

		// Wrap message in ClientMessageEnvelope with client ID
		anyMsg, err := protohelper.MsgToAny(message)
		if err != nil {
			c.gateChannelsMu.RUnlock()
			return fmt.Errorf("failed to marshal message for gate: %w", err)
		}

		envelope := &goverse_pb.ClientMessageEnvelope{
			ClientId: clientID,
			Message:  anyMsg,
		}

		select {
		case gateCh <- envelope:
			// Record metric for pushed message
			metrics.RecordNodePushedMessage(c.node.GetAdvertiseAddress(), gateAddr)
			c.gateChannelsMu.RUnlock()
			return nil
		default:
			// Record dropped message metric when channel is full
			metrics.RecordNodeDroppedMessage(c.node.GetAdvertiseAddress(), gateAddr)
			c.gateChannelsMu.RUnlock()
			return fmt.Errorf("gate %s message channel is full", gateAddr)
		}
	}
	c.gateChannelsMu.RUnlock()

	// Gate not connected to this node
	return fmt.Errorf("gate %s not connected to this node, cannot push message to client %s", gateAddr, clientID)
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
	nodesPrefix := c.etcdManager.GetPrefix() + "/nodes/"
	key := nodesPrefix + c.getAdvertiseAddr()
	value := c.getAdvertiseAddr()

	_, err := c.etcdManager.RegisterKeyLease(ctx, key, value, etcdmanager.NodeLeaseTTL)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	return nil
}

// unregisterNode unregisters this node from etcd using the shared lease API
func (c *Cluster) unregisterNode(ctx context.Context) error {
	nodesPrefix := c.etcdManager.GetPrefix() + "/nodes/"
	key := nodesPrefix + c.getAdvertiseAddr()
	return c.etcdManager.UnregisterKeyLease(ctx, key)
}

// registerGate registers this gate with etcd using the shared lease API
func (c *Cluster) registerGate(ctx context.Context) error {
	gatesPrefix := c.etcdManager.GetPrefix() + "/gates/"
	key := gatesPrefix + c.getAdvertiseAddr()
	value := c.getAdvertiseAddr()

	_, err := c.etcdManager.RegisterKeyLease(ctx, key, value, etcdmanager.NodeLeaseTTL)
	if err != nil {
		return fmt.Errorf("failed to register gate: %w", err)
	}

	return nil
}

// unregisterGate unregisters this gate from etcd using the shared lease API
func (c *Cluster) unregisterGate(ctx context.Context) error {
	gatesPrefix := c.etcdManager.GetPrefix() + "/gates/"
	key := gatesPrefix + c.getAdvertiseAddr()
	return c.etcdManager.UnregisterKeyLease(ctx, key)
}

// closeEtcd closes the etcd connection
func (c *Cluster) closeEtcd() error {
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

// GetGates returns a list of all registered gates
func (c *Cluster) GetGates() []string {
	return c.consensusManager.GetGates()
}

// GetLeaderNode returns the leader node address.
// The leader is elected via a race-safe mechanism using etcd transactions.
// Returns an empty string if no leader has been elected yet or if consensus manager is not set.
func (c *Cluster) GetLeaderNode() string {
	return c.consensusManager.GetLeaderNode()
}

// IsLeader returns true if this node is the cluster leader
func (c *Cluster) IsLeader() bool {
	if !c.isNode() {
		return false
	}
	leaderNode := c.GetLeaderNode()
	return leaderNode != "" && leaderNode == c.getAdvertiseAddr()
}

// GetShardMapping retrieves the current shard mapping
func (c *Cluster) GetShardMapping(ctx context.Context) *consensusmanager.ShardMapping {
	return c.consensusManager.GetShardMapping()
}

// IsShardMappingComplete returns true if all shards have CurrentNode == TargetNode,
// meaning no shards are in migration state and the cluster has fully stabilized.
// numShards is the expected total number of shards.
func (c *Cluster) IsShardMappingComplete(ctx context.Context, numShards int) bool {
	mapping := c.consensusManager.GetShardMapping()
	if mapping == nil {
		return false
	}
	return mapping.AllShardsHaveMatchingCurrentAndTarget(numShards)
}

// GetCurrentNodeForObject returns the node address that should handle the given object ID
// Supports three formats:
// 1. Fixed-shard format: "shard#<shardID>/<objectID>" - maps to specific shard via shard mapping
// 2. Fixed-node format: "<nodeAddress>/<objectID>" - routes directly to specified node
// 3. Regular format: any other ID - uses hash-based shard assignment
func (c *Cluster) GetCurrentNodeForObject(ctx context.Context, objectID string) (string, error) {
	// Delegate to consensus manager which handles all validation
	return c.consensusManager.GetCurrentNodeForObject(objectID)
}

// GetNodeForShard returns the node address that owns the given shard
func (c *Cluster) GetNodeForShard(ctx context.Context, shardID int) (string, error) {
	return c.consensusManager.GetNodeForShard(shardID)
}

// startClusterManagement starts a background goroutine that periodically manages shard mapping
// If this node is the leader and the node list has been stable for the configured node stability duration,
// it will update/initialize the shard mapping.
// If this node is not the leader, it will periodically refresh the shard mapping from etcd.
func (c *Cluster) startClusterManagement(ctx context.Context) error {
	if c.clusterManagementRunning {
		c.logger.Warnf("%s - Shard mapping management already running", c)
		return nil
	}

	c.clusterManagementCtx, c.clusterManagementCancel = context.WithCancel(ctx)
	c.clusterManagementRunning = true

	go c.clusterManagementLoop()
	c.logger.Infof("%s - Started shard mapping management (check interval: %v)", c,
		c.getEffectiveShardMappingCheckInterval())

	return nil
}

// stopShardMappingManagement stops the shard mapping management background task
func (c *Cluster) stopShardMappingManagement() {
	if c.clusterManagementCancel != nil {
		c.clusterManagementCancel()
		c.clusterManagementCancel = nil
	}
	c.clusterManagementRunning = false
	c.logger.Infof("%s - Stopped shard mapping management", c)
}

// clusterManagementLoop is the background loop that manages shard mapping
func (c *Cluster) clusterManagementLoop() {
	ticker := time.NewTicker(c.getEffectiveShardMappingCheckInterval())
	defer ticker.Stop()

	for {
		select {
		case <-c.clusterManagementCtx.Done():
			c.logger.Debugf("%s - Shard mapping management loop stopped", c)
			return
		case <-ticker.C:
			c.clusterManagementTick()
		}
	}
}

// clusterManagementTick performs a single tick of shard mapping management
func (c *Cluster) clusterManagementTick() {
	startTime := time.Now()
	c.handleShardMappingCheck()
	c.checkAndMarkReady()
	c.updateMetrics()
	c.logger.Infof("%s - Cluster management tick took %d ms", c, time.Since(startTime).Milliseconds())
}

// handleShardMappingCheck checks and updates shard mapping based on leadership and node stability
func (c *Cluster) handleShardMappingCheck() {
	ctx := c.clusterManagementCtx

	// Try to become leader if no leader exists or current leader is dead
	if err := c.tryBecomeLeader(); err != nil {
		c.logger.Warnf("%s - Failed to try become leader: %v", c, err)
	}

	// If leader made changes to cluster state, skip other operations this cycle
	// to allow the cluster state to stabilize before proceeding. Always update
	// shard metrics so monitoring reflects the latest assignment even during
	// transient state changes.
	stateChanged := c.leaderShardManagementLogic(ctx)
	if stateChanged {
		c.logger.Infof("%s - Cluster state changed by leader, waiting for next cycle", c)
		// Ensure metrics are updated even when leader changed state to keep
		// Prometheus/monitoring in sync with the in-memory state.
		return
	}

	// Node - Claim ownership of shards that belong to this node
	c.claimShardOwnership(ctx)
	// Node - Remove objects that no longer belong to this node
	c.removeObjectsNotBelongingToThisNode(ctx)
	// Node - Release ownership of shards that are no longer needed
	c.releaseShardOwnership(ctx)
	// Auto-load configured objects after claiming shards
	c.loadAutoLoadObjects(ctx)
	// Node and Gate - Update node connections
	c.updateNodeConnections()
	// Gate - Register with nodes
	c.registerGateWithNodes(ctx)
}

func (c *Cluster) tryBecomeLeader() error {
	if !c.isNode() {
		return nil
	}
	return c.consensusManager.TryBecomeLeader(c.clusterManagementCtx)
}

func (c *Cluster) updateMetrics() {
	c.consensusManager.UpdateShardMetrics()
}

// claimShardOwnership claims ownership of shards when cluster state is stable
func (c *Cluster) claimShardOwnership(ctx context.Context) {
	if c.isGate() {
		// Gates don't host objects, so they don't need to claim shard ownership
		return
	}
	// Claim ownership of shards where this node is the target
	// The stability check is now performed inside ClaimShardsForNode
	err := c.consensusManager.ClaimShardsForNode(ctx)
	if err != nil {
		c.logger.Warnf("%s - Failed to claim shard ownership: %v", c, err)
		return
	}
}

// loadAutoLoadObjects creates objects specified in the auto_load_objects config.
// This should be called after the node has claimed its shards.
func (c *Cluster) loadAutoLoadObjects(ctx context.Context) {
	if c.isGate() {
		// Gates don't host objects, so they don't need to auto-load objects
		return
	}

	if len(c.config.AutoLoadObjects) == 0 {
		c.logger.Warnf("%s - No auto-load objects configured, skipping", c)
		return
	}

	// Check if cluster state is stable without cloning
	if !c.consensusManager.IsStateStable() {
		c.logger.Warnf("%s - Skipping auto-load: cluster state is not stable yet", c)
		return
	}

	c.logger.Infof("Auto-loading %d configured objects", len(c.config.AutoLoadObjects))

	for _, cfg := range c.config.AutoLoadObjects {
		if cfg.PerShard {
			// Create one object per shard using fixed-shard ID format
			c.loadPerShardObject(ctx, cfg)
		} else if cfg.PerNode {
			// Create one object per node using fixed-node ID format
			c.loadPerNodeObject(ctx, cfg)
		} else {
			// Create a single object with the specified ID
			c.loadSingleObject(ctx, cfg)
		}
	}
}

// loadSingleObject creates a single auto-load object with the specified ID
func (c *Cluster) loadSingleObject(ctx context.Context, cfg config.AutoLoadObjectConfig) {
	// Check if this node owns the shard for this object
	shardID := sharding.GetShardID(cfg.ID, c.numShards)
	nodeAddr, err := c.GetNodeForShard(ctx, shardID)
	if err != nil {
		c.logger.Errorf("Failed to determine node for auto-load object %s (%s): %v", cfg.ID, cfg.Type, err)
		return
	}

	localAddr := c.getAdvertiseAddr()
	if nodeAddr != localAddr {
		return
	}

	// Create or load the object locally
	_, err = c.CreateObject(ctx, cfg.Type, cfg.ID)
	if err != nil {
		c.logger.Errorf("Failed to auto-load object %s (%s): %v", cfg.ID, cfg.Type, err)
		return
	}

	c.logger.Infof("Auto-loaded object %s (%s)", cfg.ID, cfg.Type)
}

// loadPerShardObject creates one object per shard that this node owns
func (c *Cluster) loadPerShardObject(ctx context.Context, cfg config.AutoLoadObjectConfig) {
	localAddr := c.getAdvertiseAddr()
	loadedCount := 0

	// Iterate through all shards and create objects for shards owned by this node
	for shardID := 0; shardID < c.numShards; shardID++ {
		nodeAddr, err := c.GetNodeForShard(ctx, shardID)
		if err != nil {
			c.logger.Errorf("Failed to determine node for shard %d: %v", shardID, err)
			continue
		}

		// Only create objects for shards owned by this node
		if nodeAddr != localAddr {
			continue
		}

		// Generate fixed-shard object ID: shard#<shardID>/<baseName>
		objectID := fmt.Sprintf("shard#%d/%s", shardID, cfg.ID)

		// Create or load the object locally
		_, err = c.CreateObject(ctx, cfg.Type, objectID)
		if err != nil {
			c.logger.Errorf("Failed to auto-load per-shard object %s (%s): %v", objectID, cfg.Type, err)
			continue
		}

		loadedCount++
	}

	c.logger.Infof("Auto-loaded %d per-shard objects of type %s (base name: %s)", loadedCount, cfg.Type, cfg.ID)
}

// loadPerNodeObject creates one object per node using fixed-node ID format
func (c *Cluster) loadPerNodeObject(ctx context.Context, cfg config.AutoLoadObjectConfig) {
	localAddr := c.getAdvertiseAddr()
	if localAddr == "" {
		c.logger.Errorf("Cannot create per-node object %s (%s): node address is empty", cfg.ID, cfg.Type)
		return
	}

	// Generate fixed-node object ID: <nodeAddress>/<baseName>
	objectID := fmt.Sprintf("%s/%s", localAddr, cfg.ID)

	// Create or load the object locally
	_, err := c.CreateObject(ctx, cfg.Type, objectID)
	if err != nil {
		c.logger.Errorf("Failed to auto-load per-node object %s (%s): %v", objectID, cfg.Type, err)
		return
	}

	c.logger.Infof("Auto-loaded per-node object %s (%s)", objectID, cfg.Type)
}

// releaseShardOwnership releases ownership of shards when:
// - CurrentNode is this node
// - TargetNode is another node (not empty and not this node)
// - No objects exist on this node for that shard
// Note: This is a node-only operation. Gates don't host objects so they skip this.
func (c *Cluster) releaseShardOwnership(ctx context.Context) {
	// Gates don't host objects, so they don't need to release shard ownership
	if c.isGate() {
		return
	}

	// Count objects per shard on this node
	objectIDs := c.node.ListObjectIDs()
	localObjectsPerShard := make(map[int]int)
	for _, objectID := range objectIDs {
		// Skip client objects (those with "/" in ID)
		if strings.Contains(objectID, "/") {
			continue
		}
		shardID := sharding.GetShardID(objectID, c.numShards)
		localObjectsPerShard[shardID]++
	}

	// Release ownership of shards where this node is no longer needed
	err := c.consensusManager.ReleaseShardsForNode(ctx, localObjectsPerShard)
	if err != nil {
		c.logger.Warnf("%s - Failed to release shard ownership: %v", c, err)
	}
}

// removeObjectsNotBelongingToThisNode removes objects whose shards no longer belong to this node
// Note: This is a node-only operation. Gates don't host objects so they skip this.
func (c *Cluster) removeObjectsNotBelongingToThisNode(ctx context.Context) {
	// Gates don't host objects, so they don't need to remove objects
	if c.isGate() {
		return
	}

	localAddr := c.getAdvertiseAddr()

	// Check if cluster state is stable without cloning
	if !c.consensusManager.IsStateStable() {
		c.logger.Debugf("%s - Skipping object removal: cluster state is not stable yet", c)
		return
	}

	// Get all object IDs on this node
	objectIDs := c.node.ListObjectIDs()

	// Ask ConsensusManager to determine which objects should be evicted
	// This is more efficient than cloning the entire cluster state
	// ConsensusManager will also check if this node is in the cluster
	objectsToEvict := c.consensusManager.GetObjectsToEvict(localAddr, objectIDs)
	c.logger.Infof("%s - Found %d objects out of %d to evict from this node", c, len(objectsToEvict), len(objectIDs))

	// Remove each object that should be evicted
	for _, objectID := range objectsToEvict {
		shardID := sharding.GetShardID(objectID, c.numShards)
		c.logger.Infof("%s - Removing object %s (shard %d) as it no longer belongs to this node", c,
			objectID, shardID)

		err := c.node.DeleteObject(ctx, objectID)
		if err != nil {
			c.logger.Errorf("%s - Failed to delete object %s: %v", c, objectID, err)
		} else {
			c.logger.Infof("%s - Successfully removed object %s", c, objectID)
			// Metrics are not recorded for evicted objects since we don't track object types here
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

	c.logger.Infof("%s - ACTING AS LEADER!", c)

	if !c.consensusManager.IsStateStable() {
		c.logger.Warnf("Cluster state not yet stable for this node, waiting before updating shard mapping")
		return false
	}

	// First priority: reassign shards whose target nodes are no longer in the cluster
	// This handles node failures and ensures all shards have valid target nodes
	reassignedCount, err := c.consensusManager.ReassignShardTargetNodes(ctx)
	if reassignedCount > 0 {
		c.logger.Infof("%s - Reassigned %d shards to new target nodes", c, reassignedCount)
		return true // State changed, allow it to stabilize before next operation
	}
	if err != nil {
		c.logger.Errorf("%s - Failed to reassign shard target nodes: %v", c, err)
		return false
	}

	// Second priority: rebalance shards to improve cluster balance
	// Only attempt if no reassignment was needed (cluster is stable)
	rebalanced, err := c.consensusManager.RebalanceShards(ctx)
	if err != nil {
		c.logger.Errorf("%s - Failed to rebalance shards: %v", c, err)
		return false
	}

	if rebalanced {
		c.logger.Infof("%s - Rebalanced one shard to improve cluster balance", c)
		return true // State changed
	}

	return false // No changes made
}

// startNodeConnections initializes and starts the node connections manager
// This should be called after StartWatching is started
func (c *Cluster) startNodeConnections(ctx context.Context) error {
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
	allNodes := c.GetNodes()
	thisNodeAddr := c.getAdvertiseAddr()

	// Filter out this node's address
	otherNodes := make([]string, 0, len(allNodes))
	for _, nodeAddr := range allNodes {
		if nodeAddr != thisNodeAddr {
			otherNodes = append(otherNodes, nodeAddr)
		}
	}

	c.nodeConnections.SetNodes(otherNodes)

	// Notify inspector that connected nodes have changed in background
	if c.isNode() {
		c.node.NotifyConnectedNodesChanged()
	}
	if c.isGate() {
		c.gate.NotifyConnectedNodesChanged()
	}
}

// GetNodeConnections returns the node connections manager
func (c *Cluster) GetNodeConnections() *nodeconnections.NodeConnections {
	return c.nodeConnections
}

// generateAutoLoadIDs generates object IDs for an auto-load configuration.
// Helper function to avoid code duplication.
func (c *Cluster) generateAutoLoadIDs(cfg config.AutoLoadObjectConfig) []string {
	var ids []string

	if cfg.PerShard {
		// Generate per-shard IDs for all shards
		for shardID := 0; shardID < c.numShards; shardID++ {
			objectID := fmt.Sprintf("shard#%d/%s", shardID, cfg.ID)
			ids = append(ids, objectID)
		}
	} else if cfg.PerNode {
		// Generate per-node IDs for all nodes
		nodes := c.GetNodes()
		for _, nodeAddr := range nodes {
			objectID := fmt.Sprintf("%s/%s", nodeAddr, cfg.ID)
			ids = append(ids, objectID)
		}
	} else {
		// Global object - single ID
		ids = append(ids, cfg.ID)
	}

	return ids
}

// GetAutoLoadObjectIDsByType returns all object IDs for auto-load objects of the specified type.
// For global objects, it returns a single ID.
// For per-shard objects, it returns IDs in the format: shard#<N>/<baseName>
// For per-node objects, it returns IDs in the format: <nodeAddr>/<baseName>
//
// This allows user code to discover and interact with auto-load objects at runtime.
func (c *Cluster) GetAutoLoadObjectIDsByType(objType string) []string {
	var result []string

	for _, cfg := range c.config.AutoLoadObjects {
		if cfg.Type != objType {
			continue
		}
		result = append(result, c.generateAutoLoadIDs(cfg)...)
	}

	return result
}

// GetAutoLoadObjectIDs returns a map of object type to list of object IDs for all auto-load objects.
// This provides a convenient way to discover all auto-load objects configured in the cluster.
func (c *Cluster) GetAutoLoadObjectIDs() map[string][]string {
	result := make(map[string][]string)

	for _, cfg := range c.config.AutoLoadObjects {
		ids := c.generateAutoLoadIDs(cfg)
		// Append to existing list if type already exists
		result[cfg.Type] = append(result[cfg.Type], ids...)
	}

	return result
}

// registerGateWithNodes registers this gate with all nodes that haven't been registered yet
// This is called periodically from the cluster management loop for gate clusters
func (c *Cluster) registerGateWithNodes(ctx context.Context) {
	if !c.isGate() {
		return
	}

	// Delegate to gate
	allConnections := c.nodeConnections.GetAllConnections()
	c.gate.RegisterWithNodes(ctx, allConnections)
}

func (c *Cluster) GetClusterStateStabilityDurationForTesting() time.Duration {
	return c.consensusManager.GetClusterStateStabilityDurationForTesting()
}
