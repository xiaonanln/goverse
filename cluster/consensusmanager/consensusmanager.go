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
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/workerpool"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// shardStorageWorkers defines the number of concurrent workers for storing shards in etcd.
	// This value balances parallelism with avoiding overwhelming etcd.
	// With 8192 shards and 20 workers, each worker handles ~410 shards sequentially.
	shardStorageWorkers = 20
	// defaultClusterStateStabilityDuration is the default duration to consider the cluster state stable
	defaultClusterStateStabilityDuration = 10 * time.Second
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
	if len(cs.Nodes) == 0 {
		return false
	}
	return time.Since(cs.LastChange) >= duration
}

// CloneForTesting creates a deep copy of the cluster state.
// This method should only be used in tests.
func (cs *ClusterState) CloneForTesting() *ClusterState {
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

	// Deep copy shard mapping
	// Note: cs.ShardMapping could be nil if CloneForTesting is called before Initialize
	// but cscp.ShardMapping is always initialized above
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
	shardLock   *shardlock.ShardLock

	// In-memory state
	mu    sync.RWMutex
	state *ClusterState

	// Configuration
	minQuorum                     int           // minimal number of nodes required for cluster to be considered stable
	clusterStateStabilityDuration time.Duration // duration to wait for cluster state to stabilize
	localNodeAddress              string        // local node address for this consensus manager

	// Watch management
	watchCtx     context.Context
	watchCancel  context.CancelFunc
	watchStarted bool

	// Listeners
	listenersMu sync.RWMutex
	listeners   []StateChangeListener
}

