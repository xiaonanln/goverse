package node

import (
	"context"
	"fmt"
	"sync"
	"testing"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/metrics"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestShardMethodObject is a test object for shard method call metrics
type TestShardMethodObject struct {
	object.BaseObject
	mu        sync.Mutex
	CallCount int
}

func (t *TestShardMethodObject) OnCreated() {}

// Echo is a simple method
func (t *TestShardMethodObject) Echo(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.CallCount++
	return req, nil
}

// getObjectIDForShard returns an object ID that hashes to the specified shard
func getObjectIDForShard(targetShardID int, prefix string, numShards int) string {
	// Try different suffixes until we find one that hashes to the target shard
	for i := 0; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%d-%d", prefix, targetShardID, i)
		if sharding.GetShardID(candidate, numShards) == targetShardID {
			return candidate
		}
	}
	panic(fmt.Sprintf("Could not find object ID for shard %d after 10000 attempts", targetShardID))
}

// TestShardMethodCallMetrics tests that method calls are correctly tracked per shard
func TestShardMethodCallMetrics(t *testing.T) {
	// Reset metrics before test
	metrics.ShardMethodCallsTotal.Reset()

	node := NewNode("test-node:1234", testNumShards)
	ctx := context.Background()

	// Register object type
	node.RegisterObjectType((*TestShardMethodObject)(nil))

	// Get object IDs for specific shards
	obj1ID := getObjectIDForShard(0, "TestShardMethodObject-obj1", testNumShards)
	obj2ID := getObjectIDForShard(5, "TestShardMethodObject-obj2", testNumShards)
	obj3ID := getObjectIDForShard(10, "TestShardMethodObject-obj3", testNumShards)

	// Verify the shard assignments
	if node.GetShardID(obj1ID) != 0 {
		t.Fatalf("obj1ID should map to shard 0, got %d", node.GetShardID(obj1ID))
	}
	if node.GetShardID(obj2ID) != 5 {
		t.Fatalf("obj2ID should map to shard 5, got %d", node.GetShardID(obj2ID))
	}
	if node.GetShardID(obj3ID) != 10 {
		t.Fatalf("obj3ID should map to shard 10, got %d", node.GetShardID(obj3ID))
	}

	// Get initial counts for each shard
	initialCount0 := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("0"))
	initialCount5 := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("5"))
	initialCount10 := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("10"))

	// Call methods on different shards
	_, err := node.CallObject(ctx, "TestShardMethodObject", obj1ID, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for obj1, got error: %v", err)
	}

	_, err = node.CallObject(ctx, "TestShardMethodObject", obj2ID, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for obj2, got error: %v", err)
	}

	// Call obj1 again (shard 0)
	_, err = node.CallObject(ctx, "TestShardMethodObject", obj1ID, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for obj1 second call, got error: %v", err)
	}

	// Verify shard 0 has 2 calls
	count0 := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("0"))
	if count0 != initialCount0+2.0 {
		t.Fatalf("Expected shard 0 to have %f calls, got %f", initialCount0+2.0, count0)
	}

	// Verify shard 5 has 1 call
	count5 := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("5"))
	if count5 != initialCount5+1.0 {
		t.Fatalf("Expected shard 5 to have %f calls, got %f", initialCount5+1.0, count5)
	}

	// Verify shard 10 has 0 calls (not called yet)
	count10 := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("10"))
	if count10 != initialCount10 {
		t.Fatalf("Expected shard 10 to have %f calls, got %f", initialCount10, count10)
	}

	t.Logf("Shard metrics verified: shard 0=%f, shard 5=%f, shard 10=%f", count0, count5, count10)
}

// TestShardMethodCallMetricsMultipleObjects tests that multiple objects in the same shard contribute to the same metric
func TestShardMethodCallMetricsMultipleObjects(t *testing.T) {
	// Reset metrics before test
	metrics.ShardMethodCallsTotal.Reset()

	node := NewNode("test-node:1234", testNumShards)
	ctx := context.Background()

	// Register object type
	node.RegisterObjectType((*TestShardMethodObject)(nil))

	// Get multiple object IDs for the same shard
	objA := getObjectIDForShard(3, "TestShardMethodObject-objA", testNumShards)
	objB := getObjectIDForShard(3, "TestShardMethodObject-objB", testNumShards)
	objC := getObjectIDForShard(3, "TestShardMethodObject-objC", testNumShards)

	// Verify all map to shard 3
	if node.GetShardID(objA) != 3 || node.GetShardID(objB) != 3 || node.GetShardID(objC) != 3 {
		t.Fatalf("All objects should map to shard 3, got %d, %d, %d",
			node.GetShardID(objA), node.GetShardID(objB), node.GetShardID(objC))
	}

	// Get initial count for shard 3
	initialCount := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("3"))

	// Call methods on different objects in the same shard
	_, err := node.CallObject(ctx, "TestShardMethodObject", objA, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for objA, got error: %v", err)
	}

	_, err = node.CallObject(ctx, "TestShardMethodObject", objB, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for objB, got error: %v", err)
	}

	_, err = node.CallObject(ctx, "TestShardMethodObject", objC, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for objC, got error: %v", err)
	}

	// Call objA again
	_, err = node.CallObject(ctx, "TestShardMethodObject", objA, "Echo", &emptypb.Empty{})
	if err != nil {
		t.Fatalf("CallObject should succeed for objA second call, got error: %v", err)
	}

	// Verify shard 3 has 4 calls total (1 for objA, 1 for objB, 1 for objC, 1 more for objA)
	count := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("3"))
	if count != initialCount+4.0 {
		t.Fatalf("Expected shard 3 to have %f calls, got %f", initialCount+4.0, count)
	}

	t.Logf("Shard 3 metrics verified: %f calls from multiple objects", count)
}

// TestShardMethodCallMetricsFailure tests that failed method calls also increment the shard metric
func TestShardMethodCallMetricsFailure(t *testing.T) {
	// Reset metrics before test
	metrics.ShardMethodCallsTotal.Reset()

	node := NewNode("test-node:1234", testNumShards)
	ctx := context.Background()

	// Register object type
	node.RegisterObjectType((*TestShardMethodObject)(nil))

	// Get object ID for a specific shard
	objID := getObjectIDForShard(7, "TestShardMethodObject-fail", testNumShards)

	// Get initial count for shard 7
	initialCount := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("7"))

	// Call non-existent method (should fail but still increment shard metric)
	_, err := node.CallObject(ctx, "TestShardMethodObject", objID, "NonExistentMethod", &emptypb.Empty{})
	if err == nil {
		t.Fatal("CallObject should fail for non-existent method")
	}

	// Verify shard 7 still got incremented despite the failure
	count := promtestutil.ToFloat64(metrics.ShardMethodCallsTotal.WithLabelValues("7"))
	if count != initialCount+1.0 {
		t.Fatalf("Expected shard 7 to have %f calls even on failure, got %f", initialCount+1.0, count)
	}

	t.Logf("Shard 7 metrics verified: %f calls (including failed call)", count)
}
