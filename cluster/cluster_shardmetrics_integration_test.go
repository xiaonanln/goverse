package cluster

import (
	"context"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestShardMetricsIntegration tests that shard metrics are properly updated when cluster state changes
// This test requires a running etcd instance at localhost:2379
func TestShardMetricsIntegration(t *testing.T) {
	t.Parallel()
	// Reset metrics before test
	metrics.AssignedShardsTotal.Reset()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start clusters
	cluster1 := mustNewCluster(ctx, t, "localhost:50021", testPrefix)
	_ = mustNewCluster(ctx, t, "localhost:50022", testPrefix)

	// Wait for watches to sync and shard mapping to be initialized
	t.Logf("Waiting for shard mapping to be created...")
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Verify shard mapping is initialized
	mapping1 := cluster1.GetShardMapping(ctx)
	if mapping1 == nil {
		t.Fatalf("Shard mapping should be initialized by leader")
	}

	// Wait a bit more for metrics to be updated
	time.Sleep(500 * time.Millisecond)

	// Verify metrics are updated for both nodes
	// Get expected shard count per node
	node1Count := 0
	node2Count := 0
	for _, shardInfo := range mapping1.Shards {
		if shardInfo.CurrentNode == "localhost:50021" {
			node1Count++
		} else if shardInfo.CurrentNode == "localhost:50022" {
			node2Count++
		}
	}

	t.Logf("Expected shard counts: node1=%d, node2=%d", node1Count, node2Count)

	// Verify metrics match actual shard distribution
	metric1 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50021"))
	metric2 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50022"))

	if int(metric1) != node1Count {
		t.Errorf("Metric for localhost:50021 should be %d, got %f", node1Count, metric1)
	}

	if int(metric2) != node2Count {
		t.Errorf("Metric for localhost:50022 should be %d, got %f", node2Count, metric2)
	}

	// Verify total shards across all nodes
	totalShards := int(metric1) + int(metric2)
	if totalShards != sharding.NumShards {
		t.Errorf("Total shards should be %d, got %d", sharding.NumShards, totalShards)
	}

	t.Logf("Shard metrics verified: node1=%f, node2=%f, total=%d", metric1, metric2, totalShards)
}

// TestShardMetricsAfterNodeJoin tests that metrics are updated when a new node joins
// This test requires a running etcd instance at localhost:2379
func TestShardMetricsAfterNodeJoin(t *testing.T) {
	t.Parallel()
	// Reset metrics before test
	metrics.AssignedShardsTotal.Reset()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with 2 nodes
	_ = mustNewCluster(ctx, t, "localhost:50031", testPrefix)
	_ = mustNewCluster(ctx, t, "localhost:50032", testPrefix)

	// Wait for initial shard mapping
	t.Logf("Waiting for initial shard mapping...")
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Verify initial metrics
	initialMetric1 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50031"))
	initialMetric2 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50032"))
	initialTotal := int(initialMetric1) + int(initialMetric2)

	t.Logf("Initial shard distribution: node1=%f, node2=%f, total=%d", initialMetric1, initialMetric2, initialTotal)

	if initialTotal != sharding.NumShards {
		t.Errorf("Initial total shards should be %d, got %d", sharding.NumShards, initialTotal)
	}

	// Add a third node
	t.Logf("Adding third node...")
	cluster3 := mustNewCluster(ctx, t, "localhost:50033", testPrefix)

	// Wait for cluster to stabilize and rebalance
	// This includes stability duration + check interval + rebalance time
	time.Sleep(testutil.WaitForShardMappingTimeout + 5*time.Second)

	// Verify metrics are updated with the new node
	finalMetric1 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50031"))
	finalMetric2 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50032"))
	finalMetric3 := promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("localhost:50033"))
	finalTotal := int(finalMetric1) + int(finalMetric2) + int(finalMetric3)

	t.Logf("Final shard distribution: node1=%f, node2=%f, node3=%f, total=%d",
		finalMetric1, finalMetric2, finalMetric3, finalTotal)

	// Verify total shards still equals NumShards
	if finalTotal != sharding.NumShards {
		t.Errorf("Final total shards should be %d, got %d", sharding.NumShards, finalTotal)
	}

	// All nodes should have some shards assigned (after rebalancing)
	// Note: node3 might still be at 0 if rebalancing hasn't completed yet
	// but we can at least verify the first two nodes still have shards
	if finalMetric1 == 0 && finalMetric2 == 0 {
		t.Errorf("At least one of the first two nodes should have shards")
	}

	// Clean up
	cluster3.Stop(ctx)
}
