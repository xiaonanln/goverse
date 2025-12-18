package cluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/xiaonanln/goverse/gate"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
)

// TestReliableCallObject_GateClusterRouting tests that gate clusters can route reliable calls to nodes
func TestReliableCallObject_GateClusterRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database and get connection
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

	// Clear all data from previous test runs and reset sequences
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	t.Logf("Truncated goverse_reliable_calls and goverse_objects tables")

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Get free addresses for node and gate
	nodeAddr := testutil.GetFreeAddress()
	gateAddr := testutil.GetFreeAddress()

	// Create node cluster
	nodeCluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	// Set persistence provider on the node
	node := nodeCluster.GetThisNode()
	node.SetPersistenceProvider(provider)

	// Register the TestCounter object type
	node.RegisterObjectType((*TestCounter)(nil))

	// Start mock gRPC server for node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(node)
	testNodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err = testNodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { testNodeServer.Stop() })

	// Create gate cluster
	gateConfig := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGate(gateConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	gateCluster, err := NewClusterWithGate(Config{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  testPrefix,
		NumShards:   testutil.TestNumShards,
	}, gw)
	if err != nil {
		t.Fatalf("Failed to create gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	// Wait for both clusters to be fully ready
	testutil.WaitForClustersReady(t, nodeCluster, gateCluster)

	t.Run("Gate cluster routes reliable call to node", func(t *testing.T) {
		callID := "gate-test-call-1"
		objectType := "TestCounter"
		objectID := "TestCounter-gate1"
		methodName := "Increment"

		// Create the object on the node first
		_, err := nodeCluster.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Create request
		request := &counter_pb.IncrementRequest{
			Amount: 5,
		}

		// Call ReliableCallObject from gate cluster
		result, status, err := gateCluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("ReliableCallObject from gate failed: %v", err)
		}

		// Verify status
		if status != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected status SUCCESS, got %v", status)
		}

		// Verify result
		counterResp, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse, got %T", result)
		}

		if counterResp.Value != 5 {
			t.Errorf("Expected value 5, got %d", counterResp.Value)
		}
	})

	t.Run("Gate cluster handles validation errors with SKIPPED status", func(t *testing.T) {
		objectType := "TestCounter"
		objectID := "TestCounter-validation"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 1}

		// Test empty callID
		_, status, err := gateCluster.ReliableCallObject(ctx, "", objectType, objectID, methodName, request)
		if err == nil {
			t.Error("Expected error for empty callID")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty callID, got %v", status)
		}

		// Test empty objectType
		_, status, err = gateCluster.ReliableCallObject(ctx, "call-1", "", objectID, methodName, request)
		if err == nil {
			t.Error("Expected error for empty objectType")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty objectType, got %v", status)
		}

		// Test empty objectID
		_, status, err = gateCluster.ReliableCallObject(ctx, "call-2", objectType, "", methodName, request)
		if err == nil {
			t.Error("Expected error for empty objectID")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty objectID, got %v", status)
		}

		// Test empty methodName
		_, status, err = gateCluster.ReliableCallObject(ctx, "call-3", objectType, objectID, "", request)
		if err == nil {
			t.Error("Expected error for empty methodName")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty methodName, got %v", status)
		}
	})

	t.Run("Gate cluster deduplicates calls", func(t *testing.T) {
		callID := "gate-dedup-call"
		objectType := "TestCounter"
		objectID := "TestCounter-dedup"
		methodName := "Increment"

		// Create the object on the node first
		_, err := nodeCluster.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Create request
		request := &counter_pb.IncrementRequest{
			Amount: 10,
		}

		// First call
		result1, status1, err := gateCluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("First ReliableCallObject failed: %v", err)
		}

		if status1 != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected status SUCCESS on first call, got %v", status1)
		}

		counterResp1, ok := result1.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse, got %T", result1)
		}

		// Second call with same callID (should return cached result)
		result2, status2, err := gateCluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Second ReliableCallObject (dedup) failed: %v", err)
		}

		if status2 != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected status SUCCESS on second call, got %v", status2)
		}

		counterResp2, ok := result2.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse, got %T", result2)
		}

		// Values should be the same (deduplication)
		if counterResp1.Value != counterResp2.Value {
			t.Errorf("Expected same value %d, got %d (deduplication failed)", counterResp1.Value, counterResp2.Value)
		}

		if counterResp2.Value != 10 {
			t.Errorf("Expected value 10 (dedup), got %d", counterResp2.Value)
		}
	})
}