// NewConsensusManager creates a new consensus manager
func NewConsensusManager(etcdMgr *etcdmanager.EtcdManager, shardLock *shardlock.ShardLock, clusterStateStabilityDuration time.Duration, localNodeAddress string) *ConsensusManager {
	if clusterStateStabilityDuration <= 0 {
		clusterStateStabilityDuration = defaultClusterStateStabilityDuration
	}
	return &ConsensusManager{
		etcdManager:                   etcdMgr,
		logger:                        logger.NewLogger("ConsensusManager"),
		shardLock:                     shardLock,
		clusterStateStabilityDuration: clusterStateStabilityDuration,
		localNodeAddress:              localNodeAddress,
		state: &ClusterState{
			Nodes: make(map[string]bool),
			ShardMapping: &ShardMapping{
				Shards: make(map[int]ShardInfo),
			},
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

	// Check if we have the minimum required nodes
	minQuorum := cm.getEffectiveMinQuorum()
	if len(cm.state.Nodes) < minQuorum {
		cm.logger.Warnf("ConsensusManager not ready: Only %d nodes available, minimum quorum required is %d", len(cm.state.Nodes), minQuorum)
		return false
	}

	if cm.state.ShardMapping == nil || !cm.state.ShardMapping.IsComplete() {
		cm.logger.Warnf("ConsensusManager not ready: Shard mapping incomplete")
		return false
	}

	cm.logger.Infof("ConsensusManager is ready!")
	return true
}

// SetMinQuorum sets the minimal number of nodes required for cluster stability
func (cm *ConsensusManager) SetMinQuorum(minQuorum int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.minQuorum = minQuorum
	cm.logger.Infof("ConsensusManager minimum quorum set to %d", minQuorum)
}

// GetMinQuorum returns the minimal number of nodes required for cluster stability
// If not set, returns 1 as the default
func (cm *ConsensusManager) GetMinQuorum() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.getEffectiveMinQuorum()
}

// getEffectiveMinQuorum returns the effective minimum quorum value (with default of 1)
// This method must be called with the read lock held
func (cm *ConsensusManager) getEffectiveMinQuorum() int {
	if cm.minQuorum <= 0 {
		return 1
	}
	return cm.minQuorum
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

// UpdateShardMetrics updates the Prometheus metrics for shard counts per node
func (cm *ConsensusManager) UpdateShardMetrics() {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Count shards per node using CurrentNode (actual ownership)
	shardCounts := make(map[string]int)
	migratingCount := 0

	// Initialize counts for all known nodes to ensure we set metrics even for nodes with 0 shards
	for node := range cm.state.Nodes {
		shardCounts[node] = 0
	}

	// Count shards based on CurrentNode (actual ownership) and detect migrations
	for _, shardInfo := range cm.state.ShardMapping.Shards {
		if shardInfo.CurrentNode != "" {
			shardCounts[shardInfo.CurrentNode]++
		}

		// Count shards in migration state (TargetNode != CurrentNode)
		if shardInfo.TargetNode != "" && shardInfo.CurrentNode != "" &&
			shardInfo.TargetNode != shardInfo.CurrentNode {
			migratingCount++
		}
	}

	// Update metrics for each node
	for node, count := range shardCounts {
		metrics.SetAssignedShardCount(node, float64(count))
	}

	// Update shards migrating gauge
	metrics.SetShardsMigrating(float64(migratingCount))
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

	nodesPrefix := cm.etcdManager.GetPrefix() + "/nodes/"
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

				cm.logger.Debugf("Received watch event: %s %s=%s rev %v", event.Type.String(), key, event.Kv.Value, event.Kv.ModRevision)
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
		newShardInfo := parseShardInfo(event.Kv)

		if newShardInfo.ModRevision <= cm.state.ShardMapping.Shards[shardID].ModRevision {
			return
		}

		cm.recordShardMigrationLocked(shardID, newShardInfo)
		// Update state in memory while holding lock
		cm.state.ShardMapping.Shards[shardID] = newShardInfo
		cm.logger.Debugf("Shard %d assigned to target node %s (current: %s)", shardID, newShardInfo.TargetNode, newShardInfo.CurrentNode)
		// Asynchronously notify listeners to prevent deadlocks
		go cm.notifyStateChanged()
	} else if event.Type == clientv3.EventTypeDelete {
		if event.Kv.ModRevision <= cm.state.ShardMapping.Shards[shardID].ModRevision {
			return
		}
		delete(cm.state.ShardMapping.Shards, shardID)
		cm.logger.Debugf("Shard %d mapping deleted", shardID)
		// Asynchronously notify listeners to prevent deadlocks
		go cm.notifyStateChanged()
	}
}

func (cm *ConsensusManager) recordShardMigrationLocked(shardID int, newShardInfo ShardInfo) {
	// Check if this is a migration completion (CurrentNode changed from one node to another)
	oldShardInfo, exists := cm.state.ShardMapping.Shards[shardID]
	if exists && oldShardInfo.CurrentNode != "" && newShardInfo.CurrentNode != "" &&
		oldShardInfo.CurrentNode != newShardInfo.CurrentNode {
		// Migration completed: CurrentNode changed from one node to another
		cm.logger.Debugf("Shard %d migration completed: %s -> %s", shardID, oldShardInfo.CurrentNode, newShardInfo.CurrentNode)
		metrics.RecordShardMigration(oldShardInfo.CurrentNode, newShardInfo.CurrentNode)
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

	nodesPrefix := cm.etcdManager.GetPrefix() + "/nodes/"
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
func (cm *ConsensusManager) GetShardMapping() *ShardMapping {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.ShardMapping == nil {
		return &ShardMapping{Shards: make(map[int]ShardInfo)}
	}

	// Return a deep copy to prevent data races when caller iterates over the map
	// while concurrent updates may be happening via watch events
	shardsCopy := make(map[int]ShardInfo, len(cm.state.ShardMapping.Shards))
	for shardID, shardInfo := range cm.state.ShardMapping.Shards {
		shardsCopy[shardID] = shardInfo
	}

	return &ShardMapping{Shards: shardsCopy}
}

// GetCurrentNodeForObject returns the node that should handle the given object ID
// If the object ID contains a "/" separator (e.g., "localhost:7001/object-123"),
// the part before the first "/" is treated as a fixed node address and returned directly.
// This allows objects to be pinned to specific nodes, similar to client IDs.
// Otherwise, the object is assigned to a node based on consistent hashing.
func (cm *ConsensusManager) GetCurrentNodeForObject(objectID string) (string, error) {
	// Check if object ID specifies a fixed node address
	// Format: nodeAddress/actualObjectID (e.g., "localhost:7001/object-123")
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			// Fixed node address specified (first part before /)
			nodeAddr := parts[0]
			return nodeAddr, nil
		}
	}

	// Use the sharding logic to determine the node
	shardID := sharding.GetShardID(objectID)

	// Get shard mapping and check node existence under a single lock
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.ShardMapping == nil {
		return "", fmt.Errorf("shard mapping not available")
	}

	shardInfo, ok := cm.state.ShardMapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	// Only use CurrentNode for locating objects
	// TargetNode is only used for planning/migration, not for routing
	if shardInfo.CurrentNode == "" {
		return "", fmt.Errorf("shard %d has no current node (not yet claimed)", shardID)
	}

	// Verify that CurrentNode is in the active node list
	if !cm.state.HasNode(shardInfo.CurrentNode) {
		return "", fmt.Errorf("current node %s for shard %d is not in active node list", shardInfo.CurrentNode, shardID)
	}

	// Fail if TargetNode differs from CurrentNode (shard is in migration state)
	if shardInfo.TargetNode != "" && shardInfo.TargetNode != shardInfo.CurrentNode {
		return "", fmt.Errorf("shard %d is in migration from %s to %s", shardID, shardInfo.CurrentNode, shardInfo.TargetNode)
	}

	return shardInfo.CurrentNode, nil
}

// GetNodeForShard returns the node that owns the given shard
func (cm *ConsensusManager) GetNodeForShard(shardID int) (string, error) {
	if shardID < 0 || shardID >= sharding.NumShards {
		return "", fmt.Errorf("invalid shard ID: %d", shardID)
	}

	// Get shard mapping and check node existence under a single lock
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.ShardMapping == nil {
		return "", fmt.Errorf("shard mapping not available")
	}

	shardInfo, ok := cm.state.ShardMapping.Shards[shardID]
	if !ok {
		return "", fmt.Errorf("no node assigned to shard %d", shardID)
	}

	// Only use CurrentNode for locating shards
	// TargetNode is only used for planning/migration, not for routing
	if shardInfo.CurrentNode == "" {
		return "", fmt.Errorf("shard %d has no current node (not yet claimed)", shardID)
	}

	// Verify that CurrentNode is in the active node list
	if !cm.state.HasNode(shardInfo.CurrentNode) {
		return "", fmt.Errorf("current node %s for shard %d is not in active node list", shardInfo.CurrentNode, shardID)
	}

	return shardInfo.CurrentNode, nil
}

// storeShardMapping stores a new shard mapping in etcd and updates the in-memory state
// Each shard is stored as an individual key: /goverse/shard/<shard-id> with value in format "targetNode,currentNode"
// The function uses conditional puts based on ModRevision to ensure consistency.
// Uses a fixed worker pool to write multiple shards in parallel for better performance.
// Returns the number of successfully written shards, or an error if any write fails.
func (cm *ConsensusManager) storeShardMapping(ctx context.Context, updateShards map[int]ShardInfo) (int, error) {
	cm.logger.Infof("storing %d shard mapping", len(updateShards))
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

			// Log the transaction response for diagnostics
			cm.logger.Debugf("Txn commit %s = %s for shard %d succeeded: revision=%d", key, value, id, resp.Header.Revision)

			if !resp.Succeeded {
				// The condition failed - the shard was modified by another process
				// Retrieve current ModRevision for diagnostics
				var currentModRev int64
				getResp, getErr := client.Get(ctx, key)
				if getErr == nil && len(getResp.Kvs) > 0 {
					currentModRev = getResp.Kvs[0].ModRevision
				}
				cm.logger.Errorf("ModRevision mismatch for shard %d: expected %d, current %d", id, shardInfo.ModRevision, currentModRev)
				return fmt.Errorf("failed to store shard %d: ModRevision mismatch (expected %d, got %d)", id, shardInfo.ModRevision, currentModRev)
			} else {
				// Update in-memory state under lock. This allows us to keep ModRevision in sync before the watch event arrives.
				cm.mu.Lock()
				cm.recordShardMigrationLocked(id, shardInfo)
				if cm.state.ShardMapping.Shards[id].ModRevision < resp.Header.Revision {
					cm.state.ShardMapping.Shards[id] = ShardInfo{
						TargetNode:  shardInfo.TargetNode,
						CurrentNode: shardInfo.CurrentNode,
						ModRevision: resp.Header.Revision,
					}
				}
				cm.mu.Unlock()
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

// ClaimShardsForNode checks all shards and claims ownership for shards
// where the local node is the target AND CurrentNode is empty or not alive
// Only claims shards when cluster state is stable for the configured duration
func (cm *ConsensusManager) ClaimShardsForNode(ctx context.Context) error {
	// Lock cluster state to avoid race conditions
	clusterState, unlock := cm.LockClusterState()

	// Only claim shards when cluster state is stable
	if !cm.IsStateStable() {
		unlock()
		return fmt.Errorf("cluster state not stable, skipping shard claiming")
	}
	localNode := cm.localNodeAddress

	// Collect shards that need to be claimed
	shardsToUpdate := make(map[int]ShardInfo)
	for shardID, shardInfo := range clusterState.ShardMapping.Shards {
		// Claim shard if: TargetNode is this node AND (CurrentNode is empty or not alive)
		if shardInfo.TargetNode == localNode && (shardInfo.CurrentNode == "" || !clusterState.HasNode(shardInfo.CurrentNode)) {
			shardsToUpdate[shardID] = ShardInfo{
				TargetNode:  shardInfo.TargetNode,
				CurrentNode: localNode,
				ModRevision: shardInfo.ModRevision,
			}
		}
	}

	unlock()

	if len(shardsToUpdate) == 0 {
		cm.logger.Debugf("No shards to claim for node %s", localNode)
		return nil
	}

	cm.logger.Infof("Claiming ownership of %d shards for node %s", len(shardsToUpdate), localNode)

	// Acquire write locks on all shards being claimed to prevent concurrent operations
	// This ensures no CreateObject/CallObject can proceed while we're claiming ownership
	shardIDs := make([]int, 0, len(shardsToUpdate))
	for shardID := range shardsToUpdate {
		shardIDs = append(shardIDs, shardID)
	}

	// Acquire all locks in sorted order (handled by AcquireWriteMultiple)
	unlockAll := cm.shardLock.AcquireWriteMultiple(shardIDs)
	defer unlockAll()

	// Store all updated shards at once
	successCount, err := cm.storeShardMapping(ctx, shardsToUpdate)
	if err != nil {
		cm.logger.Warnf("Failed to claim some shards: claimed %d/%d, error: %v", successCount, len(shardsToUpdate), err)
		// Record successful claims even if there was a partial failure
		if successCount > 0 {
			metrics.RecordShardClaim(localNode, successCount)
		}
		return err
	}

	cm.logger.Infof("Successfully claimed ownership of %d shards", successCount)
	// Record successful shard claims
	metrics.RecordShardClaim(localNode, successCount)
	return nil
}

// calcReleaseShardsForNode calculates which shards need to be released based on:
// - CurrentNode is the given localNode
// - TargetNode is not empty and not this node (another node should own it)
// - There are no objects on this node for that shard (localObjectsPerShard map)
// Returns a map of shard IDs to new ShardInfo, or nil if no changes are needed.
func (cm *ConsensusManager) calcReleaseShardsForNode(localNode string, localObjectsPerShard map[int]int) map[int]ShardInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.ShardMapping == nil || len(cm.state.ShardMapping.Shards) == 0 {
		return nil
	}

	// Collect shards that need to be released
	shardsToUpdate := make(map[int]ShardInfo)
	for shardID, shardInfo := range cm.state.ShardMapping.Shards {
		// Release shard if:
		// 1. CurrentNode is this node
		// 2. TargetNode is not empty and not this node (it's another node)
		// 3. No objects on this node for that shard
		localObjectCount := localObjectsPerShard[shardID]
		if shardInfo.CurrentNode == localNode &&
			shardInfo.TargetNode != "" &&
			shardInfo.TargetNode != localNode &&
			localObjectCount == 0 {
			shardsToUpdate[shardID] = ShardInfo{
				TargetNode:  shardInfo.TargetNode,
				CurrentNode: "", // Clear CurrentNode to release ownership
				ModRevision: shardInfo.ModRevision,
			}
		}
	}

	if len(shardsToUpdate) == 0 {
		return nil
	}

	return shardsToUpdate
}

// ReleaseShardsForNode checks all shards and releases ownership for shards where:
// - CurrentNode is the local node
// - TargetNode is not empty and not this node (another node should own it)
// - There are no objects on this node for that shard (localObjectsPerShard map)
// It only releases shards when the cluster state is stable for the configured duration.
func (cm *ConsensusManager) ReleaseShardsForNode(ctx context.Context, localObjectsPerShard map[int]int) error {
	// Get localNode from stored configuration
	cm.mu.RLock()
	localNode := cm.localNodeAddress
	cm.mu.RUnlock()

	if localNode == "" {
		return fmt.Errorf("localNode cannot be empty")
	}

	// Check if cluster state is stable before releasing shards
	if !cm.IsStateStable() {
		cm.logger.Debugf("Cluster state not stable, skipping shard release for node %s", localNode)
		return nil
	}

	shardsToUpdate := cm.calcReleaseShardsForNode(localNode, localObjectsPerShard)

	if shardsToUpdate == nil {
		cm.logger.Debugf("No shards to release for node %s", localNode)
		return nil
	}

	cm.logger.Infof("Releasing ownership of %d shards for node %s", len(shardsToUpdate), localNode)

	// Acquire write locks on all shards being released to prevent concurrent operations
	// This ensures no CreateObject/CallObject can proceed while we're transferring ownership
	shardIDs := make([]int, 0, len(shardsToUpdate))
	for shardID := range shardsToUpdate {
		shardIDs = append(shardIDs, shardID)
	}

	// Acquire all locks in sorted order (handled by AcquireWriteMultiple)
	unlockAll := cm.shardLock.AcquireWriteMultiple(shardIDs)
	defer unlockAll()

	// Store all updated shards at once
	successCount, err := cm.storeShardMapping(ctx, shardsToUpdate)
	if err != nil {
		cm.logger.Warnf("Failed to release some shards: released %d/%d, error: %v", successCount, len(shardsToUpdate), err)
		// Record successful releases even if there was a partial failure
		if successCount > 0 {
			metrics.RecordShardRelease(localNode, successCount)
		}
		return err
	}

	cm.logger.Infof("Successfully released ownership of %d shards", successCount)
	// Record successful shard releases
	metrics.RecordShardRelease(localNode, successCount)
	return nil
}

// calcReassignShardTargetNodes computes which shards need to be reassigned based on current nodes.
// Returns a map of shard IDs to new ShardInfo, or nil if no changes are needed.
// This only updates TargetNode fields for shards whose current target is empty or no longer in the active node list.
func (cm *ConsensusManager) calcReassignShardTargetNodes() map[int]ShardInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.state.Nodes) == 0 {
		return nil
	}

	// Sort nodes for deterministic assignment
	nodes := slices.Sorted(maps.Keys(cm.state.Nodes))
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeSet[n] = true
	}

	// Check if any changes are needed
	updateShards := make(map[int]ShardInfo)
	var currentShards map[int]ShardInfo
	if cm.state.ShardMapping != nil {
		currentShards = cm.state.ShardMapping.Shards
	} else {
		// ShardMapping can be nil if ReassignShardTargetNodes is called before Initialize
		currentShards = make(map[int]ShardInfo)
	}

	for shardID := 0; shardID < sharding.NumShards; shardID++ {
		currentInfo := currentShards[shardID]
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
		return nil
	}

	return updateShards
}

