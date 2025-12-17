package cluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
)

// TestStatusCounter is a test object for status testing
type TestStatusCounter struct {
	object.BaseObject
	value int32
}

func (c *TestStatusCounter) OnCreated() {
	c.Logger.Infof("TestStatusCounter %s created", c.Id())
	c.value = 0
}

func (c *TestStatusCounter) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (c *TestStatusCounter) FromData(data proto.Message) error {
	return nil
}

func (c *TestStatusCounter) Increment(ctx context.Context, req *counter_pb.IncrementRequest) (*counter_pb.CounterResponse, error) {
	c.value += req.Amount
	return &counter_pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

func (c *TestStatusCounter) FailMethod(ctx context.Context, req *counter_pb.IncrementRequest) (*counter_pb.CounterResponse, error) {
	// This method always fails to test FAILED status
	return nil, fmt.Errorf("intentional method failure")
}

// TestReliableCallObject_StatusPropagation tests that status values are correctly propagated
func TestReliableCallObject_StatusPropagation(t *testing.T) {
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

	// Create cluster with etcd using mustNewCluster helper
	nodeAddr := testutil.GetFreeAddress()
	cluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer cluster.Stop(ctx)

	// Set persistence provider on the node
	node := cluster.GetThisNode()
	node.SetPersistenceProvider(provider)

	// Register the TestStatusCounter object type
	node.RegisterObjectType((*TestStatusCounter)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster)

	t.Run("SUCCESS status", func(t *testing.T) {
		callID := "status-test-success"
		objectType := "TestStatusCounter"
		objectID := "TestStatusCounter-success"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 5}

		result, status, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if status != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Fatalf("Expected status SUCCESS, got %v", status)
		}

		// Verify result
		response, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Expected *counter_pb.CounterResponse, got %T", result)
		}
		if response.Value != 5 {
			t.Fatalf("Expected value 5, got %d", response.Value)
		}

		// Verify database record shows success
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc.Status != "success" {
			t.Fatalf("Expected DB status 'success', got %q", rc.Status)
		}
	})

	t.Run("FAILED status - method execution error", func(t *testing.T) {
		callID := "status-test-failed"
		objectType := "TestStatusCounter"
		objectID := "TestStatusCounter-failed"
		methodName := "FailMethod"
		request := &counter_pb.IncrementRequest{Amount: 10}

		result, status, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err == nil {
			t.Fatal("Expected error for failed method, got nil")
		}
		if status != goverse_pb.ReliableCallStatus_FAILED {
			t.Fatalf("Expected status FAILED, got %v", status)
		}
		if result != nil {
			t.Fatalf("Expected nil result for failed call, got %v", result)
		}

		// Verify error message contains expected text
		expectedErr := "intentional method failure"
		if err.Error() != fmt.Sprintf("failed to process reliable call locally: reliable call failed: %s", expectedErr) {
			t.Fatalf("Expected error containing %q, got %q", expectedErr, err.Error())
		}

		// Verify database record shows failed
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc.Status != "failed" {
			t.Fatalf("Expected DB status 'failed', got %q", rc.Status)
		}
		if rc.Error == "" {
			t.Fatal("Expected error message in DB, got empty string")
		}
	})

	t.Run("SKIPPED status - deserialization error", func(t *testing.T) {
		callID := "status-test-skipped"
		objectType := "TestStatusCounter"
		objectID := "TestStatusCounter-skipped"
		methodName := "Increment"

		// Create invalid request data that will fail deserialization
		invalidData := []byte("invalid protobuf data")

		// Insert the call directly with invalid data
		rc, err := provider.InsertOrGetReliableCall(ctx, callID, objectID, objectType, methodName, invalidData)
		if err != nil {
			t.Fatalf("Failed to insert call with invalid data: %v", err)
		}
		if rc.Status != "pending" {
			t.Fatalf("Expected initial status 'pending', got %q", rc.Status)
		}

		// Now trigger processing - this should mark it as skipped
		request := &counter_pb.IncrementRequest{Amount: 5}
		result, status, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err == nil {
			t.Fatal("Expected error for skipped call, got nil")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Fatalf("Expected status SKIPPED, got %v", status)
		}
		if result != nil {
			t.Fatalf("Expected nil result for skipped call, got %v", result)
		}

		// Verify database record shows skipped
		rc2, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc2.Status != "skipped" {
			t.Fatalf("Expected DB status 'skipped', got %q", rc2.Status)
		}
		if rc2.Error == "" {
			t.Fatal("Expected error message in DB for skipped call, got empty string")
		}
	})

	t.Run("Cached status values returned on retry", func(t *testing.T) {
		// Test that retrying a call returns the cached status

		// SUCCESS retry
		callID := "status-test-success"
		request := &counter_pb.IncrementRequest{Amount: 5}
		result, status, err := cluster.ReliableCallObject(ctx, callID, "TestStatusCounter", "TestStatusCounter-success", "Increment", request)
		if err != nil {
			t.Fatalf("Retry of success call failed: %v", err)
		}
		if status != goverse_pb.ReliableCallStatus_SUCCESS {
			t.Fatalf("Expected cached status SUCCESS, got %v", status)
		}
		response := result.(*counter_pb.CounterResponse)
		if response.Value != 5 {
			t.Fatalf("Expected cached value 5, got %d", response.Value)
		}

		// FAILED retry
		callID = "status-test-failed"
		request = &counter_pb.IncrementRequest{Amount: 10}
		_, status, err = cluster.ReliableCallObject(ctx, callID, "TestStatusCounter", "TestStatusCounter-failed", "FailMethod", request)
		if err == nil {
			t.Fatal("Expected error for cached failed call, got nil")
		}
		if status != goverse_pb.ReliableCallStatus_FAILED {
			t.Fatalf("Expected cached status FAILED, got %v", status)
		}

		// SKIPPED retry
		callID = "status-test-skipped"
		request = &counter_pb.IncrementRequest{Amount: 5}
		_, status, err = cluster.ReliableCallObject(ctx, callID, "TestStatusCounter", "TestStatusCounter-skipped", "Increment", request)
		if err == nil {
			t.Fatal("Expected error for cached skipped call, got nil")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Fatalf("Expected cached status SKIPPED, got %v", status)
		}
	})
}

