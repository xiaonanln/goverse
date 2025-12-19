package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
)

// TestReliableCallObject_RetryWithNodeFailure tests retry behavior when node is temporarily down
func TestReliableCallObject_RetryWithNodeFailure(t *testing.T) {
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

	// Create two nodes for distributed testing
	nodeAddr1 := testutil.GetFreeAddress()
	cluster1 := mustNewClusterWithMinDurations(ctx, t, nodeAddr1, testPrefix)
	defer cluster1.Stop(ctx)

	node1 := cluster1.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node1.RegisterObjectType((*TestCounter)(nil))

	nodeAddr2 := testutil.GetFreeAddress()
	cluster2 := mustNewClusterWithMinDurations(ctx, t, nodeAddr2, testPrefix)

	node2 := cluster2.GetThisNode()
	node2.SetPersistenceProvider(provider)
	node2.RegisterObjectType((*TestCounter)(nil))

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Create an object on node2
	objectType := "TestCounter"
	objectID := testutil.GetObjectIDForShard(10, "TestCounter-retry")
	methodName := "Increment"

	// Verify object will be on node2
	targetNode, err := cluster1.GetCurrentNodeForObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Failed to get target node: %v", err)
	}
	if targetNode != nodeAddr2 {
		t.Fatalf("Expected object on node2 (%s), got %s", nodeAddr2, targetNode)
	}

	// Create object on node2
	_, err = node2.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	t.Run("Retry succeeds after node restart", func(t *testing.T) {
		// Make a call that will trigger retries by stopping and restarting node2
		callID := "retry-test-call-1"
		request := &counter_pb.IncrementRequest{Amount: 5}

		// Start the call in a goroutine
		resultChan := make(chan struct {
			result proto.Message
			status goverse_pb.ReliableCallStatus
			err    error
		}, 1)

		go func() {
			result, status, err := cluster1.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
			resultChan <- struct {
				result proto.Message
				status goverse_pb.ReliableCallStatus
				err    error
			}{result, status, err}
		}()

		// Wait a moment for the first RPC attempt
		time.Sleep(500 * time.Millisecond)

		// Stop node2 to cause RPC failures
		t.Logf("Stopping node2 to simulate failure")
		cluster2.Stop(ctx)

		// Wait for retry delay (at least 1 second)
		time.Sleep(2 * time.Second)

		// Restart node2
		t.Logf("Restarting node2")
		cluster2 = mustNewClusterWithMinDurations(ctx, t, nodeAddr2, testPrefix)
		defer cluster2.Stop(ctx)

		node2 = cluster2.GetThisNode()
		node2.SetPersistenceProvider(provider)
		node2.RegisterObjectType((*TestCounter)(nil))

		// Recreate the object on node2
		_, err = node2.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to recreate object: %v", err)
		}

		// Wait for cluster to be ready
		testutil.WaitForClustersReady(t, cluster1, cluster2)

		// Wait for the call to complete
		select {
		case result := <-resultChan:
			if result.err != nil {
				t.Fatalf("ReliableCallObject failed: %v", result.err)
			}

			if result.status != goverse_pb.ReliableCallStatus_SUCCESS {
				t.Fatalf("Expected SUCCESS status, got %v", result.status)
			}

			// Verify result
			counterResp, ok := result.result.(*counter_pb.CounterResponse)
			if !ok {
				t.Fatalf("Expected CounterResponse, got %T", result.result)
			}

			if counterResp.Value != 5 {
				t.Errorf("Expected counter value 5, got %d", counterResp.Value)
			}

			t.Logf("Retry succeeded - call completed successfully after node restart")
		case <-time.After(30 * time.Second):
			t.Fatal("Test timed out waiting for call to complete")
		}
	})
}

