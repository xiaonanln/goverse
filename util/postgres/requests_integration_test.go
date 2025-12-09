package postgres

import (
	"context"
	"testing"
	"time"
)

func TestInsertRequest_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Initialize schema
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	// Test inserting a new request
	requestID := "req-123"
	objectID := "obj-456"
	objectType := "TestObject"
	methodName := "TestMethod"
	requestData := []byte("test request data")

	id, err := db.InsertRequest(ctx, requestID, objectID, objectType, methodName, requestData)
	if err != nil {
		t.Fatalf("InsertRequest() failed: %v", err)
	}

	if id <= 0 {
		t.Fatalf("InsertRequest() returned invalid ID: %d", id)
	}

	// Verify request was inserted
	req, err := db.GetRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("GetRequest() failed: %v", err)
	}

	if req == nil {
		t.Fatal("Request should exist after InsertRequest()")
	}

	if req.RequestID != requestID {
		t.Fatalf("GetRequest() returned request_id=%s, want %s", req.RequestID, requestID)
	}

	if req.ObjectID != objectID {
		t.Fatalf("GetRequest() returned object_id=%s, want %s", req.ObjectID, objectID)
	}

	if req.Status != RequestStatusPending {
		t.Fatalf("GetRequest() returned status=%s, want %s", req.Status, RequestStatusPending)
	}
}

func TestGetRequest_NotFound_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	// Try to get a non-existent request
	req, err := db.GetRequest(ctx, "nonexistent-req")
	if err != nil {
		t.Fatalf("GetRequest() failed: %v", err)
	}

	if req != nil {
		t.Fatal("GetRequest() should return nil for non-existent request")
	}
}

func TestUpdateRequestStatus_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	// Insert a request
	requestID := "req-update-123"
	objectID := "obj-789"
	objectType := "TestObject"
	methodName := "TestMethod"
	requestData := []byte("test data")

	_, err = db.InsertRequest(ctx, requestID, objectID, objectType, methodName, requestData)
	if err != nil {
		t.Fatalf("InsertRequest() failed: %v", err)
	}

	// Update to processing
	err = db.UpdateRequestStatus(ctx, requestID, RequestStatusProcessing, nil, "")
	if err != nil {
		t.Fatalf("UpdateRequestStatus() to processing failed: %v", err)
	}

	// Verify status was updated
	req, err := db.GetRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("GetRequest() failed: %v", err)
	}

	if req.Status != RequestStatusProcessing {
		t.Fatalf("GetRequest() returned status=%s, want %s", req.Status, RequestStatusProcessing)
	}

	// Update to completed
	resultData := []byte("test result")
	err = db.UpdateRequestStatus(ctx, requestID, RequestStatusCompleted, resultData, "")
	if err != nil {
		t.Fatalf("UpdateRequestStatus() to completed failed: %v", err)
	}

	// Verify status and result were updated
	req, err = db.GetRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("GetRequest() failed: %v", err)
	}

	if req.Status != RequestStatusCompleted {
		t.Fatalf("GetRequest() returned status=%s, want %s", req.Status, RequestStatusCompleted)
	}

	if string(req.ResultData) != string(resultData) {
		t.Fatalf("GetRequest() returned result_data=%s, want %s", string(req.ResultData), string(resultData))
	}
}

func TestUpdateRequestStatus_Failed_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	// Insert a request
	requestID := "req-failed-123"
	objectID := "obj-fail"
	objectType := "TestObject"
	methodName := "TestMethod"
	requestData := []byte("test data")

	_, err = db.InsertRequest(ctx, requestID, objectID, objectType, methodName, requestData)
	if err != nil {
		t.Fatalf("InsertRequest() failed: %v", err)
	}

	// Update to failed
	errorMessage := "test error message"
	err = db.UpdateRequestStatus(ctx, requestID, RequestStatusFailed, nil, errorMessage)
	if err != nil {
		t.Fatalf("UpdateRequestStatus() to failed: %v", err)
	}

	// Verify status and error message were updated
	req, err := db.GetRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("GetRequest() failed: %v", err)
	}

	if req.Status != RequestStatusFailed {
		t.Fatalf("GetRequest() returned status=%s, want %s", req.Status, RequestStatusFailed)
	}

	if req.ErrorMessage != errorMessage {
		t.Fatalf("GetRequest() returned error_message=%s, want %s", req.ErrorMessage, errorMessage)
	}
}

func TestGetPendingRequests_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	objectID := "obj-pending-test"

	// Insert multiple requests for the same object
	req1ID := "req-pending-1"
	req2ID := "req-pending-2"
	req3ID := "req-pending-3"

	id1, err := db.InsertRequest(ctx, req1ID, objectID, "TestObject", "Method1", []byte("data1"))
	if err != nil {
		t.Fatalf("InsertRequest() 1 failed: %v", err)
	}

	_, err = db.InsertRequest(ctx, req2ID, objectID, "TestObject", "Method2", []byte("data2"))
	if err != nil {
		t.Fatalf("InsertRequest() 2 failed: %v", err)
	}

	id3, err := db.InsertRequest(ctx, req3ID, objectID, "TestObject", "Method3", []byte("data3"))
	if err != nil {
		t.Fatalf("InsertRequest() 3 failed: %v", err)
	}

	// Mark one as completed
	err = db.UpdateRequestStatus(ctx, req2ID, RequestStatusCompleted, []byte("result2"), "")
	if err != nil {
		t.Fatalf("UpdateRequestStatus() failed: %v", err)
	}

	// Get pending requests
	pending, err := db.GetPendingRequests(ctx, objectID)
	if err != nil {
		t.Fatalf("GetPendingRequests() failed: %v", err)
	}

	// Should have 2 pending requests (req1 and req3)
	if len(pending) != 2 {
		t.Fatalf("GetPendingRequests() returned %d requests, want 2", len(pending))
	}

	// Verify they are in order by ID
	if pending[0].ID != id1 {
		t.Fatalf("First pending request has ID=%d, want %d", pending[0].ID, id1)
	}

	if pending[1].ID != id3 {
		t.Fatalf("Second pending request has ID=%d, want %d", pending[1].ID, id3)
	}
}

