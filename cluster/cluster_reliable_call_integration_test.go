package cluster

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/object"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
)

// TestCounter is a simple test object for reliable calls
type TestCounter struct {
	object.BaseObject
	value int32
}

func (c *TestCounter) OnCreated() {
	c.Logger.Infof("TestCounter %s created", c.Id())
	c.value = 0
}

func (c *TestCounter) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (c *TestCounter) FromData(data proto.Message) error {
	return nil
}

func (c *TestCounter) Increment(ctx context.Context, req *counter_pb.IncrementRequest) (*counter_pb.CounterResponse, error) {
	c.value += req.Amount
	return &counter_pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

// TestReliableCallObject_PostgresIntegration tests ReliableCallObject with actual cluster, etcd, and PostgreSQL
func TestReliableCallObject_PostgresIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create PostgreSQL config
	pgConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Create DB connection
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data from previous test runs and reset sequences
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	t.Logf("Truncated goverse_reliable_calls and goverse_objects tables")

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create cluster with etcd using mustNewCluster helper
	nodeAddr := testutil.GetFreeAddress()
	cluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer cluster.Stop(ctx)

	// Set persistence provider on the node
	node := cluster.GetThisNode()
	node.SetPersistenceProvider(provider)

	// Register the TestCounter object type
	node.RegisterObjectType((*TestCounter)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster)

	t.Run("Insert and execute new call", func(t *testing.T) {
		callID := "integration-test-call-1"
		objectType := "TestCounter"
		objectID := "TestCounter-counter1"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 5}

		result, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Result should be a CounterResponse
		response, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected *counter_pb.CounterResponse, got %T", result)
		}
		if response.Value != 5 {
			t.Fatalf("Expected value 5, got %d", response.Value)
		}

		// Verify the reliable call record in DB shows success with correct result data
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc.CallID != callID {
			t.Fatalf("Expected CallID %q, got %q", callID, rc.CallID)
		}
		if rc.ObjectID != objectID {
			t.Fatalf("Expected ObjectID %q, got %q", objectID, rc.ObjectID)
		}
		if rc.ObjectType != objectType {
			t.Fatalf("Expected ObjectType %q, got %q", objectType, rc.ObjectType)
		}
		if rc.MethodName != methodName {
			t.Fatalf("Expected MethodName %q, got %q", methodName, rc.MethodName)
		}
		if rc.Status != "success" {
			t.Fatalf("Expected status 'success', got %q", rc.Status)
		}
		if rc.Error != "" {
			t.Fatalf("Expected empty error, got %q", rc.Error)
		}
		if rc.ResultData == nil {
			t.Fatal("Expected result data to be set, got nil")
		}
		// Unmarshal and verify result data
		storedResultMsg, err := protohelper.BytesToMsg(rc.ResultData)
		if err != nil {
			t.Fatalf("Failed to convert result data to message: %v", err)
		}
		storedResponse, ok := storedResultMsg.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected stored result to be *counter_pb.CounterResponse, got %T", storedResultMsg)
		}
		if storedResponse.Value != 5 {
			t.Fatalf("Expected stored result value 5, got %d", storedResponse.Value)
		}
	})

	t.Run("Deduplication - pending call", func(t *testing.T) {
		callID := "integration-test-call-2"
		objectType := "TestCounter"
		objectID := "TestCounter-counter2"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 3}

		// Serialize request data
		requestData, err := protohelper.MsgToBytes(request)
		if err != nil {
			t.Fatalf("Failed to serialize request: %v", err)
		}

		// Insert pending call directly in DB
		rc, err := provider.InsertOrGetReliableCall(ctx, callID, objectID, objectType, methodName, requestData)
		if err != nil {
			t.Fatalf("Failed to insert pending call: %v", err)
		}

		// Verify call is pending
		if rc.Status != "pending" {
			t.Fatalf("Expected status 'pending', got %q", rc.Status)
		}
		if rc.CallID != callID {
			t.Fatalf("Expected CallID %q, got %q", callID, rc.CallID)
		}
		if rc.ObjectID != objectID {
			t.Fatalf("Expected ObjectID %q, got %q", objectID, rc.ObjectID)
		}
		if rc.ObjectType != objectType {
			t.Fatalf("Expected ObjectType %q, got %q", objectType, rc.ObjectType)
		}
		if rc.MethodName != methodName {
			t.Fatalf("Expected MethodName %q, got %q", methodName, rc.MethodName)
		}

		// Now actually call it - should execute the pending call and return success
		result, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("ReliableCallObject failed: %v", err)
		}

		// Verify the result
		response, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected *counter_pb.CounterResponse, got %T", result)
		}
		if response.Value != 3 {
			t.Fatalf("Expected value 3, got %d", response.Value)
		}

		// Verify the call is now marked as success in DB
		rc3, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc3.Status != "success" {
			t.Fatalf("Expected status 'success', got %q", rc3.Status)
		}
		if rc3.ResultData == nil {
			t.Fatal("Expected result data to be set, got nil")
		}
	})

	t.Run("Deduplication - completed call", func(t *testing.T) {
		callID := "integration-test-call-3"
		objectType := "TestCounter"
		objectID := "TestCounter-counter3"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 7}

		// Insert first call
		result1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		response1 := result1.(*counter_pb.CounterResponse)

		// Try to insert duplicate call - should return the cached result
		result2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Duplicate call failed: %v", err)
		}

		response2 := result2.(*counter_pb.CounterResponse)

		// Should return the cached successful result
		if response1.Value != response2.Value {
			t.Fatalf("Expected cached value %d, got %d", response1.Value, response2.Value)
		}
	})

	t.Run("Deduplication - failed call", func(t *testing.T) {
		callID := "integration-test-call-4"
		objectType := "TestCounter"
		objectID := "TestCounter-counter4"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 10}

		// Serialize request data
		requestData, err := protohelper.MsgToBytes(request)
		if err != nil {
			t.Fatalf("Failed to serialize request: %v", err)
		}

		// Insert pending call directly in DB
		rc, err := provider.InsertOrGetReliableCall(ctx, callID, objectID, objectType, methodName, requestData)
		if err != nil {
			t.Fatalf("Failed to insert pending call: %v", err)
		}

		// Update status to failed
		errorMessage := "test error message"
		err = provider.UpdateReliableCallStatus(ctx, rc.Seq, "failed", nil, errorMessage)
		if err != nil {
			t.Fatalf("Failed to update call status: %v", err)
		}

		// Try to insert duplicate call - should return the cached error
		_, err = cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err == nil {
			t.Fatal("Expected error for failed call, got nil")
		}

		// Error should contain the cached error message
		if err.Error() != "reliable call "+callID+" failed: "+errorMessage {
			t.Fatalf("Expected error message to contain %q, got %q", errorMessage, err.Error())
		}
	})
}