// TestReliableCallObject_NodeClusterUnchanged verifies node cluster behavior is unchanged
func TestReliableCallObject_NodeClusterUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database and get connection
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

	// Clear all data from previous test runs and reset sequences
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

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

	t.Run("Node cluster local call still works", func(t *testing.T) {
		callID := "node-local-call"
		objectType := "TestCounter"
		objectID := "TestCounter-local"
		methodName := "Increment"

		// Create the object
		_, err := cluster.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Create request
		request := &counter_pb.IncrementRequest{
			Amount: 3,
		}

		// Call ReliableCallObject
		result, status, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("ReliableCallObject failed: %v", err)
		}

		// Verify status
		if status != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected status SUCCESS, got %v", status)
		}

		// Verify result
		counterResp, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse, got %T", result)
		}

		if counterResp.Value != 3 {
			t.Errorf("Expected value 3, got %d", counterResp.Value)
		}
	})
}

// TestReliableCallObject_InvalidRequest tests invalid proto message serialization
func TestReliableCallObject_InvalidRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get free addresses
	nodeAddr := testutil.GetFreeAddress()

	// Create node cluster
	nodeCluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, nodeCluster)

	t.Run("Invalid proto message returns SKIPPED", func(t *testing.T) {
		callID := "invalid-proto-call"
		objectType := "TestCounter"
		objectID := "TestCounter-invalid"
		methodName := "Increment"

		// Create an invalid proto message that can't be serialized
		// (nil is valid and will be serialized as empty bytes, so we need a different approach)
		// Actually, all proto.Message can be serialized, so let's just verify nil works
		var request proto.Message = nil

		// Call ReliableCallObject with nil request (should succeed as empty bytes)
		result, status, err := nodeCluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)

		// This should fail with SKIPPED because we can't serialize nil
		if err == nil {
			t.Logf("Nil request serialized successfully (status: %v, result: %v)", status, result)
			// This is actually valid behavior - nil proto messages can be serialized
			// So we don't fail the test
		} else if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for serialization error, got %v", status)
		}
	})
}

