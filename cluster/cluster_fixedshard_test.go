package cluster

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestCreateObject_FixedShardAddress tests that fixed shard addresses correctly map to the specified shard
func TestCreateObject_FixedShardAddress(t *testing.T) {
	t.Parallel()

	// Test that fixed shard IDs correctly map to the specified shard
	numShards := testutil.TestNumShards
	objID := "shard#5/test-object-123"

	// Verify the shard mapping is correct
	expectedShard := 5
	actualShard := sharding.GetShardID(objID, numShards)
	if actualShard != expectedShard {
		t.Fatalf("Fixed shard object mapped to shard %d, want %d", actualShard, expectedShard)
	}

	// Test multiple fixed shard IDs
	testCases := []struct {
		objectID      string
		expectedShard int
	}{
		{"shard#0/obj-a", 0},
		{"shard#10/obj-b", 10},
		{"shard#63/obj-c", 63},
	}

	for _, tc := range testCases {
		actualShard := sharding.GetShardID(tc.objectID, numShards)
		if actualShard != tc.expectedShard {
			t.Fatalf("GetShardID(%s) = %d, want %d", tc.objectID, actualShard, tc.expectedShard)
		}
	}
}

// TestCallObject_FixedShardAddress tests that calling objects with fixed shard addresses uses correct shard mapping
func TestCallObject_FixedShardAddress(t *testing.T) {
	t.Parallel()

	numShards := testutil.TestNumShards
	objID := "shard#10/echo-obj"

	// Verify the shard mapping is correct
	expectedShard := 10
	actualShard := sharding.GetShardID(objID, numShards)
	if actualShard != expectedShard {
		t.Fatalf("Fixed shard object mapped to shard %d, want %d", actualShard, expectedShard)
	}
}

// TestCreateObject_FixedShardAddress_Format tests various fixed shard address formats
func TestCreateObject_FixedShardAddress_Format(t *testing.T) {
	t.Parallel()

	numShards := testutil.TestNumShards

	tests := []struct {
		name          string
		objectID      string
		expectedShard int
	}{
		{
			name:          "standard format",
			objectID:      "shard#0/obj-1",
			expectedShard: 0,
		},
		{
			name:          "double digit shard",
			objectID:      "shard#42/obj-2",
			expectedShard: 42,
		},
		{
			name:          "complex object part",
			objectID:      "shard#20/type-subtype-uuid-123",
			expectedShard: 20,
		},
		{
			name:          "empty object part",
			objectID:      "shard#15/",
			expectedShard: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the shard mapping
			actualShard := sharding.GetShardID(tt.objectID, numShards)
			if actualShard != tt.expectedShard {
				t.Fatalf("Fixed shard object mapped to shard %d, want %d", actualShard, tt.expectedShard)
			}
		})
	}
}

// TestGetShardID_FixedShardConsistency tests that fixed shard IDs are consistent
func TestGetShardID_FixedShardConsistency(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		objectID      string
		expectedShard int
	}{
		{"shard#0/obj", 0},
		{"shard#5/test", 5},
		{"shard#63/user", 63},
		{"shard#42/session-abc", 42},
	}

	numShards := testutil.TestNumShards

	for _, tc := range testCases {
		t.Run(tc.objectID, func(t *testing.T) {
			// Call multiple times to ensure consistency
			for i := 0; i < 10; i++ {
				shard := sharding.GetShardID(tc.objectID, numShards)
				if shard != tc.expectedShard {
					t.Fatalf("Iteration %d: GetShardID(%s) = %d, want %d", i, tc.objectID, shard, tc.expectedShard)
				}
			}
		})
	}
}

// TestFixedShardVsRegularID tests that fixed shard and regular IDs behave differently
func TestFixedShardVsRegularID(t *testing.T) {
	t.Parallel()

	numShards := testutil.TestNumShards
	objectPart := "my-object-123"

	// Get hash-based shard for regular object ID
	regularID := objectPart
	regularShard := sharding.GetShardID(regularID, numShards)

	// Create a fixed shard ID for a different shard
	fixedShardNum := (regularShard + 10) % numShards
	fixedID := testutil.GetObjectIDForShard(fixedShardNum, objectPart)
	fixedShard := sharding.GetShardID(fixedID, numShards)

	// Verify they map to different shards
	if regularShard == fixedShard {
		t.Fatalf("Regular and fixed shard IDs should map to different shards, both got %d", regularShard)
	}

	// Verify fixed shard maps to the expected shard
	if fixedShard != fixedShardNum {
		t.Fatalf("Fixed shard ID mapped to %d, want %d", fixedShard, fixedShardNum)
	}
}

// TestFixedShardDistinctFromFixedNode tests that fixed shard and fixed node formats don't conflict
func TestFixedShardDistinctFromFixedNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	numShards := testutil.TestNumShards

	// Test fixed shard format
	fixedShardID := "shard#5/test-obj"
	fixedShardShard := sharding.GetShardID(fixedShardID, numShards)
	if fixedShardShard != 5 {
		t.Fatalf("Fixed shard object should map to shard 5, got %d", fixedShardShard)
	}

	// Test fixed node format - should use hash-based sharding
	fixedNodeID := "localhost:7000/test-obj-2"
	fixedNodeShard := sharding.GetShardID(fixedNodeID, numShards)

	// Fixed node ID should NOT map to a fixed shard (should use hash)
	// Just verify it returns a valid shard
	if fixedNodeShard < 0 || fixedNodeShard >= numShards {
		t.Fatalf("Fixed node ID should use hash-based sharding, got invalid shard %d", fixedNodeShard)
	}

	// Test that GetCurrentNodeForObject correctly distinguishes them
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	defer testNode.Stop(ctx)
	c := newClusterForTesting(testNode, "TestFixedShardDistinctFromFixedNode")

	// Fixed node format should return the node address directly
	nodeAddr, err := c.GetCurrentNodeForObject(ctx, fixedNodeID)
	if err != nil {
		t.Fatalf("GetCurrentNodeForObject failed for fixed node: %v", err)
	}
	if nodeAddr != "localhost:7000" {
		t.Fatalf("Fixed node format should return 'localhost:7000', got %s", nodeAddr)
	}

	// Fixed shard format should go through shard mapping (will fail without shard mapping setup)
	_, err = c.GetCurrentNodeForObject(ctx, fixedShardID)
	if err == nil {
		t.Fatal("Expected error for fixed shard without shard mapping, got nil")
	}
	// The error should be about shard mapping, not about treating it as a node address
	if !strings.Contains(err.Error(), "shard") {
		t.Fatalf("Expected shard-related error, got: %v", err)
	}
}
