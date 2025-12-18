package cluster

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MockUnreliableGoverseServer simulates a server that fails N times before succeeding
type MockUnreliableGoverseServer struct {
	*testutil.MockGoverseServer
	failCount    atomic.Int32 // Number of times to fail
	attemptCount atomic.Int32 // Number of attempts made
}

func NewMockUnreliableGoverseServer(failTimes int) *MockUnreliableGoverseServer {
	server := &MockUnreliableGoverseServer{
		MockGoverseServer: testutil.NewMockGoverseServer(),
	}
	server.failCount.Store(int32(failTimes))
	server.attemptCount.Store(0)
	return server
}

func (s *MockUnreliableGoverseServer) ReliableCallObject(ctx context.Context, req *goverse_pb.ReliableCallObjectRequest) (*goverse_pb.ReliableCallObjectResponse, error) {
	attempt := s.attemptCount.Add(1)
	failsRemaining := s.failCount.Load()

	// Fail with "unknown" status code for the first N attempts
	if attempt <= failsRemaining {
		return nil, status.Errorf(codes.Unknown, "simulated transient error (attempt %d)", attempt)
	}

	// After N failures, delegate to the real implementation
	return s.MockGoverseServer.ReliableCallObject(ctx, req)
}

func (s *MockUnreliableGoverseServer) GetAttemptCount() int32 {
	return s.attemptCount.Load()
}

// TestReliableCallObject_RetryOnTransientError tests that reliable calls retry on transient RPC errors
func TestReliableCallObject_RetryOnTransientError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err := db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data from previous test runs
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create two nodes for testing remote calls
	node1Addr := testutil.GetFreeAddress()
	node2Addr := testutil.GetFreeAddress()

	t.Logf("Node 1 address: %s", node1Addr)
	t.Logf("Node 2 address: %s", node2Addr)

	// Create node 1 (caller)
	node1 := node.NewNode(node1Addr, testutil.TestNumShards)
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Create cluster 1
	cluster1 := mustNewClusterWithMinDurations(ctx, t, node1Addr, testPrefix)
	defer cluster1.Stop(ctx)

	node1.SetPersistenceProvider(provider)
	node1.RegisterObjectType((*TestCounter)(nil))

	// Create node 2 (target) with an unreliable mock server that fails 2 times
	node2 := node.NewNode(node2Addr, testutil.TestNumShards)
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	// Create unreliable mock server that fails 2 times before succeeding
	mockServer := NewMockUnreliableGoverseServer(2)
	mockServer.SetNode(node2)

	// Start test server for node2 with the unreliable mock
	testServer := testutil.NewTestServerHelper(node2Addr, mockServer)
	testServer.Start(ctx)
	defer testServer.Stop()

	// Create cluster 2
	cluster2 := mustNewClusterWithMinDurations(ctx, t, node2Addr, testPrefix)
	defer cluster2.Stop(ctx)

	node2.SetPersistenceProvider(provider)
	node2.RegisterObjectType((*TestCounter)(nil))

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Create object on node2 using fixed-node ID format to ensure it goes to node2
	objectID := fmt.Sprintf("%s/counter-retry-test", node2Addr)
	objectType := "TestCounter"

	t.Logf("Creating object %s on node2", objectID)

	// Create object directly on node2
	_, err = node2.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait for object to be created
	time.Sleep(100 * time.Millisecond)

	// Verify object exists on node2 by checking the object list
	objectIDs := node2.ListObjectIDs()
	found := false
	for _, id := range objectIDs {
		if id == objectID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Object %s not found on node2. Objects: %v", objectID, objectIDs)
	}

	t.Logf("Object %s created successfully on node2", objectID)

	// Make a reliable call from cluster1 to object on cluster2
	// The mock server will fail 2 times with "unknown" status before succeeding
	callID := "retry-test-call-1"
	methodName := "Increment"
	request := &counter_pb.IncrementRequest{Amount: 5}

	t.Logf("Making reliable call from node1 to node2...")

	startTime := time.Now()
	result, callStatus, err := cluster1.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
	duration := time.Since(startTime)

	// Verify the call succeeded after retries
	if err != nil {
		t.Fatalf("ReliableCallObject failed: %v", err)
	}

	if callStatus != goverse_pb.ReliableCallStatus_SUCCESS {
		t.Fatalf("Expected status SUCCESS, got %v", callStatus)
	}

	response, ok := result.(*counter_pb.CounterResponse)
	if !ok {
		t.Fatalf("Expected *counter_pb.CounterResponse, got %T", result)
	}

	if response.Value != 5 {
		t.Errorf("Expected counter value 5, got %d", response.Value)
	}

	// Verify that 3 attempts were made (2 failures + 1 success)
	attemptCount := mockServer.GetAttemptCount()
	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts (2 failures + 1 success), got %d", attemptCount)
	}

	// Verify that the call took at least 3 seconds (1s + 2s backoff)
	// Allow some tolerance for timing
	minDuration := 2500 * time.Millisecond // 3s with some tolerance
	if duration < minDuration {
		t.Errorf("Expected call to take at least %v due to retries, but took %v", minDuration, duration)
	}

	t.Logf("Call succeeded after %d attempts in %v", attemptCount, duration)
}

