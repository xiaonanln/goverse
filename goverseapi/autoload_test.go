package goverseapi

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestAutoLoadObject is a simple test object for auto-load API testing
type TestAutoLoadObject struct {
	object.BaseObject
}

func (o *TestAutoLoadObject) OnCreated() {}

// Ensure TestAutoLoadObject implements Object interface
var _ object.Object = (*TestAutoLoadObject)(nil)

// TestGetAutoLoadObjectIDsByType_GlobalObject tests getting IDs for global auto-load objects
func TestGetAutoLoadObjectIDsByType_GlobalObject(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47700"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with global auto-load objects
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "GlobalObj1"},
			{Type: "TestAutoLoadObject", ID: "GlobalObj2"},
			{Type: "OtherType", ID: "OtherObj1"},
		},
	}

	// Create cluster
	c, err := cluster.NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Test GetAutoLoadObjectIDsByType
	ids := c.GetAutoLoadObjectIDsByType("TestAutoLoadObject")
	if len(ids) != 2 {
		t.Fatalf("Expected 2 IDs for TestAutoLoadObject, got %d: %v", len(ids), ids)
	}

	// Check that we got the correct IDs
	expectedIDs := []string{"GlobalObj1", "GlobalObj2"}
	sort.Strings(ids)
	sort.Strings(expectedIDs)

	for i, expected := range expectedIDs {
		if ids[i] != expected {
			t.Errorf("Expected ID[%d] = %s, got %s", i, expected, ids[i])
		}
	}

	// Test with a different type
	otherIDs := c.GetAutoLoadObjectIDsByType("OtherType")
	if len(otherIDs) != 1 {
		t.Fatalf("Expected 1 ID for OtherType, got %d: %v", len(otherIDs), otherIDs)
	}
	if otherIDs[0] != "OtherObj1" {
		t.Errorf("Expected ID = OtherObj1, got %s", otherIDs[0])
	}

	// Test with non-existent type
	nonExistentIDs := c.GetAutoLoadObjectIDsByType("NonExistentType")
	if len(nonExistentIDs) != 0 {
		t.Errorf("Expected 0 IDs for non-existent type, got %d: %v", len(nonExistentIDs), nonExistentIDs)
	}
}

// TestGetAutoLoadObjectIDsByType_PerShardObject tests getting IDs for per-shard auto-load objects
func TestGetAutoLoadObjectIDsByType_PerShardObject(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47710"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with per-shard auto-load object
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "PerShardObj", PerShard: true},
		},
	}

	// Create cluster
	c, err := cluster.NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Test GetAutoLoadObjectIDsByType
	ids := c.GetAutoLoadObjectIDsByType("TestAutoLoadObject")

	// Should return one ID per shard
	expectedCount := testutil.TestNumShards
	if len(ids) != expectedCount {
		t.Fatalf("Expected %d IDs for per-shard object, got %d", expectedCount, len(ids))
	}

	// Check a few specific IDs
	expectedIDs := []string{
		"shard#0/PerShardObj",
		"shard#5/PerShardObj",
		"shard#10/PerShardObj",
		fmt.Sprintf("shard#%d/PerShardObj", testutil.TestNumShards-1),
	}

	for _, expectedID := range expectedIDs {
		found := false
		for _, id := range ids {
			if id == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find ID %s in result, but it was missing", expectedID)
		}
	}

	// Verify format of all IDs
	for _, id := range ids {
		if len(id) < 6 || id[:6] != "shard#" {
			t.Errorf("ID %s does not have correct format (should start with 'shard#')", id)
		}
	}
}

// TestGetAutoLoadObjectIDsByType_PerNodeObject tests getting IDs for per-node auto-load objects
func TestGetAutoLoadObjectIDsByType_PerNodeObject(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes
	node1Addr := "localhost:47720"
	node2Addr := "localhost:47721"

	n1 := node.NewNode(node1Addr, testutil.TestNumShards)
	n2 := node.NewNode(node2Addr, testutil.TestNumShards)

	// Register test object type on both nodes
	n1.RegisterObjectType((*TestAutoLoadObject)(nil))
	n2.RegisterObjectType((*TestAutoLoadObject)(nil))

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

	// Create cluster configs with per-node auto-load object
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "PerNodeObj", PerNode: true},
		},
	}

	// Create clusters
	c1, err := cluster.NewClusterWithNode(cfg, n1)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer c1.Stop(ctx)

	c2, err := cluster.NewClusterWithNode(cfg, n2)
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

	// Test GetAutoLoadObjectIDsByType - call directly on cluster to avoid cluster.This() issues in parallel tests
	ids := c2.GetAutoLoadObjectIDsByType("TestAutoLoadObject")

	// Should return one ID per node (2 nodes)
	expectedCount := 2
	if len(ids) != expectedCount {
		t.Fatalf("Expected %d IDs for per-node object, got %d: %v", expectedCount, len(ids), ids)
	}

	// Check that we got the correct IDs
	expectedIDs := []string{
		fmt.Sprintf("%s/PerNodeObj", node1Addr),
		fmt.Sprintf("%s/PerNodeObj", node2Addr),
	}
	sort.Strings(ids)
	sort.Strings(expectedIDs)

	for i, expected := range expectedIDs {
		if ids[i] != expected {
			t.Errorf("Expected ID[%d] = %s, got %s", i, expected, ids[i])
		}
	}
}