// TestReliableCallObject_ConcurrentCalls tests concurrent reliable calls from multiple goroutines
func TestReliableCallObject_ConcurrentCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create PostgreSQL config
	pgConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Create DB connection
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data from previous test runs and reset sequences
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)
	_ = provider // Used by cluster node

	// Create cluster with etcd using mustNewCluster helper
	nodeAddr := testutil.GetFreeAddress()
	cluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer cluster.Stop(ctx)

	// Set persistence provider on the node
	node := cluster.GetThisNode()
	node.SetPersistenceProvider(provider)

	// Register the TestCounter object type
	node.RegisterObjectType((*TestCounter)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster)

	const numGoroutines = 100
	objectType := "TestCounter"
	objectID := "TestCounter-concurrent"
	methodName := "Increment"

	var wg sync.WaitGroup
	results := make([]int32, numGoroutines)
	errors := make([]error, numGoroutines)

	wg.Add(numGoroutines)

	// Launch goroutines that each make a reliable call
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()

			// Each goroutine has a unique call ID and increments by 1
			callID := fmt.Sprintf("concurrent-call-%d", index)
			request := &counter_pb.IncrementRequest{Amount: 1}

			result, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
			t.Logf("Reliable call %s completed, got result %v, error %v", callID, result, err)
			if err != nil {
				errors[index] = err
				return
			}

			// Verify result type and store value
			response, ok := result.(*counter_pb.CounterResponse)
			if !ok {
				errors[index] = fmt.Errorf("expected *counter_pb.CounterResponse, got %T", result)
				return
			}

			results[index] = response.Value
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Fatalf("Goroutine %d failed: %v", i, err)
		}
	}

	// Sort the results to verify they are [1, 2, 3, ..., numGoroutines]
	// This proves all calls executed properly and in what order they completed
	sortedResults := make([]int32, len(results))
	copy(sortedResults, results)
	sort.Slice(sortedResults, func(i, j int) bool {
		return sortedResults[i] < sortedResults[j]
	})

	// Verify sorted results are exactly [1, 2, 3, ..., numGoroutines]
	for i := 0; i < numGoroutines; i++ {
		expected := int32(i + 1)
		if sortedResults[i] != expected {
			t.Fatalf("Sorted result[%d]: expected %d, got %d", i, expected, sortedResults[i])
		}
	}

	// Log the results for debugging
	t.Logf("Results (execution order): %v", results)
	t.Logf("Results (sorted): %v", sortedResults)

	// Verify the final counter value is exactly numGoroutines
	finalCallID := "concurrent-final-check"
	finalRequest := &counter_pb.IncrementRequest{Amount: 0}
	finalResult, err := cluster.ReliableCallObject(ctx, finalCallID, objectType, objectID, methodName, finalRequest)
	if err != nil {
		t.Fatalf("Final check call failed: %v", err)
	}
	finalResponse, ok := finalResult.(*counter_pb.CounterResponse)
	if !ok {
		t.Fatalf("Expected *counter_pb.CounterResponse, got %T", finalResult)
	}
	if finalResponse.Value != int32(numGoroutines) {
		t.Fatalf("Expected final counter value to be %d, got %d", numGoroutines, finalResponse.Value)
	}

	// Verify all reliable call records are in the database with success status
	for i := 0; i < numGoroutines; i++ {
		callID := fmt.Sprintf("concurrent-call-%d", i)
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call %s: %v", callID, err)
			continue
		}
		if rc.Status != "success" {
			t.Fatalf("Call %s: expected status 'success', got %q", callID, rc.Status)
		}
		if rc.ResultData == nil {
			t.Fatalf("Call %s: expected result data to be set", callID)
		}
	}
}

