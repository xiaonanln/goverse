package sharding

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	sharding_pb "github.com/xiaonanln/goverse/cluster/sharding/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/proto"
)

// ShardInfo represents detailed information about a single shard
type ShardInfo struct {
	// ShardID is the ID of this shard (0-8191)
	ShardID int
	// TargetNode is the node where this shard should ultimately reside (leader-specified)
	TargetNode string
	// CurrentNode is the node where this shard is currently located
	CurrentNode string
	// State is the current state of this shard
	State ShardState
}

// ShardState represents the current state of a shard
type ShardState int

const (
	// ShardStateAvailable means shard is ready and available for operations on the current node
	ShardStateAvailable ShardState = 0
	// ShardStateMigrating means shard is in the process of being migrated
	ShardStateMigrating ShardState = 1
	// ShardStateOffline means shard is offline and not available on any node
	ShardStateOffline ShardState = 2
)

// String returns a string representation of the shard state
func (s ShardState) String() string {
	switch s {
	case ShardStateAvailable:
		return "AVAILABLE"
	case ShardStateMigrating:
		return "MIGRATING"
	case ShardStateOffline:
		return "OFFLINE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

// ShardMapping represents the mapping of shards to nodes with enhanced state tracking
type ShardMapping struct {
	// Shards is a map from shard ID to shard information
	Shards map[int]*ShardInfo
	// Nodes is the sorted list of nodes in the cluster
	Nodes []string
	// Version for optimistic concurrency control
	Version int64
}

// ShardMapper manages the shard-to-node mapping
type ShardMapper struct {
	etcdManager *etcdmanager.EtcdManager
	logger      *logger.Logger
	mu          sync.RWMutex
	mapping     *ShardMapping
}

// NewShardMapper creates a new shard mapper
func NewShardMapper(etcdManager *etcdmanager.EtcdManager) *ShardMapper {
	return &ShardMapper{
		etcdManager: etcdManager,
		logger:      logger.NewLogger("ShardMapper"),
	}
}

// getShardMappingKey returns the shard mapping key based on the etcd manager's prefix
func (sm *ShardMapper) getShardMappingKey() string {
	if sm.etcdManager != nil {
		return sm.etcdManager.GetPrefix() + "/shardmapping"
	}
	// Fallback for tests where etcdManager is nil
	return etcdmanager.DefaultPrefix + "/shardmapping"
}

// CreateShardMapping creates a new shard mapping by distributing all shards across available nodes
// This should only be called by the leader node
func (sm *ShardMapper) CreateShardMapping(ctx context.Context, nodes []string) (*ShardMapping, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("cannot create shard mapping with no nodes")
	}

	sm.logger.Infof("Creating shard mapping for %d nodes", len(nodes))

	// Sort nodes for deterministic assignment
	sortedNodes := make([]string, len(nodes))
	copy(sortedNodes, nodes)
	sort.Strings(sortedNodes)

	// Create the mapping by distributing shards evenly across nodes
	mapping := &ShardMapping{
		Shards:  make(map[int]*ShardInfo, NumShards),
		Nodes:   sortedNodes,
		Version: 1,
	}

	for shardID := 0; shardID < NumShards; shardID++ {
		nodeIdx := shardID % len(sortedNodes)
		targetNode := sortedNodes[nodeIdx]
		
		// Initially, all shards are available on their target nodes
		mapping.Shards[shardID] = &ShardInfo{
			ShardID:     shardID,
			TargetNode:  targetNode,
			CurrentNode: targetNode,
			State:       ShardStateAvailable,
		}
	}

	sm.logger.Infof("Created shard mapping with %d shards distributed across %d nodes", NumShards, len(sortedNodes))
	return mapping, nil
}

// StoreShardMapping stores the shard mapping in etcd using protobuf serialization
func (sm *ShardMapper) StoreShardMapping(ctx context.Context, mapping *ShardMapping) error {
	if sm.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}

	// Convert to protobuf message
	pbMapping := &sharding_pb.ShardMappingProto{
		Shards:  make(map[int32]*sharding_pb.ShardInfo, NumShards),
		Nodes:   mapping.Nodes,
		Version: mapping.Version,
	}

	for shardID, shardInfo := range mapping.Shards {
		pbMapping.Shards[int32(shardID)] = &sharding_pb.ShardInfo{
			ShardId:     int32(shardInfo.ShardID),
			TargetNode:  shardInfo.TargetNode,
			CurrentNode: shardInfo.CurrentNode,
			State:       sharding_pb.ShardState(shardInfo.State),
		}
	}

	// Serialize to protobuf bytes
	data, err := proto.Marshal(pbMapping)
	if err != nil {
		return fmt.Errorf("failed to marshal shard mapping: %w", err)
	}

	// Store in etcd
	err = sm.etcdManager.Put(ctx, sm.getShardMappingKey(), string(data))
	if err != nil {
		return fmt.Errorf("failed to store shard mapping in etcd: %w", err)
	}

	sm.logger.Infof("Stored shard mapping (version %d) in etcd", mapping.Version)

	// Update local cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	return nil
}

