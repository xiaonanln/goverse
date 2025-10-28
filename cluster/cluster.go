package cluster

import (
	"context"
	"fmt"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/proto"
)

var (
	thisCluster Cluster
)

type Cluster struct {
	thisNode    *node.Node
	etcdManager *etcdmanager.EtcdManager
	shardMapper *sharding.ShardMapper
	logger      *logger.Logger
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
