package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Create another object of same type in same shard
	RecordObjectCreated("localhost:47000", "TestObject", 0)
	count = testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "0"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Create object of different type in different shard
	RecordObjectCreated("localhost:47000", "AnotherType", 1)
	count = testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "AnotherType", "1"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0 for AnotherType, got %f", count)
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
		t.Fatalf("Expected count to be 2.0, got %f", count)
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
		t.Fatalf("Expected count for node 47000 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47001", "TestObject", "1"))
	if count2 != 1.0 {
		t.Fatalf("Expected count for node 47001 to be 1.0, got %f", count2)
	}

	count3 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47002", "AnotherType", "2"))
	if count3 != 1.0 {
		t.Fatalf("Expected count for node 47002 to be 1.0, got %f", count3)
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Verify that metrics are properly registered with Prometheus
	// This ensures they can be collected and exposed
	if ObjectCount == nil {
		t.Fatal("ObjectCount metric should not be nil")
	}
	if AssignedShardsTotal == nil {
		t.Fatal("AssignedShardsTotal metric should not be nil")
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
		t.Fatalf("Expected shard count to be 100.0, got %f", count)
	}

	// Update shard count
	SetAssignedShardCount("localhost:47000", 150)
	count = testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count != 150.0 {
		t.Fatalf("Expected shard count to be 150.0, got %f", count)
	}

	// Set shard count for another node
	SetAssignedShardCount("localhost:47001", 50)
	count = testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47001"))
	if count != 50.0 {
		t.Fatalf("Expected shard count for node 47001 to be 50.0, got %f", count)
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
			t.Fatalf("Expected shard count for %s to be 2048.0, got %f", node, count)
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
		t.Fatalf("Expected shard count to be 100.0, got %f", count)
	}

	// Set shard count to zero (e.g., when node loses all shards)
	SetAssignedShardCount("localhost:47000", 0)
	count = testutil.ToFloat64(AssignedShardsTotal.WithLabelValues("localhost:47000"))
	if count != 0.0 {
		t.Fatalf("Expected shard count to be 0.0, got %f", count)
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
		t.Fatalf("Expected count for shard 100 to be 2.0, got %f", count100)
	}

	// Verify shard 200 has 1 object
	count200 := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "200"))
	if count200 != 1.0 {
		t.Fatalf("Expected count for shard 200 to be 1.0, got %f", count200)
	}

	// Delete one object from shard 100
	RecordObjectDeleted("localhost:47000", "TestObject", 100)

	// Verify shard 100 now has 1 object
	count100After := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "100"))
	if count100After != 1.0 {
		t.Fatalf("Expected count for shard 100 after delete to be 1.0, got %f", count100After)
	}

	// Verify shard 200 still has 1 object
	count200After := testutil.ToFloat64(ObjectCount.WithLabelValues("localhost:47000", "TestObject", "200"))
	if count200After != 1.0 {
		t.Fatalf("Expected count for shard 200 to remain 1.0, got %f", count200After)
	}
}

func TestRecordMethodCall(t *testing.T) {
	// Reset metrics before test
	MethodCallsTotal.Reset()

	// Record a successful method call
	RecordMethodCall("localhost:47000", "TestObject", "TestMethod", "success")

	// Verify the metric was recorded
	count := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "TestMethod", "success"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Record another successful call for the same method
	RecordMethodCall("localhost:47000", "TestObject", "TestMethod", "success")
	count = testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "TestMethod", "success"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Record a failed method call
	RecordMethodCall("localhost:47000", "TestObject", "TestMethod", "failure")
	failCount := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "TestMethod", "failure"))
	if failCount != 1.0 {
		t.Fatalf("Expected failure count to be 1.0, got %f", failCount)
	}

	// Success count should remain unchanged
	count = testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "TestMethod", "success"))
	if count != 2.0 {
		t.Fatalf("Expected success count to remain 2.0, got %f", count)
	}
}