// TestReliableCallObject_SkippedVsFailedDistinction tests that we can distinguish
// between pre-execution errors (skipped) and execution errors (failed)
func TestReliableCallObject_SkippedVsFailedDistinction(t *testing.T) {
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

	// Clear data
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create cluster
	nodeAddr := testutil.GetFreeAddress()
	cluster := mustNewClusterWithMinDurations(ctx, t, nodeAddr, testPrefix)
	defer cluster.Stop(ctx)

	node := cluster.GetThisNode()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestStatusCounter)(nil))

	testutil.WaitForClustersReady(t, cluster)

	t.Run("Skipped calls are safe to retry", func(t *testing.T) {
		// A skipped call never executed, so it's safe to retry with corrected data
		callID := "distinction-test-skipped"
		objectType := "TestStatusCounter"
		objectID := "TestStatusCounter-distinction-skipped"
		methodName := "Increment"

		// Insert call with invalid data
		invalidData := []byte("invalid")
		_, err := provider.InsertOrGetReliableCall(ctx, callID, objectID, objectType, methodName, invalidData)
		if err != nil {
			t.Fatalf("Failed to insert: %v", err)
		}

		// Trigger processing
		request := &counter_pb.IncrementRequest{Amount: 1}
		_, status, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err == nil {
			t.Fatal("Expected error")
		}
		if status != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Fatalf("Expected SKIPPED, got %v", status)
		}

		// Verify the call was skipped in DB
		rc2, _ := db.GetReliableCall(ctx, callID)
		if rc2.Status != "skipped" {
			t.Fatalf("Expected DB status 'skipped', got %q", rc2.Status)
		}

		// Key insight: The object state has not changed because the method was never invoked
		// This means it would be safe to retry with a new call ID and corrected data
		t.Logf("Skipped call never executed - safe to retry with corrected data")
	})

	t.Run("Failed calls may have side effects", func(t *testing.T) {
		// A failed call was executed, so it may have side effects
		callID := "distinction-test-failed"
		objectType := "TestStatusCounter"
		objectID := "TestStatusCounter-distinction-failed"
		methodName := "FailMethod"

		request := &counter_pb.IncrementRequest{Amount: 10}
		_, status, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err == nil {
			t.Fatal("Expected error")
		}
		if status != goverse_pb.ReliableCallStatus_FAILED {
			t.Fatalf("Expected FAILED, got %v", status)
		}

		// Verify the call failed in DB
		rc, _ := db.GetReliableCall(ctx, callID)
		if rc.Status != "failed" {
			t.Fatalf("Expected DB status 'failed', got %q", rc.Status)
		}

		// Key insight: The method was invoked even though it returned an error
		// This means it may have had side effects before failing
		// Retry requires caution - use a different approach or compensating transaction
		t.Logf("Failed call was executed - may have side effects, retry with caution")
	})
}
