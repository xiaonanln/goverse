package node

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/protohelper"
	"google.golang.org/protobuf/proto"
)

// TestCounterForPendingCalls is a simple test object for testing pending calls on creation
type TestCounterForPendingCalls struct {
	object.BaseObject
	value int32
}

func (c *TestCounterForPendingCalls) OnCreated() {
	c.Logger.Infof("TestCounterForPendingCalls %s created", c.Id())
	c.value = 0
}

func (c *TestCounterForPendingCalls) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (c *TestCounterForPendingCalls) FromData(data proto.Message) error {
	return nil
}

func (c *TestCounterForPendingCalls) Increment(ctx context.Context, req *counter_pb.IncrementRequest) (*counter_pb.CounterResponse, error) {
	c.value += req.Amount
	return &counter_pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

// TestCreateObject_ProcessesPendingReliableCalls verifies that pending reliable calls
// are automatically processed when an object is created
func TestCreateObject_ProcessesPendingReliableCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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
		t.Skipf("Skipping test - Failed to initialize schema: %v", err)
		return
	}

	// Create unique test prefix to avoid conflicts
	testPrefix := fmt.Sprintf("test_%d", time.Now().UnixNano())

	// Clear test data
	_, err = db.Connection().ExecContext(ctx, "DELETE FROM goverse_reliable_calls WHERE object_id LIKE $1", testPrefix+"%")
	if err != nil {
		t.Fatalf("Failed to clear test data: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create node
	// Note: This is just metadata - no actual server is started in this test,
	// so the hard-coded port won't cause conflicts with concurrent tests
	nodeAddr := "localhost:12345"
	node := NewNode(nodeAddr, testNumShards)
	node.SetPersistenceProvider(provider)

	// Register the test object type
	node.RegisterObjectType((*TestCounterForPendingCalls)(nil))

	// Test object ID with unique prefix
	objectType := "TestCounterForPendingCalls"
	objectID := testPrefix + "-counter1"

	// Insert pending reliable calls BEFORE creating the object
	// This simulates calls made while the object was inactive
	t.Run("Insert pending calls before object creation", func(t *testing.T) {
		// Insert first call
		request1 := &counter_pb.IncrementRequest{Amount: 5}
		requestData1, err := protohelper.MsgToBytes(request1)
		if err != nil {
			t.Fatalf("Failed to serialize request: %v", err)
		}

		rc1, err := provider.InsertOrGetReliableCall(ctx, testPrefix+"-call1", objectID, objectType, "Increment", requestData1)
		if err != nil {
			t.Fatalf("Failed to insert first pending call: %v", err)
		}
		if rc1.Status != "pending" {
			t.Errorf("Expected status 'pending', got %q", rc1.Status)
		}

		// Insert second call
		request2 := &counter_pb.IncrementRequest{Amount: 3}
		requestData2, err := protohelper.MsgToBytes(request2)
		if err != nil {
			t.Fatalf("Failed to serialize request: %v", err)
		}

		rc2, err := provider.InsertOrGetReliableCall(ctx, testPrefix+"-call2", objectID, objectType, "Increment", requestData2)
		if err != nil {
			t.Fatalf("Failed to insert second pending call: %v", err)
		}
		if rc2.Status != "pending" {
			t.Errorf("Expected status 'pending', got %q", rc2.Status)
		}
	})

	// Now create the object - it should automatically process the pending calls
	t.Run("Create object processes pending calls", func(t *testing.T) {
		_, err := node.CreateObject(ctx, objectType, objectID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Give some time for the processing goroutine to complete
		// The processing happens asynchronously after OnCreated
		time.Sleep(2 * time.Second)

		// Verify both calls were processed successfully
		rc1, err := db.GetReliableCall(ctx, testPrefix+"-call1")
		if err != nil {
			t.Fatalf("Failed to get first call: %v", err)
		}
		if rc1.Status != "success" {
			t.Errorf("Expected first call status 'success', got %q (error: %s)", rc1.Status, rc1.Error)
		}

		rc2, err := db.GetReliableCall(ctx, testPrefix+"-call2")
		if err != nil {
			t.Fatalf("Failed to get second call: %v", err)
		}
		if rc2.Status != "success" {
			t.Errorf("Expected second call status 'success', got %q (error: %s)", rc2.Status, rc2.Error)
		}

		// Verify the results are correct (5 + 3 = 8)
		if rc1.ResultData != nil {
			result1, err := protohelper.BytesToMsg(rc1.ResultData)
			if err != nil {
				t.Errorf("Failed to unmarshal first result: %v", err)
			} else {
				resp1, ok := result1.(*counter_pb.CounterResponse)
				if !ok {
					t.Errorf("Expected *counter_pb.CounterResponse, got %T", result1)
				} else if resp1.Value != 5 {
					t.Errorf("Expected first result value 5, got %d", resp1.Value)
				}
			}
		}

		if rc2.ResultData != nil {
			result2, err := protohelper.BytesToMsg(rc2.ResultData)
			if err != nil {
				t.Errorf("Failed to unmarshal second result: %v", err)
			} else {
				resp2, ok := result2.(*counter_pb.CounterResponse)
				if !ok {
					t.Errorf("Expected *counter_pb.CounterResponse, got %T", result2)
				} else if resp2.Value != 8 {
					t.Errorf("Expected second result value 8 (5+3), got %d", resp2.Value)
				}
			}
		}

		// Verify nextRcseq was updated correctly
		node.objectsMu.RLock()
		obj := node.objects[objectID]
		node.objectsMu.RUnlock()

		if obj != nil {
			nextRcseq := obj.GetNextRcseq()
			// After processing both calls, nextRcseq should be > both seq values
			// Since seq is auto-incremented, we expect nextRcseq to be at least rc2.Seq + 1
			expectedMinSeq := rc2.Seq + 1
			if nextRcseq < expectedMinSeq {
				t.Errorf("Expected nextRcseq >= %d, got %d", expectedMinSeq, nextRcseq)
			}
		}
	})

	// Cleanup
	node.Stop(ctx)
}