func TestRecordMethodCallDuration(t *testing.T) {
	// Reset metrics before test
	MethodCallDuration.Reset()

	// Record method call durations
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.005)
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.05)
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.5)

	// For histograms, we verify by collecting metrics count
	// The histogram itself is a collector
	count := testutil.CollectAndCount(MethodCallDuration)
	// Each label combination creates a separate metric family
	// We expect at least 1 metric (the one we're testing)
	if count < 1 {
		t.Fatalf("Expected at least 1 metric, got %d", count)
	}
}

func TestMethodCallsMultipleNodes(t *testing.T) {
	// Reset metrics before test
	MethodCallsTotal.Reset()

	// Test multiple nodes with different methods
	RecordMethodCall("localhost:47000", "TestObject", "Method1", "success")
	RecordMethodCall("localhost:47001", "TestObject", "Method1", "success")
	RecordMethodCall("localhost:47002", "AnotherType", "Method2", "failure")

	// Verify each node has the correct count
	count1 := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "Method1", "success"))
	if count1 != 1.0 {
		t.Fatalf("Expected count for node 47000 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47001", "TestObject", "Method1", "success"))
	if count2 != 1.0 {
		t.Fatalf("Expected count for node 47001 to be 1.0, got %f", count2)
	}

	count3 := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47002", "AnotherType", "Method2", "failure"))
	if count3 != 1.0 {
		t.Fatalf("Expected count for node 47002 to be 1.0, got %f", count3)
	}
}

func TestMethodCallsMultipleMethods(t *testing.T) {
	// Reset metrics before test
	MethodCallsTotal.Reset()

	// Record calls to different methods on the same object type
	RecordMethodCall("localhost:47000", "TestObject", "Method1", "success")
	RecordMethodCall("localhost:47000", "TestObject", "Method2", "success")
	RecordMethodCall("localhost:47000", "TestObject", "Method3", "failure")
	RecordMethodCall("localhost:47000", "TestObject", "Method1", "success")

	// Verify Method1 has 2 successful calls
	count1 := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "Method1", "success"))
	if count1 != 2.0 {
		t.Fatalf("Expected count for Method1 to be 2.0, got %f", count1)
	}

	// Verify Method2 has 1 successful call
	count2 := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "Method2", "success"))
	if count2 != 1.0 {
		t.Fatalf("Expected count for Method2 to be 1.0, got %f", count2)
	}

	// Verify Method3 has 1 failed call
	count3 := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "TestObject", "Method3", "failure"))
	if count3 != 1.0 {
		t.Fatalf("Expected count for Method3 to be 1.0, got %f", count3)
	}
}

func TestMethodCallDurationHistogramBuckets(t *testing.T) {
	// Reset metrics before test
	MethodCallDuration.Reset()

	// Record durations in different buckets: 0.001, 0.01, 0.1, 1, 10
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.0005) // < 0.001
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.005)  // 0.001-0.01
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.05)   // 0.01-0.1
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.5)    // 0.1-1
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 5.0)    // 1-10
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 15.0)   // > 10

	// Verify the histogram has recorded all observations
	// For histograms, we verify by checking that metrics were collected
	count := testutil.CollectAndCount(MethodCallDuration)
	if count < 1 {
		t.Fatalf("Expected at least 1 metric, got %d", count)
	}
}

func TestMethodCallsMultipleObjectTypes(t *testing.T) {
	// Reset metrics before test
	MethodCallsTotal.Reset()

	// Record calls for different object types
	RecordMethodCall("localhost:47000", "UserObject", "GetUser", "success")
	RecordMethodCall("localhost:47000", "ChatRoom", "SendMessage", "success")
	RecordMethodCall("localhost:47000", "ChatClient", "Connect", "failure")

	// Verify each object type has the correct count
	userCount := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "UserObject", "GetUser", "success"))
	if userCount != 1.0 {
		t.Fatalf("Expected count for UserObject to be 1.0, got %f", userCount)
	}

	chatRoomCount := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "ChatRoom", "SendMessage", "success"))
	if chatRoomCount != 1.0 {
		t.Fatalf("Expected count for ChatRoom to be 1.0, got %f", chatRoomCount)
	}

	chatClientCount := testutil.ToFloat64(MethodCallsTotal.WithLabelValues("localhost:47000", "ChatClient", "Connect", "failure"))
	if chatClientCount != 1.0 {
		t.Fatalf("Expected count for ChatClient to be 1.0, got %f", chatClientCount)
	}
}

