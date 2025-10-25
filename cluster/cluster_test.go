package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGet(t *testing.T) {
	// Test that Get returns a singleton
	cluster1 := Get()
	cluster2 := Get()

	if cluster1 != cluster2 {
		t.Error("Get() should return the same cluster instance")
	}
}

func TestSetThisNode(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}
	cluster.logger = logger.NewLogger("TestCluster")
	
	n := node.NewNode("test-address")
	cluster.SetThisNode(n)

	if cluster.GetThisNode() != n {
		t.Error("GetThisNode() should return the node set by SetThisNode()")
	}
}

func TestSetThisNode_Panic(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}
	cluster.logger = logger.NewLogger("TestCluster")
	
	n1 := node.NewNode("test-address-1")
	cluster.SetThisNode(n1)

	// Setting the node again should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetThisNode should panic when called twice")
		}
	}()

	n2 := node.NewNode("test-address-2")
	cluster.SetThisNode(n2)
}

func TestGetThisNode_NotSet(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	if cluster.GetThisNode() != nil {
		t.Error("GetThisNode() should return nil when node is not set")
	}
}

func TestCallObject_NodeNotSet(t *testing.T) {
	// Create a new cluster for testing
	cluster := &Cluster{}

	ctx := context.Background()
	_, err := cluster.CallObject(ctx, "test-id", "TestMethod", &emptypb.Empty{})

	if err == nil {
		t.Error("CallObject should return error when ThisNode is not set")
	}

	expectedErr := "ThisNode is not set"
	if err.Error() != expectedErr {
		t.Errorf("CallObject error = %v; want %v", err.Error(), expectedErr)
	}
}