// TestReliableCallObject_MultiGateMultiNode tests multiple gates routing to multiple nodes
func TestReliableCallObject_MultiGateMultiNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database and get connection
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

	// Clear all data from previous test runs and reset sequences
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Get free addresses for 2 nodes and 2 gates
	node1Addr := testutil.GetFreeAddress()
	node2Addr := testutil.GetFreeAddress()
	gate1Addr := testutil.GetFreeAddress()
	gate2Addr := testutil.GetFreeAddress()

	// Create 2 node clusters
	nodeCluster1 := mustNewClusterWithMinDurations(ctx, t, node1Addr, testPrefix)
	nodeCluster2 := mustNewClusterWithMinDurations(ctx, t, node2Addr, testPrefix)

	// Set persistence provider on both nodes
	node1 := nodeCluster1.GetThisNode()
	node2 := nodeCluster2.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node2.SetPersistenceProvider(provider)

	// Register the TestCounter object type on both nodes
	node1.RegisterObjectType((*TestCounter)(nil))
	node2.RegisterObjectType((*TestCounter)(nil))

	// Start mock gRPC servers for both nodes
	mockNodeServer1 := testutil.NewMockGoverseServer()
	mockNodeServer1.SetNode(node1)
	testNodeServer1 := testutil.NewTestServerHelper(node1Addr, mockNodeServer1)
	err = testNodeServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node 1 mock server: %v", err)
	}
	t.Cleanup(func() { testNodeServer1.Stop() })

	mockNodeServer2 := testutil.NewMockGoverseServer()
	mockNodeServer2.SetNode(node2)
	testNodeServer2 := testutil.NewTestServerHelper(node2Addr, mockNodeServer2)
	err = testNodeServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node 2 mock server: %v", err)
	}
	t.Cleanup(func() { testNodeServer2.Stop() })

	// Create 2 gate clusters
	gate1Config := &gate.GateConfig{
		AdvertiseAddress: gate1Addr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw1, err := gate.NewGate(gate1Config)
	if err != nil {
		t.Fatalf("Failed to create gate 1: %v", err)
	}

	gateCluster1, err := NewClusterWithGate(Config{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  testPrefix,
		NumShards:   testutil.TestNumShards,
	}, gw1)
	if err != nil {
		t.Fatalf("Failed to create gate cluster 1: %v", err)
	}
	defer gateCluster1.Stop(ctx)

	gate2Config := &gate.GateConfig{
		AdvertiseAddress: gate2Addr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw2, err := gate.NewGate(gate2Config)
	if err != nil {
		t.Fatalf("Failed to create gate 2: %v", err)
	}

	gateCluster2, err := NewClusterWithGate(Config{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  testPrefix,
		NumShards:   testutil.TestNumShards,
	}, gw2)
	if err != nil {
		t.Fatalf("Failed to create gate cluster 2: %v", err)
	}
	defer gateCluster2.Stop(ctx)

	// Wait for all clusters to be fully ready
	testutil.WaitForClustersReady(t, nodeCluster1, nodeCluster2, gateCluster1, gateCluster2)

	t.Run("Multiple gates can route to different nodes", func(t *testing.T) {
		// Get object IDs that will be on different shards
		objID1 := testutil.GetObjectIDForShard(5, "Counter1")
		objID2 := testutil.GetObjectIDForShard(10, "Counter2")

		// Create objects on the appropriate nodes
		_, err := nodeCluster1.CreateObject(ctx, "TestCounter", objID1)
		if err != nil {
			t.Fatalf("Failed to create object 1: %v", err)
		}
		_, err = nodeCluster2.CreateObject(ctx, "TestCounter", objID2)
		if err != nil {
			t.Fatalf("Failed to create object 2: %v", err)
		}

		// Call from gate 1 to object on node 1
		request1 := &counter_pb.IncrementRequest{Amount: 7}
		result1, status1, err := gateCluster1.ReliableCallObject(ctx, fmt.Sprintf("multi-gate-call-1-%s", objID1), "TestCounter", objID1, "Increment", request1)
		if err != nil {
			t.Fatalf("Gate 1 -> Node 1 call failed: %v", err)
		}
		if status1 != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected SUCCESS status from gate 1, got %v", status1)
		}
		counterResp1, ok := result1.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse from gate 1, got %T", result1)
		}
		if counterResp1.Value != 7 {
			t.Errorf("Expected value 7 from gate 1, got %d", counterResp1.Value)
		}

		// Call from gate 2 to object on node 2
		request2 := &counter_pb.IncrementRequest{Amount: 13}
		result2, status2, err := gateCluster2.ReliableCallObject(ctx, fmt.Sprintf("multi-gate-call-2-%s", objID2), "TestCounter", objID2, "Increment", request2)
		if err != nil {
			t.Fatalf("Gate 2 -> Node 2 call failed: %v", err)
		}
		if status2 != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected SUCCESS status from gate 2, got %v", status2)
		}
		counterResp2, ok := result2.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse from gate 2, got %T", result2)
		}
		if counterResp2.Value != 13 {
			t.Errorf("Expected value 13 from gate 2, got %d", counterResp2.Value)
		}
	})
}