// TestReliableCallObject_NoRetryOnNonUnknownStatus tests that non-UNKNOWN statuses don't trigger retries
func TestReliableCallObject_NoRetryOnNonUnknownStatus(t *testing.T) {
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
	nodeAddr1 := testutil.GetFreeAddress()
	cluster1 := mustNewClusterWithMinDurations(ctx, t, nodeAddr1, testPrefix)
	defer cluster1.Stop(ctx)

	node1 := cluster1.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node1.RegisterObjectType((*TestFailingCounter)(nil))

	nodeAddr2 := testutil.GetFreeAddress()
	cluster2 := mustNewClusterWithMinDurations(ctx, t, nodeAddr2, testPrefix)
	defer cluster2.Stop(ctx)

	node2 := cluster2.GetThisNode()
	node2.SetPersistenceProvider(provider)
	node2.RegisterObjectType((*TestFailingCounter)(nil))

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Create an object on node2
	objectType := "TestFailingCounter"
	objectID := testutil.GetObjectIDForShard(15, "TestFailingCounter-noretry")

	// Verify object will be on node2
	targetNode, err := cluster1.GetCurrentNodeForObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Failed to get target node: %v", err)
	}
	if targetNode != nodeAddr2 {
		t.Fatalf("Expected object on node2 (%s), got %s", nodeAddr2, targetNode)
	}

	// Create object on node2
	_, err = node2.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	t.Run("No retry on FAILED status", func(t *testing.T) {
		// Call a method that fails - should return FAILED status without retry
		callID := "noretry-test-call-1"
		request := &counter_pb.IncrementRequest{Amount: 5}

		startTime := time.Now()
		_, callStatus, err := cluster1.ReliableCallObject(ctx, callID, objectType, objectID, "FailMethod", request)
		elapsed := time.Since(startTime)

		// Should get FAILED status
		if callStatus != goverse_pb.ReliableCallStatus_FAILED {
			t.Errorf("Expected FAILED status, got %v", callStatus)
		}

		// Should have error
		if err == nil {
			t.Error("Expected error for failed call")
		}

		// Should complete quickly (no retries)
		maxDuration := 500 * time.Millisecond
		if elapsed > maxDuration {
			t.Errorf("Call took too long (%v), suggesting retries occurred", elapsed)
		}
	})
}

// TestReliableCallObject_ContextCancellationStopsRetries tests that context cancellation stops retries
func TestReliableCallObject_ContextCancellationStopsRetries(t *testing.T) {
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

	// Create one node
	nodeAddr1 := testutil.GetFreeAddress()
	cluster1 := mustNewClusterWithMinDurations(ctx, t, nodeAddr1, testPrefix)
	defer cluster1.Stop(ctx)

	node1 := cluster1.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node1.RegisterObjectType((*TestCounter)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster1)

	// Create an object on node1
	objectType := "TestCounter"
	objectID := testutil.GetObjectIDForShard(20, "TestCounter-cancel")
	methodName := "Increment"

	// Create object on node1
	_, err = node1.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	t.Run("Context cancellation stops retries", func(t *testing.T) {
		// Create context that will be cancelled after a short delay
		// We'll stop the node to cause failures, then cancel the context
		ctxWithCancel, cancel := context.WithCancel(ctx)

		// Stop the node to cause RPC failures
		cluster1.Stop(ctx)

		// Use an invalid address that will fail to connect
		badNodeAddr := testutil.GetFreeAddress() // This port will have nothing listening

		// Create a new cluster pointing to a non-existent node to cause failures
		cluster2 := mustNewClusterWithMinDurations(ctx, t, badNodeAddr, testPrefix)
		defer cluster2.Stop(ctx)

		node2 := cluster2.GetThisNode()
		node2.SetPersistenceProvider(provider)
		node2.RegisterObjectType((*TestCounter)(nil))

		// Make the call with cancelled context - should fail quickly
		go func() {
			time.Sleep(1500 * time.Millisecond)
			cancel()
		}()

		callID := "cancel-test-call-1"
		request := &counter_pb.IncrementRequest{Amount: 5}

		startTime := time.Now()
		_, callStatus, err := cluster2.ReliableCallObject(ctxWithCancel, callID, objectType, objectID, methodName, request)
		elapsed := time.Since(startTime)

		// Should return UNKNOWN or SKIPPED status and error
		if callStatus != goverse_pb.ReliableCallStatus_UNKNOWN && callStatus != goverse_pb.ReliableCallStatus_SKIPPED {
			t.Errorf("Expected UNKNOWN or SKIPPED status, got %v", callStatus)
		}

		if err == nil {
			t.Error("Expected error when context cancelled or node unavailable")
		}

		// Should complete relatively quickly (within a few seconds)
		maxDuration := 5 * time.Second
		if elapsed > maxDuration {
			t.Errorf("Call took too long (%v), context cancellation may not be working", elapsed)
		}

		t.Logf("Call completed in %v with status %v", elapsed, callStatus)
	})
}

// TestFailingCounter is a test object that has a method that always fails
type TestFailingCounter struct {
	object.BaseObject
}

func (c *TestFailingCounter) OnCreated() {
	c.Logger.Infof("TestFailingCounter %s created", c.Id())
}

func (c *TestFailingCounter) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (c *TestFailingCounter) FromData(data proto.Message) error {
	return nil
}

func (c *TestFailingCounter) FailMethod(ctx context.Context, req *counter_pb.IncrementRequest) (*counter_pb.CounterResponse, error) {
	return nil, fmt.Errorf("method always fails")
}