// GetShardMapping retrieves the shard mapping from etcd using protobuf deserialization
func (sm *ShardMapper) GetShardMapping(ctx context.Context) (*ShardMapping, error) {
	// Try to get from local cache first
	sm.mu.RLock()
	if sm.mapping != nil {
		cached := sm.mapping
		sm.mu.RUnlock()
		return cached, nil
	}
	sm.mu.RUnlock()

	// If no cache, we need etcd manager
	if sm.etcdManager == nil {
		return nil, fmt.Errorf("etcd manager not set")
	}

	// Retrieve from etcd
	data, err := sm.etcdManager.Get(ctx, sm.getShardMappingKey())
	if err != nil {
		return nil, fmt.Errorf("failed to get shard mapping from etcd: %w", err)
	}

	// Deserialize the protobuf message
	var pbMapping sharding_pb.ShardMappingProto
	err = proto.Unmarshal([]byte(data), &pbMapping)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal shard mapping: %w", err)
	}

	// Convert from protobuf to our internal structure
	mapping := &ShardMapping{
		Shards:  make(map[int]*ShardInfo, len(pbMapping.Shards)),
		Nodes:   pbMapping.Nodes,
		Version: pbMapping.Version,
	}

	for shardID, pbShardInfo := range pbMapping.Shards {
		mapping.Shards[int(shardID)] = &ShardInfo{
			ShardID:     int(pbShardInfo.ShardId),
			TargetNode:  pbShardInfo.TargetNode,
			CurrentNode: pbShardInfo.CurrentNode,
			State:       ShardState(pbShardInfo.State),
		}
	}

	sm.logger.Infof("Retrieved shard mapping (version %d) from etcd", mapping.Version)

	// Update local cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	return mapping, nil
}

// GetNodeForShard returns the node address that owns the given shard
// This returns the current node where the shard is located
func (sm *ShardMapper) GetNodeForShard(ctx context.Context, shardID int) (string, error) {
	if shardID < 0 || shardID >= NumShards {
		return "", fmt.Errorf("invalid shard ID: %d (must be in range [0, %d))", shardID, NumShards)
	}

	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get shard mapping: %w", err)
	}

	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	// Return the current node (where the shard actually is)
	// For available shards, this will be the same as target node
	// For migrating shards, this might be different from target node
	return shardInfo.CurrentNode, nil
}

// GetNodeForObject returns the node address that should handle the given object ID
// If the object ID contains a "/" separator (e.g., "localhost:7001/object-123"),
// the part before the first "/" is treated as a fixed node address and returned directly.
// This allows objects to be pinned to specific nodes, similar to client IDs.
// Otherwise, the object is assigned to a node based on consistent hashing.
func (sm *ShardMapper) GetNodeForObject(ctx context.Context, objectID string) (string, error) {
	// Check if object ID specifies a fixed node address
	// Format: nodeAddress/actualObjectID (e.g., "localhost:7001/object-123")
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			// Fixed node address specified (first part before /)
			nodeAddr := parts[0]
			sm.logger.Debugf("Object %s pinned to node %s", objectID, nodeAddr)
			return nodeAddr, nil
		}
	}

	// Standard shard-based assignment
	shardID := GetShardID(objectID)
	return sm.GetNodeForShard(ctx, shardID)
}

