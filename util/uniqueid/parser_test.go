package uniqueid

import (
	"strings"
	"testing"
)

func TestParseObjectID_FixedShard(t *testing.T) {
	testCases := []struct {
		name           string
		objectID       string
		wantType       ObjectIDType
		wantShardID    int
		wantObjectPart string
		wantErr        bool
	}{
		{
			name:           "valid_fixed_shard",
			objectID:       "shard#5/object-123",
			wantType:       TypeFixedShard,
			wantShardID:    5,
			wantObjectPart: "object-123",
			wantErr:        false,
		},
		{
			name:           "shard_zero",
			objectID:       "shard#0/test",
			wantType:       TypeFixedShard,
			wantShardID:    0,
			wantObjectPart: "test",
			wantErr:        false,
		},
		{
			name:           "shard_large_number",
			objectID:       "shard#8191/user-abc",
			wantType:       TypeFixedShard,
			wantShardID:    8191,
			wantObjectPart: "user-abc",
			wantErr:        false,
		},
		{
			name:           "empty_object_part",
			objectID:       "shard#10/",
			wantType:       TypeFixedShard,
			wantShardID:    10,
			wantObjectPart: "",
			wantErr:        false,
		},
		{
			name:     "missing_shard_number",
			objectID: "shard#/object",
			wantErr:  true,
		},
		{
			name:     "non_numeric_shard",
			objectID: "shard#abc/object",
			wantErr:  true,
		},
		{
			name:           "negative_shard",
			objectID:       "shard#-1/object",
			wantType:       TypeFixedShard,
			wantShardID:    -1,
			wantObjectPart: "object",
			wantErr:        false, // ParseObjectID allows it, ValidateObjectID will reject it
		},
		{
			name:     "shard_with_space",
			objectID: "shard# 5/object",
			wantErr:  true,
		},
		{
			name:     "shard_float",
			objectID: "shard#5.5/object",
			wantErr:  true,
		},
		{
			name:     "shard_no_slash",
			objectID: "shard#5",
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseObjectID(tc.objectID)

			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseObjectID(%q) expected error but got none", tc.objectID)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseObjectID(%q) unexpected error: %v", tc.objectID, err)
			}

			if parsed.Type != tc.wantType {
				t.Errorf("ParseObjectID(%q).Type = %v, want %v", tc.objectID, parsed.Type, tc.wantType)
			}
			if parsed.ShardID != tc.wantShardID {
				t.Errorf("ParseObjectID(%q).ShardID = %d, want %d", tc.objectID, parsed.ShardID, tc.wantShardID)
			}
			if parsed.ObjectPart != tc.wantObjectPart {
				t.Errorf("ParseObjectID(%q).ObjectPart = %q, want %q", tc.objectID, parsed.ObjectPart, tc.wantObjectPart)
			}
			if parsed.FullID != tc.objectID {
				t.Errorf("ParseObjectID(%q).FullID = %q, want %q", tc.objectID, parsed.FullID, tc.objectID)
			}
		})
	}
}

func TestParseObjectID_FixedNode(t *testing.T) {
	testCases := []struct {
		name            string
		objectID        string
		wantType        ObjectIDType
		wantNodeAddress string
		wantObjectPart  string
		wantErr         bool
	}{
		{
			name:            "localhost_port",
			objectID:        "localhost:7001/object-123",
			wantType:        TypeFixedNode,
			wantNodeAddress: "localhost:7001",
			wantObjectPart:  "object-123",
			wantErr:         false,
		},
		{
			name:            "ip_address",
			objectID:        "192.168.1.100:8080/user-456",
			wantType:        TypeFixedNode,
			wantNodeAddress: "192.168.1.100:8080",
			wantObjectPart:  "user-456",
			wantErr:         false,
		},
		{
			name:            "complex_object_part",
			objectID:        "node1:9000/type-subtype-uuid-123",
			wantType:        TypeFixedNode,
			wantNodeAddress: "node1:9000",
			wantObjectPart:  "type-subtype-uuid-123",
			wantErr:         false,
		},
		{
			name:     "empty_node_address",
			objectID: "/object-123",
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseObjectID(tc.objectID)

			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseObjectID(%q) expected error but got none", tc.objectID)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseObjectID(%q) unexpected error: %v", tc.objectID, err)
			}

			if parsed.Type != tc.wantType {
				t.Errorf("ParseObjectID(%q).Type = %v, want %v", tc.objectID, parsed.Type, tc.wantType)
			}
			if parsed.NodeAddress != tc.wantNodeAddress {
				t.Errorf("ParseObjectID(%q).NodeAddress = %q, want %q", tc.objectID, parsed.NodeAddress, tc.wantNodeAddress)
			}
			if parsed.ObjectPart != tc.wantObjectPart {
				t.Errorf("ParseObjectID(%q).ObjectPart = %q, want %q", tc.objectID, parsed.ObjectPart, tc.wantObjectPart)
			}
		})
	}
}