// TestGetAutoLoadObjectIDs tests getting all auto-load object IDs
func TestGetAutoLoadObjectIDs(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47730"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with mixed auto-load objects
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "GlobalObj1"},
			{Type: "TestAutoLoadObject", ID: "GlobalObj2"},
			{Type: "TestAutoLoadObject", ID: "PerShardObj", PerShard: true},
			{Type: "TestAutoLoadObject", ID: "PerNodeObj", PerNode: true},
			{Type: "OtherType", ID: "OtherGlobalObj"},
		},
	}

	// Create cluster
	c, err := cluster.NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Test GetAutoLoadObjectIDs - call directly on cluster to avoid cluster.This() issues in parallel tests
	allIDs := c.GetAutoLoadObjectIDs()

	// Should have 2 types
	if len(allIDs) != 2 {
		t.Fatalf("Expected 2 object types, got %d: %v", len(allIDs), allIDs)
	}

	// Check TestAutoLoadObject type
	testObjIDs, ok := allIDs["TestAutoLoadObject"]
	if !ok {
		t.Fatal("TestAutoLoadObject type not found in result")
	}

	// Should have: 2 global + 64 per-shard + 1 per-node = 67 IDs
	expectedTestObjCount := 2 + testutil.TestNumShards + 1
	if len(testObjIDs) != expectedTestObjCount {
		t.Fatalf("Expected %d IDs for TestAutoLoadObject, got %d", expectedTestObjCount, len(testObjIDs))
	}

	// Check OtherType
	otherIDs, ok := allIDs["OtherType"]
	if !ok {
		t.Fatal("OtherType not found in result")
	}

	if len(otherIDs) != 1 {
		t.Fatalf("Expected 1 ID for OtherType, got %d", len(otherIDs))
	}

	if otherIDs[0] != "OtherGlobalObj" {
		t.Errorf("Expected ID = OtherGlobalObj, got %s", otherIDs[0])
	}

	// Verify some specific IDs in TestAutoLoadObject
	expectedIDs := []string{
		"GlobalObj1",
		"GlobalObj2",
		"shard#0/PerShardObj",
		"shard#5/PerShardObj",
		fmt.Sprintf("%s/PerNodeObj", nodeAddr),
	}

	for _, expectedID := range expectedIDs {
		found := false
		for _, id := range testObjIDs {
			if id == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find ID %s in TestAutoLoadObject IDs, but it was missing", expectedID)
		}
	}
}

// TestGetAutoLoadObjectIDs_EmptyConfig tests with no auto-load objects configured
func TestGetAutoLoadObjectIDs_EmptyConfig(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47740"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with no auto-load objects
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects:               []config.AutoLoadObjectConfig{},
	}

	// Create cluster
	c, err := cluster.NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Test GetAutoLoadObjectIDs - call directly on cluster to avoid cluster.This() issues in parallel tests
	allIDs := c.GetAutoLoadObjectIDs()

	// Should return empty map
	if len(allIDs) != 0 {
		t.Errorf("Expected empty map, got %d types: %v", len(allIDs), allIDs)
	}

	// Test GetAutoLoadObjectIDsByType - call directly on cluster to avoid cluster.This() issues in parallel tests
	ids := c.GetAutoLoadObjectIDsByType("TestAutoLoadObject")
	if len(ids) != 0 {
		t.Errorf("Expected empty slice, got %d IDs: %v", len(ids), ids)
	}
}

// TestGetAutoLoadObjectIDsByType_MultipleOfSameType tests multiple configs of the same type
func TestGetAutoLoadObjectIDsByType_MultipleOfSameType(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47750"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with multiple configs of the same type
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "Config1Obj"},
			{Type: "TestAutoLoadObject", ID: "Config2Obj"},
			{Type: "TestAutoLoadObject", ID: "Config3Obj"},
		},
	}

	// Create cluster
	c, err := cluster.NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Test GetAutoLoadObjectIDsByType - call directly on cluster to avoid cluster.This() issues in parallel tests
	ids := c.GetAutoLoadObjectIDsByType("TestAutoLoadObject")

	// Should return all 3 IDs
	if len(ids) != 3 {
		t.Fatalf("Expected 3 IDs, got %d: %v", len(ids), ids)
	}

	// Check all expected IDs are present
	expectedIDs := []string{"Config1Obj", "Config2Obj", "Config3Obj"}
	sort.Strings(ids)
	sort.Strings(expectedIDs)

	for i, expected := range expectedIDs {
		if ids[i] != expected {
			t.Errorf("Expected ID[%d] = %s, got %s", i, expected, ids[i])
		}
	}

	// Test GetAutoLoadObjectIDs - call directly on cluster to avoid cluster.This() issues in parallel tests
	allIDs := c.GetAutoLoadObjectIDs()
	if len(allIDs) != 1 {
		t.Fatalf("Expected 1 type, got %d", len(allIDs))
	}

	testObjIDs := allIDs["TestAutoLoadObject"]
	if len(testObjIDs) != 3 {
		t.Fatalf("Expected 3 IDs for TestAutoLoadObject, got %d", len(testObjIDs))
	}
}
