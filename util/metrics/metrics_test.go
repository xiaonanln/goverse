package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordObjectCreated(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()

	// Record object creation with shard 0
	RecordObjectCreated("localhost:47000", "TestObject", 0)

	// Verify the metric was recorded
	count := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "0"))
	if count != 1.0 {
		t.Errorf("Expected count to be 1.0, got %f", count)
	}

	// Create another object of same type in same shard
	RecordObjectCreated("localhost:47000", "TestObject", 0)
	count = testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "0"))
	if count != 2.0 {
		t.Errorf("Expected count to be 2.0, got %f", count)
	}

	// Create object of different type in different shard
	RecordObjectCreated("localhost:47000", "AnotherType", 1)
	count = testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "AnotherType", "1"))
	if count != 1.0 {
		t.Errorf("Expected count to be 1.0 for AnotherType, got %f", count)
	}
}

func TestRecordObjectDeleted(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()

	// Create some objects first in shard 0
	RecordObjectCreated("localhost:47000", "TestObject", 0)
	RecordObjectCreated("localhost:47000", "TestObject", 0)
	RecordObjectCreated("localhost:47000", "TestObject", 0)

	// Delete one object
	RecordObjectDeleted("localhost:47000", "TestObject", 0)

	// Verify the metric was decremented
	count := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "0"))
	if count != 2.0 {
		t.Errorf("Expected count to be 2.0, got %f", count)
	}
}

func TestMultipleNodes(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()

	// Test multiple nodes with different shards
	RecordObjectCreated("localhost:47000", "TestObject", 0)
	RecordObjectCreated("localhost:47001", "TestObject", 1)
	RecordObjectCreated("localhost:47002", "AnotherType", 2)

	// Verify each node has the correct count
	count1 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "0"))
	if count1 != 1.0 {
		t.Errorf("Expected count for node 47000 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47001", "TestObject", "1"))
	if count2 != 1.0 {
		t.Errorf("Expected count for node 47001 to be 1.0, got %f", count2)
	}

	count3 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47002", "AnotherType", "2"))
	if count3 != 1.0 {
		t.Errorf("Expected count for node 47002 to be 1.0, got %f", count3)
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Verify that metrics are properly registered with Prometheus
	// This ensures they can be collected and exposed
	if ObjectCount == nil {
		t.Error("ObjectCount metric should not be nil")
	}
	if AssignedShardsTotal == nil {
		t.Error("AssignedShardsTotal metric should not be nil")
	}
}

func TestSetAssignedShardCount(t *testing.T) {
	// Reset metrics before test
	AssignedShardsTotal.Reset()

	// Set shard count for a node
	SetAssignedShardCount("localhost:47000", 100)

	// Verify the metric was recorded
	count := testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count != 100.0 {
		t.Errorf("Expected shard count to be 100.0, got %f", count)
	}

	// Update shard count
	SetAssignedShardCount("localhost:47000", 150)
	count = testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count != 150.0 {
		t.Errorf("Expected shard count to be 150.0, got %f", count)
	}

	// Set shard count for another node
	SetAssignedShardCount("localhost:47001", 50)
	count = testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47001"))
	if count != 50.0 {
		t.Errorf("Expected shard count for node 47001 to be 50.0, got %f", count)
	}
}

func TestMultipleNodeShardCounts(t *testing.T) {
	// Reset metrics before test
	AssignedShardsTotal.Reset()

	// Test multiple nodes with different shard counts
	SetAssignedShardCount("localhost:47000", 2048)
	SetAssignedShardCount("localhost:47001", 2048)
	SetAssignedShardCount("localhost:47002", 2048)
	SetAssignedShardCount("localhost:47003", 2048)

	// Verify each node has the correct count
	for i := 0; i < 4; i++ {
		node := "localhost:4700" + string(rune('0'+i))
		count := testutil.ToFloat64(AssignedShardsTotal.WithLabelValues(node))
		if count != 2048.0 {
			t.Errorf("Expected shard count for %s to be 2048.0, got %f", node, count)
		}
	}
}

func TestShardCountZero(t *testing.T) {
	// Reset metrics before test
	AssignedShardsTotal.Reset()

	// Set shard count to a non-zero value
	SetAssignedShardCount("localhost:47000", 100)
	count := testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count != 100.0 {
		t.Errorf("Expected shard count to be 100.0, got %f", count)
	}

	// Set shard count to zero (e.g., when node loses all shards)
	SetAssignedShardCount("localhost:47000", 0)
	count = testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count != 0.0 {
		t.Errorf("Expected shard count to be 0.0, got %f", count)
	}
}

func TestShardTracking(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()

	// Create objects in different shards with the same type and node
	RecordObjectCreated("localhost:47000", "TestObject", 100)
	RecordObjectCreated("localhost:47000", "TestObject", 200)
	RecordObjectCreated("localhost:47000", "TestObject", 100)

	// Verify shard 100 has 2 objects
	count100 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "100"))
	if count100 != 2.0 {
		t.Errorf("Expected count for shard 100 to be 2.0, got %f", count100)
	}

	// Verify shard 200 has 1 object
	count200 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "200"))
	if count200 != 1.0 {
		t.Errorf("Expected count for shard 200 to be 1.0, got %f", count200)
	}

	// Delete one object from shard 100
	RecordObjectDeleted("localhost:47000", "TestObject", 100)

	// Verify shard 100 now has 1 object
	count100After := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "100"))
	if count100After != 1.0 {
		t.Errorf("Expected count for shard 100 after delete to be 1.0, got %f", count100After)
	}

	// Verify shard 200 still has 1 object
	count200After := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "200"))
	if count200After != 1.0 {
		t.Errorf("Expected count for shard 200 to remain 1.0, got %f", count200After)
	}
}
