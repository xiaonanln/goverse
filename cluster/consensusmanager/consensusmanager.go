package consensusmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// ShardMapping represents the mapping of shards to nodes
// Note: Nodes and Version fields have been moved to ClusterState in consensusmanager
type ShardMapping struct {
	// Map from shard ID to node address
	Shards map[int]string `json:"shards"`
}

// ClusterState represents the current state of the cluster
type ClusterState struct {
	Nodes        map[string]bool
	ShardMapping *ShardMapping
	Revision     int64
	LastChange   time.Time
}

// StateChangeListener is the interface for components that want to be notified of state changes
type StateChangeListener interface {
	// OnClusterStateChanged is called when any cluster state changes (nodes or shard mapping)
	// Listeners should call ConsensusManager methods to get the current state
	OnClusterStateChanged()
}

// ConsensusManager handles all etcd interactions and maintains in-memory cluster state
type ConsensusManager struct {
	etcdManager *etcdmanager.EtcdManager
	logger      *logger.Logger

	// In-memory state
	mu    sync.RWMutex
	state *ClusterState

	// Watch management
	watchCtx     context.Context
	watchCancel  context.CancelFunc
	watchStarted bool

	// Listeners
	listenersMu sync.RWMutex
	listeners   []StateChangeListener
}

// NewConsensusManager creates a new consensus manager
func NewConsensusManager(etcdMgr *etcdmanager.EtcdManager) *ConsensusManager {
	return &ConsensusManager{
		etcdManager: etcdMgr,
		logger:      logger.NewLogger("ConsensusManager"),
		state: &ClusterState{
			Nodes: make(map[string]bool),
			// LastChange and Revision are zero initially, set when loading from etcd
		},
		listeners: make([]StateChangeListener, 0),
	}
}

// AddListener adds a state change listener
func (cm *ConsensusManager) AddListener(listener StateChangeListener) {
	cm.listenersMu.Lock()
	defer cm.listenersMu.Unlock()
	cm.listeners = append(cm.listeners, listener)
}

// RemoveListener removes a state change listener
func (cm *ConsensusManager) RemoveListener(listener StateChangeListener) {
	cm.listenersMu.Lock()
	defer cm.listenersMu.Unlock()

	for i, l := range cm.listeners {
		if l == listener {
			cm.listeners = append(cm.listeners[:i], cm.listeners[i+1:]...)
			break
		}
	}
}

// notifyStateChanged notifies all listeners about cluster state changes
func (cm *ConsensusManager) notifyStateChanged() {
	cm.listenersMu.RLock()
	listeners := make([]StateChangeListener, len(cm.listeners))
	copy(listeners, cm.listeners)
	cm.listenersMu.RUnlock()

	for _, listener := range listeners {
		listener.OnClusterStateChanged()
	}
}

// Initialize loads initial state from etcd
func (cm *ConsensusManager) Initialize(ctx context.Context) error {
	if cm.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}

	cm.logger.Infof("Initializing consensus manager")

	// Load all cluster data at once
	state, err := cm.loadClusterStateFromEtcd(ctx)
	if err != nil {
		return fmt.Errorf("failed to load cluster state: %w", err)
	}

	cm.mu.Lock()
	cm.state = state
	cm.mu.Unlock()

	cm.logger.Infof("Loaded cluster state: %d nodes, revision %d",
		len(state.Nodes), state.Revision)

	return nil
}

// StartWatch starts watching for changes to cluster state
func (cm *ConsensusManager) StartWatch(ctx context.Context) error {
	if cm.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}

	if cm.watchStarted {
		cm.logger.Warnf("Watch already started")
		return nil
	}

	cm.watchCtx, cm.watchCancel = context.WithCancel(ctx)
	cm.watchStarted = true

	// Start watching the entire /goverse prefix
	prefix := cm.etcdManager.GetPrefix()
	go cm.watchPrefix(prefix)
	return nil
}

// StopWatch stops watching for changes
func (cm *ConsensusManager) StopWatch() {
	if cm.watchCancel != nil {
		cm.watchCancel()
		cm.watchCancel = nil
	}
	cm.watchStarted = false
	cm.logger.Infof("Stopped watching")
}