func TestMethodCallMetricsRegistration(t *testing.T) {
	// Verify that method call metrics are properly registered with Prometheus
	if MethodCallsTotal == nil {
		t.Fatal("MethodCallsTotal metric should not be nil")
	}

	if MethodCallDuration == nil {
		t.Fatal("MethodCallDuration metric should not be nil")
	}
}

func TestMethodCallStatusTracking(t *testing.T) {
	// Reset metrics before test
	MethodCallsTotal.Reset()

	// Test that success and failure statuses are tracked separately
	node := "localhost:47000"
	objType := "TestObject"
	method := "TestMethod"

	// Record multiple success and failure calls
	for i := 0; i < 5; i++ {
		RecordMethodCall(node, objType, method, "success")
	}
	for i := 0; i < 3; i++ {
		RecordMethodCall(node, objType, method, "failure")
	}

	// Verify success count
	successCount := testutil.ToFloat64(MethodCallsTotal.WithLabelValues(node, objType, method, "success"))
	if successCount != 5.0 {
		t.Fatalf("Expected success count to be 5.0, got %f", successCount)
	}

	// Verify failure count
	failureCount := testutil.ToFloat64(MethodCallsTotal.WithLabelValues(node, objType, method, "failure"))
	if failureCount != 3.0 {
		t.Fatalf("Expected failure count to be 3.0, got %f", failureCount)
	}
}

func TestMethodCallDurationWithStatus(t *testing.T) {
	// Reset metrics before test
	MethodCallDuration.Reset()

	// Record durations for successful calls
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.1)
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "success", 0.2)

	// Record durations for failed calls
	RecordMethodCallDuration("localhost:47000", "TestObject", "TestMethod", "failure", 0.05)

	// Verify histograms were recorded
	// For histograms, we verify by checking that metrics were collected
	count := testutil.CollectAndCount(MethodCallDuration)
	// We expect at least 1 metric family (both success and failure share the same histogram)
	if count < 1 {
		t.Fatalf("Expected at least 1 metric, got %d", count)
	}
}

func TestRecordClientConnected(t *testing.T) {
	// Reset metrics before test
	ClientsConnected.Reset()

	// Record client connection with default type (grpc)
	RecordClientConnected("localhost:47000", "")

	// Verify the metric was recorded with default type
	count := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Record another client connection with explicit type
	RecordClientConnected("localhost:47000", "grpc")
	count = testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Record client connection with different type
	RecordClientConnected("localhost:47000", "websocket")
	count = testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "websocket"))
	if count != 1.0 {
		t.Fatalf("Expected count for websocket to be 1.0, got %f", count)
	}
}

func TestRecordClientDisconnected(t *testing.T) {
	// Reset metrics before test
	ClientsConnected.Reset()

	// Create some client connections first
	RecordClientConnected("localhost:47000", "grpc")
	RecordClientConnected("localhost:47000", "grpc")
	RecordClientConnected("localhost:47000", "grpc")

	// Disconnect one client
	RecordClientDisconnected("localhost:47000", "grpc")

	// Verify the metric was decremented
	count := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Disconnect with empty type (should default to grpc)
	RecordClientDisconnected("localhost:47000", "")
	count = testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0 after disconnect with empty type, got %f", count)
	}
}

func TestMultipleNodesClients(t *testing.T) {
	// Reset metrics before test
	ClientsConnected.Reset()

	// Test multiple nodes with different client types
	RecordClientConnected("localhost:47000", "grpc")
	RecordClientConnected("localhost:47001", "grpc")
	RecordClientConnected("localhost:47002", "websocket")

	// Verify each node has the correct count
	count1 := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if count1 != 1.0 {
		t.Fatalf("Expected count for node 47000 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47001", "grpc"))
	if count2 != 1.0 {
		t.Fatalf("Expected count for node 47001 to be 1.0, got %f", count2)
	}

	count3 := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47002", "websocket"))
	if count3 != 1.0 {
		t.Fatalf("Expected count for node 47002 to be 1.0, got %f", count3)
	}
}