// UpdateShardMapping updates the shard mapping when nodes are added or removed
// This should only be called by the leader node
// Returns the current mapping unchanged if no changes are needed
func (sm *ShardMapper) UpdateShardMapping(ctx context.Context, nodes []string) (*ShardMapping, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("cannot update shard mapping with no nodes")
	}

	// Get current mapping
	currentMapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		// If no mapping exists, create a new one
		sm.logger.Infof("No existing shard mapping found, creating new one")
		return sm.CreateShardMapping(ctx, nodes)
	}

	sm.logger.Infof("Updating shard mapping for %d nodes (current version: %d)", len(nodes), currentMapping.Version)

	// Sort nodes for deterministic assignment
	sortedNodes := make([]string, len(nodes))
	copy(sortedNodes, nodes)
	sort.Strings(sortedNodes)

	// Create node set for quick lookup
	nodeSet := make(map[string]bool)
	for _, node := range sortedNodes {
		nodeSet[node] = true
	}

	// Check if any changes are needed
	needsUpdate := false
	newShards := make(map[int]*ShardInfo, NumShards)

	for shardID := 0; shardID < NumShards; shardID++ {
		currentShardInfo, exists := currentMapping.Shards[shardID]
		
		if exists && nodeSet[currentShardInfo.TargetNode] {
			// Target node still exists, keep the shard assignment
			// Copy the shard info (keep current state and location)
			newShards[shardID] = &ShardInfo{
				ShardID:     currentShardInfo.ShardID,
				TargetNode:  currentShardInfo.TargetNode,
				CurrentNode: currentShardInfo.CurrentNode,
				State:       currentShardInfo.State,
			}
		} else {
			// Target node is gone, need to reassign
			nodeIdx := shardID % len(sortedNodes)
			newTargetNode := sortedNodes[nodeIdx]
			
			// Mark shard as needing migration if it was available somewhere
			var newState ShardState
			var currentNode string
			
			if exists && nodeSet[currentShardInfo.CurrentNode] {
				// Current node still exists, mark for migration
				currentNode = currentShardInfo.CurrentNode
				newState = ShardStateMigrating
			} else {
				// Current node is gone, mark as offline
				currentNode = ""
				newState = ShardStateOffline
			}
			
			newShards[shardID] = &ShardInfo{
				ShardID:     shardID,
				TargetNode:  newTargetNode,
				CurrentNode: currentNode,
				State:       newState,
			}
			needsUpdate = true
		}
	}

	// Check if node list changed (even if no shards moved)
	if !nodesEqual(currentMapping.Nodes, sortedNodes) {
		needsUpdate = true
	}

	// If no changes needed, return the current mapping
	if !needsUpdate {
		sm.logger.Infof("No changes needed to shard mapping, keeping version %d", currentMapping.Version)
		return currentMapping, nil
	}

	// Create new mapping with incremented version
	newMapping := &ShardMapping{
		Shards:  newShards,
		Nodes:   sortedNodes,
		Version: currentMapping.Version + 1,
	}

	sm.logger.Infof("Updated shard mapping to version %d", newMapping.Version)
	return newMapping, nil
}

// nodesEqual checks if two sorted node lists are equal
func nodesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// UpdateShardState updates the state of a specific shard
// This is used to coordinate shard migration between nodes
// If etcd manager is available, updates both etcd and cache, otherwise just updates cache
func (sm *ShardMapper) UpdateShardState(ctx context.Context, shardID int, newState ShardState, newCurrentNode string) error {
	if shardID < 0 || shardID >= NumShards {
		return fmt.Errorf("invalid shard ID: %d", shardID)
	}

	// Get current mapping
	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return fmt.Errorf("failed to get shard mapping: %w", err)
	}

	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return fmt.Errorf("shard %d not found in mapping", shardID)
	}

	// Update shard info
	shardInfo.State = newState
	shardInfo.CurrentNode = newCurrentNode

	// Store updated mapping (if etcd is available, otherwise just update cache)
	if sm.etcdManager != nil {
		err = sm.StoreShardMapping(ctx, mapping)
		if err != nil {
			return fmt.Errorf("failed to store updated shard mapping: %w", err)
		}
	} else {
		// Just update cache for testing
		sm.mu.Lock()
		sm.mapping = mapping
		sm.mu.Unlock()
	}

	sm.logger.Infof("Updated shard %d: state=%s, currentNode=%s", shardID, newState.String(), newCurrentNode)
	return nil
}

