package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestReliableCallObjectAnyRequest tests the optimized ReliableCallObjectAnyRequest method
func TestReliableCallObjectAnyRequest(t *testing.T) {
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
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		NumShards:                     testutil.TestNumShards,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
	}, gw)
	if err != nil {
		t.Fatalf("Failed to create gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)
	t.Cleanup(func() { gw.Stop() })

	// Start the gate cluster
	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Wait for both clusters to be fully ready
	testutil.WaitForClustersReadyWithoutGateConnections(t, nodeCluster, gateCluster)

	t.Run("ReliableCallObjectAnyRequest works with Any requests", func(t *testing.T) {
		callID := "any-test-call-1"
		objectType := "TestCounter"
		objectID := "TestCounter-any1"
		methodName := "Increment"

		// Create the object on the node first
		_, err := nodeCluster.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Create request and convert to Any
		request := &counter_pb.IncrementRequest{
			Amount: 7,
		}
		requestAny, err := protohelper.MsgToAny(request)
		if err != nil {
			t.Fatalf("Failed to convert request to Any: %v", err)
		}

		// Call ReliableCallObjectAnyRequest from gate cluster
		resultAny, status, err := gateCluster.ReliableCallObjectAnyRequest(ctx, callID, objectType, objectID, methodName, requestAny)
		if err != nil {
			t.Fatalf("ReliableCallObjectAnyRequest from gate failed: %v", err)
		}

		// Verify status
		if status != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Errorf("Expected status SUCCESS, got %v", status)
		}

		// Convert result from Any
		result, err := protohelper.AnyToMsg(resultAny)
		if err != nil {
			t.Fatalf("Failed to convert result from Any: %v", err)
		}

		// Verify result
		counterResp, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected CounterResponse, got %T", result)
		}

		if counterResp.Value != 7 {
			t.Errorf("Expected value 7, got %d", counterResp.Value)
		}
	})

	t.Run("ReliableCallObjectAnyRequest validates parameters", func(t *testing.T) {
		objectType := "TestCounter"
		objectID := "TestCounter-validation"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 1}
		requestAny, _ := protohelper.MsgToAny(request)

		// Test empty callID
		_, status, err := gateCluster.ReliableCallObjectAnyRequest(ctx, "", objectType, objectID, methodName, requestAny)
		if err == nil {
			t.Error("Expected error for empty callID")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty callID, got %v", status)
		}

		// Test empty objectType
		_, status, err = gateCluster.ReliableCallObjectAnyRequest(ctx, "call-1", "", objectID, methodName, requestAny)
		if err == nil {
			t.Error("Expected error for empty objectType")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty objectType, got %v", status)
		}

		// Test empty objectID
		_, status, err = gateCluster.ReliableCallObjectAnyRequest(ctx, "call-2", objectType, "", methodName, requestAny)
		if err == nil {
			t.Error("Expected error for empty objectID")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty objectID, got %v", status)
		}

		// Test empty methodName
		_, status, err = gateCluster.ReliableCallObjectAnyRequest(ctx, "call-3", objectType, objectID, "", requestAny)
		if err == nil {
			t.Error("Expected error for empty methodName")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected SKIPPED status for empty methodName, got %v", status)
		}
	})

	t.Run("ReliableCallObject uses ReliableCallObjectAnyRequest internally", func(t *testing.T) {
		callID := "wrapper-test-call"
		objectType := "TestCounter"
		objectID := "TestCounter-wrapper"
		methodName := "Increment"

		// Create the object on the node first
		_, err := nodeCluster.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Create request (proto.Message)
		request := &counter_pb.IncrementRequest{
			Amount: 10,
		}

		// Call ReliableCallObject from gate cluster (should use ReliableCallObjectAnyRequest internally)
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

		if counterResp.Value != 10 {
			t.Errorf("Expected value 10, got %d", counterResp.Value)
		}
	})
}
