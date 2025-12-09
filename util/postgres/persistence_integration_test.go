package postgres

import (
	"context"
	"testing"
)

func TestSaveObject_Integration(t *testing.T) {
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

	// Test saving a new object
	objectID := "test-obj-123"
	objectType := "TestType"
	data := []byte(`{"name": "test", "value": 42}`)

	err = db.SaveObject(ctx, objectID, objectType, data, 0)
	if err != nil {
		t.Fatalf("SaveObject() failed: %v", err)
	}

	// Verify object was saved
	exists, err := db.ObjectExists(ctx, objectID)
	if err != nil {
		t.Fatalf("ObjectExists() failed: %v", err)
	}
	if !exists {
		t.Fatal("Object should exist after SaveObject()")
	}
}

func TestSaveObject_Update_Integration(t *testing.T) {
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

	objectID := "test-obj-update"
	objectType := "TestType"

	// Save initial version
	data1 := []byte(`{"version": 1}`)
	err = db.SaveObject(ctx, objectID, objectType, data1, 0)
	if err != nil {
		t.Fatalf("SaveObject() initial save failed: %v", err)
	}

	// Update with new data
	data2 := []byte(`{"version": 2}`)
	err = db.SaveObject(ctx, objectID, objectType, data2, 5)
	if err != nil {
		t.Fatalf("SaveObject() update failed: %v", err)
	}

	// Load and verify updated data
	loadedData, nextRcseq, err := db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("LoadObject() failed: %v", err)
	}

	if string(loadedData) != string(data2) {
		t.Fatalf("LoadObject() returned %s, want %s", string(loadedData), string(data2))
	}
	if nextRcseq != 5 {
		t.Fatalf("LoadObject() returned next_rcseq=%d, want 5", nextRcseq)
	}
}

func TestLoadObject_Integration(t *testing.T) {
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

	objectID := "test-obj-load"
	objectType := "TestType"
	data := []byte(`{"key": "value"}`)

	// Save object first
	err = db.SaveObject(ctx, objectID, objectType, data, 0)
	if err != nil {
		t.Fatalf("SaveObject() failed: %v", err)
	}

	// Load object
	loadedData, _, err := db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("LoadObject() failed: %v", err)
	}

	if string(loadedData) != string(data) {
		t.Fatalf("LoadObject() returned %s, want %s", string(loadedData), string(data))
	}
}

func TestLoadObject_NotFound_Integration(t *testing.T) {
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

	// Try to load non-existent object
	_, _, err = db.LoadObject(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("LoadObject() should return error for non-existent object")
	}
}

func TestDeleteObject_Integration(t *testing.T) {
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

	objectID := "test-obj-delete"
	objectType := "TestType"
	data := []byte(`{"key": "value"}`)

	// Save object
	err = db.SaveObject(ctx, objectID, objectType, data, 0)
	if err != nil {
		t.Fatalf("SaveObject() failed: %v", err)
	}

	// Delete object
	err = db.DeleteObject(ctx, objectID)
	if err != nil {
		t.Fatalf("DeleteObject() failed: %v", err)
	}

	// Verify object no longer exists
	exists, err := db.ObjectExists(ctx, objectID)
	if err != nil {
		t.Fatalf("ObjectExists() failed: %v", err)
	}
	if exists {
		t.Fatal("Object should not exist after DeleteObject()")
	}
}

func TestDeleteObject_NotFound_Integration(t *testing.T) {
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

	// Try to delete non-existent object
	err = db.DeleteObject(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("DeleteObject() should return error for non-existent object")
	}
}

func TestObjectExists_Integration(t *testing.T) {
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

	objectID := "test-obj-exists"
	objectType := "TestType"
	data := []byte(`{"key": "value"}`)

	// Check object doesn't exist initially
	exists, err := db.ObjectExists(ctx, objectID)
	if err != nil {
		t.Fatalf("ObjectExists() failed: %v", err)
	}
	if exists {
		t.Fatal("Object should not exist initially")
	}

	// Save object
	err = db.SaveObject(ctx, objectID, objectType, data, 0)
	if err != nil {
		t.Fatalf("SaveObject() failed: %v", err)
	}

	// Check object exists
	exists, err = db.ObjectExists(ctx, objectID)
	if err != nil {
		t.Fatalf("ObjectExists() failed: %v", err)
	}
	if !exists {
		t.Fatal("Object should exist after SaveObject()")
	}
}

