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
