package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/proto"
)

var (
	thisCluster Cluster
)

const (
	// ShardMappingCheckInterval is how often to check if shard mapping needs updating
	ShardMappingCheckInterval = 5 * time.Second
	// NodeStabilityDuration is how long the node list must be stable before updating shard mapping
	NodeStabilityDuration = 10 * time.Second
)

type Cluster struct {
	thisNode              *node.Node
	etcdManager           *etcdmanager.EtcdManager
	shardMapper           *sharding.ShardMapper
	logger                *logger.Logger
	shardMappingCtx       context.Context
	shardMappingCancel    context.CancelFunc
	shardMappingRunning   bool
}

func init() {
	thisCluster.logger = logger.NewLogger("Cluster")
}

func Get() *Cluster {
	return &thisCluster
}

// newClusterForTesting creates a new cluster instance for testing with an initialized logger
func newClusterForTesting(name string) *Cluster {
	return &Cluster{
		logger: logger.NewLogger(name),
	}
}

func (c *Cluster) SetThisNode(n *node.Node) {
	if c.thisNode != nil {
		panic("ThisNode is already set")
	}
	c.thisNode = n
	c.logger.Infof("This Node is %s", n)
}

func (c *Cluster) GetThisNode() *node.Node {
	return c.thisNode
}

func (c *Cluster) CallObject(ctx context.Context, id string, method string, request proto.Message) (proto.Message, error) {
	if c.thisNode == nil {
		return nil, fmt.Errorf("ThisNode is not set")
	}

	return c.thisNode.CallObject(ctx, id, method, request)
}

// SetEtcdManager sets the etcd manager for the cluster
func (c *Cluster) SetEtcdManager(mgr *etcdmanager.EtcdManager) {
	c.etcdManager = mgr
	// Initialize shard mapper when etcd manager is set
	c.shardMapper = sharding.NewShardMapper(mgr)
}

// GetEtcdManager returns the cluster's etcd manager
func (c *Cluster) GetEtcdManager() *etcdmanager.EtcdManager {
	return c.etcdManager
}

// ConnectEtcd connects to etcd
func (c *Cluster) ConnectEtcd() error {
	if c.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}
	return c.etcdManager.Connect()
}

// RegisterNode registers this node with etcd
func (c *Cluster) RegisterNode(ctx context.Context) error {
	if c.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}
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

// WatchNodes starts watching for node changes in etcd
func (c *Cluster) WatchNodes(ctx context.Context) error {
	if c.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}
	return c.etcdManager.WatchNodes(ctx)
}

// GetNodes returns a list of all registered nodes
func (c *Cluster) GetNodes() []string {
	if c.etcdManager == nil {
		return []string{}
	}
	return c.etcdManager.GetNodes()
}

// GetLeaderNode returns the leader node address.
// The leader is the node with the smallest advertised address in lexicographic order.
// Returns an empty string if there are no registered nodes or if etcd manager is not set.
func (c *Cluster) GetLeaderNode() string {
	if c.etcdManager == nil {
		return ""
	}
	return c.etcdManager.GetLeaderNode()
}

// IsLeader returns true if this node is the cluster leader
func (c *Cluster) IsLeader() bool {
	if c.thisNode == nil {
		return false
	}
	leaderNode := c.GetLeaderNode()
	return leaderNode != "" && leaderNode == c.thisNode.GetAdvertiseAddress()
}

// InitializeShardMapping creates and stores the initial shard mapping in etcd
// This should only be called by the leader node
func (c *Cluster) InitializeShardMapping(ctx context.Context) error {
	if !c.IsLeader() {
		return fmt.Errorf("only the leader can initialize shard mapping")
	}

	if c.shardMapper == nil {
		return fmt.Errorf("shard mapper not initialized")
	}

	nodes := c.GetNodes()
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes available to initialize shard mapping")
	}

	c.logger.Infof("Initializing shard mapping with %d nodes", len(nodes))

	mapping, err := c.shardMapper.CreateShardMapping(ctx, nodes)
	if err != nil {
		return fmt.Errorf("failed to create shard mapping: %w", err)
	}

	err = c.shardMapper.StoreShardMapping(ctx, mapping)
	if err != nil {
		return fmt.Errorf("failed to store shard mapping: %w", err)
	}

	c.logger.Infof("Successfully initialized shard mapping (version %d)", mapping.Version)
	return nil
}

