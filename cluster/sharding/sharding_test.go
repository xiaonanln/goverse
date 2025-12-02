package sharding

import (
	"fmt"
	"testing"
)

const testNumShards = 64 // Use smaller shard count for tests

func TestNumShards(t *testing.T) {
	// NumShards constant is still 8192 for production use
	if NumShards != 8192 {
		t.Fatalf("NumShards should be 8192, got %d", NumShards)
	}
}

func TestGetShardID_BasicFunctionality(t *testing.T) {
	// Test that GetShardID returns a valid shard ID
	objectID := "exampleObjectID"
	shardID := GetShardID(objectID, testNumShards)

	if shardID < 0 || shardID >= testNumShards {
		t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", objectID, shardID, testNumShards)
	}
}

func TestGetShardID_Consistency(t *testing.T) {
	// Test that the same object ID always returns the same shard ID
	objectID := "testObject123"
	shardID1 := GetShardID(objectID, testNumShards)
	shardID2 := GetShardID(objectID, testNumShards)

	if shardID1 != shardID2 {
		t.Fatalf("GetShardID(%s) should be consistent: got %d and %d", objectID, shardID1, shardID2)
	}
}

func TestGetShardID_DifferentInputs(t *testing.T) {
	// Test that different object IDs can produce different shard IDs
	id1 := "object1"
	id2 := "object2"

	shardID1 := GetShardID(id1, testNumShards)
	shardID2 := GetShardID(id2, testNumShards)

	// They should be different (though theoretically they could collide)
	// We're just checking that they're both valid
	if shardID1 < 0 || shardID1 >= testNumShards {
		t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", id1, shardID1, testNumShards)
	}
	if shardID2 < 0 || shardID2 >= testNumShards {
		t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", id2, shardID2, testNumShards)
	}
}

func TestGetShardID_EmptyString(t *testing.T) {
	// Test with empty string
	shardID := GetShardID("", testNumShards)

	if shardID < 0 || shardID >= testNumShards {
		t.Fatalf("GetShardID(\"\") = %d, want value in range [0, %d)", shardID, testNumShards)
	}
}

func TestGetShardID_SpecialCharacters(t *testing.T) {
	// Test with special characters
	testCases := []string{
		"object-with-dashes",
		"object_with_underscores",
		"object.with.dots",
		"object/with/slashes",
		"object@with@ats",
		"object#with#hashes",
		"unicode-测试-test",
	}

	for _, objectID := range testCases {
		shardID := GetShardID(objectID, testNumShards)
		if shardID < 0 || shardID >= testNumShards {
			t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", objectID, shardID, testNumShards)
		}
	}
}

func TestGetShardID_Distribution(t *testing.T) {
	// Test that shard IDs are distributed across all possible values
	// Generate a reasonable number of test cases
	shardCounts := make(map[int]int)
	numTests := 10000

	for i := 0; i < numTests; i++ {
		objectID := fmt.Sprintf("object-%d", i)
		shardID := GetShardID(objectID, testNumShards)

		if shardID < 0 || shardID >= testNumShards {
			t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", objectID, shardID, testNumShards)
		}

		shardCounts[shardID]++
	}

	// Verify we have at least some distribution (not all objects in one shard)
	if len(shardCounts) < 2 {
		t.Fatalf("Expected distribution across multiple shards, got only %d shard(s)", len(shardCounts))
	}

	// With 10000 objects and 64 shards, we expect most shards to have multiple objects
	// Just verify that no single shard has all objects
	for shardID, count := range shardCounts {
		if count == numTests {
			t.Fatalf("All objects mapped to shard %d, expected better distribution", shardID)
		}
	}
}

func TestGetShardID_KnownValues(t *testing.T) {
	// Test with some known values to ensure consistency across runs
	// These expected values are based on FNV-1a hash
	testCases := []struct {
		objectID string
		// We're not hardcoding expected shard IDs as they might change
		// Just verify consistency
	}{
		{"exampleObjectID"},
		{"user-12345"},
		{"session-abc-def"},
		{""},
	}

	for _, tc := range testCases {
		// Call multiple times to ensure consistency
		results := make(map[int]bool)
		for i := 0; i < 10; i++ {
			shardID := GetShardID(tc.objectID, testNumShards)
			results[shardID] = true
		}

		// Should only have one unique result
		if len(results) != 1 {
			t.Fatalf("GetShardID(%s) produced multiple different results: %v", tc.objectID, results)
		}
	}
}

func BenchmarkGetShardID(b *testing.B) {
	objectID := "benchmark-test-object-12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetShardID(objectID, testNumShards)
	}
}

func BenchmarkGetShardID_VaryingLength(b *testing.B) {
	testCases := []string{
		"short",
		"medium-length-object-id",
		"very-long-object-id-with-many-characters-to-test-performance-with-longer-strings",
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				GetShardID(tc, testNumShards)
			}
		})
	}
}

