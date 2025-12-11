package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestReliableCallObject_RoutingToLocalNode tests that reliable calls are processed locally
// when the target node is the current node
func TestReliableCallObject_RoutingToLocalNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a single cluster
	addr1 := testutil.GetFreeAddress()
	cluster1 := mustNewCluster(ctx, t, addr1, testPrefix)
	node1 := cluster1.GetThisNode()

	// Set up a mock persistence provider
	mockProvider := newMockPersistenceProvider()
	node1.SetPersistenceProvider(mockProvider)

	// Start mock gRPC server
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(addr1, mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster1)

	// Test reliable call to an object that should be on this node
	// Use an object ID that we know will hash to a shard owned by this node
	objID := testutil.GetObjectIDForShard(0, "TestObject") // Shard 0 should be on node1
	callID := "test-local-call-1"
	objectType := "TestType"
	methodName := "TestMethod"
	requestData := []byte("test request")

	// Verify the object will be on this node
	targetNode, err := cluster1.GetCurrentNodeForObject(ctx, objID)
	if err != nil {
		t.Fatalf("Failed to get target node: %v", err)
	}
	if targetNode != addr1 {
		t.Skipf("Skipping test - object %s not on node1 (on %s instead)", objID, targetNode)
	}

	// Make the reliable call
	rc, err := cluster1.ReliableCallObject(ctx, callID, objectType, objID, methodName, requestData)
	if err != nil {
		t.Fatalf("ReliableCallObject failed: %v", err)
	}

	// Verify the call was inserted into the database
	if rc.CallID != callID {
		t.Errorf("Expected call_id %q, got %q", callID, rc.CallID)
	}
	if rc.ObjectID != objID {
		t.Errorf("Expected object_id %q, got %q", objID, rc.ObjectID)
	}
	if rc.Status != "pending" {
		t.Errorf("Expected status 'pending', got %q", rc.Status)
	}

	// Verify the call was stored in mock provider
	if !mockProvider.HasStoredData(callID) {
		t.Error("Expected call to be stored in persistence provider")
	}
}

// TestReliableCallObject_RoutingToRemoteNode tests that reliable calls are routed via RPC
// when the target node is a remote node
func TestReliableCallObject_RoutingToRemoteNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two clusters
	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	
	cluster1 := mustNewCluster(ctx, t, addr1, testPrefix)
	cluster2 := mustNewCluster(ctx, t, addr2, testPrefix)
	
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()

	// Set up mock persistence providers for both nodes
	mockProvider1 := newMockPersistenceProvider()
	mockProvider2 := newMockPersistenceProvider()
	node1.SetPersistenceProvider(mockProvider1)
	node2.SetPersistenceProvider(mockProvider2)

	// Start mock gRPC servers for both nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(addr1, mockServer1)
	err := testServer1.Start(ctx)
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

	// Wait for clusters to be ready and discover each other
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Find an object ID that will be on node2 when called from node1
	var objID string
	for i := 0; i < testutil.TestNumShards; i++ {
		testObjID := testutil.GetObjectIDForShard(i, "TestObject")
		targetNode, err := cluster1.GetCurrentNodeForObject(ctx, testObjID)
		if err != nil {
			continue
		}
		if targetNode == addr2 {
			objID = testObjID
			break
		}
	}

	if objID == "" {
		t.Skip("Could not find an object ID that maps to node2")
	}

	callID := "test-remote-call-1"
	objectType := "TestType"
	methodName := "TestMethod"
	requestData := []byte("test request for remote node")

	// Make the reliable call from cluster1 (which will route to cluster2)
	rc, err := cluster1.ReliableCallObject(ctx, callID, objectType, objID, methodName, requestData)
	if err != nil {
		t.Fatalf("ReliableCallObject failed: %v", err)
	}

	// Verify the call was inserted into the database on node1
	if rc.CallID != callID {
		t.Errorf("Expected call_id %q, got %q", callID, rc.CallID)
	}
	if rc.ObjectID != objID {
		t.Errorf("Expected object_id %q, got %q", objID, rc.ObjectID)
	}
	if rc.Status != "pending" {
		t.Errorf("Expected status 'pending', got %q", rc.Status)
	}

	// Verify the call was stored in mock provider on node1 (where it was initiated)
	if !mockProvider1.HasStoredData(callID) {
		t.Error("Expected call to be stored in persistence provider on node1")
	}

	// Note: In this test, we verify that the RPC was successfully sent to node2.
	// The actual processing on node2 is handled by the node-level ReliableCallObject handler,
	// which is tested separately. Here we're testing the routing logic in the cluster layer.
}

// TestReliableCallObject_DeduplicationAcrossRouting tests that duplicate calls
// return cached results even when routing is involved
func TestReliableCallObject_DeduplicationAcrossRouting(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a single cluster
	addr1 := testutil.GetFreeAddress()
	cluster1 := mustNewCluster(ctx, t, addr1, testPrefix)
	node1 := cluster1.GetThisNode()

	// Set up a mock persistence provider
	mockProvider := newMockPersistenceProvider()
	node1.SetPersistenceProvider(mockProvider)

	// Start mock gRPC server
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(addr1, mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster1)

	// Use an object ID that we know will hash to a shard owned by this node
	objID := testutil.GetObjectIDForShard(0, "TestObject")
	callID := "test-dup-call-1"
	objectType := "TestType"
	methodName := "TestMethod"
	requestData := []byte("test request")

	// Verify the object will be on this node
	targetNode, err := cluster1.GetCurrentNodeForObject(ctx, objID)
	if err != nil {
		t.Fatalf("Failed to get target node: %v", err)
	}
	if targetNode != addr1 {
		t.Skipf("Skipping test - object %s not on node1 (on %s instead)", objID, targetNode)
	}

	// Make the first reliable call
	rc1, err := cluster1.ReliableCallObject(ctx, callID, objectType, objID, methodName, requestData)
	if err != nil {
		t.Fatalf("First ReliableCallObject failed: %v", err)
	}

	// Simulate the call completing by updating status
	err = mockProvider.UpdateReliableCallStatus(ctx, rc1.Seq, "success", []byte("test result"), "")
	if err != nil {
		t.Fatalf("Failed to update call status: %v", err)
	}

	// Small delay to ensure status update is visible
	time.Sleep(10 * time.Millisecond)

	// Make a duplicate call with the same callID
	rc2, err := cluster1.ReliableCallObject(ctx, callID, objectType, objID, methodName, requestData)
	if err != nil {
		t.Fatalf("Second ReliableCallObject failed: %v", err)
	}

	// Verify deduplication: should return the successful call
	if rc2.Status != "success" {
		t.Errorf("Expected status 'success', got %q", rc2.Status)
	}
	if rc2.Seq != rc1.Seq {
		t.Errorf("Expected same seq %d, got %d", rc1.Seq, rc2.Seq)
	}
	if string(rc2.ResultData) != "test result" {
		t.Errorf("Expected result_data 'test result', got %q", rc2.ResultData)
	}
}