func TestClientTypeTracking(t *testing.T) {
	// Reset metrics before test
	ClientsConnected.Reset()

	// Create connections with different types on the same node
	RecordClientConnected("localhost:47000", "grpc")
	RecordClientConnected("localhost:47000", "grpc")
	RecordClientConnected("localhost:47000", "websocket")
	RecordClientConnected("localhost:47000", "websocket")
	RecordClientConnected("localhost:47000", "http")

	// Verify grpc has 2 connections
	countGrpc := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if countGrpc != 2.0 {
		t.Fatalf("Expected grpc count to be 2.0, got %f", countGrpc)
	}

	// Verify websocket has 2 connections
	countWs := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "websocket"))
	if countWs != 2.0 {
		t.Fatalf("Expected websocket count to be 2.0, got %f", countWs)
	}

	// Verify http has 1 connection
	countHttp := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "http"))
	if countHttp != 1.0 {
		t.Fatalf("Expected http count to be 1.0, got %f", countHttp)
	}

	// Disconnect one websocket client
	RecordClientDisconnected("localhost:47000", "websocket")

	// Verify websocket now has 1 connection
	countWsAfter := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "websocket"))
	if countWsAfter != 1.0 {
		t.Fatalf("Expected websocket count after disconnect to be 1.0, got %f", countWsAfter)
	}

	// Verify other types unchanged
	countGrpcAfter := testutil.ToFloat64(ClientsConnected.WithLabelValues("localhost:47000", "grpc"))
	if countGrpcAfter != 2.0 {
		t.Fatalf("Expected grpc count to remain 2.0, got %f", countGrpcAfter)
	}
}

func TestClientMetricsRegistration(t *testing.T) {
	// Verify that client metrics are properly registered with Prometheus
	if ClientsConnected == nil {
		t.Fatal("ClientsConnected metric should not be nil")
	}
}

func TestRecordShardClaim(t *testing.T) {
	// Reset metrics before test
	ShardClaimsTotal.Reset()

	// Record shard claims for a node
	RecordShardClaim("localhost:47000", 3)

	// Verify the metric was recorded
	count := testutil.ToFloat64(ShardClaimsTotal.WithLabelValues("localhost:47000"))
	if count != 3.0 {
		t.Fatalf("Expected count to be 3.0, got %f", count)
	}

	// Record more shard claims for the same node
	RecordShardClaim("localhost:47000", 2)
	count = testutil.ToFloat64(ShardClaimsTotal.WithLabelValues("localhost:47000"))
	if count != 5.0 {
		t.Fatalf("Expected count to be 5.0, got %f", count)
	}

	// Record shard claims for another node
	RecordShardClaim("localhost:47001", 4)
	count = testutil.ToFloat64(ShardClaimsTotal.WithLabelValues("localhost:47001"))
	if count != 4.0 {
		t.Fatalf("Expected count for node 47001 to be 4.0, got %f", count)
	}
}

func TestRecordShardClaimZero(t *testing.T) {
	// Reset metrics before test
	ShardClaimsTotal.Reset()

	// Record zero claims (should not increment)
	RecordShardClaim("localhost:47000", 0)

	// Verify the metric was not recorded
	count := testutil.ToFloat64(ShardClaimsTotal.WithLabelValues("localhost:47000"))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0, got %f", count)
	}
}

