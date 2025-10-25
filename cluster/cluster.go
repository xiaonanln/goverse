package cluster

import (
	"context"
	"fmt"

	"github.com/simonlingoogle/pulse/node"
	"github.com/simonlingoogle/pulse/util/logger"
	"google.golang.org/protobuf/proto"
)

var (
	thisCluster Cluster
)

type Cluster struct {
	thisNode *node.Node
	logger   *logger.Logger
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