// ReassignShardTargetNodes reassigns shard target nodes based on the current node list.
// This only updates TargetNode fields for shards whose current target is empty or no longer in the active node list.
// It does not modify CurrentNode fields - use ClaimShardsForNode for that.
// Returns the number of shards successfully updated and any error encountered.
func (cm *ConsensusManager) ReassignShardTargetNodes(ctx context.Context) (int, error) {
	updateShards := cm.calcReassignShardTargetNodes()

	if updateShards == nil {
		return 0, nil
	}

	cm.logger.Infof("Reassigning shard target nodes for %d shards", len(updateShards))
	n, err := cm.storeShardMapping(ctx, updateShards)
	cm.logger.Infof("Successfully updated shard mapping for %d shards - error %v", n, err)
	return n, err
}

// RebalanceShards checks if all shards are assigned and rebalances if there's significant imbalance.
// If all shards are assigned, finds the node with max shards (a) and min shards (b).
// If a >= b + 2 and a > 2*b, migrates shards from overloaded nodes to underloaded nodes.
// The function batches up to 100 shard migrations per call, checking imbalance conditions after each
// selection to avoid over-migrating. All collected shards are updated in a single storeShardMapping call.
// Returns true if a rebalance operation was performed, false otherwise, and any error encountered.
func (cm *ConsensusManager) RebalanceShards(ctx context.Context) (bool, error) {
	cm.mu.RLock()

	if len(cm.state.Nodes) == 0 {
		cm.mu.RUnlock()
		cm.logger.Debugf("No nodes available for rebalancing")
		return false, nil
	}

	if cm.state.ShardMapping == nil {
		cm.mu.RUnlock()
		cm.logger.Debugf("No shard mapping available")
		return false, nil
	}

	// Sort nodes for deterministic selection
	nodes := slices.Sorted(maps.Keys(cm.state.Nodes))

	// Count target shards per node (use TargetNode for rebalance decisions)
	shardCounts := make(map[string]int)
	shardsPerNode := make(map[string][]int) // Track which shards each node is targeted for
	for _, node := range nodes {
		shardCounts[node] = 0
		shardsPerNode[node] = []int{}
	}

	allAssigned := true
	for shardID := 0; shardID < sharding.NumShards; shardID++ {
		shardInfo, exists := cm.state.ShardMapping.Shards[shardID]
		// Require TargetNode to be assigned for all shards for rebalance to proceed
		if !exists || shardInfo.TargetNode == "" {
			allAssigned = false
			break
		}

		// Count this shard for its target node
		if _, nodeExists := shardCounts[shardInfo.TargetNode]; nodeExists {
			shardCounts[shardInfo.TargetNode]++
			shardsPerNode[shardInfo.TargetNode] = append(shardsPerNode[shardInfo.TargetNode], shardID)
		}
	}

	if !allAssigned {
		cm.mu.RUnlock()
		cm.logger.Debugf("Not all shards are assigned, skipping rebalance")
		return false, nil
	}

	// Collect shards to migrate (up to 100 per batch)
	const maxBatchSize = 100
	updateShards := make(map[int]ShardInfo)

	// Keep track of which shards we've already selected for migration
	selectedShards := make(map[int]bool)

	// Iterate up to maxBatchSize times to collect shards
	for batchCount := 0; batchCount < maxBatchSize; batchCount++ {
		// Find max and min shard counts based on current state
		var maxNode, minNode string
		maxCount := -1
		minCount := sharding.NumShards + 1

		for _, node := range nodes {
			count := shardCounts[node]
			if maxCount == -1 || count > maxCount {
				maxNode = node
				maxCount = count
			}
			if count < minCount {
				minNode = node
				minCount = count
			}
		}

		// Check rebalance conditions: a >= b + 2 and a > 2*b
		if maxCount < minCount+2 || maxCount <= 2*minCount {
			// No more imbalance to fix
			break
		}

		// Find available shards to migrate from maxNode (excluding already selected ones)
		var shardToMigrate int
		found := false
		for _, shardID := range shardsPerNode[maxNode] {
			if !selectedShards[shardID] {
				shardToMigrate = shardID
				found = true
				break
			}
		}

		if !found {
			// No more shards available to migrate from maxNode
			break
		}

		// Get the existing shard info
		existingInfo := cm.state.ShardMapping.Shards[shardToMigrate]

		// Add this shard to the batch
		updateShards[shardToMigrate] = ShardInfo{
			TargetNode:  minNode,
			CurrentNode: existingInfo.CurrentNode,
			ModRevision: existingInfo.ModRevision,
		}

		// Mark this shard as selected
		selectedShards[shardToMigrate] = true

		// Update local counts to reflect this migration
		shardCounts[maxNode]--
		shardCounts[minNode]++

		// Update shardsPerNode to reflect the change
		// Remove from maxNode's list
		for i, sid := range shardsPerNode[maxNode] {
			if sid == shardToMigrate {
				shardsPerNode[maxNode] = append(shardsPerNode[maxNode][:i], shardsPerNode[maxNode][i+1:]...)
				break
			}
		}
		// Add to minNode's list
		shardsPerNode[minNode] = append(shardsPerNode[minNode], shardToMigrate)
	}

	cm.mu.RUnlock()

	// If no shards were selected, cluster is balanced
	if len(updateShards) == 0 {
		cm.logger.Debugf("Cluster is balanced, no shards to migrate")
		return false, nil
	}

	cm.logger.Infof("Rebalancing: migrating %d shards in batch", len(updateShards))

	n, err := cm.storeShardMapping(ctx, updateShards)
	if err != nil {
		cm.logger.Errorf("Failed to rebalance shards: %v", err)
		return false, err
	}

	if n > 0 {
		cm.logger.Infof("Successfully initiated rebalance for %d shards", n)
		return true, nil
	}

	return false, nil
}

