package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestDistributedCreateObject tests that CreateObject correctly routes to the appropriate node
// This test requires a running etcd instance at localhost:2379
func TestDistributedCreateObject(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create cluster 1
	cluster1 := newClusterForTesting("TestCluster1")
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	node1 := node.NewNode("localhost:47001")
	cluster1.SetThisNode(node1)
	node1.RegisterObjectType((*TestDistributedObject)(nil))

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Create cluster 2
	cluster2 := newClusterForTesting("TestCluster2")
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}
	cluster2.SetEtcdManager(etcdMgr2)

	node2 := node.NewNode("localhost:47002")
	cluster2.SetThisNode(node2)
	node2.RegisterObjectType((*TestDistributedObject)(nil))

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Start NodeConnections for both clusters
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster1: %v", err)
	}
	defer cluster1.StopNodeConnections()

	err = cluster2.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster2: %v", err)
	}
	defer cluster2.StopNodeConnections()

	// Wait for connections to be established
	time.Sleep(1 * time.Second)

	// Start shard mapping management
	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster1: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	err = cluster2.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster2: %v", err)
	}
	defer cluster2.StopShardMappingManagement()

	// Wait for shard mapping to be initialized
	time.Sleep(12 * time.Second)

	// Test CreateObject from cluster1
	t.Run("CreateObject from cluster1", func(t *testing.T) {
		objID, err := cluster1.CreateObject(ctx, "TestDistributedObject", "test-obj-1", nil)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		if objID == "" {
			t.Error("CreateObject returned empty ID")
		}

		t.Logf("Created object %s", objID)

		// Determine which node should have the object
		targetNode, err := cluster1.GetNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetNodeForObject failed: %v", err)
		}

		t.Logf("Object %s should be on node %s", objID, targetNode)

		// Verify the object was created on the correct node
		var numObjects int
		if targetNode == "localhost:47001" {
			numObjects = node1.NumObjects()
		} else if targetNode == "localhost:47002" {
			numObjects = node2.NumObjects()
		}

		if numObjects == 0 {
			t.Errorf("Object should have been created on node %s", targetNode)
		}
	})

	// Test CreateObject from cluster2
	t.Run("CreateObject from cluster2", func(t *testing.T) {
		objID, err := cluster2.CreateObject(ctx, "TestDistributedObject", "test-obj-2", nil)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		if objID == "" {
			t.Error("CreateObject returned empty ID")
		}

		t.Logf("Created object %s", objID)

		// Determine which node should have the object
		targetNode, err := cluster2.GetNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetNodeForObject failed: %v", err)
		}

		t.Logf("Object %s should be on node %s", objID, targetNode)
	})
}

// TestDistributedObject is a simple object for testing distributed creation
type TestDistributedObject struct {
	object.BaseObject
}

func (o *TestDistributedObject) OnCreated() {}
