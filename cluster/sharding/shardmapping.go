package sharding

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/util/logger"
)

// ShardMapping represents the mapping of shards to nodes
type ShardMapping struct {
	// Map from shard ID to node address
	Shards map[int]string `json:"shards"`
	// Sorted list of nodes in the cluster
	Nodes []string `json:"nodes"`
	// Version for optimistic concurrency control
	Version int64 `json:"version"`
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
		Shards:  make(map[int]string, NumShards),
		Nodes:   sortedNodes,
		Version: 1,
	}

	for shardID := 0; shardID < NumShards; shardID++ {
		nodeIdx := shardID % len(sortedNodes)
		mapping.Shards[shardID] = sortedNodes[nodeIdx]
	}

	sm.logger.Infof("Created shard mapping with %d shards distributed across %d nodes", NumShards, len(sortedNodes))
	return mapping, nil
}

// StoreShardMapping stores the shard mapping in etcd
func (sm *ShardMapper) StoreShardMapping(ctx context.Context, mapping *ShardMapping) error {
	if sm.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}

	// Serialize the mapping to JSON
	data, err := json.Marshal(mapping)
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

// GetShardMapping retrieves the shard mapping from etcd
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

	// Deserialize the mapping
	var mapping ShardMapping
	err = json.Unmarshal([]byte(data), &mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal shard mapping: %w", err)
	}

	sm.logger.Infof("Retrieved shard mapping (version %d) from etcd", mapping.Version)

	// Update local cache
	sm.mu.Lock()
	sm.mapping = &mapping
	sm.mu.Unlock()

	return &mapping, nil
}

// GetNodeForShard returns the node address that owns the given shard
func (sm *ShardMapper) GetNodeForShard(ctx context.Context, shardID int) (string, error) {
	if shardID < 0 || shardID >= NumShards {
		return "", fmt.Errorf("invalid shard ID: %d (must be in range [0, %d))", shardID, NumShards)
	}

	mapping, err := sm.GetShardMapping(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get shard mapping: %w", err)
	}

	node, ok := mapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	return node, nil
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
	newShards := make(map[int]string, NumShards)

	for shardID := 0; shardID < NumShards; shardID++ {
		currentNode, exists := currentMapping.Shards[shardID]
		if exists && nodeSet[currentNode] {
			// Keep the shard on the same node if it's still available
			newShards[shardID] = currentNode
		} else {
			// Assign to a new node using round-robin
			nodeIdx := shardID % len(sortedNodes)
			newShards[shardID] = sortedNodes[nodeIdx]
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
