package consensusmanager

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/objectid"
	"github.com/xiaonanln/goverse/util/taskpool"
	"github.com/xiaonanln/goverse/util/workerpool"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// defaultClusterStateStabilityDuration is the default duration to consider the cluster state stable
	defaultClusterStateStabilityDuration = 10 * time.Second
	// defaultCleanShutdownTimeout is the default timeout for clean shutdown operations
	defaultCleanShutdownTimeout = 30 * time.Second
)

// ShutdownMode specifies how the node should shutdown
type ShutdownMode int

const (
	// ShutdownModeQuick keeps shard assignments and node registration for fast restart (default)
	ShutdownModeQuick ShutdownMode = iota
	// ShutdownModeClean releases shards and waits for handoff before unregistering
	ShutdownModeClean
)

// String returns the string representation of the shutdown mode
func (m ShutdownMode) String() string {
	switch m {
	case ShutdownModeQuick:
		return "quick"
	case ShutdownModeClean:
		return "clean"
	default:
		return "unknown"
	}
}

// ShardInfo contains information about a shard's node assignment
type ShardInfo struct {
	// TargetNode is the node that should handle this shard
	TargetNode string
	// CurrentNode is the node currently handling this shard
	// Empty initially - will be populated when shard migration/handoff logic is implemented
	// to enable tracking of active shard transfers during cluster rebalancing
	CurrentNode string
	// Flags contains flag values (e.g., ["pinned"] for f=pinned)
	// Used to indicate special handling for shards (e.g., pinned assignment prevents rebalancing)
	Flags []string

	// modRevision is the etcd revision when this shard info was last modified
	ModRevision int64
}

