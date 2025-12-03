package consensusmanager

import (
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGetCurrentNodeForObject_InvalidShardFormat tests that invalid shard# formats fail
func TestGetCurrentNodeForObject_InvalidShardFormat(t *testing.T) {
	// Create a minimal consensus manager for validation testing
	shardMapping := &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Set up a valid shard mapping for reference
	for i := 0; i < testutil.TestNumShards; i++ {
		shardMapping.Shards[i] = ShardInfo{
			CurrentNode: "localhost:7001",
			TargetNode:  "localhost:7001",
		}
	}

	cm := &ConsensusManager{
		numShards: testutil.TestNumShards,
		state: &ClusterState{
			ShardMapping: shardMapping,
			Nodes:        map[string]bool{"localhost:7001": true},
		},
	}

	testCases := []struct {
		name        string
		objectID    string
		wantErr     bool
		errContains string
	}{
		// Valid formats should not error during validation
		{
			name:     "valid_fixed_shard",
			objectID: "shard#5/object-123",
			wantErr:  false,
		},
		{
			name:     "valid_fixed_node",
			objectID: "localhost:7001/object-123",
			wantErr:  false,
		},
		{
			name:     "valid_regular",
			objectID: "regular-object-123",
			wantErr:  false,
		},

		// Invalid shard# formats should fail
		{
			name:        "shard_no_number",
			objectID:    "shard#/object-123",
			wantErr:     true,
			errContains: "shard# must be followed by a number",
		},
		{
			name:        "shard_non_numeric",
			objectID:    "shard#abc/object-123",
			wantErr:     true,
			errContains: "shard# must be followed by a valid number",
		},
		{
			name:        "shard_negative",
			objectID:    "shard#-1/object-123",
			wantErr:     true,
			errContains: "out of range",
		},
		{
			name:        "shard_out_of_range",
			objectID:    "shard#999/object-123",
			wantErr:     true,
			errContains: "out of range",
		},
		{
			name:        "shard_prefix_no_slash",
			objectID:    "shard#5",
			wantErr:     true,
			errContains: "shard# prefix requires format shard#<number>/<objectID>",
		},
		{
			name:        "shard_prefix_only",
			objectID:    "shard#",
			wantErr:     true,
			errContains: "shard# prefix requires format shard#<number>/<objectID>",
		},

		// Edge cases
		{
			name:        "shard_with_spaces",
			objectID:    "shard# 5/object-123",
			wantErr:     true,
			errContains: "shard# must be followed by a valid number",
		},
		{
			name:        "shard_float",
			objectID:    "shard#5.5/object-123",
			wantErr:     true,
			errContains: "shard# must be followed by a valid number",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cm.GetCurrentNodeForObject(tc.objectID)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("GetCurrentNodeForObject(%s) expected error but got none", tc.objectID)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("GetCurrentNodeForObject(%s) error = %v, want error containing %q", tc.objectID, err, tc.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("GetCurrentNodeForObject(%s) unexpected error: %v", tc.objectID, err)
				}
			}
		})
	}
}

// TestGetCurrentNodeForObject_ValidShardFormats tests that valid shard# formats work correctly
func TestGetCurrentNodeForObject_ValidShardFormats(t *testing.T) {
	shardMapping := &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Set up shard mapping
	for i := 0; i < testutil.TestNumShards; i++ {
		shardMapping.Shards[i] = ShardInfo{
			CurrentNode: "localhost:7001",
			TargetNode:  "localhost:7001",
		}
	}

	cm := &ConsensusManager{
		numShards: testutil.TestNumShards,
		state: &ClusterState{
			ShardMapping: shardMapping,
			Nodes:        map[string]bool{"localhost:7001": true},
		},
	}

	testCases := []struct {
		name     string
		objectID string
		wantNode string
	}{
		{
			name:     "shard_0",
			objectID: "shard#0/object-123",
			wantNode: "localhost:7001",
		},
		{
			name:     "shard_max",
			objectID: "shard#63/object-123",
			wantNode: "localhost:7001",
		},
		{
			name:     "shard_middle",
			objectID: "shard#32/object-123",
			wantNode: "localhost:7001",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := cm.GetCurrentNodeForObject(tc.objectID)
			if err != nil {
				t.Fatalf("GetCurrentNodeForObject(%s) unexpected error: %v", tc.objectID, err)
			}
			if node != tc.wantNode {
				t.Fatalf("GetCurrentNodeForObject(%s) = %s, want %s", tc.objectID, node, tc.wantNode)
			}
		})
	}
}
