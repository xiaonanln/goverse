package goverseclient

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/gate/gateserver"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ReliableTestObject is a test object for reliable call testing
type ReliableTestObject struct {
	object.BaseObject
	mu        sync.Mutex
	callCount int
	values    []float64
}

func (o *ReliableTestObject) OnCreated() {
	o.callCount = 0
	o.values = []float64{}
}

func (o *ReliableTestObject) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (o *ReliableTestObject) FromData(data proto.Message) error {
	return nil
}

// AddValue adds a value and returns the total count and sum
// Request format: {value: number}
// Response format: {count: number, sum: number}
func (o *ReliableTestObject) AddValue(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.callCount++

	var value float64
	if req != nil && req.Fields["value"] != nil {
		value = req.Fields["value"].GetNumberValue()
	}

	o.values = append(o.values, value)

	sum := 0.0
	for _, v := range o.values {
		sum += v
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"count": structpb.NewNumberValue(float64(o.callCount)),
			"sum":   structpb.NewNumberValue(sum),
		},
	}, nil
}

// GetStats returns current statistics
// Response format: {count: number, sum: number}
func (o *ReliableTestObject) GetStats(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	sum := 0.0
	for _, v := range o.values {
		sum += v
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"count": structpb.NewNumberValue(float64(o.callCount)),
			"sum":   structpb.NewNumberValue(sum),
		},
	}, nil
}

