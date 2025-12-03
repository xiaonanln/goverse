package uniqueid

import (
	"fmt"
	"strconv"
	"strings"
)

// ObjectIDType represents the type of object ID format
type ObjectIDType int

const (
	// TypeRegular indicates a regular object ID (hash-based routing)
	TypeRegular ObjectIDType = iota
	// TypeFixedShard indicates a fixed-shard format: "shard#<shardID>/<objectID>"
	TypeFixedShard
	// TypeFixedNode indicates a fixed-node format: "<nodeAddress>/<objectID>"
	TypeFixedNode
)

// ParsedObjectID represents a parsed object ID with its components
type ParsedObjectID struct {
	// Type is the format type of the object ID
	Type ObjectIDType
	// FullID is the complete object ID string
	FullID string
	// ShardID is the shard number for TypeFixedShard (0 for other types)
	ShardID int
	// NodeAddress is the node address for TypeFixedNode (empty for other types)
	NodeAddress string
	// ObjectPart is the unique object identifier part after the prefix
	ObjectPart string
}

// ParseObjectID parses an object ID string and returns its components.
// It does not validate shard ID ranges - use ValidateObjectID for validation.
//
// Supported formats:
//  1. Fixed-shard: "shard#<shardID>/<objectID>" - routes to specific shard
//  2. Fixed-node: "<nodeAddress>/<objectID>" - routes to specific node
//  3. Regular: any other ID - uses hash-based routing
//
// Example:
//
//	parsed, err := uniqueid.ParseObjectID("shard#5/object-123")
//	// parsed.Type == TypeFixedShard, parsed.ShardID == 5, parsed.ObjectPart == "object-123"
func ParseObjectID(objectID string) (*ParsedObjectID, error) {
	if objectID == "" {
		return nil, fmt.Errorf("object ID cannot be empty")
	}

	parsed := &ParsedObjectID{
		FullID: objectID,
	}

	// Check for "/" separator
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid object ID format: %s (malformed separator)", objectID)
		}

		prefix := parts[0]
		parsed.ObjectPart = parts[1]

		// Check if it's a fixed-shard format
		if strings.HasPrefix(prefix, "shard#") {
			parsed.Type = TypeFixedShard

			const shardPrefix = "shard#"
			if len(prefix) <= len(shardPrefix) {
				return nil, fmt.Errorf("invalid object ID format: %s (shard# must be followed by a number)", objectID)
			}

			// Extract and parse the shard ID
			shardIDStr := prefix[len(shardPrefix):]
			shardID, err := strconv.Atoi(shardIDStr)
			if err != nil {
				return nil, fmt.Errorf("invalid object ID format: %s (shard# must be followed by a valid number, got %q)", objectID, shardIDStr)
			}

			// ParseObjectID doesn't validate range, but we do reject negative numbers here
			// since they're never valid in any context
			if shardID < 0 {
				return nil, fmt.Errorf("invalid object ID format: %s (shard ID cannot be negative)", objectID)
			}

			parsed.ShardID = shardID
			return parsed, nil
		}

		// Fixed-node format
		if prefix == "" {
			return nil, fmt.Errorf("invalid object ID format: %s (node address cannot be empty)", objectID)
		}
		parsed.Type = TypeFixedNode
		parsed.NodeAddress = prefix
		return parsed, nil
	}

	// Check for invalid shard# prefix without slash
	if strings.HasPrefix(objectID, "shard#") {
		return nil, fmt.Errorf("invalid object ID format: %s (shard# prefix requires format shard#<number>/<objectID>)", objectID)
	}

	// Regular format - no special routing
	parsed.Type = TypeRegular
	parsed.ObjectPart = objectID
	return parsed, nil
}

// ValidateObjectID validates an object ID format and shard ID range.
// Returns the parsed object ID if valid, or an error if invalid.
//
// Example:
//
//	parsed, err := uniqueid.ValidateObjectID("shard#5/object-123", 64)
//	if err != nil {
//	    // Handle invalid format or out-of-range shard ID
//	}
func ValidateObjectID(objectID string, numShards int) (*ParsedObjectID, error) {
	parsed, err := ParseObjectID(objectID)
	if err != nil {
		return nil, err
	}

	// Validate shard ID is in range for fixed-shard format
	if parsed.Type == TypeFixedShard {
		if parsed.ShardID < 0 || parsed.ShardID >= numShards {
			return nil, fmt.Errorf("invalid object ID format: %s (shard ID %d out of range [0, %d))", objectID, parsed.ShardID, numShards)
		}
	}

	return parsed, nil
}

// IsFixedShardFormat returns true if the object ID uses fixed-shard format
func (p *ParsedObjectID) IsFixedShardFormat() bool {
	return p.Type == TypeFixedShard
}

// IsFixedNodeFormat returns true if the object ID uses fixed-node format
func (p *ParsedObjectID) IsFixedNodeFormat() bool {
	return p.Type == TypeFixedNode
}

// IsRegularFormat returns true if the object ID uses regular format
func (p *ParsedObjectID) IsRegularFormat() bool {
	return p.Type == TypeRegular
}
