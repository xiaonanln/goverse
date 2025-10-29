package etcdmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/util/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	DefaultPrefix    = "/goverse"
	NodeLeaseTTL     = 15 // seconds
	NodeKeepAliveTTL = 5  // seconds
)

// EtcdManager manages the connection to etcd
type EtcdManager struct {
	client           *clientv3.Client
	endpoints        []string
	logger           *logger.Logger
	prefix           string          // global prefix for all etcd keys
	leaseID          clientv3.LeaseID
	registeredNodeID string          // the node ID that was registered
	nodes            map[string]bool // map of node addresses
	nodesMu          sync.RWMutex
	watchCancel      context.CancelFunc
	watchCtx         context.Context
	watchStarted     bool
	lastNodeChange   time.Time // timestamp of last node list change
}

// NewEtcdManager creates a new etcd manager.
//
// The optional prefix parameter specifies the root path for all etcd keys.
// If prefix is empty or not provided, DefaultPrefix ("/goverse") will be used.
// The prefix is used as the root for all etcd keys (e.g., nodes will be stored under "<prefix>/nodes/").
// This allows multiple goverse clusters or test environments to coexist in the same etcd instance.
//
// Examples:
//
//	mgr, _ := NewEtcdManager("localhost:2379")              // Uses "/goverse" prefix
//	mgr, _ := NewEtcdManager("localhost:2379", "/myapp")    // Uses "/myapp" prefix
//	mgr, _ := NewEtcdManager("localhost:2379", "")          // Uses "/goverse" prefix (empty string treated as default)
func NewEtcdManager(etcdAddress string, prefix ...string) (*EtcdManager, error) {
	endpoints := []string{etcdAddress}

	// Use default prefix if none provided or if empty string is provided
	globalPrefix := DefaultPrefix
	if len(prefix) > 0 && prefix[0] != "" {
		globalPrefix = prefix[0]
	}

	mgr := &EtcdManager{
		endpoints: endpoints,
		logger:    logger.NewLogger("EtcdManager"),
		prefix:    globalPrefix,
		nodes:     make(map[string]bool),
	}

	return mgr, nil
}

// Connect establishes a connection to etcd
func (mgr *EtcdManager) Connect() error {
	mgr.logger.Infof("Connecting to etcd at %v", mgr.endpoints)

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   mgr.endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}

	mgr.client = cli
	mgr.logger.Infof("Successfully created etcd client to %v", mgr.endpoints)

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = cli.Get(ctx, "test-connection")
	if err != nil {
		mgr.logger.Warnf("etcd connection test failed: %v", err)
	} else {
		mgr.logger.Infof("etcd connection test successful")
	}

	return nil
}

// Close closes the etcd connection
func (mgr *EtcdManager) Close() error {
	if mgr.watchCancel != nil {
		mgr.watchCancel()
		mgr.watchCancel = nil
	}

	if mgr.client != nil {
		mgr.logger.Infof("Closing etcd connection")
		err := mgr.client.Close()
		mgr.client = nil
		return err
	}
	return nil
}

// GetClient returns the etcd client
func (mgr *EtcdManager) GetClient() *clientv3.Client {
	return mgr.client
}

// GetNodesPrefix returns the nodes prefix based on the global prefix.
// For example, if the global prefix is "/goverse", this returns "/goverse/nodes/".
func (mgr *EtcdManager) GetNodesPrefix() string {
	return mgr.prefix + "/nodes/"
}

// GetPrefix returns the global prefix used for all etcd keys.
func (mgr *EtcdManager) GetPrefix() string {
	return mgr.prefix
}

// Put stores a key-value pair in etcd
func (mgr *EtcdManager) Put(ctx context.Context, key, value string) error {
	if mgr.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	_, err := mgr.client.Put(ctx, key, value)
	if err != nil {
		return fmt.Errorf("failed to put key %s: %w", key, err)
	}

	mgr.logger.Debugf("Put key=%s, value=%s", key, value)
	return nil
}

