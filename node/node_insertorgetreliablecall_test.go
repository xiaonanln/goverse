package node

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestNode_InsertOrGetReliableCall tests the Node.InsertOrGetReliableCall function with real postgres
func TestNode_InsertOrGetReliableCall(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping postgres test in short mode")
	}

	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}

	ctx := context.Background()

	// Initialize schema
	err := db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Create node with postgres persistence
	node := NewNode("localhost:47000", testNumShards)
	provider := postgres.NewPostgresPersistenceProvider(db)
	node.SetPersistenceProvider(provider)

	// Test data
	callID := "test-call-123"
	objectType := "TestObject"
	objectID := "test-obj-123"
	methodName := "TestMethod"

	// Create a simple request message
	request, err := structpb.NewStruct(map[string]interface{}{
		"param1": "value1",
		"param2": 42,
	})
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Test 1: First call should insert a new reliable call
	rc1, err := node.InsertOrGetReliableCall(ctx, callID, objectType, objectID, methodName, request)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() first call failed: %v", err)
	}

	// Verify the returned reliable call
	if rc1.CallID != callID {
		t.Fatalf("CallID = %s, want %s", rc1.CallID, callID)
	}
	if rc1.ObjectID != objectID {
		t.Fatalf("ObjectID = %s, want %s", rc1.ObjectID, objectID)
	}
	if rc1.ObjectType != objectType {
		t.Fatalf("ObjectType = %s, want %s", rc1.ObjectType, objectType)
	}
	if rc1.MethodName != methodName {
		t.Fatalf("MethodName = %s, want %s", rc1.MethodName, methodName)
	}
	if rc1.Status != "pending" {
		t.Fatalf("Status = %s, want pending", rc1.Status)
	}

	// Test 2: Second call with same callID should return the existing reliable call
	rc2, err := node.InsertOrGetReliableCall(ctx, callID, objectType, objectID, methodName, request)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() second call failed: %v", err)
	}

	// Verify we got the same reliable call (same Seq)
	if rc2.Seq != rc1.Seq {
		t.Fatalf("Second call returned different Seq: %d, want %d", rc2.Seq, rc1.Seq)
	}
	if rc2.CallID != rc1.CallID {
		t.Fatalf("Second call returned different CallID: %s, want %s", rc2.CallID, rc1.CallID)
	}

	// Test 3: Call without persistence provider should return error
	nodeNoPersistence := NewNode("localhost:47001", testNumShards)
	_, err = nodeNoPersistence.InsertOrGetReliableCall(ctx, "test-call-456", objectType, objectID, methodName, request)
	if err == nil {
		t.Fatal("Expected error when persistence provider is not configured")
	}
	if err.Error() != "persistence provider is not configured" {
		t.Fatalf("Expected 'persistence provider is not configured' error, got: %v", err)
	}
}
