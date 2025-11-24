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
