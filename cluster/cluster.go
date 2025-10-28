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
	
	// Assign the cluster's etcdManager to the node
	if c.etcdManager != nil {
		n.SetEtcdManager(c.etcdManager)
	}
	
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
	// If node is already set, assign the manager to it as well
	if c.thisNode != nil {
		c.thisNode.SetEtcdManager(mgr)
	}
}

// GetEtcdManager returns the cluster's etcd manager
func (c *Cluster) GetEtcdManager() *etcdmanager.EtcdManager {
	return c.etcdManager
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