// Get retrieves a value from etcd
func (mgr *EtcdManager) Get(ctx context.Context, key string) (string, error) {
	if mgr.client == nil {
		return "", fmt.Errorf("etcd client not connected")
	}

	resp, err := mgr.client.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if len(resp.Kvs) == 0 {
		return "", fmt.Errorf("key not found: %s", key)
	}

	value := string(resp.Kvs[0].Value)
	mgr.logger.Debugf("Get key=%s, value=%s", key, value)
	return value, nil
}

// Delete removes a key from etcd
func (mgr *EtcdManager) Delete(ctx context.Context, key string) error {
	if mgr.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	_, err := mgr.client.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	mgr.logger.Debugf("Delete key=%s", key)
	return nil
}

// Watch watches for changes to a key
func (mgr *EtcdManager) Watch(ctx context.Context, key string) clientv3.WatchChan {
	if mgr.client == nil {
		mgr.logger.Errorf("etcd client not connected")
		return nil
	}

	mgr.logger.Infof("Watching key=%s", key)
	return mgr.client.Watch(ctx, key)
}

// RegisterNode registers a node with etcd using a lease
func (mgr *EtcdManager) RegisterNode(ctx context.Context, nodeAddress string) error {
	if mgr.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	// Check if a node has already been registered
	if mgr.registeredNodeID != "" {
		if mgr.registeredNodeID != nodeAddress {
			return fmt.Errorf("node already registered with ID %s, cannot register different node ID %s", mgr.registeredNodeID, nodeAddress)
		}
		// Same node ID - this is a no-op
		mgr.logger.Debugf("Node %s already registered, skipping", nodeAddress)
		return nil
	}

	// Create a lease
	lease, err := mgr.client.Grant(ctx, NodeLeaseTTL)
	if err != nil {
		return fmt.Errorf("failed to grant lease: %w", err)
	}

	mgr.leaseID = lease.ID

	// Register the node with the lease
	key := mgr.GetNodesPrefix() + nodeAddress
	_, err = mgr.client.Put(ctx, key, nodeAddress, clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	// Mark this node as registered
	mgr.registeredNodeID = nodeAddress

	mgr.logger.Infof("Registered node %s with lease ID %d", nodeAddress, lease.ID)

	// Keep the lease alive
	keepAliveCh, err := mgr.client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return fmt.Errorf("failed to keep alive lease: %w", err)
	}

	// Start a goroutine to process keep-alive responses
	go func() {
		for {
			select {
			case ka, ok := <-keepAliveCh:
				if !ok {
					mgr.logger.Warnf("Keep-alive channel closed for lease %d", lease.ID)
					return
				}
				if ka != nil {
					mgr.logger.Debugf("Keep-alive response for lease %d, TTL: %d", ka.ID, ka.TTL)
				}
			case <-ctx.Done():
				mgr.logger.Infof("Context cancelled, stopping keep-alive for lease %d", lease.ID)
				return
			}
		}
	}()

	return nil
}

// UnregisterNode removes a node from etcd
func (mgr *EtcdManager) UnregisterNode(ctx context.Context, nodeAddress string) error {
	if mgr.client == nil {
		// No-op if client is not connected
		return nil
	}

	// Revoke the lease
	if mgr.leaseID != 0 {
		_, err := mgr.client.Revoke(ctx, mgr.leaseID)
		if err != nil {
			mgr.logger.Warnf("Failed to revoke lease: %v", err)
		}
		mgr.leaseID = 0
	}

	// Delete the node key
	key := mgr.GetNodesPrefix() + nodeAddress
	_, err := mgr.client.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to unregister node: %w", err)
	}

	// Clear the registered node ID
	mgr.registeredNodeID = ""

	mgr.logger.Infof("Unregistered node %s", nodeAddress)
	return nil
}

// getAllNodes retrieves all registered nodes from etcd (internal use only)
func (mgr *EtcdManager) getAllNodes(ctx context.Context) ([]string, int64, error) {
	if mgr.client == nil {
		return nil, 0, fmt.Errorf("etcd client not connected")
	}

	resp, err := mgr.client.Get(ctx, mgr.GetNodesPrefix(), clientv3.WithPrefix())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get nodes: %w", err)
	}

	nodes := make([]string, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		nodes = append(nodes, string(kv.Value))
	}

	mgr.logger.Debugf("Retrieved %d nodes from etcd", len(nodes))
	return nodes, resp.Header.Revision, nil
}