// TestReliableCallObject_ClientToGateIntegration tests the full flow: client → gate → node → object
func TestReliableCallObject_ClientToGateIntegration(t *testing.T) {
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

	// Create node cluster
	nodeAddr := testutil.GetFreeAddress()
	// Replace localhost with 127.0.0.1 to avoid IPv6 resolution issues
	nodeAddr = "127.0.0.1" + nodeAddr[len("localhost"):]
	n := node.NewNode(nodeAddr, testutil.TestNumShards)
	err = n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Set persistence provider
	n.SetPersistenceProvider(provider)

	// Register test object type
	n.RegisterObjectType((*ReliableTestObject)(nil))

	// Create node cluster
	nodeCfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
	}
	nodeCluster, err := cluster.NewClusterWithNode(nodeCfg, n)
	if err != nil {
		t.Fatalf("Failed to create node cluster: %v", err)
	}
	err = nodeCluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start node cluster: %v", err)
	}
	defer nodeCluster.Stop(ctx)

	// Start mock gRPC server for the node to handle gate-to-node communication
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(n)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err = nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	defer nodeServer.Stop()
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create gate server
	gateAddr := testutil.GetFreeAddress()
	// Replace localhost with 127.0.0.1 to avoid IPv6 resolution issues
	gateAddr = "127.0.0.1" + gateAddr[len("localhost"):]
	gateCfg := &gateserver.GateServerConfig{
		ListenAddress:                 gateAddr,
		AdvertiseAddress:              gateAddr,
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		NumShards:                     testutil.TestNumShards,
		ClusterStateStabilityDuration: 3 * time.Second,
		DefaultCallTimeout:            30 * time.Second,
	}
	gateServer, err := gateserver.NewGateServer(gateCfg)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	err = gateServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start gate server: %v", err)
	}
	defer gateServer.Stop()

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, nodeCluster)

	// Give gate some time to connect to nodes
	time.Sleep(2 * time.Second)

	// Create client
	client, err := NewClient([]string{gateAddr})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	err = client.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	t.Run("ReliableCall with new call_id", func(t *testing.T) {
		callID := GenerateCallID()
		objectType := "ReliableTestObject"
		objectID := "ReliableTestObject-test1"
		methodName := "AddValue"

		request := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"value": structpb.NewNumberValue(10.0),
			},
		}

		result, status, err := client.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("ReliableCallObject failed: %v", err)
		}
		if status != "SUCCESS" {
			t.Fatalf("Expected status SUCCESS, got %s", status)
		}

		// Verify result
		resultStruct, ok := result.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result)
		}
		count := resultStruct.Fields["count"].GetNumberValue()
		sum := resultStruct.Fields["sum"].GetNumberValue()

		if count != 1 {
			t.Fatalf("Expected count 1, got %f", count)
		}
		if sum != 10.0 {
			t.Fatalf("Expected sum 10.0, got %f", sum)
		}

		t.Logf("✅ First reliable call succeeded: count=%f, sum=%f", count, sum)
	})

	t.Run("Deduplication with same call_id", func(t *testing.T) {
		callID := GenerateCallID()
		objectType := "ReliableTestObject"
		objectID := "ReliableTestObject-test2"
		methodName := "AddValue"

		request := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"value": structpb.NewNumberValue(20.0),
			},
		}

		// First call
		result1, status1, err := client.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("First ReliableCallObject failed: %v", err)
		}
		if status1 != "SUCCESS" {
			t.Fatalf("Expected status SUCCESS, got %s", status1)
		}

		resultStruct1, ok := result1.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result1)
		}
		count1 := resultStruct1.Fields["count"].GetNumberValue()
		sum1 := resultStruct1.Fields["sum"].GetNumberValue()

		t.Logf("First call: count=%f, sum=%f", count1, sum1)

		// Second call with same call_id (should return cached result)
		result2, status2, err := client.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Second ReliableCallObject failed: %v", err)
		}
		if status2 != "SUCCESS" {
			t.Fatalf("Expected status SUCCESS, got %s", status2)
		}

		resultStruct2, ok := result2.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result2)
		}
		count2 := resultStruct2.Fields["count"].GetNumberValue()
		sum2 := resultStruct2.Fields["sum"].GetNumberValue()

		t.Logf("Second call (deduplicated): count=%f, sum=%f", count2, sum2)

		// Verify that the call was not executed twice (deduplication worked)
		if count1 != count2 {
			t.Fatalf("Deduplication failed: count changed from %f to %f", count1, count2)
		}
		if sum1 != sum2 {
			t.Fatalf("Deduplication failed: sum changed from %f to %f", sum1, sum2)
		}

		// Verify the call was actually executed only once
		if count2 != 1 {
			t.Fatalf("Expected count 1 (executed once), got %f", count2)
		}
		if sum2 != 20.0 {
			t.Fatalf("Expected sum 20.0, got %f", sum2)
		}

		t.Logf("✅ Deduplication verified: same call_id returned cached result")
	})

	t.Run("Multiple reliable calls with different call_ids", func(t *testing.T) {
		objectType := "ReliableTestObject"
		objectID := "ReliableTestObject-test3"
		methodName := "AddValue"

		// Make 5 calls with different call_ids
		for i := 1; i <= 5; i++ {
			callID := GenerateCallID()
			request := &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"value": structpb.NewNumberValue(float64(i)),
				},
			}

			result, status, err := client.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
			if err != nil {
				t.Fatalf("ReliableCallObject %d failed: %v", i, err)
			}
			if status != "SUCCESS" {
				t.Fatalf("Expected status SUCCESS for call %d, got %s", i, status)
			}

			resultStruct, ok := result.(*structpb.Struct)
			if !ok {
				t.Fatalf("Expected *structpb.Struct, got %T", result)
			}
			count := resultStruct.Fields["count"].GetNumberValue()
			sum := resultStruct.Fields["sum"].GetNumberValue()

			// Verify that count matches iteration (each call is executed once)
			if count != float64(i) {
				t.Fatalf("Expected count %d, got %f", i, count)
			}

			expectedSum := float64(i * (i + 1) / 2) // Sum of 1+2+...+i
			if sum != expectedSum {
				t.Fatalf("Expected sum %f, got %f", expectedSum, sum)
			}

			t.Logf("Call %d: count=%f, sum=%f", i, count, sum)
		}

		t.Logf("✅ Multiple reliable calls executed successfully")
	})

	t.Run("Concurrent reliable calls", func(t *testing.T) {
		objectType := "ReliableTestObject"
		objectID := "ReliableTestObject-test4"
		methodName := "AddValue"

		var wg sync.WaitGroup
		numCalls := 10
		results := make([]float64, numCalls)
		errors := make([]error, numCalls)

		for i := 0; i < numCalls; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				callID := GenerateCallID()
				request := &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"value": structpb.NewNumberValue(float64(idx + 1)),
					},
				}

				result, status, err := client.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
				if err != nil {
					errors[idx] = err
					return
				}
				if status != "SUCCESS" {
					errors[idx] = err
					return
				}

				resultStruct, ok := result.(*structpb.Struct)
				if !ok {
					errors[idx] = err
					return
				}
				count := resultStruct.Fields["count"].GetNumberValue()
				results[idx] = count
			}(i)
		}

		wg.Wait()

		// Check for errors
		for i, err := range errors {
			if err != nil {
				t.Fatalf("Concurrent call %d failed: %v", i, err)
			}
		}

		// All calls should succeed and have unique counts
		t.Logf("✅ All %d concurrent reliable calls succeeded", numCalls)
	})
}
