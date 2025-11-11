package node

import (
	"testing"
)

// TestListObjectIDs tests the ListObjectIDs method
func TestListObjectIDs(t *testing.T) {
	t.Parallel()

	n := NewNode("localhost:50000")

	// Initially should have no objects
	ids := n.ListObjectIDs()
	if len(ids) != 0 {
		t.Errorf("Expected 0 object IDs initially, got %d", len(ids))
	}

	// The method should return an empty slice, not nil
	if ids == nil {
		t.Errorf("Expected empty slice, got nil")
	}
}

// TestListObjectIDs_EmptySlice tests that empty list returns empty slice not nil
func TestListObjectIDs_EmptySlice(t *testing.T) {
	t.Parallel()

	n := NewNode("localhost:50001")

	ids := n.ListObjectIDs()

	// Should return empty slice, not nil
	if ids == nil {
		t.Errorf("Expected empty slice, got nil")
	}

	if len(ids) != 0 {
		t.Errorf("Expected length 0, got %d", len(ids))
	}
}