// HasFlag checks if the shard has the specified flag
func (si *ShardInfo) HasFlag(flag string) bool {
	for _, f := range si.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// Clone returns a deep copy of the ShardInfo
func (si ShardInfo) Clone() ShardInfo {
	// Create a defensive copy of the flags slice to prevent unintended mutations
	var flagsCopy []string
	if si.Flags != nil {
		flagsCopy = make([]string, len(si.Flags))
		copy(flagsCopy, si.Flags)
	}
	return ShardInfo{
		TargetNode:  si.TargetNode,
		CurrentNode: si.CurrentNode,
		ModRevision: si.ModRevision,
		Flags:       flagsCopy,
	}
}

// WithCurrentNode returns a copy with updated CurrentNode, preserving all other fields
func (si ShardInfo) WithCurrentNode(node string) ShardInfo {
	cloned := si.Clone()
	cloned.CurrentNode = node
	return cloned
}

// WithTargetNode returns a copy with updated TargetNode, preserving all other fields
func (si ShardInfo) WithTargetNode(node string) ShardInfo {
	cloned := si.Clone()
	cloned.TargetNode = node
	return cloned
}

// ShardMapping represents the mapping of shards to nodes
// Note: Nodes and Version fields have been moved to ClusterState in consensusmanager
type ShardMapping struct {
	// Map from shard ID to shard info (target and current node)
	Shards map[int]ShardInfo `json:"shards"`
}

func (sm *ShardMapping) IsFullyAssignedAndClaimed(numShards int) bool {
	if len(sm.Shards) != numShards {
		return false
	}
	// Check that all shards have both TargetNode and CurrentNode set
	for _, shardInfo := range sm.Shards {
		if shardInfo.TargetNode == "" || shardInfo.CurrentNode == "" {
			return false
		}
	}
	return true
}

// AllShardsHaveMatchingCurrentAndTarget returns true if all shards have their
// CurrentNode equal to their TargetNode, meaning no shards are in migration state.
// This indicates that the cluster has fully stabilized and all shard migrations are complete.
func (sm *ShardMapping) AllShardsHaveMatchingCurrentAndTarget(numShards int) bool {
	// First check if all shards are fully assigned and claimed
	if !sm.IsFullyAssignedAndClaimed(numShards) {
		return false
	}
	// Then check that all shards have matching CurrentNode and TargetNode
	for _, shardInfo := range sm.Shards {
		if shardInfo.TargetNode != shardInfo.CurrentNode {
			return false
		}
	}
	return true
}

// ClusterState represents the current state of the cluster
type ClusterState struct {
	Nodes             map[string]bool
	Gates             map[string]bool
	ShardMapping      *ShardMapping
	Leader            string // current leader address from /leader key
	LeaderModRevision int64  // ModRevision of /leader key for Txn
	Revision          int64
	LastChange        time.Time
}

// HasNode returns true if the given node address exists in the cluster state.
func (cs *ClusterState) HasNode(nodeAddr string) bool {
	if cs == nil {
		return false
	}
	_, ok := cs.Nodes[nodeAddr]
	return ok
}

// HasGate returns true if the given gate address exists in the cluster state.
func (cs *ClusterState) HasGate(gateAddr string) bool {
	if cs == nil {
		return false
	}
	_, ok := cs.Gates[gateAddr]
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
	numShards                     int           // number of shards in the cluster
	rebalanceShardsBatchSize      atomic.Int32  // maximum number of shards to migrate in a single rebalance operation
	imbalanceThreshold            float64       // threshold for shard imbalance as a fraction of ideal load

	// Watch management
	watchCtx     context.Context
	watchCancel  context.CancelFunc
	watchStarted bool

	// Listeners
	listenersMu sync.RWMutex
	listeners   []StateChangeListener
}

// NewConsensusManager creates a new consensus manager
func NewConsensusManager(etcdMgr *etcdmanager.EtcdManager, shardLock *shardlock.ShardLock, clusterStateStabilityDuration time.Duration, localNodeAddress string, numShards int) *ConsensusManager {
	if clusterStateStabilityDuration <= 0 {
		clusterStateStabilityDuration = defaultClusterStateStabilityDuration
	}
	if numShards <= 0 {
		numShards = sharding.NumShards
	}
	cm := &ConsensusManager{
		etcdManager:                   etcdMgr,
		logger:                        logger.NewLogger("ConsensusManager"),
		shardLock:                     shardLock,
		clusterStateStabilityDuration: clusterStateStabilityDuration,
		localNodeAddress:              localNodeAddress,
		numShards:                     numShards,
		imbalanceThreshold:            0.2, // Default value
		state: &ClusterState{
			Nodes: make(map[string]bool),
			ShardMapping: &ShardMapping{
				Shards: make(map[int]ShardInfo),
			},
			// LastChange and Revision are zero initially, set when loading from etcd
		},
		listeners: make([]StateChangeListener, 0),
	}
	// Set default batch size
	cm.rebalanceShardsBatchSize.Store(int32(cm.calculateDefaultRebalanceShardsBatchSize()))
	return cm
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

	if cm.state.ShardMapping == nil || !cm.state.ShardMapping.IsFullyAssignedAndClaimed(cm.numShards) {
		cm.logger.Warnf("ConsensusManager not ready: Shard mapping not fully assigned and claimed")
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

// calculateDefaultRebalanceShardsBatchSize calculates the default batch size based on number of shards
func (cm *ConsensusManager) calculateDefaultRebalanceShardsBatchSize() int {
	return max(1, cm.numShards/128)
}

// SetRebalanceShardsBatchSize sets the maximum number of shards to migrate in a single rebalance operation
// If batchSize <= 0, the default value of max(1, numShards/128) is used
func (cm *ConsensusManager) SetRebalanceShardsBatchSize(batchSize int) {
	if batchSize <= 0 {
		defaultSize := cm.calculateDefaultRebalanceShardsBatchSize()
		if batchSize == 0 {
			cm.logger.Infof("Using default batch size %d", defaultSize)
		} else {
			cm.logger.Infof("Invalid batch size %d, using default %d", batchSize, defaultSize)
		}
		batchSize = defaultSize
	}
	cm.rebalanceShardsBatchSize.Store(int32(batchSize))
	cm.logger.Infof("ConsensusManager rebalance shards batch size set to %d", batchSize)
}

// GetRebalanceShardsBatchSize returns the current batch size for shard rebalancing
func (cm *ConsensusManager) GetRebalanceShardsBatchSize() int {
	return int(cm.rebalanceShardsBatchSize.Load())
}

// SetImbalanceThreshold sets the threshold for shard imbalance as a fraction of ideal load
// If threshold <= 0, the default value of 0.2 is used
func (cm *ConsensusManager) SetImbalanceThreshold(threshold float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if threshold <= 0 {
		cm.logger.Infof("Invalid imbalance threshold %f, using default 0.2", threshold)
		threshold = 0.2
	}
	cm.imbalanceThreshold = threshold
	cm.logger.Infof("ConsensusManager imbalance threshold set to %f", threshold)
}

// GetImbalanceThreshold returns the current imbalance threshold
func (cm *ConsensusManager) GetImbalanceThreshold() float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.imbalanceThreshold
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

// GetNumShards returns the number of shards in the cluster
func (cm *ConsensusManager) GetNumShards() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.numShards
}

// notifyStateChanged notifies all listeners about cluster state changes
// Uses taskpool to avoid blocking the caller and prevent deadlocks
func (cm *ConsensusManager) notifyStateChanged() {
	taskpool.Submit(func(ctx context.Context) {
		cm.listenersMu.RLock()
		listeners := make([]StateChangeListener, len(cm.listeners))
		copy(listeners, cm.listeners)
		cm.listenersMu.RUnlock()

		for _, listener := range listeners {
			listener.OnClusterStateChanged()
		}
	})
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

	// Capture the context for this watch session so the goroutine is tied
	// to a single lifecycle even if StopWatch/StartWatch is called quickly.
	watchCtx := cm.watchCtx

	// Start watching the entire /goverse prefix
	prefix := cm.etcdManager.GetPrefix()
	go cm.watchPrefix(watchCtx, prefix)

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

// watchPrefix watches the entire etcd prefix for changes.
// If the watch channel closes (e.g. network disconnect, etcd restart, compaction),
// it reloads the full cluster state and re-establishes the watch with exponential backoff.
func (cm *ConsensusManager) watchPrefix(ctx context.Context, prefix string) {
	const (
		initialBackoff = 1 * time.Second
		maxBackoff     = 30 * time.Second
	)
	backoff := initialBackoff

	for {
		err := cm.watchPrefixOnce(ctx, prefix)
		if err == nil {
			// Context was cancelled — clean shutdown
			return
		}

		// Check if context is done before retrying
		select {
		case <-ctx.Done():
			cm.logger.Infof("Watch stopped during reconnection")
			return
		default:
		}

		cm.logger.Errorf("Watch disconnected: %v — reconnecting in %v", err, backoff)

		// Wait with backoff
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			cm.logger.Infof("Watch stopped during backoff")
			return
		case <-timer.C:
		}

		// Reload full state before re-establishing watch
		cm.logger.Infof("Reloading cluster state before watch reconnection")
		state, loadErr := cm.loadClusterStateFromEtcd(ctx)
		if loadErr != nil {
			cm.logger.Errorf("Failed to reload cluster state: %v", loadErr)
			// Increase backoff and retry
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		cm.mu.Lock()
		cm.state = state
		cm.mu.Unlock()
		cm.logger.Infof("Cluster state reloaded at revision %d", state.Revision)

		// Notify listeners of the refreshed state
		cm.notifyStateChanged()

		// Reset backoff on successful reload
		backoff = initialBackoff
	}
}

// watchPrefixOnce runs a single watch session. Returns nil if context was cancelled
// (clean shutdown), or an error describing why the watch ended.
func (cm *ConsensusManager) watchPrefixOnce(ctx context.Context, prefix string) error {
	client := cm.etcdManager.GetClient()
	if client == nil {
		return fmt.Errorf("etcd client not available")
	}

	// Get the revision from the loaded state to prevent missing events
	cm.mu.RLock()
	revision := cm.state.Revision
	cm.mu.RUnlock()

	nodesPrefix := cm.etcdManager.GetPrefix() + "/nodes/"
	gatesPrefix := cm.etcdManager.GetPrefix() + "/gates/"
	shardPrefix := prefix + "/shard/"
	leaderKey := prefix + "/leader"

	// Watch from the next revision after our load to prevent missing events
	watchChan := client.Watch(ctx, prefix, clientv3.WithPrefix(), clientv3.WithRev(revision+1))

	cm.logger.Infof("Started watching prefix %s from revision %d", prefix, revision+1)
	for {
		select {
		case <-ctx.Done():
			cm.logger.Infof("Watch stopped")
			return nil
		case watchResp, ok := <-watchChan:
			if !ok {
				// etcd closes the watch channel when the context is cancelled,
				// so check whether this is a clean shutdown before treating it
				// as an unexpected disconnect.
				if ctx.Err() != nil {
					cm.logger.Infof("Watch stopped (context cancelled)")
					return nil
				}
				return fmt.Errorf("watch channel closed")
			}
			if watchResp.Err() != nil {
				return fmt.Errorf("watch error: %w", watchResp.Err())
			}

			for _, event := range watchResp.Events {
				key := string(event.Kv.Key)

				cm.logger.Debugf("Received watch event: %s %s=%s rev %v", event.Type.String(), key, event.Kv.Value, event.Kv.ModRevision)
				// Handle leader key changes
				if key == leaderKey {
					cm.handleLeaderEvent(event)
				} else if len(key) > len(nodesPrefix) && key[:len(nodesPrefix)] == nodesPrefix {
					// Handle node changes
					cm.handleNodeEvent(event, nodesPrefix)
				} else if len(key) > len(gatesPrefix) && key[:len(gatesPrefix)] == gatesPrefix {
					// Handle gate changes
					cm.handleGateEvent(event, gatesPrefix)
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
		cm.notifyStateChanged()

	case clientv3.EventTypeDelete:
		// Extract node address from the key
		key := string(event.Kv.Key)
		nodeAddress := key[len(nodesPrefix):]
		delete(cm.state.Nodes, nodeAddress)
		cm.state.LastChange = time.Now()
		cm.logger.Infof("Node removed: %s", nodeAddress)
		cm.mu.Unlock()
		cm.notifyStateChanged()
	default:
		cm.mu.Unlock()
	}
}

// handleGateEvent processes gate addition/removal events
func (cm *ConsensusManager) handleGateEvent(event *clientv3.Event, gatesPrefix string) {
	cm.mu.Lock()

	switch event.Type {
	case clientv3.EventTypePut:
		gateAddress := string(event.Kv.Value)
		cm.state.Gates[gateAddress] = true
		cm.state.LastChange = time.Now()
		cm.logger.Infof("Gate added: %s", gateAddress)
		cm.mu.Unlock()
		cm.notifyStateChanged()

	case clientv3.EventTypeDelete:
		// Extract gate address from the key
		key := string(event.Kv.Key)
		gateAddress := key[len(gatesPrefix):]
		delete(cm.state.Gates, gateAddress)
		cm.state.LastChange = time.Now()
		cm.logger.Infof("Gate removed: %s", gateAddress)
		cm.mu.Unlock()
		cm.notifyStateChanged()
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

	if shardID < 0 || shardID >= cm.numShards {
		cm.logger.Debugf("Invalid shard ID %d from key %s", shardID, key)
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
		cm.notifyStateChanged()
	} else if event.Type == clientv3.EventTypeDelete {
		if event.Kv.ModRevision <= cm.state.ShardMapping.Shards[shardID].ModRevision {
			return
		}
		delete(cm.state.ShardMapping.Shards, shardID)
		cm.logger.Debugf("Shard %d mapping deleted", shardID)
		cm.notifyStateChanged()
	}
}

// handleLeaderEvent processes leader key changes
func (cm *ConsensusManager) handleLeaderEvent(event *clientv3.Event) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if event.Kv.ModRevision <= cm.state.LeaderModRevision {
		return
	}

	if event.Type == clientv3.EventTypeDelete {
		cm.state.Leader = ""
		cm.state.LeaderModRevision = 0
		cm.logger.Infof("Leader key deleted")
	} else {
		cm.state.Leader = string(event.Kv.Value)
		cm.state.LeaderModRevision = event.Kv.ModRevision
		cm.logger.Infof("Leader updated to: %s", cm.state.Leader)
	}
	cm.state.LastChange = time.Now()
	cm.notifyStateChanged()
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
	value := string(kv.Value)
	parts := strings.Split(value, ",")

	var targetNode, currentNode string
	var flags []string
	nodePartCount := 0

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)

		// Check if this is a flag (starts with "f=")
		if strings.HasPrefix(trimmed, "f=") {
			flagValue := strings.TrimPrefix(trimmed, "f=")
			if flagValue != "" {
				flags = append(flags, flagValue)
			}
			continue
		}

		// This is a node part (including empty strings)
		if nodePartCount == 0 {
			targetNode = trimmed // Can be empty
			nodePartCount++
		} else if nodePartCount == 1 {
			currentNode = trimmed // Can be empty
			nodePartCount++
		} else {
			// More than 2 node parts - log warning
			if trimmed != "" {
				log.Printf("WARNING: parseShardInfo: too many node parts in value %q, ignoring extra parts", value)
			}
		}
	}

	return ShardInfo{
		TargetNode:  targetNode,
		CurrentNode: currentNode,
		Flags:       flags,
		ModRevision: kv.ModRevision,
	}
}

// formatShardInfo formats shard information for etcd storage
// Format: "targetNode,currentNode" or "targetNode,currentNode,f=flag1,f=flag2"
func formatShardInfo(info ShardInfo) string {
	result := info.TargetNode + "," + info.CurrentNode
	if len(info.Flags) > 0 {
		// Prefix each flag with "f="
		for _, flag := range info.Flags {
			flag = strings.TrimSpace(flag)
			if flag != "" {
				result += ",f=" + flag
			}
		}
	}
	return result
}

// loadClusterStateFromEtcd loads all cluster data at once from etcd
func (cm *ConsensusManager) loadClusterStateFromEtcd(ctx context.Context) (*ClusterState, error) {
	client := cm.etcdManager.GetClient()
	if client == nil {
		return nil, fmt.Errorf("etcd client not connected")
	}

	prefix := cm.etcdManager.GetPrefix()

	// Ensure context has a deadline for etcd operation
	ctx, cancel := etcdmanager.WithEtcdDeadline(ctx)
	defer cancel()

	// Load ALL cluster data in one transaction
	resp, err := client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster state: %w", err)
	}

	state := &ClusterState{
		Nodes: make(map[string]bool),
		Gates: make(map[string]bool),
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
		Revision:   resp.Header.Revision,
		LastChange: time.Now(),
	}

	nodesPrefix := cm.etcdManager.GetPrefix() + "/nodes/"
	gatesPrefix := cm.etcdManager.GetPrefix() + "/gates/"
	shardPrefix := prefix + "/shard/"
	leaderKey := prefix + "/leader"

	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if key == leaderKey {
			state.Leader = string(kv.Value)
			state.LeaderModRevision = kv.ModRevision
		} else if strings.HasPrefix(key, nodesPrefix) {
			state.Nodes[string(kv.Value)] = true
		} else if strings.HasPrefix(key, gatesPrefix) {
			state.Gates[string(kv.Value)] = true
		} else if strings.HasPrefix(key, shardPrefix) {
			// Parse shard ID from key
			shardIDStr := key[len(shardPrefix):]
			shardID, err := strconv.Atoi(shardIDStr)
			if err != nil {
				cm.logger.Warnf("Failed to parse shard ID from key %s: %v", key, err)
				continue
			}

			if shardID < 0 || shardID >= cm.numShards {
				cm.logger.Debugf("Invalid shard ID %d from key %s", shardID, key)
				continue
			}

			state.ShardMapping.Shards[shardID] = parseShardInfo(kv)
		}
	}

	cm.logger.Infof("Loaded %d nodes, %d gates, %d shards, and leader=%s from etcd", len(state.Nodes), len(state.Gates), len(state.ShardMapping.Shards), state.Leader)

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

// GetGates returns a copy of the current gate list
func (cm *ConsensusManager) GetGates() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	gates := make([]string, 0, len(cm.state.Gates))
	for gate := range cm.state.Gates {
		gates = append(gates, gate)
	}
	return gates
}

// GetLeaderNode returns the leader node address from the cluster state
func (cm *ConsensusManager) GetLeaderNode() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.state.Leader
}

// TryBecomeLeader attempts to become the leader if no leader exists or the current leader is not alive
func (cm *ConsensusManager) TryBecomeLeader(ctx context.Context) error {
	cm.mu.RLock()
	currentLeader := cm.state.Leader
	leaderAlive := cm.state.Nodes[currentLeader]
	leaderModRevision := cm.state.LeaderModRevision
	selfAlive := cm.state.Nodes[cm.localNodeAddress]
	cm.mu.RUnlock()

	// Leader is alive, do nothing
	if currentLeader != "" && leaderAlive {
		return nil
	}

	// Already the leader, do nothing
	if currentLeader == cm.localNodeAddress {
		return nil
	}

	// Don't attempt to become leader if we're not in the cluster ourselves
	if !selfAlive {
		return fmt.Errorf("not attempting to become leader: node %s is not in the cluster", cm.localNodeAddress)
	}

	client := cm.etcdManager.GetClient()
	if client == nil {
		return fmt.Errorf("etcd client is not available")
	}

	leaderKey := cm.etcdManager.GetPrefix() + "/leader"

	// Ensure context has a deadline for etcd operation
	txnCtx, cancel := etcdmanager.WithEtcdDeadline(ctx)
	defer cancel()

	// Only write if key hasn't changed since we last saw it
	cm.logger.Infof("Attempting to become leader (current leader: %s, alive: %v, mod_revision: %d)", currentLeader, leaderAlive, leaderModRevision)
	txnResp, err := client.Txn(txnCtx).
		If(clientv3.Compare(clientv3.ModRevision(leaderKey), "=", leaderModRevision)).
		Then(clientv3.OpPut(leaderKey, cm.localNodeAddress)).
		Commit()

	if err != nil {
		return err
	}

	if !txnResp.Succeeded {
		// Someone else updated it first, watch will update our state
		return nil
	}

	// Successfully became leader, update in-memory state
	cm.mu.Lock()
	if cm.state.LeaderModRevision < txnResp.Header.Revision {
		cm.state.Leader = cm.localNodeAddress
		cm.state.LeaderModRevision = txnResp.Header.Revision
	}
	cm.mu.Unlock()

	// Notify listeners
	cm.notifyStateChanged()

	cm.logger.Infof("This node became leader: %s", cm.localNodeAddress)
	return nil
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
// Supports three formats:
// 1. Fixed-shard format: "shard#<shardID>/<objectID>" - maps to specific shard via shard mapping
// 2. Fixed-node format: "<nodeAddress>/<objectID>" - routes directly to specified node
// 3. Regular format: any other ID - uses hash-based shard assignment
func (cm *ConsensusManager) GetCurrentNodeForObject(objectID string) (string, error) {
	// Parse and validate the object ID format
	parsed, err := objectid.ValidateObjectID(objectID, cm.numShards)
	if err != nil {
		return "", err
	}

	// Handle fixed-node format - route directly to the specified node
	if parsed.IsFixedNodeFormat() {
		return parsed.NodeAddress, nil
	}

	// Use the sharding logic to determine the node
	// This handles both regular IDs and fixed-shard format (shard#<shardID>/...)
	shardID := sharding.GetShardID(objectID, cm.numShards)

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
	if shardID < 0 || shardID >= cm.numShards {
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

// categorizeEtcdError categorizes an etcd error into one of the predefined error types
// for metrics tracking: timeout, connection_error, or other
func categorizeEtcdError(err error) string {
	if err == nil {
		return "other"
	}

	// Check for context errors (timeout/cancellation)
	if err == context.DeadlineExceeded {
		return "timeout"
	}
	if err == context.Canceled {
		return "timeout"
	}

	// Check error message for common connection errors
	errMsg := err.Error()
	if strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "network is unreachable") ||
		strings.Contains(errMsg, "etcd client not connected") {
		return "connection_error"
	}

	// Check for context deadline exceeded in error message
	if strings.Contains(errMsg, "deadline exceeded") ||
		strings.Contains(errMsg, "context deadline exceeded") {
		return "timeout"
	}

	return "other"
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

	// Calculate worker pool size based on numShards (numShards/512, minimum 1)
	poolSize := cm.numShards / 512
	if poolSize < 1 {
		poolSize = 1
	}

	// Create and start a fixed worker pool
	pool := workerpool.New(ctx, poolSize)
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

			// Ensure context has a deadline for etcd operation
			txnCtx, cancel := etcdmanager.WithEtcdDeadline(ctx)
			defer cancel()

			// Build conditional transaction based on ModRevision
			// Always check that ModRevision matches expected value (0 for new shards)
			txn := client.Txn(txnCtx).
				If(clientv3.Compare(clientv3.ModRevision(key), "=", shardInfo.ModRevision)).
				Then(clientv3.OpPut(key, value))

			resp, err := txn.Commit()

			if err != nil {
				// Categorize error and record metric
				errorType := categorizeEtcdError(err)
				metrics.RecordShardMappingWriteFailure(errorType)
				return fmt.Errorf("failed to store shard %d: %w", id, err)
			}

			// Log the transaction response for diagnostics
			cm.logger.Debugf("Txn commit %s = %s for shard %d succeeded: revision=%d", key, value, id, resp.Header.Revision)

			if !resp.Succeeded {
				// The condition failed - the shard was modified by another process
				// Record ModRevision conflict metric
				metrics.RecordShardMappingWriteFailure("modrevision_conflict")

				// Retrieve current ModRevision for diagnostics with deadline
				getCtx, getCancel := etcdmanager.WithEtcdDeadline(ctx)
				defer getCancel()
				var currentModRev int64
				getResp, getErr := client.Get(getCtx, key)
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
						Flags:       shardInfo.Flags,
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

	// Don't update LastChange here - this is a self-initiated change by the leader
	// The cluster state is stable from this node's perspective since it controls the change
	// Watch events will update other nodes' state, and they will track LastChange appropriately
	if successCount > 0 {
		cm.notifyStateChanged()
	}

	if firstError != nil {
		return successCount, firstError
	}

	return successCount, nil
}

// ClaimShardsForNode checks all shards and claims ownership for shards
// where the local node is the target AND CurrentNode is empty or not alive
// Only claims shards when cluster state is stable for the configured duration
func (cm *ConsensusManager) ClaimShardsForNode(ctx context.Context) error {
	// Only claim shards when cluster state is stable
	if !cm.IsStateStable() {
		return fmt.Errorf("cluster state not stable, skipping shard claiming")
	}

	// Lock cluster state to avoid race conditions
	clusterState, unlock := cm.LockClusterState()

	localNode := cm.localNodeAddress

	// Collect shards that need to be claimed
	shardsToUpdate := make(map[int]ShardInfo)
	for shardID, shardInfo := range clusterState.ShardMapping.Shards {
		// Claim shard if: TargetNode is this node AND (CurrentNode is empty or not alive)
		if shardInfo.TargetNode == localNode && (shardInfo.CurrentNode == "" || !clusterState.HasNode(shardInfo.CurrentNode)) {
			shardsToUpdate[shardID] = shardInfo.WithCurrentNode(localNode)
		}
	}

	// Release lock before storing to avoid deadlock (storeShardMapping needs write lock)
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
			shardsToUpdate[shardID] = shardInfo.WithCurrentNode("")
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

	for shardID := 0; shardID < cm.numShards; shardID++ {
		currentInfo := currentShards[shardID]

		// Check if shard has pinned flag
		isPinned := currentInfo.HasFlag("pinned")

		// If shard is pinned and TargetNode is set (even if node is not alive), don't change it
		if isPinned && currentInfo.TargetNode != "" {
			continue
		}

		if !nodeSet[currentInfo.TargetNode] {
			// If TargetNode is empty but CurrentNode is already set to a valid node,
			// respect the existing assignment and set TargetNode to CurrentNode
			var targetNode string
			if currentInfo.TargetNode == "" && currentInfo.CurrentNode != "" && nodeSet[currentInfo.CurrentNode] {
				targetNode = currentInfo.CurrentNode
			} else {
				// Assign to a new node using round-robin
				nodeIdx := shardID % len(nodes)
				targetNode = nodes[nodeIdx]
			}
			updateShards[shardID] = currentInfo.WithTargetNode(targetNode)
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

// StoreShardMapping stores shard mapping updates in etcd.
// This is an exported wrapper around storeShardMapping for use by external components (e.g., inspector).
// It allows manual shard assignment by updating TargetNode fields.
// Returns the number of shards successfully updated and any error encountered.
func (cm *ConsensusManager) StoreShardMapping(ctx context.Context, updateShards map[int]ShardInfo) (int, error) {
	return cm.storeShardMapping(ctx, updateShards)
}

// RebalanceShards checks if all shards are assigned and rebalances if there's significant imbalance.
// If all shards are assigned, finds the node with max shards (a) and min shards (b).
// If a >= b + 2 and the imbalance exceeds 20% of the ideal load per node, migrates shards
// from overloaded nodes to underloaded nodes.
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
	for shardID := 0; shardID < cm.numShards; shardID++ {
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

	// Collect shards to migrate (up to configured batch size per batch)
	maxBatchSize := int(cm.rebalanceShardsBatchSize.Load())
	updateShards := make(map[int]ShardInfo)

	// Keep track of which shards we've already selected for migration
	selectedShards := make(map[int]bool)

	// Iterate up to maxBatchSize times to collect shards
	for batchCount := 0; batchCount < maxBatchSize; batchCount++ {
		// Find max and min shard counts based on current state
		var maxNode, minNode string
		maxCount := -1
		minCount := cm.numShards + 1

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

		// Check rebalance conditions: a >= b + 2 and imbalance > threshold * ideal load
		// Ideal load per node is numShards / numNodes
		// We rebalance if: maxCount >= minCount + 2 AND (maxCount - minCount) > imbalanceThreshold * idealLoad
		// Note: len(nodes) is guaranteed to be > 0 by the check at the start of this function
		idealLoad := float64(cm.numShards) / float64(len(nodes))
		imbalanceThreshold := cm.imbalanceThreshold * idealLoad
		if maxCount < minCount+2 || float64(maxCount-minCount) <= imbalanceThreshold {
			// No more imbalance to fix
			break
		}

		// Find available shards to migrate from maxNode (excluding already selected ones and pinned shards)
		var shardToMigrate int
		found := false
		for _, shardID := range shardsPerNode[maxNode] {
			if !selectedShards[shardID] {
				// Check if shard is pinned - skip if it is
				existingInfo := cm.state.ShardMapping.Shards[shardID]
				if existingInfo.HasFlag("pinned") {
					continue
				}
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
		updateShards[shardToMigrate] = existingInfo.WithTargetNode(minNode)

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

	// Check if local address (node or gate) is in the cluster
	// Gates should check the Gates map, nodes should check the Nodes map
	isNode := cm.state.HasNode(cm.localNodeAddress)
	isGate := cm.state.HasGate(cm.localNodeAddress)
	if !isNode && !isGate {
		cm.logger.Debugf("Cluster state not stable: local address %s not in cluster", cm.localNodeAddress)
		return false
	}

	if !cm.state.IsStable(cm.clusterStateStabilityDuration) {
		return false
	}

	// Check if we have the minimum required nodes (only for nodes, not gates)
	if isNode {
		minQuorum := cm.getEffectiveMinQuorum()
		if len(cm.state.Nodes) < minQuorum {
			cm.logger.Debugf("Cluster state not stable: Only %d nodes available, minimum quorum required is %d", len(cm.state.Nodes), minQuorum)
			return false
		}
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
		shardID := sharding.GetShardID(objectID, cm.numShards)

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

func (cm *ConsensusManager) GetClusterStateStabilityDurationForTesting() time.Duration {
	return cm.clusterStateStabilityDuration
}

// Shutdown gracefully shuts down the consensus manager with the specified mode.
// 
// ShutdownModeQuick: Stop immediately, keeping shard assignments for fast restart
// ShutdownModeClean: Release owned shards, wait for handoff, then allow normal shutdown
//
// For clean shutdown, this method will:
// 1. Release all shards owned by this node (set CurrentNode to empty)
// 2. Wait for other nodes to claim them (with timeout)  
// 3. Return to allow normal unregistration to proceed
//
// The method respects context cancellation and has a built-in timeout for clean shutdowns.
func (cm *ConsensusManager) Shutdown(ctx context.Context, mode ShutdownMode) error {
	cm.logger.Infof("Starting shutdown in %s mode", mode)
	
	if mode == ShutdownModeQuick {
		cm.logger.Infof("Quick shutdown: no shard release needed")
		return nil
	}
	
	if mode != ShutdownModeClean {
		return fmt.Errorf("unsupported shutdown mode: %s", mode)
	}
	
	// Clean shutdown: release shards and wait for handoff
	return cm.performCleanShutdown(ctx)
}

// performCleanShutdown releases all shards owned by this node and waits for handoff
func (cm *ConsensusManager) performCleanShutdown(ctx context.Context) error {
	cm.mu.RLock()
	localNode := cm.localNodeAddress
	cm.mu.RUnlock()
	
	if localNode == "" {
		return fmt.Errorf("local node address not set")
	}
	
	// Add timeout to context if none exists
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCleanShutdownTimeout)
		defer cancel()
	}
	
	cm.logger.Infof("Clean shutdown: releasing shards owned by node %s", localNode)
	
	// Step 1: Identify and release shards owned by this node
	shardsToRelease := cm.identifyOwnedShards(localNode)
	if len(shardsToRelease) == 0 {
		cm.logger.Infof("Clean shutdown: no shards to release")
		return nil
	}
	
	cm.logger.Infof("Clean shutdown: releasing %d shards", len(shardsToRelease))
	
	// Step 2: Release the shards (set CurrentNode to empty)
	releasedCount, err := cm.storeShardMapping(ctx, shardsToRelease)
	if err != nil {
		cm.logger.Warnf("Clean shutdown: failed to release some shards: %d/%d released, error: %v", 
			releasedCount, len(shardsToRelease), err)
		// Continue with waiting for whatever we did release
	} else {
		cm.logger.Infof("Clean shutdown: successfully released %d shards", releasedCount)
	}
	
	// Step 3: Wait for other nodes to claim the released shards
	// Only wait for shards that were actually written to etcd successfully
	if releasedCount > 0 {
		releasedShardIDs := make([]int, 0, releasedCount)
		for shardID := range shardsToRelease {
			// Check if this shard was actually released (CurrentNode is now empty in our state)
			cm.mu.RLock()
			if cm.state.ShardMapping != nil {
				if info, ok := cm.state.ShardMapping.Shards[shardID]; ok && info.CurrentNode == "" {
					releasedShardIDs = append(releasedShardIDs, shardID)
				}
			}
			cm.mu.RUnlock()
		}
		
		err := cm.waitForShardHandoff(ctx, releasedShardIDs)
		if err != nil {
			cm.logger.Warnf("Clean shutdown: handoff wait completed with issues: %v", err)
			// Don't fail shutdown due to handoff timeout - allow unregistration to proceed
		} else {
			cm.logger.Infof("Clean shutdown: all released shards have been claimed")
		}
	}
	
	cm.logger.Infof("Clean shutdown: shard release phase completed")
	return nil
}

// identifyOwnedShards finds all shards where CurrentNode matches the local node
func (cm *ConsensusManager) identifyOwnedShards(localNode string) map[int]ShardInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	if cm.state.ShardMapping == nil {
		return nil
	}
	
	shardsToRelease := make(map[int]ShardInfo)
	for shardID, shardInfo := range cm.state.ShardMapping.Shards {
		if shardInfo.CurrentNode == localNode {
			// Release this shard by clearing CurrentNode only.
			// TargetNode is managed by the leader — after this node unregisters,
			// the leader will detect TargetNode pointing to a dead node and reassign.
			shardsToRelease[shardID] = shardInfo.WithCurrentNode("")
		}
	}
	
	return shardsToRelease
}

// waitForShardHandoff waits for other nodes to claim the released shards
func (cm *ConsensusManager) waitForShardHandoff(ctx context.Context, releasedShardIDs []int) error {
	cm.logger.Infof("Clean shutdown: waiting for %d shards to be claimed by other nodes", len(releasedShardIDs))
	
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			unclaimed := cm.countUnclaimedShards(releasedShardIDs)
			cm.logger.Warnf("Clean shutdown: handoff timeout - %d shards still unclaimed", unclaimed)
			return fmt.Errorf("handoff timeout: %d shards still unclaimed", unclaimed)
			
		case <-ticker.C:
			unclaimed := cm.countUnclaimedShards(releasedShardIDs)
			if unclaimed == 0 {
				cm.logger.Infof("Clean shutdown: all %d shards have been claimed", len(releasedShardIDs))
				return nil
			}
			cm.logger.Debugf("Clean shutdown: %d shards still unclaimed", unclaimed)
		}
	}
}

// countUnclaimedShards returns the number of shards that are still unclaimed (CurrentNode is empty)
func (cm *ConsensusManager) countUnclaimedShards(shardIDs []int) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	if cm.state.ShardMapping == nil {
		return len(shardIDs)
	}
	
	unclaimed := 0
	for _, shardID := range shardIDs {
		shardInfo, exists := cm.state.ShardMapping.Shards[shardID]
		if !exists || shardInfo.CurrentNode == "" {
			unclaimed++
		}
	}
	
	return unclaimed
}