func TestListObjectsByType_Integration(t *testing.T) {
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

	objectType := "TestTypeList"

	// Save multiple objects of the same type
	objects := []struct {
		id   string
		data []byte
	}{
		{"obj-1", []byte(`{"id": 1}`)},
		{"obj-2", []byte(`{"id": 2}`)},
		{"obj-3", []byte(`{"id": 3}`)},
	}

	for _, obj := range objects {
		err = db.SaveObject(ctx, obj.id, objectType, obj.data, 0)
		if err != nil {
			t.Fatalf("SaveObject(%s) failed: %v", obj.id, err)
		}
	}

	// Save an object of a different type
	err = db.SaveObject(ctx, "other-obj", "OtherType", []byte(`{}`), 0)
	if err != nil {
		t.Fatalf("SaveObject(other-obj) failed: %v", err)
	}

	// List objects by type
	results, err := db.ListObjectsByType(ctx, objectType)
	if err != nil {
		t.Fatalf("ListObjectsByType() failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("ListObjectsByType() returned %d objects, want 3", len(results))
	}

	// Verify all objects are of the correct type
	for _, result := range results {
		if result.ObjectType != objectType {
			t.Fatalf("Object %s has type %s, want %s", result.ObjectID, result.ObjectType, objectType)
		}
	}
}

func TestListObjectsByType_Empty_Integration(t *testing.T) {
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

	// List objects of a type that doesn't exist
	results, err := db.ListObjectsByType(ctx, "NonExistentType")
	if err != nil {
		t.Fatalf("ListObjectsByType() failed: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("ListObjectsByType() returned %d objects, want 0", len(results))
	}
}

func TestPersistence_FullWorkflow_Integration(t *testing.T) {
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

	// Full workflow test: create, read, update, list, delete
	objectID := "workflow-test"
	objectType := "WorkflowType"
	initialData := []byte(`{"status": "created"}`)
	updatedData := []byte(`{"status": "updated"}`)

	// 1. Create
	err = db.SaveObject(ctx, objectID, objectType, initialData, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 2. Read
	data, nextRcseq, err := db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != string(initialData) {
		t.Fatalf("Read returned %s, want %s", string(data), string(initialData))
	}
	if nextRcseq != 0 {
		t.Fatalf("Read returned next_rcseq=%d, want 0", nextRcseq)
	}

	// 3. Update
	err = db.SaveObject(ctx, objectID, objectType, updatedData, 10)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// 4. Verify update
	data, nextRcseq, err = db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Read after update failed: %v", err)
	}
	if string(data) != string(updatedData) {
		t.Fatalf("Read after update returned %s, want %s", string(data), string(updatedData))
	}
	if nextRcseq != 10 {
		t.Fatalf("Read after update returned next_rcseq=%d, want 10", nextRcseq)
	}

	// 5. List
	objects, err := db.ListObjectsByType(ctx, objectType)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("List returned %d objects, want 1", len(objects))
	}

	// 6. Delete
	err = db.DeleteObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 7. Verify deletion
	exists, err := db.ObjectExists(ctx, objectID)
	if err != nil {
		t.Fatalf("Exists check after delete failed: %v", err)
	}
	if exists {
		t.Fatal("Object should not exist after delete")
	}
}

func TestInsertOrGetReliableCall_Integration(t *testing.T) {
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

	requestID := "test-req-123"
	objectID := "test-obj-123"
	objectType := "TestType"
	methodName := "TestMethod"
	requestData := []byte("test-data")

	// First call should insert
	rc1, err := db.InsertOrGetReliableCall(ctx, requestID, objectID, objectType, methodName, requestData)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() first call failed: %v", err)
	}

	if rc1.CallID != requestID {
		t.Fatalf("CallID = %s, want %s", rc1.CallID, requestID)
	}
	if rc1.ObjectID != objectID {
		t.Fatalf("ObjectID = %s, want %s", rc1.ObjectID, objectID)
	}
	if rc1.Status != "pending" {
		t.Fatalf("Status = %s, want pending", rc1.Status)
	}

	// Second call with same requestID should return existing
	rc2, err := db.InsertOrGetReliableCall(ctx, requestID, objectID, objectType, methodName, requestData)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() second call failed: %v", err)
	}

	if rc2.Seq != rc1.Seq {
		t.Fatalf("Second call returned different Seq: %d, want %d", rc2.Seq, rc1.Seq)
	}
}

