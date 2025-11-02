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
	prefix           string // global prefix for all etcd keys
	leaseID          clientv3.LeaseID
	registeredNodeID string // the node ID that was registered
	watchStarted     bool
	lastNodeChange   time.Time // timestamp of last node list change
	keepAliveCancel  context.CancelFunc
	keepAliveMu      sync.Mutex
	keepAliveCtx     context.Context
	keepAliveStopped bool
	keepAliveWg      sync.WaitGroup
}

// NewEtcdManager creates a new etcd manager.
//
// The prefix parameter specifies the root path for all etcd keys.
// If prefix is empty, DefaultPrefix ("/goverse") will be used.
// The prefix is used as the root for all etcd keys (e.g., nodes will be stored under "<prefix>/nodes/").
// This allows multiple goverse clusters or test environments to coexist in the same etcd instance.
//
// Examples:
//
//	mgr, _ := NewEtcdManager("localhost:2379", "")          // Uses "/goverse" prefix
//	mgr, _ := NewEtcdManager("localhost:2379", "/myapp")    // Uses "/myapp" prefix
func NewEtcdManager(etcdAddress string, prefix string) (*EtcdManager, error) {
	endpoints := []string{etcdAddress}

	// Use default prefix if empty string is provided
	if prefix == "" {
		prefix = DefaultPrefix
	}

	mgr := &EtcdManager{
		endpoints: endpoints,
		logger:    logger.NewLogger("EtcdManager"),
		prefix:    prefix,
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
	// Stop the keep-alive loop
	mgr.keepAliveMu.Lock()
	if mgr.keepAliveCancel != nil {
		mgr.keepAliveCancel()
		mgr.keepAliveCancel = nil
	}
	mgr.keepAliveStopped = true
	mgr.keepAliveMu.Unlock()

	// Wait for the keep-alive loop to stop
	mgr.keepAliveWg.Wait()

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

	// Check if keep-alive loop is already running for this node
	mgr.keepAliveMu.Lock()
	if mgr.keepAliveCtx != nil && mgr.keepAliveCtx.Err() == nil && !mgr.keepAliveStopped {
		// Keep-alive loop is already running
		mgr.keepAliveMu.Unlock()
		mgr.logger.Debugf("Keep-alive loop already running for node %s, skipping", nodeAddress)
		return nil
	}

	// Mark this node as registered
	mgr.registeredNodeID = nodeAddress

	// Create a cancellable context for the keep-alive loop
	mgr.keepAliveStopped = false
	mgr.keepAliveCtx, mgr.keepAliveCancel = context.WithCancel(context.Background())
	mgr.keepAliveMu.Unlock()

	// Start the keep-alive loop in a goroutine
	mgr.keepAliveWg.Add(1)
	go mgr.keepAliveLoop(nodeAddress)

	mgr.logger.Infof("Started keep-alive loop for node %s", nodeAddress)
	return nil
}

// keepAliveLoop maintains the lease and registration for a node with automatic retry
func (mgr *EtcdManager) keepAliveLoop(nodeAddress string) {
	defer mgr.keepAliveWg.Done()

	retryDelay := 1 * time.Second
	maxRetryDelay := 4 * time.Second

	for {
		// Check if we should stop
		mgr.keepAliveMu.Lock()
		if mgr.keepAliveStopped {
			mgr.keepAliveMu.Unlock()
			mgr.logger.Infof("Keep-alive loop stopped for node %s", nodeAddress)
			return
		}
		ctx := mgr.keepAliveCtx
		mgr.keepAliveMu.Unlock()

		if ctx.Err() != nil {
			mgr.logger.Infof("Keep-alive context cancelled for node %s", nodeAddress)
			return
		}

		// Try to establish lease and keep-alive
		err := mgr.maintainLease(ctx, nodeAddress)

		if ctx.Err() != nil {
			// Context was cancelled, exit cleanly
			mgr.logger.Infof("Keep-alive context cancelled for node %s", nodeAddress)
			return
		}

		if err != nil {
			mgr.logger.Warnf("Failed to maintain lease for node %s: %v, retrying in %v", nodeAddress, err, retryDelay)

			// Wait before retrying with exponential backoff
			select {
			case <-time.After(retryDelay):
				// Exponential backoff
				retryDelay = retryDelay * 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
			case <-ctx.Done():
				mgr.logger.Infof("Keep-alive context cancelled during retry wait for node %s", nodeAddress)
				return
			}
		} else {
			// Successfully maintained lease, reset retry delay
			retryDelay = 1 * time.Second
		}
	}
}

// maintainLease creates a lease and maintains it with keep-alive
// This function is responsible for the complete lifecycle of a lease:
// - Revoking any previous lease
// - Creating a new lease
// - Maintaining the lease with keep-alive
// - Revoking the lease when done (on context cancellation or error)
// Returns when keep-alive channel closes or context is cancelled
func (mgr *EtcdManager) maintainLease(ctx context.Context, nodeAddress string) error {
	if mgr.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	// Get and revoke any previous lease before creating a new one
	// This prevents lease leaks when retrying after keep-alive failures
	mgr.keepAliveMu.Lock()
	oldLeaseID := mgr.leaseID
	mgr.keepAliveMu.Unlock()

	if oldLeaseID != 0 {
		// Best effort revoke - don't fail if it doesn't work
		// The lease will expire naturally if revoke fails
		mgr.logger.Infof("Revoking stale lease %d before creating new lease", oldLeaseID)
		_, err := mgr.client.Revoke(ctx, oldLeaseID)
		if err != nil {
			mgr.logger.Debugf("Failed to revoke old lease %d: %v (will create new lease anyway)", oldLeaseID, err)
		}
	}

	// Create a lease
	lease, err := mgr.client.Grant(ctx, NodeLeaseTTL)
	if err != nil {
		return fmt.Errorf("failed to grant lease: %w", err)
	}

	// Store the lease ID (protected by mutex)
	mgr.keepAliveMu.Lock()
	mgr.leaseID = lease.ID
	mgr.keepAliveMu.Unlock()

	// Ensure the lease is always revoked when this function exits
	defer func() {
		// Revoke the lease using a background context since the original context may be cancelled
		revokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := mgr.client.Revoke(revokeCtx, lease.ID)
		if err != nil {
			mgr.logger.Debugf("Failed to revoke lease %d on cleanup: %v", lease.ID, err)
		} else {
			mgr.logger.Debugf("Revoked lease %d on cleanup", lease.ID)
		}

		// Clear the lease ID
		mgr.keepAliveMu.Lock()
		if mgr.leaseID == lease.ID {
			mgr.leaseID = 0
		}
		mgr.keepAliveMu.Unlock()
	}()

	// Register the node with the lease
	key := mgr.GetNodesPrefix() + nodeAddress
	_, err = mgr.client.Put(ctx, key, nodeAddress, clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	mgr.logger.Infof("Registered node %s with lease ID %d", nodeAddress, lease.ID)

	// Keep the lease alive
	keepAliveCh, err := mgr.client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return fmt.Errorf("failed to keep alive lease: %w", err)
	}

	// Process keep-alive responses until channel closes or context is cancelled
	for {
		select {
		case ka, ok := <-keepAliveCh:
			if !ok {
				mgr.logger.Warnf("Keep-alive channel closed for lease %d, will retry", lease.ID)
				return fmt.Errorf("keep-alive channel closed")
			}
			if ka != nil {
				mgr.logger.Debugf("Keep-alive response for lease %d, TTL: %d", ka.ID, ka.TTL)
			}
		case <-ctx.Done():
			mgr.logger.Infof("Context cancelled, stopping keep-alive for lease %d", lease.ID)
			return nil
		}
	}
}

// UnregisterNode removes a node from etcd
func (mgr *EtcdManager) UnregisterNode(ctx context.Context, nodeAddress string) error {
	// Stop the keep-alive loop first
	mgr.keepAliveMu.Lock()
	if mgr.keepAliveCancel != nil {
		mgr.keepAliveCancel()
		mgr.keepAliveCancel = nil
	}
	mgr.keepAliveStopped = true
	mgr.keepAliveMu.Unlock()

	// Wait for the keep-alive loop to stop
	// The loop will clean up the lease via the defer in maintainLease
	mgr.keepAliveWg.Wait()

	if mgr.client == nil {
		// No-op if client is not connected
		return nil
	}

	// Delete the node key
	// Note: The lease is revoked by maintainLease's defer, so we only need to delete the key
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

// getAllNodesForTesting retrieves all registered nodes from etcd (internal use only)
func (mgr *EtcdManager) getAllNodesForTesting(ctx context.Context) ([]string, int64, error) {
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
