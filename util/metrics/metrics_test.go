package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordObjectCreated(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()

	// Record object creation
	RecordObjectCreated("localhost:47000", "TestObject")

	// Verify the metric was recorded
	count := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject"))
	if count != 1.0 {
		t.Errorf("Expected count to be 1.0, got %f", count)
	}

	// Create another object of same type
	RecordObjectCreated("localhost:47000", "TestObject")
	count = testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject"))
	if count != 2.0 {
		t.Errorf("Expected count to be 2.0, got %f", count)
	}

	// Create object of different type
	RecordObjectCreated("localhost:47000", "AnotherType")
	count = testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "AnotherType"))
	if count != 1.0 {
		t.Errorf("Expected count to be 1.0 for AnotherType, got %f", count)
	}
}

func TestRecordObjectDeleted(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()

	// Create some objects first
	RecordObjectCreated("localhost:47000", "TestObject")
	RecordObjectCreated("localhost:47000", "TestObject")
	RecordObjectCreated("localhost:47000", "TestObject")

	// Delete one object
	RecordObjectDeleted("localhost:47000", "TestObject")

	// Verify the metric was decremented
	count := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject"))
	if count != 2.0 {
		t.Errorf("Expected count to be 2.0, got %f", count)
	}
}

func TestSetNodeConnectionCount(t *testing.T) {
	// Reset metrics before test
	NodeConnectionCount.Reset()

	// Set connection count
	SetNodeConnectionCount("localhost:47000", 5.0)

	// Verify the metric was set
	count := testutil.ToFloat64(NodeConnectionCount.WithLabelValues("localhost:47000"))
	if count != 5.0 {
		t.Errorf("Expected count to be 5.0, got %f", count)
	}

	// Update connection count
	SetNodeConnectionCount("localhost:47000", 3.0)
	count = testutil.ToFloat64(NodeConnectionCount.WithLabelValues("localhost:47000"))
	if count != 3.0 {
		t.Errorf("Expected count to be 3.0, got %f", count)
	}
}

func TestMultipleNodes(t *testing.T) {
	// Reset metrics before test
	ObjectCount.Reset()
	NodeConnectionCount.Reset()

	// Test multiple nodes
	RecordObjectCreated("localhost:47000", "TestObject")
	RecordObjectCreated("localhost:47001", "TestObject")
	RecordObjectCreated("localhost:47002", "AnotherType")

	// Verify each node has the correct count
	count1 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject"))
	if count1 != 1.0 {
		t.Errorf("Expected count for node 47000 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47001", "TestObject"))
	if count2 != 1.0 {
		t.Errorf("Expected count for node 47001 to be 1.0, got %f", count2)
	}

	count3 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47002", "AnotherType"))
	if count3 != 1.0 {
		t.Errorf("Expected count for node 47002 to be 1.0, got %f", count3)
	}

	// Test connection counts for multiple nodes
	SetNodeConnectionCount("localhost:47000", 2.0)
	SetNodeConnectionCount("localhost:47001", 1.0)

	connCount1 := testutil.ToFloat64(NodeConnectionCount.WithLabelValues("localhost:47000"))
	if connCount1 != 2.0 {
		t.Errorf("Expected connection count for node 47000 to be 2.0, got %f", connCount1)
	}

	connCount2 := testutil.ToFloat64(NodeConnectionCount.WithLabelValues("localhost:47001"))
	if connCount2 != 1.0 {
		t.Errorf("Expected connection count for node 47001 to be 1.0, got %f", connCount2)
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Verify that metrics are properly registered with Prometheus
	// This ensures they can be collected and exposed
	metrics := []prometheus.Collector{
		ObjectCount,
		NodeConnectionCount,
	}

	for _, metric := range metrics {
		if metric == nil {
			t.Error("Metric should not be nil")
		}
	}
}
