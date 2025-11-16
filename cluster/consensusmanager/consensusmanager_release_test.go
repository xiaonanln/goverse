package consensusmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/testutil"
"github.com/xiaonanln/goverse/cluster/shardlock"
)

// setupShardMapping is a helper function that sets up shard mapping in both ConsensusManager
// state and etcd with proper ModRevision tracking. It handles the synchronization between
// in-memory state and etcd storage.
//
// Parameters:
//   - t: testing.T for error reporting
//   - ctx: context for etcd operations
//   - cm: ConsensusManager to configure
//   - mgr: EtcdManager for etcd operations
//   - nodes: map of node addresses to set up in cluster state
//   - shards: map of shard IDs to ShardInfo to store
//
// The function will:
//  1. Set up nodes in the ConsensusManager state
//  2. Initialize the shard mapping structure
//  3. Store all shards in both in-memory state and etcd
//  4. Update ModRevision values to match etcd's actual revisions
func setupShardMapping(t *testing.T, ctx context.Context, cm *ConsensusManager, mgr *etcdmanager.EtcdManager, nodes map[string]bool, shards map[int]ShardInfo) {
	t.Helper()

	prefix := mgr.GetPrefix()
	client := mgr.GetClient()

	// Set up nodes and shard mapping in ConsensusManager state
	cm.mu.Lock()
	cm.state.Nodes = make(map[string]bool)
	for node, active := range nodes {
		cm.state.Nodes[node] = active
	}
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.state.LastChange = time.Now().Add(-20 * time.Second) // Mark as stable

	// Set up shards in in-memory state
	for shardID, shardInfo := range shards {
		cm.state.ShardMapping.Shards[shardID] = shardInfo
	}
	cm.mu.Unlock()

	// Store all shards in etcd and update ModRevision
	for shardID, shardInfo := range shards {
		key := prefix + "/shard/" + fmt.Sprintf("%d", shardID)
		value := formatShardInfo(shardInfo)
		resp, err := client.Put(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to store shard %d in etcd: %v", shardID, err)
		}

		// Update the in-memory ModRevision with the actual value from etcd
		cm.mu.Lock()
		shardInfo.ModRevision = resp.Header.Revision
		cm.state.ShardMapping.Shards[shardID] = shardInfo
		cm.mu.Unlock()
	}
}

// TestReleaseShardsForNode_EmptyNode tests releasing shards when localNode is empty
func TestReleaseShardsForNode_EmptyNode(t *testing.T) {
	t.Parallel()

	// Create a consensus manager without connecting to etcd
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	ctx := context.Background()
	objectsPerShard := make(map[int]int)

	// Call ReleaseShardsForNode with empty string - should return error
	err := cm.ReleaseShardsForNode(ctx, "", objectsPerShard, 1*time.Second)
	if err == nil {
		t.Error("ReleaseShardsForNode should return error for empty localNode")
	}
}

// TestReleaseShardsForNode_NoShardMapping tests releasing when no shard mapping exists
func TestReleaseShardsForNode_NoShardMapping(t *testing.T) {
	t.Parallel()

	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	ctx := context.Background()
	objectsPerShard := make(map[int]int)

	// Call with valid node but no shard mapping - should not error
	err := cm.ReleaseShardsForNode(ctx, "localhost:47001", objectsPerShard, 1*time.Second)
	if err != nil {
		t.Errorf("ReleaseShardsForNode should not error when no mapping exists: %v", err)
	}
}