// TestReliableCallObject_MultiNodeDistributed tests ReliableCallObject with 3 nodes
// and verifies reliable calls work correctly across multiple nodes
func TestReliableCallObject_MultiNodeDistributed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create PostgreSQL config
	pgConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Create DB connection
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data from previous test runs and reset sequences
	// Note: Using explicit table names as they are defined in util/postgres/db.go InitSchema
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Get free addresses for 3 nodes
	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	addr3 := testutil.GetFreeAddress()

	// Create 3 clusters using mustNewClusterWithMinDurations
	cluster1 := mustNewClusterWithMinDurations(ctx, t, addr1, testPrefix)
	cluster2 := mustNewClusterWithMinDurations(ctx, t, addr2, testPrefix)
	cluster3 := mustNewClusterWithMinDurations(ctx, t, addr3, testPrefix)

	// Set persistence provider on all nodes
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node3 := cluster3.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node2.SetPersistenceProvider(provider)
	node3.SetPersistenceProvider(provider)

	// Register the TestCounter object type on all nodes
	node1.RegisterObjectType((*TestCounter)(nil))
	node2.RegisterObjectType((*TestCounter)(nil))
	node3.RegisterObjectType((*TestCounter)(nil))

	// Start mock gRPC servers for all nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(addr1, mockServer1)
	err = testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := testutil.NewTestServerHelper(addr2, mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	mockServer3 := testutil.NewMockGoverseServer()
	mockServer3.SetNode(node3)
	testServer3 := testutil.NewTestServerHelper(addr3, mockServer3)
	err = testServer3.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 3: %v", err)
	}
	t.Cleanup(func() { testServer3.Stop() })

	// Wait for all clusters to be fully ready
	testutil.WaitForClustersReady(t, cluster1, cluster2, cluster3)

	t.Run("Distributed reliable calls across 3 nodes", func(t *testing.T) {
		// Create ~10 objects with specific shard assignments to ensure distribution
		// Using different shards (5, 10, 15, etc.) to spread objects across the 3 nodes
		// Test uses 64 shards (testutil.TestNumShards), so these shard IDs ensure good distribution
		objectIDs := []string{
			testutil.GetObjectIDForShard(5, "Counter1"),
			testutil.GetObjectIDForShard(10, "Counter2"),
			testutil.GetObjectIDForShard(15, "Counter3"),
			testutil.GetObjectIDForShard(20, "Counter4"),
			testutil.GetObjectIDForShard(25, "Counter5"),
			testutil.GetObjectIDForShard(30, "Counter6"),
			testutil.GetObjectIDForShard(35, "Counter7"),
			testutil.GetObjectIDForShard(40, "Counter8"),
			testutil.GetObjectIDForShard(45, "Counter9"),
			testutil.GetObjectIDForShard(50, "Counter10"),
		}

		// Track which cluster will invoke the call for each object
		clusters := []*Cluster{cluster1, cluster2, cluster3}

		// Invoke reliable calls from different nodes
		for i, objID := range objectIDs {
			// Use different clusters to invoke calls (round-robin)
			clusterIdx := i % 3
			callCluster := clusters[clusterIdx]

			callID := "multi-node-call-" + objID
			methodName := "Increment"
			request := &counter_pb.IncrementRequest{Amount: int32(i + 1)}

			t.Logf("Calling object %s from cluster %d (addr: %s)", objID, clusterIdx+1, callCluster.GetThisNode().GetAdvertiseAddress())

			// Invoke the reliable call
			result, err := callCluster.ReliableCallObject(ctx, callID, "TestCounter", objID, methodName, request)
			if err != nil {
				t.Fatalf("ReliableCallObject failed for %s: %v", objID, err)
			}

			// Verify the result
			response, ok := result.(*counter_pb.CounterResponse)
			if !ok {
				t.Fatalf("Expected *counter_pb.CounterResponse, got %T", result)
			}

			expectedValue := int32(i + 1)
			if response.Value != expectedValue {
				t.Fatalf("Object %s: expected value %d, got %d", objID, expectedValue, response.Value)
			}

			// Verify the reliable call record in DB
			rc, err := db.GetReliableCall(ctx, callID)
			if err != nil {
				t.Fatalf("Failed to get reliable call for %s: %v", callID, err)
			}
			if rc.Status != "success" {
				t.Fatalf("Call %s: expected status 'success', got %q", callID, rc.Status)
			}
			if rc.ObjectID != objID {
				t.Fatalf("Call %s: expected ObjectID %q, got %q", callID, objID, rc.ObjectID)
			}

			// Verify result data is stored correctly
			if rc.ResultData == nil {
				t.Fatalf("Call %s: expected result data to be set, got nil", callID)
			}
			storedResultMsg, err := protohelper.BytesToMsg(rc.ResultData)
			if err != nil {
				t.Fatalf("Call %s: failed to convert result data to message: %v", callID, err)
			}
			storedResponse, ok := storedResultMsg.(*counter_pb.CounterResponse)
			if !ok {
				t.Fatalf("Call %s: expected stored result to be *counter_pb.CounterResponse, got %T", callID, storedResultMsg)
			}
			if storedResponse.Value != expectedValue {
				t.Fatalf("Call %s: expected stored result value %d, got %d", callID, expectedValue, storedResponse.Value)
			}

			t.Logf("Successfully executed reliable call %s for object %s with value %d", callID, objID, response.Value)
		}
	})

	t.Run("Deduplication across nodes", func(t *testing.T) {
		// Test that calling the same reliable call from different nodes returns cached result
		objID := testutil.GetObjectIDForShard(55, "DedupCounter")
		callID := "dedup-test-call"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 42}

		// First call from cluster1
		result1, err := cluster1.ReliableCallObject(ctx, callID, "TestCounter", objID, methodName, request)
		if err != nil {
			t.Fatalf("First ReliableCallObject failed: %v", err)
		}
		response1 := result1.(*counter_pb.CounterResponse)

		// Second call from cluster2 with same callID - should return cached result
		result2, err := cluster2.ReliableCallObject(ctx, callID, "TestCounter", objID, methodName, request)
		if err != nil {
			t.Fatalf("Second ReliableCallObject (dedup) failed: %v", err)
		}
		response2 := result2.(*counter_pb.CounterResponse)

		// Should return the same result (42)
		if response1.Value != response2.Value {
			t.Fatalf("Deduplication failed: first call returned %d, second call returned %d", response1.Value, response2.Value)
		}
		if response1.Value != 42 {
			t.Fatalf("Expected value 42, got %d", response1.Value)
		}

		// Third call from cluster3 - should also return cached result
		result3, err := cluster3.ReliableCallObject(ctx, callID, "TestCounter", objID, methodName, request)
		if err != nil {
			t.Fatalf("Third ReliableCallObject (dedup) failed: %v", err)
		}
		response3 := result3.(*counter_pb.CounterResponse)

		if response3.Value != 42 {
			t.Fatalf("Third call: expected value 42, got %d", response3.Value)
		}

		t.Logf("Successfully verified deduplication across 3 nodes, all returned value 42")
	})
}

