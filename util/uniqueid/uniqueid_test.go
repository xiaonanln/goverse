package uniqueid

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestUniqueId(t *testing.T) {
	// Test that UniqueId returns a non-empty string
	id := UniqueId()
	if id == "" {
		t.Fatal("UniqueId() returned empty string")
	}

	// Test that the ID is valid base64 URL encoding
	_, err := base64.URLEncoding.DecodeString(id)
	if err != nil {
		t.Fatalf("UniqueId() returned invalid base64 URL encoded string: %v", err)
	}

	// Test that multiple calls return different IDs
	id1 := UniqueId()
	id2 := UniqueId()
	if id1 == id2 {
		t.Fatal("UniqueId() returned same ID for consecutive calls")
	}

	// Test that IDs have consistent length (16 bytes = 20 chars in base64 URL)
	const expectedLen = 20 // base64 URL encoding of 16 bytes without padding
	for i := 0; i < 10; i++ {
		id := UniqueId()
		if len(id) != expectedLen {
			t.Fatalf("UniqueId() returned string %s of length %d, expected %d", id, len(id), expectedLen)
		}
	}
}

func TestUniqueIdUniqueness(t *testing.T) {
	// Generate many IDs and check for duplicates
	const numIds = 10000
	ids := make(map[string]bool, numIds)

	for i := 0; i < numIds; i++ {
		id := UniqueId()
		if ids[id] {
			t.Fatalf("Duplicate ID found: %s", id)
		}
		ids[id] = true
	}
}

func TestUniqueIdTiming(t *testing.T) {
	// Test that IDs generated close in time have different values
	start := time.Now()
	var ids []string

	// Generate IDs as fast as possible for 10ms
	for time.Since(start) < 10*time.Millisecond {
		ids = append(ids, UniqueId())
	}

	// Check for uniqueness
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("Duplicate ID found in rapid generation: %s", id)
		}
		seen[id] = true
	}

	t.Logf("Generated %d unique IDs in 10ms", len(ids))
}

func TestUniqueIdNoSlash(t *testing.T) {
	// Test that UniqueId never contains '/' character
	// This is important because '/' is commonly used as a path separator
	// and could cause issues in various contexts (URLs, file paths, etc.)
	const numTests = 10000
	for i := 0; i < numTests; i++ {
		id := UniqueId()
		if strings.Contains(id, "/") {
			t.Fatalf("UniqueId() returned string containing '/': %s", id)
		}
	}
}

func BenchmarkUniqueId(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UniqueId()
	}
}