func TestRecordShardRelease(t *testing.T) {
	// Reset metrics before test
	ShardReleasesTotal.Reset()

	// Record shard releases for a node
	RecordShardRelease("localhost:47000", 2)

	// Verify the metric was recorded
	count := testutil.ToFloat64(ShardReleasesTotal.WithLabelValues("localhost:47000"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Record more shard releases for the same node
	RecordShardRelease("localhost:47000", 3)
	count = testutil.ToFloat64(ShardReleasesTotal.WithLabelValues("localhost:47000"))
	if count != 5.0 {
		t.Fatalf("Expected count to be 5.0, got %f", count)
	}

	// Record shard releases for another node
	RecordShardRelease("localhost:47001", 1)
	count = testutil.ToFloat64(ShardReleasesTotal.WithLabelValues("localhost:47001"))
	if count != 1.0 {
		t.Fatalf("Expected count for node 47001 to be 1.0, got %f", count)
	}
}

func TestRecordShardReleaseZero(t *testing.T) {
	// Reset metrics before test
	ShardReleasesTotal.Reset()

	// Record zero releases (should not increment)
	RecordShardRelease("localhost:47000", 0)

	// Verify the metric was not recorded
	count := testutil.ToFloat64(ShardReleasesTotal.WithLabelValues("localhost:47000"))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0, got %f", count)
	}
}

func TestRecordShardMigration(t *testing.T) {
	// Reset metrics before test
	ShardMigrationsTotal.Reset()

	// Record a shard migration
	RecordShardMigration("localhost:47000", "localhost:47001")

	// Verify the metric was recorded
	count := testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", "localhost:47001"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Record another migration with same direction
	RecordShardMigration("localhost:47000", "localhost:47001")
	count = testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", "localhost:47001"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Record migration in opposite direction (should be tracked separately)
	RecordShardMigration("localhost:47001", "localhost:47000")
	count = testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47001", "localhost:47000"))
	if count != 1.0 {
		t.Fatalf("Expected count for reverse migration to be 1.0, got %f", count)
	}

	// Original direction should remain unchanged
	count = testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", "localhost:47001"))
	if count != 2.0 {
		t.Fatalf("Expected count for original direction to remain 2.0, got %f", count)
	}
}

func TestRecordShardMigrationInvalid(t *testing.T) {
	// Reset metrics before test
	ShardMigrationsTotal.Reset()

	// Test with empty fromNode (should not record)
	RecordShardMigration("", "localhost:47001")
	count := testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("", "localhost:47001"))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0 for empty fromNode, got %f", count)
	}

	// Test with empty toNode (should not record)
	RecordShardMigration("localhost:47000", "")
	count = testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", ""))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0 for empty toNode, got %f", count)
	}

	// Test with same node (should not record - not a migration)
	RecordShardMigration("localhost:47000", "localhost:47000")
	count = testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", "localhost:47000"))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0 for same node, got %f", count)
	}
}

func TestSetShardsMigrating(t *testing.T) {
	// Reset metrics before test
	ShardsMigrating = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goverse_shards_migrating_test",
			Help: "Test gauge for shards migrating",
		},
	)

	// Set the number of shards migrating
	SetShardsMigrating(5.0)

	// Verify the metric was set
	count := testutil.ToFloat64(ShardsMigrating)
	if count != 5.0 {
		t.Fatalf("Expected count to be 5.0, got %f", count)
	}

	// Update the count
	SetShardsMigrating(10.0)
	count = testutil.ToFloat64(ShardsMigrating)
	if count != 10.0 {
		t.Fatalf("Expected count to be 10.0, got %f", count)
	}

	// Set to zero
	SetShardsMigrating(0.0)
	count = testutil.ToFloat64(ShardsMigrating)
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0, got %f", count)
	}
}

func TestShardMigrationMetricsRegistration(t *testing.T) {
	// Verify that shard migration metrics are properly registered with Prometheus
	if ShardClaimsTotal == nil {
		t.Fatal("ShardClaimsTotal metric should not be nil")
	}

	if ShardReleasesTotal == nil {
		t.Fatal("ShardReleasesTotal metric should not be nil")
	}

	if ShardMigrationsTotal == nil {
		t.Fatal("ShardMigrationsTotal metric should not be nil")
	}

	if ShardsMigrating == nil {
		t.Fatal("ShardsMigrating metric should not be nil")
	}
}

