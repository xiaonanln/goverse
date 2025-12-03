package uniqueid

import (
	"encoding/base64"
	"fmt"
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

func TestUniqueIdNoSpecialChars(t *testing.T) {
	// Test that UniqueId never contains '/' or '#' characters.
	// These characters are reserved for object ID routing:
	// - '/' is used in fixed-node format: "nodeAddr/uuid"
	// - '#' is used in fixed-shard format: "shard#N/uuid"
	// base64.URLEncoding guarantees this by only producing [A-Za-z0-9_-]
	const numTests = 10000
	for i := 0; i < numTests; i++ {
		id := UniqueId()
		if strings.ContainsAny(id, "/#") {
			t.Fatalf("UniqueId() returned string containing '/' or '#': %s", id)
		}
	}
}

func BenchmarkUniqueId(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UniqueId()
	}
}

func TestCreateObjectID(t *testing.T) {
	// Test that CreateObjectID returns a non-empty string
	id := CreateObjectID()
	if id == "" {
		t.Fatal("CreateObjectID() returned empty string")
	}

	// Test that the ID is valid base64 URL encoding
	_, err := base64.URLEncoding.DecodeString(id)
	if err != nil {
		t.Fatalf("CreateObjectID() returned invalid base64 URL encoded string: %v", err)
	}

	// Test that multiple calls return different IDs
	id1 := CreateObjectID()
	id2 := CreateObjectID()
	if id1 == id2 {
		t.Fatal("CreateObjectID() returned same ID for consecutive calls")
	}

	// Test that IDs don't contain '/' or '#' (important for routing)
	if strings.ContainsAny(id, "/#") {
		t.Fatalf("CreateObjectID() returned string containing '/' or '#': %s", id)
	}
}

func TestCreateObjectIDOnShard(t *testing.T) {
	const numShards = 8192

	// Test that CreateObjectIDOnShard returns correct format
	shardID := 5
	id := CreateObjectIDOnShard(shardID, numShards)

	// Check format: should be "shard#5/..."
	expectedPrefix := "shard#5/"
	if !strings.HasPrefix(id, expectedPrefix) {
		t.Fatalf("CreateObjectIDOnShard(%d, %d) = %s, want prefix %s", shardID, numShards, id, expectedPrefix)
	}

	// Check that the part after the slash is a valid unique ID
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("CreateObjectIDOnShard(%d, %d) = %s, expected format 'shard#<id>/<uniqueid>'", shardID, numShards, id)
	}

	uniquePart := parts[1]
	if uniquePart == "" {
		t.Fatal("CreateObjectIDOnShard() generated empty unique ID part")
	}

	// Validate unique part is base64 URL encoding
	_, err := base64.URLEncoding.DecodeString(uniquePart)
	if err != nil {
		t.Fatalf("CreateObjectIDOnShard() unique part is not valid base64 URL: %v", err)
	}

	// Test multiple calls generate different unique IDs
	id1 := CreateObjectIDOnShard(shardID, numShards)
	id2 := CreateObjectIDOnShard(shardID, numShards)
	if id1 == id2 {
		t.Fatalf("CreateObjectIDOnShard(%d, %d) returned same ID twice: %s", shardID, numShards, id1)
	}

	// Test different shard IDs
	id3 := CreateObjectIDOnShard(10, numShards)
	if !strings.HasPrefix(id3, "shard#10/") {
		t.Fatalf("CreateObjectIDOnShard(10, %d) = %s, want prefix 'shard#10/'", numShards, id3)
	}
}

func TestCreateObjectIDOnShard_EdgeCases(t *testing.T) {
	const numShards = 8192

	testCases := []struct {
		name        string
		shardID     int
		numShards   int
		shouldPanic bool
	}{
		{"minimum valid shard", 0, numShards, false},
		{"maximum valid shard", numShards - 1, numShards, false},
		{"mid-range shard", 4096, numShards, false},
		{"negative shard", -1, numShards, true},
		{"shard equals numShards", numShards, numShards, true},
		{"shard exceeds numShards", numShards + 1, numShards, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("CreateObjectIDOnShard(%d, %d) should panic, but didn't", tc.shardID, tc.numShards)
					}
				}()
				CreateObjectIDOnShard(tc.shardID, tc.numShards)
			} else {
				id := CreateObjectIDOnShard(tc.shardID, tc.numShards)
				expectedPrefix := fmt.Sprintf("shard#%d/", tc.shardID)
				if !strings.HasPrefix(id, expectedPrefix) {
					t.Errorf("CreateObjectIDOnShard(%d, %d) = %s, want prefix %s", tc.shardID, tc.numShards, id, expectedPrefix)
				}
			}
		})
	}
}