func TestParseObjectID_Regular(t *testing.T) {
	testCases := []struct {
		name           string
		objectID       string
		wantObjectPart string
	}{
		{
			name:           "simple_id",
			objectID:       "object-123",
			wantObjectPart: "object-123",
		},
		{
			name:           "uuid_style",
			objectID:       "550e8400-e29b-41d4-a716-446655440000",
			wantObjectPart: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:           "with_hash_but_not_shard",
			objectID:       "object#123",
			wantObjectPart: "object#123",
		},
		{
			name:           "empty_string",
			objectID:       "",
			wantObjectPart: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseObjectID(tc.objectID)

			if tc.objectID == "" {
				if err == nil {
					t.Errorf("ParseObjectID(%q) expected error for empty string", tc.objectID)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseObjectID(%q) unexpected error: %v", tc.objectID, err)
			}

			if parsed.Type != TypeRegular {
				t.Errorf("ParseObjectID(%q).Type = %v, want TypeRegular", tc.objectID, parsed.Type)
			}
			if parsed.ObjectPart != tc.wantObjectPart {
				t.Errorf("ParseObjectID(%q).ObjectPart = %q, want %q", tc.objectID, parsed.ObjectPart, tc.wantObjectPart)
			}
		})
	}
}

func TestValidateObjectID(t *testing.T) {
	testCases := []struct {
		name        string
		objectID    string
		numShards   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid_shard_in_range",
			objectID:  "shard#5/object",
			numShards: 64,
			wantErr:   false,
		},
		{
			name:      "valid_shard_at_boundary",
			objectID:  "shard#63/object",
			numShards: 64,
			wantErr:   false,
		},
		{
			name:        "invalid_shard_out_of_range",
			objectID:    "shard#64/object",
			numShards:   64,
			wantErr:     true,
			errContains: "out of range",
		},
		{
			name:        "invalid_shard_negative",
			objectID:    "shard#-1/object",
			numShards:   64,
			wantErr:     true,
			errContains: "out of range",
		},
		{
			name:        "invalid_shard_way_out_of_range",
			objectID:    "shard#999/object",
			numShards:   64,
			wantErr:     true,
			errContains: "out of range",
		},
		{
			name:      "valid_fixed_node",
			objectID:  "localhost:7001/object",
			numShards: 64,
			wantErr:   false,
		},
		{
			name:      "valid_regular",
			objectID:  "regular-object",
			numShards: 64,
			wantErr:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ValidateObjectID(tc.objectID, tc.numShards)

			if tc.wantErr {
				if err == nil {
					t.Errorf("ValidateObjectID(%q, %d) expected error but got none", tc.objectID, tc.numShards)
					return
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("ValidateObjectID(%q, %d) error = %v, want error containing %q", tc.objectID, tc.numShards, err, tc.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateObjectID(%q, %d) unexpected error: %v", tc.objectID, tc.numShards, err)
				return
			}

			if parsed == nil {
				t.Errorf("ValidateObjectID(%q, %d) returned nil parsed object", tc.objectID, tc.numShards)
			}
		})
	}
}

func TestParsedObjectID_TypeCheckers(t *testing.T) {
	testCases := []struct {
		objectID     string
		isFixedShard bool
		isFixedNode  bool
		isRegular    bool
	}{
		{
			objectID:     "shard#5/object",
			isFixedShard: true,
			isFixedNode:  false,
			isRegular:    false,
		},
		{
			objectID:     "localhost:7001/object",
			isFixedShard: false,
			isFixedNode:  true,
			isRegular:    false,
		},
		{
			objectID:     "regular-object",
			isFixedShard: false,
			isFixedNode:  false,
			isRegular:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.objectID, func(t *testing.T) {
			parsed, err := ParseObjectID(tc.objectID)
			if err != nil {
				t.Fatalf("ParseObjectID(%q) unexpected error: %v", tc.objectID, err)
			}

			if parsed.IsFixedShardFormat() != tc.isFixedShard {
				t.Errorf("IsFixedShardFormat() = %v, want %v", parsed.IsFixedShardFormat(), tc.isFixedShard)
			}
			if parsed.IsFixedNodeFormat() != tc.isFixedNode {
				t.Errorf("IsFixedNodeFormat() = %v, want %v", parsed.IsFixedNodeFormat(), tc.isFixedNode)
			}
			if parsed.IsRegularFormat() != tc.isRegular {
				t.Errorf("IsRegularFormat() = %v, want %v", parsed.IsRegularFormat(), tc.isRegular)
			}
		})
	}
}