// MarkShardForMigration marks a shard as migrating from its current node to the target node
// This should be called when beginning a shard migration
// If etcd manager is available, updates both etcd and cache, otherwise just updates cache
func (sm *ShardMapper) MarkShardForMigration(ctx context.Context, shardID int) error {
	if shardID < 0 || shardID >= NumShards {
		return fmt.Errorf("invalid shard ID: %d", shardID)
	}

	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return fmt.Errorf("failed to get shard mapping: %w", err)
	}

	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return fmt.Errorf("shard %d not found in mapping", shardID)
	}

	// Only mark for migration if not already migrating
	if shardInfo.State == ShardStateMigrating {
		sm.logger.Debugf("Shard %d already marked as migrating", shardID)
		return nil
	}

	// Mark as migrating
	shardInfo.State = ShardStateMigrating
	
	// Store updated mapping (if etcd is available, otherwise just update cache)
	if sm.etcdManager != nil {
		err = sm.StoreShardMapping(ctx, mapping)
		if err != nil {
			return fmt.Errorf("failed to store updated shard mapping: %w", err)
		}
	} else {
		// Just update cache for testing
		sm.mu.Lock()
		sm.mapping = mapping
		sm.mu.Unlock()
	}

	sm.logger.Infof("Marked shard %d for migration from %s to %s", shardID, shardInfo.CurrentNode, shardInfo.TargetNode)
	return nil
}

// CompleteMigration completes the migration of a shard to its target node
// This should be called after the shard has been successfully migrated and cleaned up from the source node
// If etcd manager is available, updates both etcd and cache, otherwise just updates cache
func (sm *ShardMapper) CompleteMigration(ctx context.Context, shardID int) error {
	if shardID < 0 || shardID >= NumShards {
		return fmt.Errorf("invalid shard ID: %d", shardID)
	}

	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return fmt.Errorf("failed to get shard mapping: %w", err)
	}

	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return fmt.Errorf("shard %d not found in mapping", shardID)
	}

	// Update to available state on target node
	shardInfo.CurrentNode = shardInfo.TargetNode
	shardInfo.State = ShardStateAvailable

	// Store updated mapping (if etcd is available, otherwise just update cache)
	if sm.etcdManager != nil {
		err = sm.StoreShardMapping(ctx, mapping)
		if err != nil {
			return fmt.Errorf("failed to store updated shard mapping: %w", err)
		}
	} else {
		// Just update cache for testing
		sm.mu.Lock()
		sm.mapping = mapping
		sm.mu.Unlock()
	}

	sm.logger.Infof("Completed migration of shard %d to %s", shardID, shardInfo.TargetNode)
	return nil
}

// GetShardInfo returns detailed information about a specific shard
func (sm *ShardMapper) GetShardInfo(ctx context.Context, shardID int) (*ShardInfo, error) {
	if shardID < 0 || shardID >= NumShards {
		return nil, fmt.Errorf("invalid shard ID: %d", shardID)
	}

	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard mapping: %w", err)
	}

	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return nil, fmt.Errorf("shard %d not found in mapping", shardID)
	}

	// Return a copy to prevent external modification
	return &ShardInfo{
		ShardID:     shardInfo.ShardID,
		TargetNode:  shardInfo.TargetNode,
		CurrentNode: shardInfo.CurrentNode,
		State:       shardInfo.State,
	}, nil
}

// GetShardsInState returns all shard IDs that are in the specified state
func (sm *ShardMapper) GetShardsInState(ctx context.Context, state ShardState) ([]int, error) {
	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard mapping: %w", err)
	}

	var shardIDs []int
	for shardID, shardInfo := range mapping.Shards {
		if shardInfo.State == state {
			shardIDs = append(shardIDs, shardID)
		}
	}

	sort.Ints(shardIDs)
	return shardIDs, nil
}

// GetShardsOnNode returns all shard IDs currently on the specified node
func (sm *ShardMapper) GetShardsOnNode(ctx context.Context, nodeAddr string) ([]int, error) {
	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard mapping: %w", err)
	}

	var shardIDs []int
	for shardID, shardInfo := range mapping.Shards {
		if shardInfo.CurrentNode == nodeAddr {
			shardIDs = append(shardIDs, shardID)
		}
	}

	sort.Ints(shardIDs)
	return shardIDs, nil
}

// InvalidateCache clears the local cache, forcing a reload from etcd on next access
func (sm *ShardMapper) InvalidateCache() {
	sm.mu.Lock()
	sm.mapping = nil
	sm.mu.Unlock()
	sm.logger.Debugf("Shard mapping cache invalidated")
}

// SetMappingForTesting sets the cached mapping for testing purposes
// This should only be used in tests
func (sm *ShardMapper) SetMappingForTesting(mapping *ShardMapping) {
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()
}