func TestCreateObjectIDOnNode(t *testing.T) {
	// Test that CreateObjectIDOnNode returns correct format
	nodeAddr := "localhost:7001"
	id := CreateObjectIDOnNode(nodeAddr)

	// Check format: should be "localhost:7001/..."
	expectedPrefix := "localhost:7001/"
	if !strings.HasPrefix(id, expectedPrefix) {
		t.Fatalf("CreateObjectIDOnNode(%s) = %s, want prefix %s", nodeAddr, id, expectedPrefix)
	}

	// Check that the part after the slash is a valid unique ID
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("CreateObjectIDOnNode(%s) = %s, expected format '<nodeAddr>/<uniqueid>'", nodeAddr, id)
	}

	uniquePart := parts[1]
	if uniquePart == "" {
		t.Fatal("CreateObjectIDOnNode() generated empty unique ID part")
	}

	// Validate unique part is base64 URL encoding
	_, err := base64.URLEncoding.DecodeString(uniquePart)
	if err != nil {
		t.Fatalf("CreateObjectIDOnNode() unique part is not valid base64 URL: %v", err)
	}

	// Test multiple calls generate different unique IDs
	id1 := CreateObjectIDOnNode(nodeAddr)
	id2 := CreateObjectIDOnNode(nodeAddr)
	if id1 == id2 {
		t.Fatalf("CreateObjectIDOnNode(%s) returned same ID twice: %s", nodeAddr, id1)
	}

	// Test different node addresses
	id3 := CreateObjectIDOnNode("192.168.1.100:8080")
	if !strings.HasPrefix(id3, "192.168.1.100:8080/") {
		t.Fatalf("CreateObjectIDOnNode(192.168.1.100:8080) = %s, want prefix '192.168.1.100:8080/'", id3)
	}
}

func TestCreateObjectIDOnNode_InvalidInput(t *testing.T) {
	// Test that empty node address panics
	defer func() {
		if r := recover(); r == nil {
			t.Error("CreateObjectIDOnNode(\"\") should panic, but didn't")
		}
	}()
	CreateObjectIDOnNode("")
}

func TestCreateObjectIDOnNode_VariousFormats(t *testing.T) {
	testCases := []string{
		"localhost:7001",
		"192.168.1.100:8080",
		"node1.example.com:9000",
		"[::1]:7001",
		"my-node:12345",
	}

	for _, nodeAddr := range testCases {
		t.Run(nodeAddr, func(t *testing.T) {
			id := CreateObjectIDOnNode(nodeAddr)
			expectedPrefix := nodeAddr + "/"
			if !strings.HasPrefix(id, expectedPrefix) {
				t.Errorf("CreateObjectIDOnNode(%s) = %s, want prefix %s", nodeAddr, id, expectedPrefix)
			}

			// Verify uniqueness
			id2 := CreateObjectIDOnNode(nodeAddr)
			if id == id2 {
				t.Errorf("CreateObjectIDOnNode(%s) returned duplicate IDs", nodeAddr)
			}
		})
	}
}

func BenchmarkCreateObjectID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CreateObjectID()
	}
}

func BenchmarkCreateObjectIDOnShard(b *testing.B) {
	const numShards = 8192
	for i := 0; i < b.N; i++ {
		CreateObjectIDOnShard(42, numShards)
	}
}

func BenchmarkCreateObjectIDOnNode(b *testing.B) {
	nodeAddr := "localhost:7001"
	for i := 0; i < b.N; i++ {
		CreateObjectIDOnNode(nodeAddr)
	}
}