// watchPrefix watches the entire etcd prefix for changes
func (cm *ConsensusManager) watchPrefix(prefix string) {
	client := cm.etcdManager.GetClient()
	if client == nil {
		cm.logger.Errorf("etcd client not available")
		return
	}

	// Get the revision from the loaded state to prevent missing events
	cm.mu.RLock()
	revision := cm.state.Revision
	cm.mu.RUnlock()

	nodesPrefix := cm.etcdManager.GetNodesPrefix()
	shardMappingKey := prefix + "/shardmapping"

	// Watch from the next revision after our load to prevent missing events
	watchChan := client.Watch(cm.watchCtx, prefix, clientv3.WithPrefix(), clientv3.WithRev(revision+1))

	cm.logger.Infof("Started watching prefix %s from revision %d", prefix, revision+1)
	for {
		select {
		case <-cm.watchCtx.Done():
			cm.logger.Infof("Watch stopped")
			return
		case watchResp, ok := <-watchChan:
			if !ok {
				cm.logger.Warnf("Watch channel closed")
				return
			}
			if watchResp.Err() != nil {
				cm.logger.Errorf("Watch error: %v", watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				key := string(event.Kv.Key)

				cm.logger.Infof("Received watch event: %s %s=%dB", event.Type.String(), key, len(event.Kv.Value))
				// Handle node changes
				if len(key) > len(nodesPrefix) && key[:len(nodesPrefix)] == nodesPrefix {
					cm.handleNodeEvent(event, nodesPrefix)
				} else if key == shardMappingKey {
					// Handle shard mapping changes
					cm.handleShardMappingEvent(event)
				}
			}
		}
	}
}

// handleNodeEvent processes node addition/removal events
func (cm *ConsensusManager) handleNodeEvent(event *clientv3.Event, nodesPrefix string) {
	cm.mu.Lock()

	switch event.Type {
	case clientv3.EventTypePut:
		nodeAddress := string(event.Kv.Value)
		cm.state.Nodes[nodeAddress] = true
		cm.state.LastChange = time.Now()
		cm.logger.Infof("Node added: %s", nodeAddress)
		cm.mu.Unlock()

		// Asynchronously notify listeners after releasing lock to prevent deadlocks
		// Notifications may arrive out of order if rapid changes occur
		go cm.notifyStateChanged()

	case clientv3.EventTypeDelete:
		// Extract node address from the key
		key := string(event.Kv.Key)
		nodeAddress := key[len(nodesPrefix):]
		delete(cm.state.Nodes, nodeAddress)
		cm.state.LastChange = time.Now()
		cm.logger.Infof("Node removed: %s", nodeAddress)
		cm.mu.Unlock()

		// Asynchronously notify listeners after releasing lock to prevent deadlocks
		// Notifications may arrive out of order if rapid changes occur
		go cm.notifyStateChanged()
	default:
		cm.mu.Unlock()
	}
}

// handleShardMappingEvent processes shard mapping changes
func (cm *ConsensusManager) handleShardMappingEvent(event *clientv3.Event) {
	if event.Type == clientv3.EventTypePut {
		var mapping ShardMapping
		err := json.Unmarshal(event.Kv.Value, &mapping)
		if err != nil {
			cm.logger.Errorf("Failed to unmarshal shard mapping: %v", err)
			return
		}

		cm.mu.Lock()
		cm.state.ShardMapping = &mapping
		cm.mu.Unlock()

		cm.logger.Infof("Shard mapping updated")
		// Asynchronously notify listeners to prevent deadlocks
		// Note: rapid mapping changes may result in out-of-order notifications
		go cm.notifyStateChanged()
	} else if event.Type == clientv3.EventTypeDelete {
		cm.mu.Lock()
		cm.state.ShardMapping = nil
		cm.mu.Unlock()
		cm.logger.Infof("Shard mapping deleted")
		// Notify listeners about the deletion
		go cm.notifyStateChanged()
	}
}

// loadClusterStateFromEtcd loads all cluster data at once from etcd
func (cm *ConsensusManager) loadClusterStateFromEtcd(ctx context.Context) (*ClusterState, error) {
	client := cm.etcdManager.GetClient()
	if client == nil {
		return nil, fmt.Errorf("etcd client not connected")
	}

	prefix := cm.etcdManager.GetPrefix()

	// Load ALL cluster data in one transaction
	resp, err := client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster state: %w", err)
	}

	state := &ClusterState{
		Nodes:        make(map[string]bool),
		ShardMapping: nil,
		Revision:     resp.Header.Revision,
		LastChange:   time.Now(),
	}

	nodesPrefix := cm.etcdManager.GetNodesPrefix()
	shardMappingKey := prefix + "/shardmapping"

	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if strings.HasPrefix(key, nodesPrefix) {
			state.Nodes[string(kv.Value)] = true
		} else if key == shardMappingKey {
			var mapping ShardMapping
			if err := json.Unmarshal(kv.Value, &mapping); err == nil {
				state.ShardMapping = &mapping
			}
		}
	}

	return state, nil
}

// GetNodes returns a copy of the current node list
func (cm *ConsensusManager) GetNodes() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	nodes := make([]string, 0, len(cm.state.Nodes))
	for node := range cm.state.Nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetLeaderNode returns the leader node address
// The leader is the node with the smallest advertised address in lexicographic order
func (cm *ConsensusManager) GetLeaderNode() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.state.Nodes) == 0 {
		return ""
	}

	// Find the smallest address (leader)
	var leader string
	for node := range cm.state.Nodes {
		if leader == "" || node < leader {
			leader = node
		}
	}
	return leader
}

