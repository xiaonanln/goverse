package consensusmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// StateChangeListener is the interface for components that want to be notified of state changes
type StateChangeListener interface {
	// OnNodesChanged is called when the node list changes
	OnNodesChanged(nodes []string)
	// OnShardMappingChanged is called when the shard mapping changes
	OnShardMappingChanged(mapping *sharding.ShardMapping)
}

// ConsensusManager handles all etcd interactions and maintains in-memory cluster state
type ConsensusManager struct {
	etcdManager      *etcdmanager.EtcdManager
	logger           *logger.Logger
	
	// In-memory state
	mu               sync.RWMutex
	nodes            map[string]bool
	shardMapping     *sharding.ShardMapping
	lastNodeChange   time.Time
	
	// Watch management
	watchCtx         context.Context
	watchCancel      context.CancelFunc
	watchStarted     bool
	
	// Listeners
	listenersMu      sync.RWMutex
	listeners        []StateChangeListener
}

// NewConsensusManager creates a new consensus manager
func NewConsensusManager(etcdMgr *etcdmanager.EtcdManager) *ConsensusManager {
	return &ConsensusManager{
		etcdManager: etcdMgr,
		logger:      logger.NewLogger("ConsensusManager"),
		nodes:       make(map[string]bool),
		listeners:   make([]StateChangeListener, 0),
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

// notifyNodesChanged notifies all listeners about node list changes
func (cm *ConsensusManager) notifyNodesChanged() {
	cm.mu.RLock()
	nodes := make([]string, 0, len(cm.nodes))
	for node := range cm.nodes {
		nodes = append(nodes, node)
	}
	cm.mu.RUnlock()
	
	cm.listenersMu.RLock()
	listeners := make([]StateChangeListener, len(cm.listeners))
	copy(listeners, cm.listeners)
	cm.listenersMu.RUnlock()
	
	for _, listener := range listeners {
		listener.OnNodesChanged(nodes)
	}
}

// notifyShardMappingChanged notifies all listeners about shard mapping changes
func (cm *ConsensusManager) notifyShardMappingChanged(mapping *sharding.ShardMapping) {
	cm.listenersMu.RLock()
	listeners := make([]StateChangeListener, len(cm.listeners))
	copy(listeners, cm.listeners)
	cm.listenersMu.RUnlock()
	
	for _, listener := range listeners {
		listener.OnShardMappingChanged(mapping)
	}
}

// Initialize loads initial state from etcd
func (cm *ConsensusManager) Initialize(ctx context.Context) error {
	if cm.etcdManager == nil {
		return fmt.Errorf("etcd manager not set")
	}
	
	cm.logger.Infof("Initializing consensus manager")
	
	// Load initial nodes
	nodes, err := cm.loadNodesFromEtcd(ctx)
	if err != nil {
		return fmt.Errorf("failed to load initial nodes: %w", err)
	}
	
	cm.mu.Lock()
	cm.nodes = make(map[string]bool)
	for _, node := range nodes {
		cm.nodes[node] = true
	}
	cm.lastNodeChange = time.Now()
	cm.mu.Unlock()
	
	cm.logger.Infof("Loaded %d initial nodes", len(nodes))
	
	// Load initial shard mapping if it exists
	mapping, err := cm.loadShardMappingFromEtcd(ctx)
	if err != nil {
		// It's OK if shard mapping doesn't exist yet
		cm.logger.Debugf("No initial shard mapping found: %v", err)
	} else {
		cm.mu.Lock()
		cm.shardMapping = mapping
		cm.mu.Unlock()
		cm.logger.Infof("Loaded initial shard mapping (version %d)", mapping.Version)
	}
	
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
	
	cm.logger.Infof("Started watching prefix: %s", prefix)
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
	
	nodesPrefix := cm.etcdManager.GetNodesPrefix()
	shardMappingKey := prefix + "/shardmapping"
	
	watchChan := client.Watch(cm.watchCtx, prefix, clientv3.WithPrefix())
	
	cm.logger.Infof("Started watching prefix: %s", prefix)
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
		cm.nodes[nodeAddress] = true
		cm.lastNodeChange = time.Now()
		cm.logger.Infof("Node added: %s", nodeAddress)
		cm.mu.Unlock()
		
		// Asynchronously notify listeners after releasing lock to prevent deadlocks
		// Notifications may arrive out of order if rapid changes occur
		go cm.notifyNodesChanged()
		
	case clientv3.EventTypeDelete:
		// Extract node address from the key
		key := string(event.Kv.Key)
		nodeAddress := key[len(nodesPrefix):]
		delete(cm.nodes, nodeAddress)
		cm.lastNodeChange = time.Now()
		cm.logger.Infof("Node removed: %s", nodeAddress)
		cm.mu.Unlock()
		
		// Asynchronously notify listeners after releasing lock to prevent deadlocks
		// Notifications may arrive out of order if rapid changes occur
		go cm.notifyNodesChanged()
	default:
		cm.mu.Unlock()
	}
}

// handleShardMappingEvent processes shard mapping changes
func (cm *ConsensusManager) handleShardMappingEvent(event *clientv3.Event) {
	if event.Type == clientv3.EventTypePut {
		var mapping sharding.ShardMapping
		err := json.Unmarshal(event.Kv.Value, &mapping)
		if err != nil {
			cm.logger.Errorf("Failed to unmarshal shard mapping: %v", err)
			return
		}
		
		cm.mu.Lock()
		oldVersion := int64(-1)
		if cm.shardMapping != nil {
			oldVersion = cm.shardMapping.Version
		}
		cm.shardMapping = &mapping
		cm.mu.Unlock()
		
		if mapping.Version != oldVersion {
			cm.logger.Infof("Shard mapping updated (version %d -> %d)", oldVersion, mapping.Version)
			// Asynchronously notify listeners to prevent deadlocks
			// Note: rapid mapping changes may result in out-of-order notifications
			go cm.notifyShardMappingChanged(&mapping)
		}
	} else if event.Type == clientv3.EventTypeDelete {
		cm.mu.Lock()
		cm.shardMapping = nil
		cm.mu.Unlock()
		cm.logger.Infof("Shard mapping deleted")
	}
}

// loadNodesFromEtcd retrieves all nodes from etcd
func (cm *ConsensusManager) loadNodesFromEtcd(ctx context.Context) ([]string, error) {
	client := cm.etcdManager.GetClient()
	if client == nil {
		return nil, fmt.Errorf("etcd client not connected")
	}
	
	resp, err := client.Get(ctx, cm.etcdManager.GetNodesPrefix(), clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}
	
	nodes := make([]string, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		nodes = append(nodes, string(kv.Value))
	}
	
	return nodes, nil
}

// loadShardMappingFromEtcd retrieves the shard mapping from etcd
func (cm *ConsensusManager) loadShardMappingFromEtcd(ctx context.Context) (*sharding.ShardMapping, error) {
	client := cm.etcdManager.GetClient()
	if client == nil {
		return nil, fmt.Errorf("etcd client not connected")
	}
	
	key := cm.etcdManager.GetPrefix() + "/shardmapping"
	resp, err := client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard mapping: %w", err)
	}
	
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("shard mapping not found")
	}
	
	var mapping sharding.ShardMapping
	err = json.Unmarshal(resp.Kvs[0].Value, &mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal shard mapping: %w", err)
	}
	
	return &mapping, nil
}