// TestReleaseShardsForNode_WithEtcd tests the full release flow with etcd
func TestReleaseShardsForNode_WithEtcd(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create etcd manager and connect
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	// Create consensus manager
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Set up test nodes
	thisNodeAddr := "localhost:47001"
	otherNodeAddr := "localhost:47002"
	prefix := mgr.GetPrefix()

	// Set up nodes
	nodes := map[string]bool{
		thisNodeAddr:  true,
		otherNodeAddr: true,
	}

	// Set up test shards:
	// Shard 0: CurrentNode=thisNode, TargetNode=otherNode, objectCount=0 -> SHOULD RELEASE
	// Shard 1: CurrentNode=thisNode, TargetNode=otherNode, objectCount=1 -> SHOULD NOT RELEASE (has objects)
	// Shard 2: CurrentNode=thisNode, TargetNode=thisNode, objectCount=0 -> SHOULD NOT RELEASE (target is self)
	// Shard 3: CurrentNode=thisNode, TargetNode="", objectCount=0 -> SHOULD NOT RELEASE (target is empty)
	// Shard 4: CurrentNode=otherNode, TargetNode=thisNode, objectCount=0 -> SHOULD NOT RELEASE (current is not this node)
	shards := map[int]ShardInfo{
		0: {
			TargetNode:  otherNodeAddr,
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		},
		1: {
			TargetNode:  otherNodeAddr,
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		},
		2: {
			TargetNode:  thisNodeAddr,
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		},
		3: {
			TargetNode:  "",
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		},
		4: {
			TargetNode:  thisNodeAddr,
			CurrentNode: otherNodeAddr,
			ModRevision: 0,
		},
	}

	// Use helper to set up shard mapping in both CM state and etcd
	setupShardMapping(t, ctx, cm, mgr, nodes, shards)

	// Get client for verification later
	client := mgr.GetClient()

	// Create objectsPerShard map
	objectsPerShard := map[int]int{
		0: 0, // No objects for shard 0
		1: 1, // One object for shard 1
		2: 0, // No objects for shard 2
		3: 0, // No objects for shard 3
		4: 0, // No objects for shard 4
	}

	// Call ReleaseShardsForNode with stability duration
	err = cm.ReleaseShardsForNode(ctx, thisNodeAddr, objectsPerShard, 1*time.Second)
	if err != nil {
		t.Fatalf("ReleaseShardsForNode failed: %v", err)
	}

	// Wait a bit for the async update to complete
	time.Sleep(100 * time.Millisecond)

	// Verify shard 0 was released (CurrentNode cleared)
	key0 := prefix + "/shard/0"
	resp0, err := client.Get(ctx, key0)
	if err != nil {
		t.Fatalf("Failed to get shard 0 from etcd: %v", err)
	}
	if len(resp0.Kvs) == 0 {
		t.Error("Shard 0 should exist in etcd after releasing")
	} else {
		shardInfo0 := parseShardInfo(resp0.Kvs[0])
		if shardInfo0.CurrentNode != "" {
			t.Errorf("Shard 0 CurrentNode should be empty (released), got %s", shardInfo0.CurrentNode)
		}
		if shardInfo0.TargetNode != otherNodeAddr {
			t.Errorf("Shard 0 TargetNode should remain %s, got %s", otherNodeAddr, shardInfo0.TargetNode)
		}
	}

	// Verify shard 1 was NOT released (has objects)
	key1 := prefix + "/shard/1"
	resp1, err := client.Get(ctx, key1)
	if err != nil {
		t.Fatalf("Failed to get shard 1 from etcd: %v", err)
	}
	if len(resp1.Kvs) == 0 {
		t.Error("Shard 1 should exist in etcd")
	} else {
		shardInfo1 := parseShardInfo(resp1.Kvs[0])
		if shardInfo1.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 1 CurrentNode should remain %s (has objects), got %s", thisNodeAddr, shardInfo1.CurrentNode)
		}
	}

	// Verify shard 2 was NOT released (target is self)
	key2 := prefix + "/shard/2"
	resp2, err := client.Get(ctx, key2)
	if err != nil {
		t.Fatalf("Failed to get shard 2 from etcd: %v", err)
	}
	if len(resp2.Kvs) == 0 {
		t.Error("Shard 2 should exist in etcd")
	} else {
		shardInfo2 := parseShardInfo(resp2.Kvs[0])
		if shardInfo2.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 2 CurrentNode should remain %s (target is self), got %s", thisNodeAddr, shardInfo2.CurrentNode)
		}
	}

	// Verify shard 3 was NOT released (target is empty)
	key3 := prefix + "/shard/3"
	resp3, err := client.Get(ctx, key3)
	if err != nil {
		t.Fatalf("Failed to get shard 3 from etcd: %v", err)
	}
	if len(resp3.Kvs) == 0 {
		t.Error("Shard 3 should exist in etcd")
	} else {
		shardInfo3 := parseShardInfo(resp3.Kvs[0])
		if shardInfo3.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 3 CurrentNode should remain %s (target is empty), got %s", thisNodeAddr, shardInfo3.CurrentNode)
		}
	}

	// Verify shard 4 was NOT released (current is not this node)
	key4 := prefix + "/shard/4"
	resp4, err := client.Get(ctx, key4)
	if err != nil {
		t.Fatalf("Failed to get shard 4 from etcd: %v", err)
	}
	if len(resp4.Kvs) == 0 {
		t.Error("Shard 4 should exist in etcd")
	} else {
		shardInfo4 := parseShardInfo(resp4.Kvs[0])
		if shardInfo4.CurrentNode != otherNodeAddr {
			t.Errorf("Shard 4 CurrentNode should remain %s (current is other node), got %s", otherNodeAddr, shardInfo4.CurrentNode)
		}
	}
}