// GetShardMapping returns the current shard mapping
func (cm *ConsensusManager) GetShardMapping() (*ShardMapping, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.ShardMapping == nil {
		return nil, fmt.Errorf("shard mapping not available")
	}

	return cm.state.ShardMapping, nil
}

// GetNodeForObject returns the node that should handle the given object ID
// If the object ID contains a "/" separator (e.g., "localhost:7001/object-123"),
// the part before the first "/" is treated as a fixed node address and returned directly.
// This allows objects to be pinned to specific nodes, similar to client IDs.
// Otherwise, the object is assigned to a node based on consistent hashing.
func (cm *ConsensusManager) GetNodeForObject(objectID string) (string, error) {
	// Check if object ID specifies a fixed node address
	// Format: nodeAddress/actualObjectID (e.g., "localhost:7001/object-123")
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			// Fixed node address specified (first part before /)
			nodeAddr := parts[0]
			cm.logger.Debugf("Object %s pinned to node %s", objectID, nodeAddr)
			return nodeAddr, nil
		}
	}

	// Standard shard-based assignment requires shard mapping
	mapping, err := cm.GetShardMapping()
	if err != nil {
		return "", err
	}

	// Use the sharding logic to determine the node
	shardID := sharding.GetShardID(objectID)
	node, ok := mapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	return node, nil
}

// GetNodeForShard returns the node that owns the given shard
func (cm *ConsensusManager) GetNodeForShard(shardID int) (string, error) {
	if shardID < 0 || shardID >= sharding.NumShards {
		return "", fmt.Errorf("invalid shard ID: %d", shardID)
	}

	mapping, err := cm.GetShardMapping()
	if err != nil {
		return "", err
	}

	node, ok := mapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	return node, nil
}

// StoreShardMapping stores a new shard mapping in etcd and updates the in-memory state
func (cm *ConsensusManager) StoreShardMapping(ctx context.Context, mapping *ShardMapping) error {
	if cm.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}

	// Serialize the mapping
	data, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("failed to marshal shard mapping: %w", err)
	}

	// Store in etcd
	key := cm.etcdManager.GetPrefix() + "/shardmapping"
	err = cm.etcdManager.Put(ctx, key, string(data))
	if err != nil {
		return fmt.Errorf("failed to store shard mapping: %w", err)
	}

	// Update in-memory state
	cm.mu.Lock()
	cm.state.ShardMapping = mapping
	cm.mu.Unlock()

	cm.logger.Infof("Stored shard mapping in etcd")

	return nil
}

// CreateShardMapping creates a new shard mapping from the current node list
func (cm *ConsensusManager) CreateShardMapping() (*ShardMapping, error) {
	nodes := cm.GetNodes()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available to create shard mapping")
	}

	// Sort nodes for deterministic assignment
	sortedNodes := make([]string, len(nodes))
	copy(sortedNodes, nodes)
	sort.Strings(sortedNodes)

	// Create the mapping
	mapping := &ShardMapping{
		Shards: make(map[int]string, sharding.NumShards),
	}

	for shardID := 0; shardID < sharding.NumShards; shardID++ {
		nodeIdx := shardID % len(sortedNodes)
		mapping.Shards[shardID] = sortedNodes[nodeIdx]
	}

	cm.logger.Infof("Created shard mapping with %d shards across %d nodes", sharding.NumShards, len(sortedNodes))
	return mapping, nil
}

// UpdateShardMapping updates the shard mapping based on the current node list
func (cm *ConsensusManager) UpdateShardMapping() (*ShardMapping, error) {
	nodes := cm.GetNodes()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available to update shard mapping")
	}

	// Get current mapping
	currentMapping, err := cm.GetShardMapping()
	if err != nil {
		// If no mapping exists, create a new one
		cm.logger.Infof("No existing shard mapping, creating new one")
		return cm.CreateShardMapping()
	}

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
	newShards := make(map[int]string, sharding.NumShards)

	for shardID := 0; shardID < sharding.NumShards; shardID++ {
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

	// If no changes needed, return current mapping
	if !needsUpdate {
		cm.mu.RLock()
		cm.mu.RUnlock()
		cm.logger.Infof("No changes needed to shard mapping")
		return currentMapping, nil
	}

	// Create new mapping
	newMapping := &ShardMapping{
		Shards: newShards,
	}

	cm.logger.Infof("Updated shard mapping")
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

// IsNodeListStable returns true if the node list has not changed for the specified duration
func (cm *ConsensusManager) IsNodeListStable(duration time.Duration) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.LastChange.IsZero() {
		return false
	}

	return time.Since(cm.state.LastChange) >= duration
}

// GetLastNodeChangeTime returns the timestamp of the last node list change
func (cm *ConsensusManager) GetLastNodeChangeTime() time.Time {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.state.LastChange
}

// SetMappingForTesting sets the shard mapping for testing purposes
// This should only be used in tests
func (cm *ConsensusManager) SetMappingForTesting(mapping *ShardMapping) {
	cm.mu.Lock()
	cm.state.ShardMapping = mapping
	cm.mu.Unlock()
}