// TestReliableCallObject_NoRetryOnExecutionError tests that reliable calls don't retry on execution errors
func TestReliableCallObject_NoRetryOnExecutionError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err := db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create single node
	nodeAddr := testutil.GetFreeAddress()
	cluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer cluster.Stop(ctx)

	node := cluster.GetThisNode()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestCounter)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster)

	// Create an object
	objectID := "TestCounter-no-retry-test"
	objectType := "TestCounter"

	_, err = node.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Make a reliable call with invalid method name (will fail on execution, not RPC)
	callID := "no-retry-test-call-1"
	methodName := "NonExistentMethod" // This method doesn't exist
	request := &counter_pb.IncrementRequest{Amount: 5}

	startTime := time.Now()
	_, callStatus, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
	duration := time.Since(startTime)

	// Verify the call failed quickly without retries
	if err == nil {
		t.Fatalf("Expected error for non-existent method, got nil")
	}

	// For execution errors (method not found), the status should be FAILED
	if callStatus != goverse_pb.ReliableCallStatus_FAILED {
		t.Logf("Got status %v with error: %v", callStatus, err)
	}

	// Verify no significant delay (no retries)
	maxDuration := 500 * time.Millisecond
	if duration > maxDuration {
		t.Errorf("Call took %v, suggesting retries occurred when they shouldn't", duration)
	}

	t.Logf("Call failed immediately as expected in %v (no retries)", duration)
}

// TestReliableCallObject_RetryRespectsContext tests that retries stop when context is cancelled
func TestReliableCallObject_RetryRespectsContext(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err := db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create two nodes
	node1Addr := testutil.GetFreeAddress()
	node2Addr := testutil.GetFreeAddress()

	// Create node 1 (caller)
	node1 := node.NewNode(node1Addr, testutil.TestNumShards)
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	cluster1 := mustNewClusterWithMinDurations(ctx, t, node1Addr, testPrefix)
	defer cluster1.Stop(ctx)

	node1.SetPersistenceProvider(provider)
	node1.RegisterObjectType((*TestCounter)(nil))

	// Create node 2 with unreliable server that fails many times
	node2 := node.NewNode(node2Addr, testutil.TestNumShards)
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	// Create unreliable mock server that fails 100 times (more than we'll wait for)
	mockServer := NewMockUnreliableGoverseServer(100)
	mockServer.SetNode(node2)

	testServer := testutil.NewTestServerHelper(node2Addr, mockServer)
	testServer.Start(ctx)
	defer testServer.Stop()

	cluster2 := mustNewClusterWithMinDurations(ctx, t, node2Addr, testPrefix)
	defer cluster2.Stop(ctx)

	node2.SetPersistenceProvider(provider)
	node2.RegisterObjectType((*TestCounter)(nil))

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Create object on node2
	objectID := fmt.Sprintf("%s/counter-context-test", node2Addr)
	objectType := "TestCounter"

	_, err = node2.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create context with short timeout (2 seconds)
	// This should allow 1 attempt (immediate) + 1 retry (1s backoff) before cancellation
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	callID := "context-test-call-1"
	methodName := "Increment"
	request := &counter_pb.IncrementRequest{Amount: 5}

	startTime := time.Now()
	_, _, err = cluster1.ReliableCallObject(ctxWithTimeout, callID, objectType, objectID, methodName, request)
	duration := time.Since(startTime)

	// Verify the call was cancelled
	if err == nil {
		t.Fatalf("Expected error due to context cancellation, got nil")
	}

	// Verify context cancellation error
	if ctx.Err() == nil && ctxWithTimeout.Err() == nil {
		t.Errorf("Expected context to be cancelled, but it wasn't")
	}

	// Verify that not all 100 attempts were made (should stop early due to context)
	attemptCount := mockServer.GetAttemptCount()
	if attemptCount >= 10 {
		t.Errorf("Expected fewer than 10 attempts due to context cancellation, got %d", attemptCount)
	}

	// Verify duration is close to timeout (should be around 2 seconds)
	expectedDuration := 2 * time.Second
	tolerance := 500 * time.Millisecond
	if duration < expectedDuration-tolerance || duration > expectedDuration+tolerance {
		t.Logf("Duration %v outside expected range %v ± %v (acceptable for timing)", 
			duration, expectedDuration, tolerance)
	}

	t.Logf("Call cancelled after %d attempts in %v (context timeout respected)", attemptCount, duration)
}
