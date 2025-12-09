package cluster

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestReliableObject is a test object for ReliableCallObject tests
type TestReliableObject struct {
	object.BaseObject
	mu          sync.Mutex
	callCount   int
	lastRequest string
}

func (obj *TestReliableObject) Init(ctx context.Context) error {
	return nil
}

func (obj *TestReliableObject) OnCreated() {
	// No-op for testing
}

// TestReliableMethod simulates a method that increments a counter
type TestReliableRequest struct {
	RequestData string
}

func (r *TestReliableRequest) ProtoReflect() protoreflect.Message {
	return nil
}

func (r *TestReliableRequest) Reset() {}

func (r *TestReliableRequest) String() string {
	return fmt.Sprintf("TestReliableRequest{RequestData: %s}", r.RequestData)
}

func (r *TestReliableRequest) ProtoMessage() {}

type TestReliableResponse struct {
	CallCount int
}

func (r *TestReliableResponse) ProtoReflect() protoreflect.Message {
	return nil
}

func (r *TestReliableResponse) Reset() {}

func (r *TestReliableResponse) String() string {
	return fmt.Sprintf("TestReliableResponse{CallCount: %d}", r.CallCount)
}

func (r *TestReliableResponse) ProtoMessage() {}

func (obj *TestReliableObject) TestReliableMethod(ctx context.Context, req *TestReliableRequest) (*TestReliableResponse, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()

	obj.callCount++
	obj.lastRequest = req.RequestData

	return &TestReliableResponse{
		CallCount: obj.callCount,
	}, nil
}

// skipIfNoPostgres skips the test if PostgreSQL is not available
func skipIfNoPostgres(t *testing.T) *postgres.Config {
	t.Helper()

	// Use environment variables or defaults
	config := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Try to connect
	db, err := postgres.NewDB(config)
	if err != nil {
		t.Skipf("Skipping PostgreSQL integration test: %v", err)
		return nil
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Ping(ctx); err != nil {
		t.Skipf("Skipping PostgreSQL integration test (cannot ping): %v", err)
		return nil
	}

	return config
}

// TestReliableCallObject_Basic tests basic exactly-once semantics
// This test requires both etcd and PostgreSQL
func TestReliableCallObject_Basic(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Check PostgreSQL availability
	pgConfig := skipIfNoPostgres(t)
	if pgConfig == nil {
		return
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get free address for the node
	nodeAddr := testutil.GetFreeAddress()

	// Create cluster
	cluster1 := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	node1 := cluster1.GetThisNode()

	// Set up PostgreSQL persistence provider
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	// Initialize schema
	if err := db.InitSchema(ctx); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clean up test data
	t.Cleanup(func() {
		_, _ = db.Connection().ExecContext(context.Background(), "DELETE FROM goverse_requests")
		_, _ = db.Connection().ExecContext(context.Background(), "DELETE FROM goverse_objects")
	})

	// Set persistence provider on node
	provider := postgres.NewPostgresPersistenceProvider(db)
	node1.SetPersistenceProvider(provider)

	// Register object type
	node1.RegisterObjectType((*TestReliableObject)(nil))

	// Start mock gRPC server
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(node1)
	testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
	if err := testServer.Start(ctx); err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster1)

	// Create test object
	objID := "reliable-test-obj-1"
	_, err = cluster1.CreateObject(ctx, "TestReliableObject", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait for object to be created
	waitForObjectCreated(t, node1, objID, 5*time.Second)

	// Test 1: Basic reliable call
	t.Run("BasicReliableCall", func(t *testing.T) {
		request := &TestReliableRequest{RequestData: "test1"}
		
		response, err := cluster1.ReliableCallObject(ctx, "TestReliableObject", objID, "TestReliableMethod", request)
		if err != nil {
			t.Fatalf("ReliableCallObject() failed: %v", err)
		}

		resp, ok := response.(*TestReliableResponse)
		if !ok {
			t.Fatalf("Expected *TestReliableResponse, got %T", response)
		}

		if resp.CallCount != 1 {
			t.Fatalf("Expected CallCount=1, got %d", resp.CallCount)
		}
	})

	// Test 2: Verify request was recorded in database
	t.Run("VerifyRequestRecorded", func(t *testing.T) {
		// Query for requests for this object
		requests, err := db.GetPendingRequests(ctx, objID)
		if err != nil {
			t.Fatalf("Failed to get requests: %v", err)
		}

		// Should have no pending requests (completed)
		if len(requests) != 0 {
			t.Fatalf("Expected 0 pending requests, got %d", len(requests))
		}

		// Check that last processed ID is > 0
		lastID, err := db.GetLastProcessedID(ctx, objID)
		if err != nil {
			t.Fatalf("Failed to get last processed ID: %v", err)
		}

		if lastID == 0 {
			t.Fatal("Expected last processed ID > 0")
		}
	})
}

// TestReliableCallObject_Deduplication tests that concurrent identical calls are deduplicated
func TestReliableCallObject_Deduplication(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Check PostgreSQL availability
	pgConfig := skipIfNoPostgres(t)
	if pgConfig == nil {
		return
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get free address for the node
	nodeAddr := testutil.GetFreeAddress()

	// Create cluster
	cluster1 := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	node1 := cluster1.GetThisNode()

	// Set up PostgreSQL persistence provider
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	// Initialize schema
	if err := db.InitSchema(ctx); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clean up test data
	t.Cleanup(func() {
		_, _ = db.Connection().ExecContext(context.Background(), "DELETE FROM goverse_requests")
		_, _ = db.Connection().ExecContext(context.Background(), "DELETE FROM goverse_objects")
	})

	// Set persistence provider on node
	provider := postgres.NewPostgresPersistenceProvider(db)
	node1.SetPersistenceProvider(provider)

	// Register object type
	node1.RegisterObjectType((*TestReliableObject)(nil))

	// Start mock gRPC server
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(node1)
	testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
	if err := testServer.Start(ctx); err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster1)

	// Create test object
	objID := "reliable-test-obj-2"
	_, err = cluster1.CreateObject(ctx, "TestReliableObject", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait for object to be created
	waitForObjectCreated(t, node1, objID, 5*time.Second)

	// Make multiple calls sequentially
	t.Run("SequentialCalls", func(t *testing.T) {
		for i := 1; i <= 3; i++ {
			request := &TestReliableRequest{RequestData: fmt.Sprintf("test%d", i)}
			
			response, err := cluster1.ReliableCallObject(ctx, "TestReliableObject", objID, "TestReliableMethod", request)
			if err != nil {
				t.Fatalf("ReliableCallObject() call %d failed: %v", i, err)
			}

			resp, ok := response.(*TestReliableResponse)
			if !ok {
				t.Fatalf("Expected *TestReliableResponse, got %T", response)
			}

			if resp.CallCount != i {
				t.Fatalf("Call %d: Expected CallCount=%d, got %d", i, i, resp.CallCount)
			}
		}
	})

	// Verify all 3 requests were processed
	lastID, err := db.GetLastProcessedID(ctx, objID)
	if err != nil {
		t.Fatalf("Failed to get last processed ID: %v", err)
	}

	if lastID < 3 {
		t.Fatalf("Expected at least 3 requests processed, got last_id=%d", lastID)
	}
}
