package consensusmanager

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/workerpool"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// shardStorageWorkers defines the number of concurrent workers for storing shards in etcd.
	// This value balances parallelism with avoiding overwhelming etcd.
	// With 8192 shards and 20 workers, each worker handles ~410 shards sequentially.
	shardStorageWorkers = 20
)

// ShardInfo contains information about a shard's node assignment
type ShardInfo struct {
	// TargetNode is the node that should handle this shard
	TargetNode string
	// CurrentNode is the node currently handling this shard
	// Empty initially - will be populated when shard migration/handoff logic is implemented
	// to enable tracking of active shard transfers during cluster rebalancing
	CurrentNode string

	// modRevision is the etcd revision when this shard info was last modified
	ModRevision int64
}

// ShardMapping represents the mapping of shards to nodes
// Note: Nodes and Version fields have been moved to ClusterState in consensusmanager
type ShardMapping struct {
	// Map from shard ID to shard info (target and current node)
	Shards map[int]ShardInfo `json:"shards"`
}

func (sm *ShardMapping) IsComplete() bool {
	return len(sm.Shards) == sharding.NumShards
}

// ClusterState represents the current state of the cluster
type ClusterState struct {
	Nodes        map[string]bool
	ShardMapping *ShardMapping
	Revision     int64
	LastChange   time.Time
}

// HasNode returns true if the given node address exists in the cluster state.
func (cs *ClusterState) HasNode(nodeAddr string) bool {
	if cs == nil {
		return false
	}
	_, ok := cs.Nodes[nodeAddr]
	return ok
}

func (cs *ClusterState) IsStable(duration time.Duration) bool {
	if cs == nil {
		return false
	}
	if cs.LastChange.IsZero() {
		return false
	}
	return time.Since(cs.LastChange) >= duration
}