// GetNodes returns a copy of the current node list
func (cm *ConsensusManager) GetNodes() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	nodes := make([]string, 0, len(cm.nodes))
	for node := range cm.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetLeaderNode returns the leader node address
// The leader is the node with the smallest advertised address in lexicographic order
func (cm *ConsensusManager) GetLeaderNode() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	if len(cm.nodes) == 0 {
		return ""
	}
	
	// Find the smallest address (leader)
	var leader string
	for node := range cm.nodes {
		if leader == "" || node < leader {
			leader = node
		}
	}
	return leader
}

// GetShardMapping returns the current shard mapping
func (cm *ConsensusManager) GetShardMapping() (*sharding.ShardMapping, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	if cm.shardMapping == nil {
		return nil, fmt.Errorf("shard mapping not available")
	}
	
	return cm.shardMapping, nil
}

// GetNodeForObject returns the node that should handle the given object ID
func (cm *ConsensusManager) GetNodeForObject(objectID string) (string, error) {
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
func (cm *ConsensusManager) StoreShardMapping(ctx context.Context, mapping *sharding.ShardMapping) error {
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
	
	cm.logger.Infof("Stored shard mapping (version %d) in etcd", mapping.Version)
	
	// Update in-memory state
	cm.mu.Lock()
	cm.shardMapping = mapping
	cm.mu.Unlock()
	
	return nil
}

// CreateShardMapping creates a new shard mapping from the current node list
func (cm *ConsensusManager) CreateShardMapping() (*sharding.ShardMapping, error) {
	nodes := cm.GetNodes()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available to create shard mapping")
	}
	
	// Sort nodes for deterministic assignment
	sortedNodes := make([]string, len(nodes))
	copy(sortedNodes, nodes)
	sort.Strings(sortedNodes)
	
	// Create the mapping
	mapping := &sharding.ShardMapping{
		Shards:  make(map[int]string, sharding.NumShards),
		Nodes:   sortedNodes,
		Version: 1,
	}
	
	for shardID := 0; shardID < sharding.NumShards; shardID++ {
		nodeIdx := shardID % len(sortedNodes)
		mapping.Shards[shardID] = sortedNodes[nodeIdx]
	}
	
	cm.logger.Infof("Created shard mapping with %d shards across %d nodes", sharding.NumShards, len(sortedNodes))
	return mapping, nil
}

// UpdateShardMapping updates the shard mapping based on the current node list
func (cm *ConsensusManager) UpdateShardMapping() (*sharding.ShardMapping, error) {
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
	
	// Check if node list changed
	if !nodesEqual(currentMapping.Nodes, sortedNodes) {
		needsUpdate = true
	}
	
	// If no changes needed, return current mapping
	if !needsUpdate {
		cm.logger.Infof("No changes needed to shard mapping (version %d)", currentMapping.Version)
		return currentMapping, nil
	}
	
	// Create new mapping with incremented version
	newMapping := &sharding.ShardMapping{
		Shards:  newShards,
		Nodes:   sortedNodes,
		Version: currentMapping.Version + 1,
	}
	
	cm.logger.Infof("Updated shard mapping to version %d", newMapping.Version)
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
	
	if cm.lastNodeChange.IsZero() {
		return false
	}
	
	return time.Since(cm.lastNodeChange) >= duration
}

// GetLastNodeChangeTime returns the timestamp of the last node list change
func (cm *ConsensusManager) GetLastNodeChangeTime() time.Time {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.lastNodeChange
}
