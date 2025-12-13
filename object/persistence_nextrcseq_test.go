package object

import (
	"context"
	"testing"
)

func TestNextRcseq_DefaultValue(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")

	// Verify default next_rcseq is 0
	if obj.GetNextRcseq() != 0 {
		t.Fatalf("GetNextRcseq() = %d; want 0", obj.GetNextRcseq())
	}
}

func TestNextRcseq_SetAndGet(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")

	// Set next_rcseq
	obj.SetNextRcseq(42)

	// Verify it was set correctly
	if obj.GetNextRcseq() != 42 {
		t.Fatalf("GetNextRcseq() = %d; want 42", obj.GetNextRcseq())
	}

	// Update next_rcseq
	obj.SetNextRcseq(100)

	// Verify it was updated
	if obj.GetNextRcseq() != 100 {
		t.Fatalf("GetNextRcseq() = %d; want 100", obj.GetNextRcseq())
	}
}

func TestNextRcseq_Persistence(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Create and save object with next_rcseq
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")
	obj.CustomData = "test-data"
	obj.SetNextRcseq(123)

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcseq())
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Verify next_rcseq was saved
	savedNextRcseq, ok := provider.GetStoredNextRcseq("test-id")
	if !ok {
		t.Fatal("next_rcseq was not saved")
	}
	if savedNextRcseq != 123 {
		t.Fatalf("Saved next_rcseq = %d; want 123", savedNextRcseq)
	}

	// Load object
	loadedObj := &TestPersistentObject{}
	loadedObj.OnInit(loadedObj, "test-id")

	loadedData, err := loadedObj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	nextRcseq, err := LoadObject(ctx, provider, "test-id", loadedData)
	if err != nil {
		t.Fatalf("LoadObject() returned error: %v", err)
	}

	// Verify next_rcseq was loaded correctly
	if nextRcseq != 123 {
		t.Fatalf("Loaded next_rcseq = %d; want 123", nextRcseq)
	}

	// Restore object state
	err = loadedObj.FromData(loadedData)
	if err != nil {
		t.Fatalf("FromData() returned error: %v", err)
	}

	// Set the loaded next_rcseq on the object
	loadedObj.SetNextRcseq(nextRcseq)

	// Verify object state and next_rcseq
	if loadedObj.CustomData != "test-data" {
		t.Fatalf("CustomData = %s; want test-data", loadedObj.CustomData)
	}
	if loadedObj.GetNextRcseq() != 123 {
		t.Fatalf("GetNextRcseq() = %d; want 123", loadedObj.GetNextRcseq())
	}
}

func TestNextRcseq_UpdatePersistence(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Create and save object
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")
	obj.CustomData = "initial"
	obj.SetNextRcseq(10)

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcseq())
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Update object and next_rcseq
	obj.CustomData = "updated"
	obj.SetNextRcseq(20)

	data, err = obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcseq())
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Load and verify updated next_rcseq
	loadedObj := &TestPersistentObject{}
	loadedObj.OnInit(loadedObj, "test-id")

	loadedData, err := loadedObj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	nextRcseq, err := LoadObject(ctx, provider, "test-id", loadedData)
	if err != nil {
		t.Fatalf("LoadObject() returned error: %v", err)
	}

	if nextRcseq != 20 {
		t.Fatalf("Loaded next_rcseq = %d; want 20", nextRcseq)
	}

	err = loadedObj.FromData(loadedData)
	if err != nil {
		t.Fatalf("FromData() returned error: %v", err)
	}

	if loadedObj.CustomData != "updated" {
		t.Fatalf("CustomData = %s; want updated", loadedObj.CustomData)
	}
}

func TestNextRcseq_LoadObjectNotFound(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	obj := &TestPersistentObject{}
	obj.OnInit(obj, "nonexistent-id")

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	nextRcseq, err := LoadObject(ctx, provider, "nonexistent-id", data)
	if err == nil {
		t.Fatal("LoadObject() should return error for nonexistent object")
	}

	// Verify next_rcseq is 0 when object not found
	if nextRcseq != 0 {
		t.Fatalf("next_rcseq = %d; want 0 for nonexistent object", nextRcseq)
	}
}

func TestNextRcseq_MultipleObjects(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Create and save multiple objects with different next_rcseq values
	objects := []struct {
		id        string
		data      string
		nextRcseq int64
	}{
		{"obj-1", "data-1", 100},
		{"obj-2", "data-2", 200},
		{"obj-3", "data-3", 300},
	}

	for _, tc := range objects {
		obj := &TestPersistentObject{}
		obj.OnInit(obj, tc.id)
		obj.CustomData = tc.data
		obj.SetNextRcseq(tc.nextRcseq)

		data, err := obj.ToData()
		if err != nil {
			t.Fatalf("ToData() returned error for %s: %v", tc.id, err)
		}

		err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcseq())
		if err != nil {
			t.Fatalf("SaveObject() returned error for %s: %v", tc.id, err)
		}
	}

	// Load and verify each object
	for _, tc := range objects {
		obj := &TestPersistentObject{}
		obj.OnInit(obj, tc.id)

		data, err := obj.ToData()
		if err != nil {
			t.Fatalf("ToData() returned error for %s: %v", tc.id, err)
		}

		nextRcseq, err := LoadObject(ctx, provider, tc.id, data)
		if err != nil {
			t.Fatalf("LoadObject() returned error for %s: %v", tc.id, err)
		}

		if nextRcseq != tc.nextRcseq {
			t.Fatalf("Object %s: next_rcseq = %d; want %d", tc.id, nextRcseq, tc.nextRcseq)
		}

		err = obj.FromData(data)
		if err != nil {
			t.Fatalf("FromData() returned error for %s: %v", tc.id, err)
		}

		if obj.CustomData != tc.data {
			t.Fatalf("Object %s: CustomData = %s; want %s", tc.id, obj.CustomData, tc.data)
		}
	}
}