func TestMultipleNodeShardMigrations(t *testing.T) {
	// Reset metrics before test
	ShardMigrationsTotal.Reset()

	// Test migrations between multiple nodes
	RecordShardMigration("localhost:47000", "localhost:47001")
	RecordShardMigration("localhost:47000", "localhost:47002")
	RecordShardMigration("localhost:47001", "localhost:47002")
	RecordShardMigration("localhost:47002", "localhost:47000")

	// Verify each migration direction has correct count
	count1 := testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", "localhost:47001"))
	if count1 != 1.0 {
		t.Fatalf("Expected count for 47000->47001 to be 1.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47000", "localhost:47002"))
	if count2 != 1.0 {
		t.Fatalf("Expected count for 47000->47002 to be 1.0, got %f", count2)
	}

	count3 := testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47001", "localhost:47002"))
	if count3 != 1.0 {
		t.Fatalf("Expected count for 47001->47002 to be 1.0, got %f", count3)
	}

	count4 := testutil.ToFloat64(ShardMigrationsTotal.WithLabelValues("localhost:47002", "localhost:47000"))
	if count4 != 1.0 {
		t.Fatalf("Expected count for 47002->47000 to be 1.0, got %f", count4)
	}
}

func TestShardClaimAndReleaseSequence(t *testing.T) {
	// Reset metrics before test
	ShardClaimsTotal.Reset()
	ShardReleasesTotal.Reset()

	// Simulate a sequence of claims and releases
	node := "localhost:47000"

	// Initial claims
	RecordShardClaim(node, 10)
	claimCount := testutil.ToFloat64(ShardClaimsTotal.WithLabelValues(node))
	if claimCount != 10.0 {
		t.Fatalf("Expected claim count to be 10.0, got %f", claimCount)
	}

	// Release some shards
	RecordShardRelease(node, 3)
	releaseCount := testutil.ToFloat64(ShardReleasesTotal.WithLabelValues(node))
	if releaseCount != 3.0 {
		t.Fatalf("Expected release count to be 3.0, got %f", releaseCount)
	}

	// Claim more
	RecordShardClaim(node, 5)
	claimCount = testutil.ToFloat64(ShardClaimsTotal.WithLabelValues(node))
	if claimCount != 15.0 {
		t.Fatalf("Expected claim count to be 15.0, got %f", claimCount)
	}

	// Release more
	RecordShardRelease(node, 7)
	releaseCount = testutil.ToFloat64(ShardReleasesTotal.WithLabelValues(node))
	if releaseCount != 10.0 {
		t.Fatalf("Expected release count to be 10.0, got %f", releaseCount)
	}
}

