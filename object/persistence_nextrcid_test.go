package object

import (
	"context"
	"testing"
)

func TestNextRcid_DefaultValue(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")

	// Verify default next_rcid is 0
	if obj.GetNextRcid() != 0 {
		t.Fatalf("GetNextRcid() = %d; want 0", obj.GetNextRcid())
	}
}

func TestNextRcid_SetAndGet(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")

	// Set next_rcid
	obj.SetNextRcid(42)

	// Verify it was set correctly
	if obj.GetNextRcid() != 42 {
		t.Fatalf("GetNextRcid() = %d; want 42", obj.GetNextRcid())
	}

	// Update next_rcid
	obj.SetNextRcid(100)

	// Verify it was updated
	if obj.GetNextRcid() != 100 {
		t.Fatalf("GetNextRcid() = %d; want 100", obj.GetNextRcid())
	}
}

func TestNextRcid_Persistence(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Create and save object with next_rcid
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")
	obj.CustomData = "test-data"
	obj.SetNextRcid(123)

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcid())
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Verify next_rcid was saved
	savedNextRcid, ok := provider.nextRcids["test-id"]
	if !ok {
		t.Fatal("next_rcid was not saved")
	}
	if savedNextRcid != 123 {
		t.Fatalf("Saved next_rcid = %d; want 123", savedNextRcid)
	}

	// Load object
	loadedObj := &TestPersistentObject{}
	loadedObj.OnInit(loadedObj, "test-id")

	loadedData, err := loadedObj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	nextRcid, err := LoadObject(ctx, provider, "test-id", loadedData)
	if err != nil {
		t.Fatalf("LoadObject() returned error: %v", err)
	}

	// Verify next_rcid was loaded correctly
	if nextRcid != 123 {
		t.Fatalf("Loaded next_rcid = %d; want 123", nextRcid)
	}

	// Restore object state
	err = loadedObj.FromData(loadedData)
	if err != nil {
		t.Fatalf("FromData() returned error: %v", err)
	}

	// Set the loaded next_rcid on the object
	loadedObj.SetNextRcid(nextRcid)

	// Verify object state and next_rcid
	if loadedObj.CustomData != "test-data" {
		t.Fatalf("CustomData = %s; want test-data", loadedObj.CustomData)
	}
	if loadedObj.GetNextRcid() != 123 {
		t.Fatalf("GetNextRcid() = %d; want 123", loadedObj.GetNextRcid())
	}
}

func TestNextRcid_UpdatePersistence(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Create and save object
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id")
	obj.CustomData = "initial"
	obj.SetNextRcid(10)

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcid())
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Update object and next_rcid
	obj.CustomData = "updated"
	obj.SetNextRcid(20)

	data, err = obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcid())
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Load and verify updated next_rcid
	loadedObj := &TestPersistentObject{}
	loadedObj.OnInit(loadedObj, "test-id")

	loadedData, err := loadedObj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	nextRcid, err := LoadObject(ctx, provider, "test-id", loadedData)
	if err != nil {
		t.Fatalf("LoadObject() returned error: %v", err)
	}

	if nextRcid != 20 {
		t.Fatalf("Loaded next_rcid = %d; want 20", nextRcid)
	}

	err = loadedObj.FromData(loadedData)
	if err != nil {
		t.Fatalf("FromData() returned error: %v", err)
	}

	if loadedObj.CustomData != "updated" {
		t.Fatalf("CustomData = %s; want updated", loadedObj.CustomData)
	}
}

func TestNextRcid_LoadObjectNotFound(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	obj := &TestPersistentObject{}
	obj.OnInit(obj, "nonexistent-id")

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	nextRcid, err := LoadObject(ctx, provider, "nonexistent-id", data)
	if err == nil {
		t.Fatal("LoadObject() should return error for nonexistent object")
	}

	// Verify next_rcid is 0 when object not found
	if nextRcid != 0 {
		t.Fatalf("next_rcid = %d; want 0 for nonexistent object", nextRcid)
	}
}

func TestNextRcid_MultipleObjects(t *testing.T) {
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Create and save multiple objects with different next_rcid values
	objects := []struct {
		id       string
		data     string
		nextRcid int64
	}{
		{"obj-1", "data-1", 100},
		{"obj-2", "data-2", 200},
		{"obj-3", "data-3", 300},
	}

	for _, tc := range objects {
		obj := &TestPersistentObject{}
		obj.OnInit(obj, tc.id)
		obj.CustomData = tc.data
		obj.SetNextRcid(tc.nextRcid)

		data, err := obj.ToData()
		if err != nil {
			t.Fatalf("ToData() returned error for %s: %v", tc.id, err)
		}

		err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data, obj.GetNextRcid())
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

		nextRcid, err := LoadObject(ctx, provider, tc.id, data)
		if err != nil {
			t.Fatalf("LoadObject() returned error for %s: %v", tc.id, err)
		}

		if nextRcid != tc.nextRcid {
			t.Fatalf("Object %s: next_rcid = %d; want %d", tc.id, nextRcid, tc.nextRcid)
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
