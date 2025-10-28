package cluster

import (
	"context"
	"fmt"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
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
	logger      *logger.Logger
}

func init() {
	thisCluster.logger = logger.NewLogger("Cluster")
}

func Get() *Cluster {
	return &thisCluster
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