func TestSetGateActiveClients(t *testing.T) {
	// Reset metrics before test
	GateActiveClients.Reset()

	// Set client count
	SetGateActiveClients("localhost:49000", 1)

	// Verify the metric was set
	count := testutil.ToFloat64(GateActiveClients.WithLabelValues("localhost:49000"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Set to different count
	SetGateActiveClients("localhost:49000", 5)
	count = testutil.ToFloat64(GateActiveClients.WithLabelValues("localhost:49000"))
	if count != 5.0 {
		t.Fatalf("Expected count to be 5.0, got %f", count)
	}

	// Set client count for different gate
	SetGateActiveClients("localhost:49001", 3)
	count = testutil.ToFloat64(GateActiveClients.WithLabelValues("localhost:49001"))
	if count != 3.0 {
		t.Fatalf("Expected count for gate 49001 to be 3.0, got %f", count)
	}

	// Set to zero
	SetGateActiveClients("localhost:49000", 0)
	count = testutil.ToFloat64(GateActiveClients.WithLabelValues("localhost:49000"))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0, got %f", count)
	}
}

func TestRecordGatePushedMessage(t *testing.T) {
	// Reset metrics before test
	GatePushedMessages.Reset()

	// Record pushed messages
	RecordGatePushedMessage("localhost:49000")

	// Verify the metric was recorded
	count := testutil.ToFloat64(GatePushedMessages.WithLabelValues("localhost:49000"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Record more pushed messages
	RecordGatePushedMessage("localhost:49000")
	RecordGatePushedMessage("localhost:49000")
	count = testutil.ToFloat64(GatePushedMessages.WithLabelValues("localhost:49000"))
	if count != 3.0 {
		t.Fatalf("Expected count to be 3.0, got %f", count)
	}

	// Record pushed message for different gate
	RecordGatePushedMessage("localhost:49001")
	count = testutil.ToFloat64(GatePushedMessages.WithLabelValues("localhost:49001"))
	if count != 1.0 {
		t.Fatalf("Expected count for gate 49001 to be 1.0, got %f", count)
	}
}

func TestSetNodeConnectedGates(t *testing.T) {
	// Reset metrics before test
	NodeConnectedGates.Reset()

	// Set gate count
	SetNodeConnectedGates("localhost:47000", 1)

	// Verify the metric was set
	count := testutil.ToFloat64(NodeConnectedGates.WithLabelValues("localhost:47000"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Set to different count
	SetNodeConnectedGates("localhost:47000", 3)
	count = testutil.ToFloat64(NodeConnectedGates.WithLabelValues("localhost:47000"))
	if count != 3.0 {
		t.Fatalf("Expected count to be 3.0, got %f", count)
	}

	// Set gate count for different node
	SetNodeConnectedGates("localhost:47001", 2)
	count = testutil.ToFloat64(NodeConnectedGates.WithLabelValues("localhost:47001"))
	if count != 2.0 {
		t.Fatalf("Expected count for node 47001 to be 2.0, got %f", count)
	}

	// Set to zero
	SetNodeConnectedGates("localhost:47000", 0)
	count = testutil.ToFloat64(NodeConnectedGates.WithLabelValues("localhost:47000"))
	if count != 0.0 {
		t.Fatalf("Expected count to be 0.0, got %f", count)
	}
}

func TestRecordNodePushedMessage(t *testing.T) {
	// Reset metrics before test
	NodePushedMessages.Reset()

	// Record pushed message from node to gate
	RecordNodePushedMessage("localhost:47000", "localhost:49000")

	// Verify the metric was recorded
	count := testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47000", "localhost:49000"))
	if count != 1.0 {
		t.Fatalf("Expected count to be 1.0, got %f", count)
	}

	// Record more pushed messages for same node-gate pair
	RecordNodePushedMessage("localhost:47000", "localhost:49000")
	RecordNodePushedMessage("localhost:47000", "localhost:49000")
	count = testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47000", "localhost:49000"))
	if count != 3.0 {
		t.Fatalf("Expected count to be 3.0, got %f", count)
	}

	// Record pushed message for different gate
	RecordNodePushedMessage("localhost:47000", "localhost:49001")
	count = testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47000", "localhost:49001"))
	if count != 1.0 {
		t.Fatalf("Expected count for different gate to be 1.0, got %f", count)
	}

	// Verify original gate count unchanged
	count = testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47000", "localhost:49000"))
	if count != 3.0 {
		t.Fatalf("Expected original gate count to remain 3.0, got %f", count)
	}
}

func TestGateAndNodeMetricsRegistration(t *testing.T) {
	// Verify that gate and node metrics are properly registered with Prometheus
	if GateActiveClients == nil {
		t.Fatal("GateActiveClients metric should not be nil")
	}

	if GatePushedMessages == nil {
		t.Fatal("GatePushedMessages metric should not be nil")
	}

	if GateDroppedMessages == nil {
		t.Fatal("GateDroppedMessages metric should not be nil")
	}

	if NodeConnectedGates == nil {
		t.Fatal("NodeConnectedGates metric should not be nil")
	}

	if NodePushedMessages == nil {
		t.Fatal("NodePushedMessages metric should not be nil")
	}

	if NodeDroppedMessages == nil {
		t.Fatal("NodeDroppedMessages metric should not be nil")
	}
}

func TestMultipleNodesAndGates(t *testing.T) {
	// Reset metrics before test
	NodeConnectedGates.Reset()
	NodePushedMessages.Reset()
	GateActiveClients.Reset()
	GatePushedMessages.Reset()

	// Simulate multiple nodes with multiple gates
	SetNodeConnectedGates("localhost:47000", 2)
	SetNodeConnectedGates("localhost:47001", 1)

	// Verify each node has the correct gate count
	count1 := testutil.ToFloat64(NodeConnectedGates.WithLabelValues("localhost:47000"))
	if count1 != 2.0 {
		t.Fatalf("Expected gate count for node 47000 to be 2.0, got %f", count1)
	}

	count2 := testutil.ToFloat64(NodeConnectedGates.WithLabelValues("localhost:47001"))
	if count2 != 1.0 {
		t.Fatalf("Expected gate count for node 47001 to be 1.0, got %f", count2)
	}

	// Simulate messages from nodes to gates
	RecordNodePushedMessage("localhost:47000", "localhost:49000")
	RecordNodePushedMessage("localhost:47000", "localhost:49001")
	RecordNodePushedMessage("localhost:47001", "localhost:49000")

	// Verify message counts
	msgCount1 := testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47000", "localhost:49000"))
	if msgCount1 != 1.0 {
		t.Fatalf("Expected message count for 47000->49000 to be 1.0, got %f", msgCount1)
	}

	msgCount2 := testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47000", "localhost:49001"))
	if msgCount2 != 1.0 {
		t.Fatalf("Expected message count for 47000->49001 to be 1.0, got %f", msgCount2)
	}

	msgCount3 := testutil.ToFloat64(NodePushedMessages.WithLabelValues("localhost:47001", "localhost:49000"))
	if msgCount3 != 1.0 {
		t.Fatalf("Expected message count for 47001->49000 to be 1.0, got %f", msgCount3)
	}

	// Simulate gate clients
	SetGateActiveClients("localhost:49000", 2)
	SetGateActiveClients("localhost:49001", 1)

	// Verify client counts
	clientCount1 := testutil.ToFloat64(GateActiveClients.WithLabelValues("localhost:49000"))
	if clientCount1 != 2.0 {
		t.Fatalf("Expected client count for gate 49000 to be 2.0, got %f", clientCount1)
	}

	clientCount2 := testutil.ToFloat64(GateActiveClients.WithLabelValues("localhost:49001"))
	if clientCount2 != 1.0 {
		t.Fatalf("Expected client count for gate 49001 to be 1.0, got %f", clientCount2)
	}

	// Simulate pushed messages from gates to clients
	RecordGatePushedMessage("localhost:49000")
	RecordGatePushedMessage("localhost:49000")
	RecordGatePushedMessage("localhost:49001")

	// Verify pushed message counts
	pushedCount1 := testutil.ToFloat64(GatePushedMessages.WithLabelValues("localhost:49000"))
	if pushedCount1 != 2.0 {
		t.Fatalf("Expected pushed message count for gate 49000 to be 2.0, got %f", pushedCount1)
	}

	pushedCount2 := testutil.ToFloat64(GatePushedMessages.WithLabelValues("localhost:49001"))
	if pushedCount2 != 1.0 {
		t.Fatalf("Expected pushed message count for gate 49001 to be 1.0, got %f", pushedCount2)
	}
}

func TestRecordGateDroppedMessage(t *testing.T) {
	// Reset metrics before test
	GateDroppedMessages.Reset()

	// Record dropped messages
	RecordGateDroppedMessage("localhost:49000")
	RecordGateDroppedMessage("localhost:49000")

	// Verify the metric was recorded
	count := testutil.ToFloat64(GateDroppedMessages.WithLabelValues("localhost:49000"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Record dropped message for different gate
	RecordGateDroppedMessage("localhost:49001")
	count = testutil.ToFloat64(GateDroppedMessages.WithLabelValues("localhost:49001"))
	if count != 1.0 {
		t.Fatalf("Expected count for gate 49001 to be 1.0, got %f", count)
	}
}

func TestRecordNodeDroppedMessage(t *testing.T) {
	// Reset metrics before test
	NodeDroppedMessages.Reset()

	// Record dropped messages
	RecordNodeDroppedMessage("localhost:47000", "localhost:49000")
	RecordNodeDroppedMessage("localhost:47000", "localhost:49000")

	// Verify the metric was recorded
	count := testutil.ToFloat64(NodeDroppedMessages.WithLabelValues("localhost:47000", "localhost:49000"))
	if count != 2.0 {
		t.Fatalf("Expected count to be 2.0, got %f", count)
	}

	// Record dropped message for different node-gate pair
	RecordNodeDroppedMessage("localhost:47000", "localhost:49001")
	count = testutil.ToFloat64(NodeDroppedMessages.WithLabelValues("localhost:47000", "localhost:49001"))
	if count != 1.0 {
		t.Fatalf("Expected count for 47000->49001 to be 1.0, got %f", count)
	}
}