func TestGetShardID_FixedShard_BasicFunctionality(t *testing.T) {
	// Test that fixed shard object IDs return the correct shard
	testCases := []struct {
		objectID        string
		expectedShard   int
		expectedInRange bool
	}{
		{"shard#0/object-123", 0, true},
		{"shard#5/test-object", 5, true},
		{"shard#63/another-obj", 63, true},
		{"shard#10/user-12345", 10, true},
		{"shard#0/", 0, true},
	}

	for _, tc := range testCases {
		t.Run(tc.objectID, func(t *testing.T) {
			shardID := GetShardID(tc.objectID, testNumShards)

			if shardID != tc.expectedShard {
				t.Fatalf("GetShardID(%s) = %d, want %d", tc.objectID, shardID, tc.expectedShard)
			}

			if shardID < 0 || shardID >= testNumShards {
				t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", tc.objectID, shardID, testNumShards)
			}
		})
	}
}

func TestGetShardID_FixedShard_Consistency(t *testing.T) {
	// Test that the same fixed shard object ID always returns the same shard
	objectID := "shard#42/consistent-object"
	shardID1 := GetShardID(objectID, testNumShards)
	shardID2 := GetShardID(objectID, testNumShards)

	if shardID1 != shardID2 {
		t.Fatalf("GetShardID(%s) should be consistent: got %d and %d", objectID, shardID1, shardID2)
	}

	if shardID1 != 42 {
		t.Fatalf("GetShardID(%s) = %d, want 42", objectID, shardID1)
	}
}

func TestGetShardID_FixedShard_InvalidFormats(t *testing.T) {
	// Test that invalid fixed shard formats fall back to hash-based sharding
	testCases := []struct {
		name     string
		objectID string
	}{
		{"missing shard number", "shard#/object"},
		{"no slash", "shard#5"},
		{"negative shard", "shard#-1/object"},
		{"out of range shard", "shard#9999/object"},
		{"non-numeric shard", "shard#abc/object"},
		{"wrong prefix", "shared#5/object"},
		{"shard with spaces", "shard# 5/object"},
		{"multiple slashes", "shard#5/sub/object"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shardID := GetShardID(tc.objectID, testNumShards)

			// Should return a valid shard ID (falls back to hash)
			if shardID < 0 || shardID >= testNumShards {
				t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", tc.objectID, shardID, testNumShards)
			}
		})
	}
}

func TestGetShardID_FixedShard_EdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		objectID      string
		expectedShard int
	}{
		{"minimum shard", "shard#0/obj", 0},
		{"maximum shard", "shard#63/obj", 63},
		{"single digit", "shard#5/x", 5},
		{"double digit", "shard#42/x", 42},
		{"empty object part", "shard#10/", 10},
		{"complex object part", "shard#20/user-type-subtype-uuid-123", 20},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shardID := GetShardID(tc.objectID, testNumShards)

			if shardID != tc.expectedShard {
				t.Fatalf("GetShardID(%s) = %d, want %d", tc.objectID, shardID, tc.expectedShard)
			}
		})
	}
}

func TestGetShardID_FixedShard_VsHashBased(t *testing.T) {
	// Test that fixed shard IDs don't collide with hash-based IDs
	objectIDPart := "test-object-123"

	// Get hash-based shard for the object ID part
	hashBasedShard := GetShardID(objectIDPart, testNumShards)

	// Create a fixed shard object ID for a different shard
	differentShard := (hashBasedShard + 1) % testNumShards
	fixedShardID := fmt.Sprintf("shard#%d/%s", differentShard, objectIDPart)

	// Get shard for fixed shard ID
	fixedShard := GetShardID(fixedShardID, testNumShards)

	// They should be different
	if hashBasedShard == fixedShard {
		t.Fatalf("Hash-based and fixed shard should be different: both got %d", hashBasedShard)
	}

	// Fixed shard should match the specified shard
	if fixedShard != differentShard {
		t.Fatalf("Fixed shard = %d, want %d", fixedShard, differentShard)
	}
}

func TestGetShardID_MixedFormats(t *testing.T) {
	// Test that mixing fixed-node and fixed-shard formats works correctly
	testCases := []struct {
		name     string
		objectID string
	}{
		// Fixed shard formats (should use shard extraction)
		{"fixed shard only", "shard#10/object"},

		// Regular formats (should use hash)
		{"regular ID", "regular-object-123"},
		{"fixed node format", "localhost:7000/object-123"},
		{"path-like", "path/to/object"},

		// Ambiguous or invalid fixed shard formats (should fall back to hash)
		{"shard prefix but no number", "shard#/object"},
		{"shard prefix but invalid", "shard#abc/object"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shardID := GetShardID(tc.objectID, testNumShards)

			// All should return valid shard IDs
			if shardID < 0 || shardID >= testNumShards {
				t.Fatalf("GetShardID(%s) = %d, want value in range [0, %d)", tc.objectID, shardID, testNumShards)
			}
		})
	}
}

func BenchmarkGetShardID_FixedShard(b *testing.B) {
	objectID := "shard#42/benchmark-test-object-12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetShardID(objectID, testNumShards)
	}
}