func (cs *ClusterState) Clone() *ClusterState {
	if cs == nil {
		return nil
	}

	// Copy basic fields
	cscp := &ClusterState{
		Nodes: make(map[string]bool, len(cs.Nodes)),
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
		Revision:   cs.Revision,
		LastChange: cs.LastChange,
	}

	// Copy nodes map
	for n, v := range cs.Nodes {
		cscp.Nodes[n] = v
	}

	// Deep copy shard mapping if present
	if cs.ShardMapping != nil {
		for sid, info := range cs.ShardMapping.Shards {
			cscp.ShardMapping.Shards[sid] = info
		}
	}

	return cscp
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

func (cm *ConsensusManager) IsReady() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.etcdManager == nil {
		cm.logger.Warnf("ConsensusManager not ready: Etcd manager not set")
		return false
	}

	// Consider ready if we have loaded initial state
	if cm.state.Revision == 0 {
		cm.logger.Warnf("ConsensusManager not ready: Initial state not loaded")
		return false
	}

	if len(cm.state.Nodes) == 0 {
		cm.logger.Warnf("ConsensusManager not ready: No nodes available")
		return false
	}

	if cm.state.ShardMapping == nil || !cm.state.ShardMapping.IsComplete() {
		cm.logger.Warnf("ConsensusManager not ready: Shard mapping incomplete")
		return false
	}

	cm.logger.Infof("ConsensusManager is ready!")
	return true
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
	shardPrefix := prefix + "/shard/"

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

				// cm.logger.Infof("Received watch event: %s %s=%s", event.Type.String(), key, event.Kv.Value)
				// Handle node changes
				if len(key) > len(nodesPrefix) && key[:len(nodesPrefix)] == nodesPrefix {
					cm.handleNodeEvent(event, nodesPrefix)
				} else if len(key) > len(shardPrefix) && key[:len(shardPrefix)] == shardPrefix {
					// Handle individual shard changes
					cm.handleShardEvent(event, shardPrefix)
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

// handleShardEvent processes individual shard changes
func (cm *ConsensusManager) handleShardEvent(event *clientv3.Event, shardPrefix string) {
	// Extract shard ID from key
	key := string(event.Kv.Key)
	shardIDStr := key[len(shardPrefix):]

	shardID, err := strconv.Atoi(shardIDStr)
	if err != nil {
		cm.logger.Errorf("Failed to parse shard ID from key %s: %v", key, err)
		return
	}

	if shardID < 0 || shardID >= sharding.NumShards {
		cm.logger.Errorf("Invalid shard ID %d from key %s", shardID, key)
		return
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if event.Type == clientv3.EventTypePut {
		shardInfo := parseShardInfo(event.Kv)
		cm.state.ShardMapping.Shards[shardID] = shardInfo
		cm.logger.Debugf("Shard %d assigned to target node %s (current: %s)", shardID, shardInfo.TargetNode, shardInfo.CurrentNode)

		// Asynchronously notify listeners to prevent deadlocks
		go cm.notifyStateChanged()
	} else if event.Type == clientv3.EventTypeDelete {
		delete(cm.state.ShardMapping.Shards, shardID)
		cm.logger.Debugf("Shard %d mapping deleted", shardID)

		// Asynchronously notify listeners to prevent deadlocks
		go cm.notifyStateChanged()
	}
}

// parseShardInfo parses shard information from the etcd value
// Format: "targetNode,currentNode" or just "targetNode" (for backward compatibility)
func parseShardInfo(kv *mvccpb.KeyValue) ShardInfo {
	// Split into exactly 2 parts max
	value := string(kv.Value)
	parts := strings.SplitN(value, ",", 2)
	if len(parts) == 2 {
		return ShardInfo{
			TargetNode:  strings.TrimSpace(parts[0]),
			CurrentNode: strings.TrimSpace(parts[1]),
			ModRevision: kv.ModRevision,
		}
	}
	// Backward compatibility: if only one part, it's the target node
	return ShardInfo{
		TargetNode:  strings.TrimSpace(value),
		CurrentNode: "",
		ModRevision: kv.ModRevision,
	}
}

// formatShardInfo formats shard information for etcd storage
// Format: "targetNode,currentNode"
func formatShardInfo(info ShardInfo) string {
	return info.TargetNode + "," + info.CurrentNode
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
		Nodes: make(map[string]bool),
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
		Revision:   resp.Header.Revision,
		LastChange: time.Now(),
	}

	nodesPrefix := cm.etcdManager.GetNodesPrefix()
	shardPrefix := prefix + "/shard/"

	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if strings.HasPrefix(key, nodesPrefix) {
			state.Nodes[string(kv.Value)] = true
		} else if strings.HasPrefix(key, shardPrefix) {
			// Parse shard ID from key
			shardIDStr := key[len(shardPrefix):]
			shardID, err := strconv.Atoi(shardIDStr)
			if err != nil {
				cm.logger.Warnf("Failed to parse shard ID from key %s: %v", key, err)
				continue
			}

			if shardID < 0 || shardID >= sharding.NumShards {
				cm.logger.Warnf("Invalid shard ID %d from key %s", shardID, key)
				continue
			}

			state.ShardMapping.Shards[shardID] = parseShardInfo(kv)
		}
	}

	cm.logger.Infof("Loaded %d nodes and %d shards from etcd", len(state.Nodes), len(state.ShardMapping.Shards))

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
	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	return shardInfo.TargetNode, nil
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

	shardInfo, ok := mapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	return shardInfo.TargetNode, nil
}

// storeShardMapping stores a new shard mapping in etcd and updates the in-memory state
// Each shard is stored as an individual key: /goverse/shard/<shard-id> with value in format "targetNode,currentNode"
// The function uses conditional puts based on ModRevision to ensure consistency.
// Uses a fixed worker pool to write multiple shards in parallel for better performance.
// Returns the number of successfully written shards, or an error if any write fails.
func (cm *ConsensusManager) storeShardMapping(ctx context.Context, updateShards map[int]ShardInfo) (int, error) {
	if cm.etcdManager == nil {
		return 0, fmt.Errorf("etcd manager not set")
	}

	client := cm.etcdManager.GetClient()
	if client == nil {
		return 0, fmt.Errorf("etcd client not connected")
	}

	prefix := cm.etcdManager.GetPrefix()
	shardPrefix := prefix + "/shard/"

	// Collect all shard IDs to process
	shardIDs := slices.Collect(maps.Keys(updateShards))
	startTime := time.Now()

	// Create and start a fixed worker pool
	pool := workerpool.New(ctx, shardStorageWorkers)
	pool.Start()
	defer pool.Stop()

	// Result tracking
	type result struct {
		shardID int
		err     error
	}
	
	// Create tasks for each shard
	tasks := make([]workerpool.Task, len(shardIDs))
	for i, shardID := range shardIDs {
		id := shardID
		tasks[i] = func(ctx context.Context) error {
			shardInfo := updateShards[id]
			key := fmt.Sprintf("%s%d", shardPrefix, id)
			value := formatShardInfo(shardInfo)

			// Build conditional transaction based on ModRevision
			// Always check that ModRevision matches expected value (0 for new shards)
			txn := client.Txn(ctx).
				If(clientv3.Compare(clientv3.ModRevision(key), "=", shardInfo.ModRevision)).
				Then(clientv3.OpPut(key, value))

			resp, err := txn.Commit()

			if err != nil {
				return fmt.Errorf("failed to store shard %d: %w", id, err)
			}

			if !resp.Succeeded {
				// The condition failed - the shard was modified by another process
				return fmt.Errorf("failed to store shard %d: ModRevision mismatch (expected %d)", id, shardInfo.ModRevision)
			}

			return nil
		}
	}

	// Execute all tasks using the worker pool
	results := pool.SubmitAndWait(ctx, tasks)

	// Collect results
	successCount := 0
	var firstError error
	for _, res := range results {
		if res.Err != nil && firstError == nil {
			firstError = res.Err
		} else if res.Err == nil {
			successCount++
		}
	}

	cm.logger.Infof("Stored %d/%d shards in etcd in %d ms",
		successCount, len(shardIDs), time.Since(startTime).Milliseconds())

	if firstError != nil {
		return successCount, firstError
	}

	return successCount, nil
}

// UpdateShardMapping updates the shard mapping based on the current node list
func (cm *ConsensusManager) UpdateShardMapping(ctx context.Context) error {
	cm.mu.RLock()
	clusterState := cm.state
	cm.mu.RUnlock()

	if len(clusterState.Nodes) == 0 {
		return fmt.Errorf("no nodes available to assign shards")
	}

	// Sort nodes for deterministic assignment
	nodes := slices.Sorted(maps.Keys(clusterState.Nodes))
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeSet[n] = true
	}

	// Check if any changes are needed
	updateShards := make(map[int]ShardInfo)
	currentMapping := clusterState.ShardMapping
	if currentMapping == nil {
		currentMapping = &ShardMapping{
			Shards: make(map[int]ShardInfo),
		}
	}

	for shardID := 0; shardID < sharding.NumShards; shardID++ {
		currentInfo := currentMapping.Shards[shardID]
		if !nodeSet[currentInfo.TargetNode] {
			// Assign to a new node using round-robin
			nodeIdx := shardID % len(nodes)
			newInfo := ShardInfo{
				TargetNode:  nodes[nodeIdx],
				CurrentNode: currentInfo.CurrentNode,
				ModRevision: currentInfo.ModRevision,
			}
			updateShards[shardID] = newInfo
		}
	}

	if len(updateShards) == 0 {
		cm.logger.Infof("No changes needed to shard mapping")
		return nil
	}

	cm.logger.Infof("Updating shard mapping for %d shards", len(updateShards))
	n, err := cm.storeShardMapping(ctx, updateShards)
	cm.logger.Infof("Successfully updated shard mapping for %d shards - error %v", n, err)
	return err
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

// IsStateStable returns true if the node list has not changed for the specified duration
func (cm *ConsensusManager) IsStateStable(duration time.Duration) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.state.IsStable(duration)
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

func (cm *ConsensusManager) GetClusterState() *ClusterState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.state.Clone()
}
