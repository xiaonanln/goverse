package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

func TestClusterSetShardMappingCheckInterval(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	c := newClusterForTesting(testNode, "TestClusterSetShardMappingCheckInterval")

	// Test default value
	if c.GetShardMappingCheckInterval() != ShardMappingCheckInterval {
		t.Errorf("Expected default ShardMappingCheckInterval to be %v, got %v", ShardMappingCheckInterval, c.GetShardMappingCheckInterval())
	}

	// Set custom interval
	customInterval := 3 * time.Second
	c.SetShardMappingCheckInterval(customInterval)
	if c.GetShardMappingCheckInterval() != customInterval {
		t.Errorf("Expected ShardMappingCheckInterval to be %v, got %v", customInterval, c.GetShardMappingCheckInterval())
	}
}

func TestClusterShardMappingCheckIntervalZeroValue(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	c := newClusterForTesting(testNode, "TestClusterShardMappingCheckIntervalZeroValue")

	// Set to zero (should return default)
	c.SetShardMappingCheckInterval(0)
	if c.GetShardMappingCheckInterval() != ShardMappingCheckInterval {
		t.Errorf("Expected default ShardMappingCheckInterval to be %v when set to 0, got %v", ShardMappingCheckInterval, c.GetShardMappingCheckInterval())
	}
}

func TestClusterShardMappingCheckIntervalNegativeValue(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:47000")
	c := newClusterForTesting(testNode, "TestClusterShardMappingCheckIntervalNegativeValue")

	// Set to negative (should return default)
	c.SetShardMappingCheckInterval(-3 * time.Second)
	if c.GetShardMappingCheckInterval() != ShardMappingCheckInterval {
		t.Errorf("Expected default ShardMappingCheckInterval to be %v when set to negative, got %v", ShardMappingCheckInterval, c.GetShardMappingCheckInterval())
	}
}
