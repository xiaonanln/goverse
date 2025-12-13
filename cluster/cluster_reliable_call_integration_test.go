package cluster

import (
	"context"
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

	// Clear all data from previous test runs
	_, err = db.Connection().ExecContext(ctx, "TRUNCATE goverse_reliable_calls, goverse_objects CASCADE")
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
			t.Errorf("Expected value 5, got %d", response.Value)
		}

		// Verify the reliable call record in DB shows success with correct result data
		rc, err := provider.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
		}
		if rc.CallID != callID {
			t.Errorf("Expected CallID %q, got %q", callID, rc.CallID)
		}
		if rc.ObjectID != objectID {
			t.Errorf("Expected ObjectID %q, got %q", objectID, rc.ObjectID)
		}
		if rc.ObjectType != objectType {
			t.Errorf("Expected ObjectType %q, got %q", objectType, rc.ObjectType)
		}
		if rc.MethodName != methodName {
			t.Errorf("Expected MethodName %q, got %q", methodName, rc.MethodName)
		}
		if rc.Status != "success" {
			t.Errorf("Expected status 'success', got %q", rc.Status)
		}
		if rc.Error != "" {
			t.Errorf("Expected empty error, got %q", rc.Error)
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
			t.Errorf("Expected stored result value 5, got %d", storedResponse.Value)
		}
	})

	t.Run("Deduplication - pending call", func(t *testing.T) {
		callID := "integration-test-call-2"
		objectType := "TestCounter"
		objectID := "TestCounter-counter2"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 3}

		// Insert first call
		result1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		// Insert duplicate call - should return the same result
		result2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Duplicate call failed: %v", err)
		}

		// Both results should be identical
		response1 := result1.(*counter_pb.CounterResponse)
		response2 := result2.(*counter_pb.CounterResponse)

		if response1.Value != response2.Value {
			t.Errorf("Expected same value, got %d and %d", response1.Value, response2.Value)
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
			t.Errorf("Expected cached value %d, got %d", response1.Value, response2.Value)
		}
	})

	t.Run("Deduplication - failed call", func(t *testing.T) {
		callID := "integration-test-call-4"
		objectType := "TestCounter"
		objectID := "TestCounter-counter4"
		methodName := "Increment"
		request := &counter_pb.IncrementRequest{Amount: 10}

		// Insert first call
		_, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		// Get the reliable call record
		rc, err := provider.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Failed to get reliable call: %v", err)
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
			t.Errorf("Expected error message to contain %q, got %q", errorMessage, err.Error())
		}
	})
}