// IsStateStable returns true if the node list has not changed for the configured duration,
// has at least the minimum required number of nodes, and the local node is present in the cluster
func (cm *ConsensusManager) IsStateStable() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check if local node is in the cluster
	if !cm.state.HasNode(cm.localNodeAddress) {
		cm.logger.Debugf("Cluster state not stable: local node %s not in cluster", cm.localNodeAddress)
		return false
	}

	if !cm.state.IsStable(cm.clusterStateStabilityDuration) {
		return false
	}

	// Check if we have the minimum required nodes
	minQuorum := cm.getEffectiveMinQuorum()
	if len(cm.state.Nodes) < minQuorum {
		cm.logger.Debugf("Cluster state not stable: Only %d nodes available, minimum quorum required is %d", len(cm.state.Nodes), minQuorum)
		return false
	}

	return true
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

// LockClusterState acquires a read lock on the cluster state and returns the state and an unlock function.
// The caller must call the unlock function when done accessing the state.
// Usage:
//
//	state, unlock := cm.LockClusterState()
//	defer unlock()
//	// Access state safely here
func (cm *ConsensusManager) LockClusterState() (*ClusterState, func()) {
	cm.mu.RLock()
	return cm.state, func() {
		cm.mu.RUnlock()
	}
}

// GetClusterStateForTesting returns a cloned copy of the cluster state.
// This method should only be used in tests to get an independent copy that won't be affected by concurrent changes.
func (cm *ConsensusManager) GetClusterStateForTesting() *ClusterState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	startTime := time.Now()
	clonedState := cm.state.CloneForTesting()
	cm.logger.Infof("GetClusterStateForTesting Clone operation took %d us", time.Since(startTime).Microseconds())

	return clonedState
}

// GetObjectsToEvict returns the list of object IDs that should be evicted from the given node
// This is more efficient than cloning the entire cluster state
func (cm *ConsensusManager) GetObjectsToEvict(localAddr string, objectIDs []string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.state.ShardMapping == nil {
		return nil
	}

	// Check if this node is in the cluster
	if _, hasNode := cm.state.Nodes[localAddr]; !hasNode {
		return nil
	}

	var objectsToEvict []string

	for _, objectID := range objectIDs {
		// Skip client objects (those with "/" in ID) as they are pinned to nodes
		if strings.Contains(objectID, "/") {
			continue
		}

		// Get the shard for this object
		shardID := sharding.GetShardID(objectID)

		// Check if this shard belongs to this node
		shardInfo, exists := cm.state.ShardMapping.Shards[shardID]

		// Evict objects in the following cases:
		// 1. Shard does not exist in the mapping (orphaned shard)
		// 2. CurrentNode is not this node (object belongs to another node)
		// 3. TargetNode is not this node (object should migrate to another node)
		if !exists || shardInfo.CurrentNode != localAddr || shardInfo.TargetNode != localAddr {
			objectsToEvict = append(objectsToEvict, objectID)
		}
	}

	return objectsToEvict
}