// TestReleaseShardsForNode_MultipleShards tests releasing multiple shards at once
func TestReleaseShardsForNode_MultipleShards(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create etcd manager and connect
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	// Create consensus manager
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Set up test nodes
	thisNodeAddr := "localhost:47001"
	otherNodeAddr := "localhost:47002"
	prefix := mgr.GetPrefix()

	// Set up nodes
	nodes := map[string]bool{
		thisNodeAddr:  true,
		otherNodeAddr: true,
	}

	// Set up 5 shards that should all be released
	shards := make(map[int]ShardInfo)
	for i := 0; i < 5; i++ {
		shards[i] = ShardInfo{
			TargetNode:  otherNodeAddr,
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		}
	}

	// Use helper to set up shard mapping in both CM state and etcd
	setupShardMapping(t, ctx, cm, mgr, nodes, shards)

	// Get client for verification later
	client := mgr.GetClient()

	// Create objectsPerShard map with no objects for any shard
	objectsPerShard := map[int]int{
		0: 0,
		1: 0,
		2: 0,
		3: 0,
		4: 0,
	}

	// Call ReleaseShardsForNode with stability duration
	err = cm.ReleaseShardsForNode(ctx, thisNodeAddr, objectsPerShard, 1*time.Second)
	if err != nil {
		t.Fatalf("ReleaseShardsForNode failed: %v", err)
	}

	// Wait a bit for the async update to complete
	time.Sleep(100 * time.Millisecond)

	// Verify all 5 shards were released
	for i := 0; i < 5; i++ {
		key := prefix + "/shard/" + fmt.Sprintf("%d", i)
		resp, err := client.Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get shard %d from etcd: %v", i, err)
		}
		if len(resp.Kvs) == 0 {
			t.Errorf("Shard %d should exist in etcd after releasing", i)
		} else {
			shardInfo := parseShardInfo(resp.Kvs[0])
			if shardInfo.CurrentNode != "" {
				t.Errorf("Shard %d CurrentNode should be empty (released), got %s", i, shardInfo.CurrentNode)
			}
			if shardInfo.TargetNode != otherNodeAddr {
				t.Errorf("Shard %d TargetNode should remain %s, got %s", i, otherNodeAddr, shardInfo.TargetNode)
			}
		}
	}
}

// TestReleaseShardsForNode_RealShardIDs tests with realistic shard IDs
func TestReleaseShardsForNode_RealShardIDs(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create etcd manager and connect
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	// Create consensus manager
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Set up test nodes
	thisNodeAddr := "localhost:47001"
	otherNodeAddr := "localhost:47002"
	prefix := mgr.GetPrefix()

	// Set up nodes
	nodes := map[string]bool{
		thisNodeAddr:  true,
		otherNodeAddr: true,
	}

	// Use real shard IDs computed from object IDs
	testObjectID1 := "TestObject-123"
	testObjectID2 := "TestObject-456"
	shard1 := sharding.GetShardID(testObjectID1)
	shard2 := sharding.GetShardID(testObjectID2)

	// Set up shards that should be released
	shards := map[int]ShardInfo{
		shard1: {
			TargetNode:  otherNodeAddr,
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		},
		shard2: {
			TargetNode:  otherNodeAddr,
			CurrentNode: thisNodeAddr,
			ModRevision: 0,
		},
	}

	// Use helper to set up shard mapping in both CM state and etcd
	setupShardMapping(t, ctx, cm, mgr, nodes, shards)

	// Get client for verification later
	client := mgr.GetClient()

	// Create objectsPerShard map with no objects
	objectsPerShard := map[int]int{
		shard1: 0,
		shard2: 0,
	}

	// Call ReleaseShardsForNode with stability duration
	err = cm.ReleaseShardsForNode(ctx, thisNodeAddr, objectsPerShard, 1*time.Second)
	if err != nil {
		t.Fatalf("ReleaseShardsForNode failed: %v", err)
	}

	// Wait a bit for the async update to complete
	time.Sleep(100 * time.Millisecond)

	// Verify both shards were released
	for _, shardID := range []int{shard1, shard2} {
		key := prefix + "/shard/" + fmt.Sprintf("%d", shardID)
		resp, err := client.Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get shard %d from etcd: %v", shardID, err)
		}
		if len(resp.Kvs) == 0 {
			t.Errorf("Shard %d should exist in etcd after releasing", shardID)
		} else {
			shardInfo := parseShardInfo(resp.Kvs[0])
			if shardInfo.CurrentNode != "" {
				t.Errorf("Shard %d CurrentNode should be empty (released), got %s", shardID, shardInfo.CurrentNode)
			}
		}
	}
}