func TestGetLastProcessedID_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	objectID := "obj-last-processed"

	// Initially, last processed ID should be 0
	lastID, err := db.GetLastProcessedID(ctx, objectID)
	if err != nil {
		t.Fatalf("GetLastProcessedID() failed: %v", err)
	}

	if lastID != 0 {
		t.Fatalf("GetLastProcessedID() returned %d, want 0 for new object", lastID)
	}

	// Insert and complete some requests
	req1ID := "req-lp-1"
	req2ID := "req-lp-2"
	req3ID := "req-lp-3"

	id1, err := db.InsertRequest(ctx, req1ID, objectID, "TestObject", "Method1", []byte("data1"))
	if err != nil {
		t.Fatalf("InsertRequest() 1 failed: %v", err)
	}

	id2, err := db.InsertRequest(ctx, req2ID, objectID, "TestObject", "Method2", []byte("data2"))
	if err != nil {
		t.Fatalf("InsertRequest() 2 failed: %v", err)
	}

	_, err = db.InsertRequest(ctx, req3ID, objectID, "TestObject", "Method3", []byte("data3"))
	if err != nil {
		t.Fatalf("InsertRequest() 3 failed: %v", err)
	}

	// Complete first request
	err = db.UpdateRequestStatus(ctx, req1ID, RequestStatusCompleted, []byte("result1"), "")
	if err != nil {
		t.Fatalf("UpdateRequestStatus() 1 failed: %v", err)
	}

	// Last processed ID should be id1
	lastID, err = db.GetLastProcessedID(ctx, objectID)
	if err != nil {
		t.Fatalf("GetLastProcessedID() failed: %v", err)
	}

	if lastID != id1 {
		t.Fatalf("GetLastProcessedID() returned %d, want %d", lastID, id1)
	}

	// Complete second request
	err = db.UpdateRequestStatus(ctx, req2ID, RequestStatusCompleted, []byte("result2"), "")
	if err != nil {
		t.Fatalf("UpdateRequestStatus() 2 failed: %v", err)
	}

	// Last processed ID should be id2
	lastID, err = db.GetLastProcessedID(ctx, objectID)
	if err != nil {
		t.Fatalf("GetLastProcessedID() failed: %v", err)
	}

	if lastID != id2 {
		t.Fatalf("GetLastProcessedID() returned %d, want %d", lastID, id2)
	}
}

func TestWaitForRequestCompletion_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	requestID := "req-wait-test"
	objectID := "obj-wait"

	// Insert a pending request
	_, err = db.InsertRequest(ctx, requestID, objectID, "TestObject", "TestMethod", []byte("data"))
	if err != nil {
		t.Fatalf("InsertRequest() failed: %v", err)
	}

	// Start a goroutine to complete the request after a delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		resultData := []byte("completed result")
		err := db.UpdateRequestStatus(context.Background(), requestID, RequestStatusCompleted, resultData, "")
		if err != nil {
			t.Errorf("UpdateRequestStatus() in goroutine failed: %v", err)
		}
	}()

	// Wait for completion
	completedReq, err := db.WaitForRequestCompletion(ctx, requestID, 50*time.Millisecond, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForRequestCompletion() failed: %v", err)
	}

	if completedReq == nil {
		t.Fatal("WaitForRequestCompletion() returned nil request")
	}

	if completedReq.Status != RequestStatusCompleted {
		t.Fatalf("WaitForRequestCompletion() returned status=%s, want %s", completedReq.Status, RequestStatusCompleted)
	}

	if string(completedReq.ResultData) != "completed result" {
		t.Fatalf("WaitForRequestCompletion() returned result_data=%s, want 'completed result'", string(completedReq.ResultData))
	}
}

func TestWaitForRequestCompletion_Timeout_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}
	defer cleanupTestTable(t, db)

	requestID := "req-timeout-test"
	objectID := "obj-timeout"

	// Insert a pending request
	_, err = db.InsertRequest(ctx, requestID, objectID, "TestObject", "TestMethod", []byte("data"))
	if err != nil {
		t.Fatalf("InsertRequest() failed: %v", err)
	}

	// Wait for completion with a short timeout (request will never complete)
	_, err = db.WaitForRequestCompletion(ctx, requestID, 50*time.Millisecond, 200*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForRequestCompletion() should have timed out but didn't")
	}

	// Verify it's a timeout error
	if err.Error() != "timeout waiting for request completion" {
		t.Fatalf("Expected timeout error, got: %v", err)
	}
}