// UpdateShardMapping updates the shard mapping when nodes are added or removed
// This should only be called by the leader node
func (c *Cluster) UpdateShardMapping(ctx context.Context) error {
	if !c.IsLeader() {
		return fmt.Errorf("only the leader can update shard mapping")
	}

	if c.shardMapper == nil {
		return fmt.Errorf("shard mapper not initialized")
	}

	nodes := c.GetNodes()
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes available to update shard mapping")
	}

	c.logger.Infof("Updating shard mapping with %d nodes", len(nodes))

	mapping, err := c.shardMapper.UpdateShardMapping(ctx, nodes)
	if err != nil {
		return fmt.Errorf("failed to update shard mapping: %w", err)
	}

	err = c.shardMapper.StoreShardMapping(ctx, mapping)
	if err != nil {
		return fmt.Errorf("failed to store shard mapping: %w", err)
	}

	c.logger.Infof("Successfully updated shard mapping (version %d)", mapping.Version)
	return nil
}

// GetShardMapping retrieves the current shard mapping
func (c *Cluster) GetShardMapping(ctx context.Context) (*sharding.ShardMapping, error) {
	if c.shardMapper == nil {
		return nil, fmt.Errorf("shard mapper not initialized")
	}

	return c.shardMapper.GetShardMapping(ctx)
}

// GetNodeForObject returns the node address that should handle the given object ID
func (c *Cluster) GetNodeForObject(ctx context.Context, objectID string) (string, error) {
	if c.shardMapper == nil {
		return "", fmt.Errorf("shard mapper not initialized")
	}

	return c.shardMapper.GetNodeForObject(ctx, objectID)
}

// GetNodeForShard returns the node address that owns the given shard
func (c *Cluster) GetNodeForShard(ctx context.Context, shardID int) (string, error) {
	if c.shardMapper == nil {
		return "", fmt.Errorf("shard mapper not initialized")
	}

	return c.shardMapper.GetNodeForShard(ctx, shardID)
}

// InvalidateShardMappingCache clears the local shard mapping cache
// This forces the next GetShardMapping call to reload from etcd
func (c *Cluster) InvalidateShardMappingCache() {
	if c.shardMapper != nil {
		c.shardMapper.InvalidateCache()
	}
}

// StartShardMappingManagement starts a background goroutine that periodically manages shard mapping
// If this node is the leader and the node list has been stable for NodeStabilityDuration,
// it will update/initialize the shard mapping.
// If this node is not the leader, it will periodically refresh the shard mapping from etcd.
func (c *Cluster) StartShardMappingManagement(ctx context.Context) error {
	if c.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}
	if c.shardMapper == nil {
		return fmt.Errorf("shard mapper not initialized")
	}
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

	if c.IsLeader() {
		// Leader: manage shard mapping if nodes are stable
		if c.etcdManager.IsNodeListStable(NodeStabilityDuration) {
			nodes := c.GetNodes()
			if len(nodes) == 0 {
				c.logger.Debugf("No nodes available, skipping shard mapping update")
				return
			}

			c.logger.Debugf("Node list stable, managing shard mapping as leader")
			
			// Try to get existing mapping first
			_, err := c.shardMapper.GetShardMapping(ctx)
			if err != nil {
				// No existing mapping, initialize
				c.logger.Infof("No existing shard mapping, initializing")
				err = c.InitializeShardMapping(ctx)
				if err != nil {
					c.logger.Errorf("Failed to initialize shard mapping: %v", err)
				}
			} else {
				// Existing mapping, update if needed
				err = c.UpdateShardMapping(ctx)
				if err != nil {
					c.logger.Errorf("Failed to update shard mapping: %v", err)
				}
			}
		} else {
			c.logger.Debugf("Node list not yet stable, waiting before updating shard mapping")
		}
	} else {
		// Not leader: just refresh shard mapping from etcd
		c.logger.Debugf("Not leader, refreshing shard mapping from etcd")
		
		// Invalidate cache to force refresh from etcd
		c.shardMapper.InvalidateCache()
		
		// Try to load the mapping
		_, err := c.shardMapper.GetShardMapping(ctx)
		if err != nil {
			c.logger.Debugf("Could not load shard mapping: %v", err)
		} else {
			c.logger.Debugf("Refreshed shard mapping from etcd")
		}
	}
}