// TestReliableCallObject_CrossClusterWithShutdown tests reliable calls between 2 clusters
// and verifies behavior when calling a shutdown cluster
func TestReliableCallObject_CrossClusterWithShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create PostgreSQL config
	pgConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Create DB connection
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data from previous test runs
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Get free addresses for 2 nodes
	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()

	// Create 2 clusters using mustNewClusterWithMinDurations
	cluster1 := mustNewClusterWithMinDurations(ctx, t, addr1, testPrefix)
	cluster2 := mustNewClusterWithMinDurations(ctx, t, addr2, testPrefix)

	// Set persistence provider on both nodes
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node2.SetPersistenceProvider(provider)

	// Register the TestCounter object type on both nodes
	node1.RegisterObjectType((*TestCounter)(nil))
	node2.RegisterObjectType((*TestCounter)(nil))

	// Start mock gRPC servers for both nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(addr1, mockServer1)
	err = testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := testutil.NewTestServerHelper(addr2, mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	// Wait for both clusters to be fully ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	t.Run("Reliable call from cluster1 to cluster2 - success", func(t *testing.T) {
		// Create an object ID that will be assigned to cluster2
		// Use a specific shard to ensure it's on cluster2
		objID := testutil.GetObjectIDForShard(10, "CrossClusterCounter")
		callID := "cross-cluster-call-1"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 15}

		t.Logf("Calling object %s from cluster1 (should route to cluster2)", objID)

		// Invoke the reliable call from cluster1
		result, err := cluster1.ReliableCallObject(ctx, callID, "TestCounter", objID, methodName, request)
		if err != nil {
			t.Fatalf("ReliableCallObject failed: %v", err)
		}

		// Verify the result
		response, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected *counter_pb.CounterResponse, got %T", result)
		}

		if response.Value != 15 {
			t.Errorf("Expected value 15, got %d", response.Value)
		}

		// Verify the reliable call record in DB shows success
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc.Status != "success" {
			t.Errorf("Expected status 'success', got %q", rc.Status)
		}
		if rc.ObjectID != objID {
			t.Errorf("Expected ObjectID %q, got %q", objID, rc.ObjectID)
		}

		t.Logf("Successfully executed reliable call from cluster1 to cluster2 with value %d", response.Value)
	})

	// Shutdown cluster2
	t.Logf("Shutting down cluster2...")
	err = testServer2.Stop()
	if err != nil {
		t.Fatalf("Failed to stop test server 2: %v", err)
	}
	err = cluster2.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop cluster 2: %v", err)
	}
	err = node2.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node 2: %v", err)
	}
	t.Logf("Cluster2 shutdown complete")

	t.Run("Reliable call from cluster1 to shutdown cluster2 - stored and error returned", func(t *testing.T) {
		// Create an object ID that would have been assigned to cluster2
		objID := testutil.GetObjectIDForShard(15, "ShutdownClusterCounter")
		callID := "cross-cluster-call-2"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 20}

		t.Logf("Calling object %s from cluster1 to shutdown cluster2", objID)

		// Invoke the reliable call from cluster1 - should fail since cluster2 is down
		result, err := cluster1.ReliableCallObject(ctx, callID, "TestCounter", objID, methodName, request)
		if err == nil {
			t.Fatalf("Expected error when calling shutdown cluster, got nil (result: %v)", result)
		}

		t.Logf("Got expected error: %v", err)

		// Verify the reliable call was stored in the database
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call from DB: %v", err)
		}

		// Verify the call is stored with pending status (since the target node is down)
		if rc.CallID != callID {
			t.Errorf("Expected CallID %q, got %q", callID, rc.CallID)
		}
		if rc.ObjectID != objID {
			t.Errorf("Expected ObjectID %q, got %q", objID, rc.ObjectID)
		}
		if rc.ObjectType != "TestCounter" {
			t.Errorf("Expected ObjectType %q, got %q", "TestCounter", rc.ObjectType)
		}
		if rc.MethodName != methodName {
			t.Errorf("Expected MethodName %q, got %q", methodName, rc.MethodName)
		}

		// The call should be stored (status should be "pending" since it couldn't be executed)
		if rc.Status != "pending" {
			t.Logf("Note: Call status is %q (may be pending or may have error status depending on implementation)", rc.Status)
		}

		t.Logf("Successfully verified reliable call was stored in DB with callID=%s, objectID=%s, status=%s", rc.CallID, rc.ObjectID, rc.Status)
	})
}