// WatchNodes starts watching for node changes and maintains an internal list
func (mgr *EtcdManager) WatchNodes(ctx context.Context) error {
	if mgr.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	if mgr.watchStarted {
		mgr.logger.Warnf("Watch already started")
		return nil
	}

	// First, get all existing nodes
	nodes, rev, err := mgr.getAllNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get initial nodes: %w", err)
	}

	mgr.nodesMu.Lock()
	for _, node := range nodes {
		mgr.nodes[node] = true
	}
	mgr.lastNodeChange = time.Now()
	mgr.nodesMu.Unlock()

	mgr.logger.Infof("Initialized with %d nodes", len(nodes))

	// Create a cancellable context for watching
	mgr.watchCtx, mgr.watchCancel = context.WithCancel(ctx)

	// Start watching for changes
	watchChan := mgr.client.Watch(mgr.watchCtx, mgr.GetNodesPrefix(), clientv3.WithPrefix(), clientv3.WithRev(rev))

	go func() {
		mgr.logger.Infof("Started watching nodes at prefix %s", mgr.GetNodesPrefix())
		for {
			select {
			case <-mgr.watchCtx.Done():
				mgr.logger.Infof("Node watch stopped")
				return
			case watchResp, ok := <-watchChan:
				if !ok {
					mgr.logger.Warnf("Watch channel closed")
					return
				}
				if watchResp.Err() != nil {
					mgr.logger.Errorf("Watch error: %v", watchResp.Err())
					continue
				}

				for _, event := range watchResp.Events {
					mgr.nodesMu.Lock()
					switch event.Type {
					case clientv3.EventTypePut:
						nodeAddress := string(event.Kv.Value)
						mgr.nodes[nodeAddress] = true
						mgr.lastNodeChange = time.Now()
						mgr.logger.Infof("Node added: %s", nodeAddress)
					case clientv3.EventTypeDelete:
						// For DELETE events, extract node address from the key
						key := string(event.Kv.Key)
						nodeAddress := key[len(mgr.GetNodesPrefix()):]
						delete(mgr.nodes, nodeAddress)
						mgr.lastNodeChange = time.Now()
						mgr.logger.Infof("Node removed: %s", nodeAddress)
					}
					mgr.nodesMu.Unlock()
				}
			}
		}
	}()

	mgr.watchStarted = true
	return nil
}

// GetNodes returns a copy of the current node list
func (mgr *EtcdManager) GetNodes() []string {
	mgr.nodesMu.RLock()
	defer mgr.nodesMu.RUnlock()

	nodes := make([]string, 0, len(mgr.nodes))
	for node := range mgr.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetLeaderNode returns the leader node address.
// The leader is the node with the smallest advertised address in lexicographic order.
// Returns an empty string if there are no registered nodes.
func (mgr *EtcdManager) GetLeaderNode() string {
	mgr.nodesMu.RLock()
	defer mgr.nodesMu.RUnlock()

	if len(mgr.nodes) == 0 {
		return ""
	}

	// Find the smallest address (leader)
	var leader string
	for node := range mgr.nodes {
		if leader == "" || node < leader {
			leader = node
		}
	}
	return leader
}

// GetLastNodeChangeTime returns the timestamp of the last node list change
func (mgr *EtcdManager) GetLastNodeChangeTime() time.Time {
	mgr.nodesMu.RLock()
	defer mgr.nodesMu.RUnlock()
	return mgr.lastNodeChange
}

// IsNodeListStable returns true if the node list has not changed for the specified duration
func (mgr *EtcdManager) IsNodeListStable(duration time.Duration) bool {
	mgr.nodesMu.RLock()
	defer mgr.nodesMu.RUnlock()
	
	// If lastNodeChange is zero, nodes have never changed (not initialized)
	if mgr.lastNodeChange.IsZero() {
		return false
	}
	
	return time.Since(mgr.lastNodeChange) >= duration
}

// SetLastNodeChangeForTesting sets the last node change timestamp for testing purposes
// This should only be used in tests
func (mgr *EtcdManager) SetLastNodeChangeForTesting(t time.Time) {
	mgr.nodesMu.Lock()
	defer mgr.nodesMu.Unlock()
	mgr.lastNodeChange = t
}