func TestUpdateReliableCallStatus_Integration(t *testing.T) {
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

	// Create a reliable call
	requestID := "test-req-update"
	rc, err := db.InsertOrGetReliableCall(ctx, requestID, "obj-1", "TestType", "Method", []byte("data"))
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() failed: %v", err)
	}

	// Update status to completed
	resultData := []byte("result-data")
	err = db.UpdateReliableCallStatus(ctx, rc.Seq, "completed", resultData, "")
	if err != nil {
		t.Fatalf("UpdateReliableCallStatus() failed: %v", err)
	}

	// Verify update
	updated, err := db.GetReliableCall(ctx, requestID)
	if err != nil {
		t.Fatalf("GetReliableCall() failed: %v", err)
	}

	if updated.Status != "completed" {
		t.Fatalf("Status = %s, want completed", updated.Status)
	}
	if string(updated.ResultData) != string(resultData) {
		t.Fatalf("ResultData = %s, want %s", string(updated.ResultData), string(resultData))
	}
}

func TestGetPendingReliableCalls_Integration(t *testing.T) {
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

	objectID := "test-obj-pending"

	// Insert multiple calls
	rc1, err := db.InsertOrGetReliableCall(ctx, "req-1", objectID, "Type", "Method", []byte("data1"))
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() req-1 failed: %v", err)
	}

	rc2, err := db.InsertOrGetReliableCall(ctx, "req-2", objectID, "Type", "Method", []byte("data2"))
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() req-2 failed: %v", err)
	}

	rc3, err := db.InsertOrGetReliableCall(ctx, "req-3", objectID, "Type", "Method", []byte("data3"))
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() req-3 failed: %v", err)
	}

	// Update one to completed
	err = db.UpdateReliableCallStatus(ctx, rc2.Seq, "completed", []byte("result"), "")
	if err != nil {
		t.Fatalf("UpdateReliableCallStatus() failed: %v", err)
	}

	// Get pending calls with nextRcseq = 0
	pending, err := db.GetPendingReliableCalls(ctx, objectID, 0)
	if err != nil {
		t.Fatalf("GetPendingReliableCalls() failed: %v", err)
	}

	if len(pending) != 2 {
		t.Fatalf("GetPendingReliableCalls() returned %d calls, want 2", len(pending))
	}

	// Verify order and content
	if pending[0].Seq != rc1.Seq {
		t.Fatalf("First pending call Seq = %d, want %d", pending[0].Seq, rc1.Seq)
	}
	if pending[1].Seq != rc3.Seq {
		t.Fatalf("Second pending call Seq = %d, want %d", pending[1].Seq, rc3.Seq)
	}

	// Get pending calls with nextRcseq = rc1.Seq (should only return rc3)
	pending2, err := db.GetPendingReliableCalls(ctx, objectID, rc1.Seq)
	if err != nil {
		t.Fatalf("GetPendingReliableCalls() with nextRcseq failed: %v", err)
	}

	if len(pending2) != 1 {
		t.Fatalf("GetPendingReliableCalls() with nextRcseq returned %d calls, want 1", len(pending2))
	}
	if pending2[0].Seq != rc3.Seq {
		t.Fatalf("Pending call Seq = %d, want %d", pending2[0].Seq, rc3.Seq)
	}
}

func TestGetReliableCall_Integration(t *testing.T) {
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

	requestID := "test-req-get"
	objectID := "test-obj-get"
	objectType := "TestType"
	methodName := "TestMethod"
	requestData := []byte("test-data")

	// Insert a call
	inserted, err := db.InsertOrGetReliableCall(ctx, requestID, objectID, objectType, methodName, requestData)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() failed: %v", err)
	}

	// Get by request ID
	retrieved, err := db.GetReliableCall(ctx, requestID)
	if err != nil {
		t.Fatalf("GetReliableCall() failed: %v", err)
	}

	if retrieved.Seq != inserted.Seq {
		t.Fatalf("Seq = %d, want %d", retrieved.Seq, inserted.Seq)
	}
	if retrieved.CallID != requestID {
		t.Fatalf("CallID = %s, want %s", retrieved.CallID, requestID)
	}
	if retrieved.ObjectID != objectID {
		t.Fatalf("ObjectID = %s, want %s", retrieved.ObjectID, objectID)
	}
	if string(retrieved.RequestData) != string(requestData) {
		t.Fatalf("RequestData = %s, want %s", string(retrieved.RequestData), string(requestData))
	}
}

func TestGetReliableCall_NotFound_Integration(t *testing.T) {
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

	// Try to get non-existent call
	_, err = db.GetReliableCall(ctx, "non-existent-req")
	if err == nil {
		t.Fatal("GetReliableCall() should return error for non-existent request")
	}
}
