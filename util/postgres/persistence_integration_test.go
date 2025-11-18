package postgres

import (
	"context"
	"testing"
)

func TestSaveObject_Integration(t *testing.T) {
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

	err = db.SaveObject(ctx, objectID, objectType, data)
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
	err = db.SaveObject(ctx, objectID, objectType, data1)
	if err != nil {
		t.Fatalf("SaveObject() initial save failed: %v", err)
	}

	// Update with new data
	data2 := []byte(`{"version": 2}`)
	err = db.SaveObject(ctx, objectID, objectType, data2)
	if err != nil {
		t.Fatalf("SaveObject() update failed: %v", err)
	}

	// Load and verify updated data
	loadedData, err := db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("LoadObject() failed: %v", err)
	}

	if string(loadedData) != string(data2) {
		t.Fatalf("LoadObject() returned %s, want %s", string(loadedData), string(data2))
	}
}

func TestLoadObject_Integration(t *testing.T) {
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
	err = db.SaveObject(ctx, objectID, objectType, data)
	if err != nil {
		t.Fatalf("SaveObject() failed: %v", err)
	}

	// Load object
	loadedData, err := db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("LoadObject() failed: %v", err)
	}

	if string(loadedData) != string(data) {
		t.Fatalf("LoadObject() returned %s, want %s", string(loadedData), string(data))
	}
}

func TestLoadObject_NotFound_Integration(t *testing.T) {
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
	_, err = db.LoadObject(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("LoadObject() should return error for non-existent object")
	}
}

func TestDeleteObject_Integration(t *testing.T) {
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
	err = db.SaveObject(ctx, objectID, objectType, data)
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
	err = db.SaveObject(ctx, objectID, objectType, data)
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
		err = db.SaveObject(ctx, obj.id, objectType, obj.data)
		if err != nil {
			t.Fatalf("SaveObject(%s) failed: %v", obj.id, err)
		}
	}

	// Save an object of a different type
	err = db.SaveObject(ctx, "other-obj", "OtherType", []byte(`{}`))
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
	err = db.SaveObject(ctx, objectID, objectType, initialData)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 2. Read
	data, err := db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != string(initialData) {
		t.Fatalf("Read returned %s, want %s", string(data), string(initialData))
	}

	// 3. Update
	err = db.SaveObject(ctx, objectID, objectType, updatedData)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// 4. Verify update
	data, err = db.LoadObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Read after update failed: %v", err)
	}
	if string(data) != string(updatedData) {
		t.Fatalf("Read after update returned %s, want %s", string(data), string(updatedData))
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
