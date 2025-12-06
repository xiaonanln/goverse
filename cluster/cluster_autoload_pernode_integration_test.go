package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestNodeSpecificAutoLoadObject is a test object for per-node auto-load testing
type TestNodeSpecificAutoLoadObject struct {
	object.BaseObject
}

func (o *TestNodeSpecificAutoLoadObject) OnCreated() {}

var _ object.Object = (*TestNodeSpecificAutoLoadObject)(nil)

// TestClusterAutoLoadObjects_PerNodeConfig verifies that per-node auto-load objects
// are only created on the specific node that owns the shard, while cluster-level objects
// are created on nodes that own their respective shards
func TestClusterAutoLoadObjects_PerNodeConfig(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get object IDs for testing
	clusterObjID := testutil.GetObjectIDForShard(5, "ClusterWideObj")
	node1ObjID := testutil.GetObjectIDForShard(10, "Node1SpecificObj")
	node2ObjID := testutil.GetObjectIDForShard(15, "Node2SpecificObj")

	// Create two nodes
	node1Addr := "localhost:47600"
	node2Addr := "localhost:47601"

	n1 := node.NewNode(node1Addr, testutil.TestNumShards)
	n2 := node.NewNode(node2Addr, testutil.TestNumShards)

	// Register test object types on both nodes
	n1.RegisterObjectType((*TestAutoLoadObject)(nil))
	n1.RegisterObjectType((*TestNodeSpecificAutoLoadObject)(nil))
	n2.RegisterObjectType((*TestAutoLoadObject)(nil))
	n2.RegisterObjectType((*TestNodeSpecificAutoLoadObject)(nil))

	// Start nodes
	err := n1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer n1.Stop(ctx)

	err = n2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer n2.Stop(ctx)

	// Create cluster configs with cluster-level auto-load and node-specific auto-load
	// Node 1 gets: cluster-level + node1-specific
	cfg1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			// Cluster-level: should be created on whichever node owns the shard
			{Type: "TestAutoLoadObject", ID: clusterObjID},
			// Node1-specific: should only be created on node1
			{Type: "TestNodeSpecificAutoLoadObject", ID: node1ObjID},
		},
	}

	// Node 2 gets: cluster-level + node2-specific
	cfg2 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			// Cluster-level: should be created on whichever node owns the shard
			{Type: "TestAutoLoadObject", ID: clusterObjID},
			// Node2-specific: should only be created on node2
			{Type: "TestNodeSpecificAutoLoadObject", ID: node2ObjID},
		},
	}

	// Create clusters
	c1, err := NewClusterWithNode(cfg1, n1)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer c1.Stop(ctx)

	c2, err := NewClusterWithNode(cfg2, n2)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer c2.Stop(ctx)

	// Start clusters
	err = c1.Start(ctx, n1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}

	err = c2.Start(ctx, n2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, c1, c2)

	// Give time for auto-load to complete
	time.Sleep(3 * time.Second)

	// Check which node owns which shard
	clusterObjShard := 5
	node1ObjShard := 10
	node2ObjShard := 15

	nodeForClusterObj, err := c1.GetNodeForShard(ctx, clusterObjShard)
	if err != nil {
		t.Fatalf("Failed to get node for cluster object shard: %v", err)
	}

	nodeForNode1Obj, err := c1.GetNodeForShard(ctx, node1ObjShard)
	if err != nil {
		t.Fatalf("Failed to get node for node1 object shard: %v", err)
	}

	nodeForNode2Obj, err := c1.GetNodeForShard(ctx, node2ObjShard)
	if err != nil {
		t.Fatalf("Failed to get node for node2 object shard: %v", err)
	}

	t.Logf("Cluster object shard %d owned by: %s", clusterObjShard, nodeForClusterObj)
	t.Logf("Node1 object shard %d owned by: %s", node1ObjShard, nodeForNode1Obj)
	t.Logf("Node2 object shard %d owned by: %s", node2ObjShard, nodeForNode2Obj)

	// Verify cluster-level object was created on the owning node
	n1Objects := n1.ListObjectIDs()
	n2Objects := n2.ListObjectIDs()

	t.Logf("Node1 objects: %v", n1Objects)
	t.Logf("Node2 objects: %v", n2Objects)

	// Check cluster-level object
	clusterObjFound := false
	if nodeForClusterObj == node1Addr {
		for _, id := range n1Objects {
			if id == clusterObjID {
				clusterObjFound = true
				break
			}
		}
	} else {
		for _, id := range n2Objects {
			if id == clusterObjID {
				clusterObjFound = true
				break
			}
		}
	}
	if !clusterObjFound {
		t.Errorf("Cluster-level auto-load object %s was not created on owning node", clusterObjID)
	}

	// Check node1-specific object
	node1ObjFound := false
	if nodeForNode1Obj == node1Addr {
		for _, id := range n1Objects {
			if id == node1ObjID {
				node1ObjFound = true
				break
			}
		}
		if !node1ObjFound {
			t.Errorf("Node1-specific auto-load object %s was not created on node1", node1ObjID)
		}
	} else {
		// If node1 doesn't own the shard, the object shouldn't exist anywhere
		for _, id := range n1Objects {
			if id == node1ObjID {
				t.Errorf("Node1-specific auto-load object %s was created on node1 even though it doesn't own the shard", node1ObjID)
			}
		}
		for _, id := range n2Objects {
			if id == node1ObjID {
				t.Errorf("Node1-specific auto-load object %s was created on node2", node1ObjID)
			}
		}
	}

	// Check node2-specific object
	node2ObjFound := false
	if nodeForNode2Obj == node2Addr {
		for _, id := range n2Objects {
			if id == node2ObjID {
				node2ObjFound = true
				break
			}
		}
		if !node2ObjFound {
			t.Errorf("Node2-specific auto-load object %s was not created on node2", node2ObjID)
		}
	} else {
		// If node2 doesn't own the shard, the object shouldn't exist anywhere
		for _, id := range n2Objects {
			if id == node2ObjID {
				t.Errorf("Node2-specific auto-load object %s was created on node2 even though it doesn't own the shard", node2ObjID)
			}
		}
		for _, id := range n1Objects {
			if id == node2ObjID {
				t.Errorf("Node2-specific auto-load object %s was created on node1", node2ObjID)
			}
		}
	}
}
