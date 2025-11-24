package node

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestListObjectIDs tests the ListObjectIDs method
func TestListObjectIDs(t *testing.T) {
	t.Parallel()

	n := NewNode("localhost:50000", sharding.NumShards)

	// Initially should have no objects
	ids := n.ListObjectIDs()
	if len(ids) != 0 {
		t.Fatalf("Expected 0 object IDs initially, got %d", len(ids))
	}

	// The method should return nil when no objects exist
	if ids != nil {
		t.Fatalf("Expected nil, got %v", ids)
	}
}

// TestListObjectIDs_EmptySlice tests that empty list returns nil
func TestListObjectIDs_EmptySlice(t *testing.T) {
	t.Parallel()

	n := NewNode("localhost:50001", sharding.NumShards)

	ids := n.ListObjectIDs()

	// Should return nil when no objects exist
	if ids != nil {
		t.Fatalf("Expected nil, got %v", ids)
	}

	if len(ids) != 0 {
		t.Fatalf("Expected length 0, got %d", len(ids))
	}
}

// TestListObjectIDs_WithObjects tests that ListObjectIDs returns the correct object IDs
func TestListObjectIDs_WithObjects(t *testing.T) {
	t.Parallel()

	n := NewNode("localhost:50002", sharding.NumShards)
	n.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Create multiple objects
	objID1 := "list-obj-1"
	objID2 := "list-obj-2"
	objID3 := "list-obj-3"

	_, err := n.CreateObject(ctx, "TestPersistentObject", objID1)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}

	_, err = n.CreateObject(ctx, "TestPersistentObject", objID2)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	_, err = n.CreateObject(ctx, "TestPersistentObject", objID3)
	if err != nil {
		t.Fatalf("Failed to create object 3: %v", err)
	}

	// Wait for all objects to be created (CreateObject is async)
	waitForObjectCreated(t, n, objID1, 5*time.Second)
	waitForObjectCreated(t, n, objID2, 5*time.Second)
	waitForObjectCreated(t, n, objID3, 5*time.Second)

	// Get object IDs
	ids := n.ListObjectIDs()

	// Should return non-nil slice with 3 objects
	if ids == nil {
		t.Fatalf("Expected non-nil slice, got nil")
	}

	if len(ids) != 3 {
		t.Fatalf("Expected 3 object IDs, got %d", len(ids))
	}

	// Verify all object IDs are present
	idMap := make(map[string]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	for _, expectedID := range []string{objID1, objID2, objID3} {
		if !idMap[expectedID] {
			t.Fatalf("Expected object ID %s not found in list", expectedID)
		}
	}
}
