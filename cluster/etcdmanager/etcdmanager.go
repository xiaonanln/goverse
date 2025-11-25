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

	// DefaultEtcdTimeout is the default timeout for etcd operations.
	// Per TIMEOUT_DESIGN.md, etcd operations should have a 60 second timeout.
	DefaultEtcdTimeout = 60 * time.Second
)

// WithEtcdDeadline ensures the context has a deadline for etcd operations.
// If the context has no deadline, or if the existing deadline is longer than
// DefaultEtcdTimeout, it returns a new context with DefaultEtcdTimeout.
// This ensures etcd operations don't hang even if the parent context has a very long deadline.
func WithEtcdDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		// If existing deadline is shorter than etcd timeout, use it
		if time.Until(deadline) <= DefaultEtcdTimeout {
			return ctx, func() {}
		}
	}
	// Apply etcd timeout if no deadline or deadline is too long
	return context.WithTimeout(ctx, DefaultEtcdTimeout)
}

// EtcdManager manages the connection to etcd
type EtcdManager struct {
	client    *clientv3.Client
	clientMu  sync.RWMutex // protects client access
	endpoints []string
	logger    *logger.Logger
	prefix    string // global prefix for all etcd keys

	// Shared lease fields for generic key-value registration
	sharedLeaseID      clientv3.LeaseID
	sharedLeaseCtx     context.Context
	sharedLeaseCancel  context.CancelFunc
	sharedLeaseWg      sync.WaitGroup
	sharedLeaseRunning bool
	sharedKeysMu       sync.Mutex
	sharedKeys         map[string]string // map of key -> value for all keys using shared lease
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
		endpoints:  endpoints,
		logger:     logger.NewLogger("EtcdManager"),
		prefix:     prefix,
		sharedKeys: make(map[string]string),
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

	mgr.clientMu.Lock()
	mgr.client = cli
	mgr.clientMu.Unlock()

	mgr.logger.Infof("Successfully created etcd client to %v", mgr.endpoints)

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), DefaultEtcdTimeout)
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
	// Stop the shared lease loop
	mgr.sharedKeysMu.Lock()
	if mgr.sharedLeaseCancel != nil {
		mgr.sharedLeaseCancel()
		mgr.sharedLeaseCancel = nil
	}
	mgr.sharedLeaseRunning = false
	mgr.sharedKeysMu.Unlock()

	// Wait for the shared lease loop to stop
	mgr.sharedLeaseWg.Wait()

	mgr.clientMu.Lock()
	defer mgr.clientMu.Unlock()

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
	mgr.clientMu.RLock()
	defer mgr.clientMu.RUnlock()
	return mgr.client
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

	ctx, cancel := WithEtcdDeadline(ctx)
	defer cancel()

	_, err := mgr.client.Put(ctx, key, value)
	if err != nil {
		return fmt.Errorf("failed to put key %s: %w", key, err)
	}

	mgr.logger.Debugf("Put key=%s, value=%s", key, value)
	return nil
}

// RegisterKeyLease registers a key-value pair with the shared lease
// If the shared lease is not running, it will be started
// Multiple calls with the same key will overwrite the value
func (mgr *EtcdManager) RegisterKeyLease(ctx context.Context, key string, value string, ttl int64) (clientv3.LeaseID, error) {
	if mgr.client == nil {
		return 0, fmt.Errorf("etcd client not connected")
	}

	mgr.sharedKeysMu.Lock()
	defer mgr.sharedKeysMu.Unlock()

	// Add or update the key in our map
	mgr.sharedKeys[key] = value

	// If shared lease is not running, start it
	if !mgr.sharedLeaseRunning {
		mgr.sharedLeaseRunning = true
		mgr.sharedLeaseCtx, mgr.sharedLeaseCancel = context.WithCancel(context.Background())

		mgr.sharedLeaseWg.Add(1)
		go mgr.sharedLeaseLoop(ttl)

		mgr.logger.Infof("Started shared lease loop with TTL %d", ttl)
	}

	// Put the key immediately with the current lease (if exists)
	// The sharedLeaseLoop will re-put all keys when creating a new lease
	if mgr.sharedLeaseID != 0 {
		putCtx, cancel := WithEtcdDeadline(ctx)
		_, err := mgr.client.Put(putCtx, key, value, clientv3.WithLease(mgr.sharedLeaseID))
		cancel()
		if err != nil {
			mgr.logger.Warnf("Failed to put key %s: %v (will be retried by lease loop)", key, err)
		} else {
			mgr.logger.Debugf("Put key=%s, value=%s with shared lease %d", key, value, mgr.sharedLeaseID)
		}
	}

	return mgr.sharedLeaseID, nil
}

// UnregisterKeyLease removes a key from etcd and from the shared lease
// If this is the last key, the shared lease will be stopped and revoked
func (mgr *EtcdManager) UnregisterKeyLease(ctx context.Context, key string) error {
	mgr.sharedKeysMu.Lock()
	defer mgr.sharedKeysMu.Unlock()

	// Delete the key from our map
	delete(mgr.sharedKeys, key)

	// Delete the key from etcd
	if mgr.client != nil {
		deleteCtx, cancel := WithEtcdDeadline(ctx)
		_, err := mgr.client.Delete(deleteCtx, key)
		cancel()
		if err != nil {
			mgr.logger.Warnf("Failed to delete key %s: %v", key, err)
		} else {
			mgr.logger.Debugf("Deleted key=%s", key)
		}
	}

	// If no more keys, stop the shared lease loop
	if len(mgr.sharedKeys) == 0 && mgr.sharedLeaseRunning {
		mgr.logger.Infof("No more keys, stopping shared lease loop")
		mgr.sharedLeaseRunning = false

		if mgr.sharedLeaseCancel != nil {
			mgr.sharedLeaseCancel()
		}

		// Wait for the loop to stop (release lock temporarily to avoid deadlock)
		mgr.sharedKeysMu.Unlock()
		mgr.sharedLeaseWg.Wait()
		mgr.sharedKeysMu.Lock()

		// Revoke the lease
		if mgr.sharedLeaseID != 0 && mgr.client != nil {
			revokeCtx, cancel := context.WithTimeout(context.Background(), DefaultEtcdTimeout)
			defer cancel()

			_, err := mgr.client.Revoke(revokeCtx, mgr.sharedLeaseID)
			if err != nil {
				mgr.logger.Debugf("Failed to revoke shared lease %d: %v", mgr.sharedLeaseID, err)
			} else {
				mgr.logger.Debugf("Revoked shared lease %d", mgr.sharedLeaseID)
			}
			mgr.sharedLeaseID = 0
		}
	}

	return nil
}

// sharedLeaseLoop maintains the shared lease and keeps all registered keys alive
// This runs in a background goroutine and handles lease creation, keepalive, and recovery
func (mgr *EtcdManager) sharedLeaseLoop(ttl int64) {
	defer mgr.sharedLeaseWg.Done()

	retryDelay := 1 * time.Second
	maxRetryDelay := 8 * time.Second

	for {
		// Check if we should stop
		select {
		case <-mgr.sharedLeaseCtx.Done():
			mgr.logger.Infof("Shared lease loop context cancelled")
			return
		default:
		}

		// Try to maintain the shared lease
		err := mgr.maintainSharedLease(mgr.sharedLeaseCtx, ttl)

		// Check again if context was cancelled
		select {
		case <-mgr.sharedLeaseCtx.Done():
			mgr.logger.Infof("Shared lease loop context cancelled")
			return
		default:
		}

		if err != nil {
			mgr.logger.Warnf("Failed to maintain shared lease: %v, retrying in %v", err, retryDelay)

			// Wait before retrying with exponential backoff
			select {
			case <-time.After(retryDelay):
				// Exponential backoff
				retryDelay = retryDelay * 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
			case <-mgr.sharedLeaseCtx.Done():
				mgr.logger.Infof("Shared lease loop context cancelled during retry wait")
				return
			}
		} else {
			// Successfully maintained lease, reset retry delay
			retryDelay = 1 * time.Second
		}
	}
}

// maintainSharedLease creates and maintains a shared lease for all registered keys
// Returns when the keepalive channel closes or the context is cancelled
func (mgr *EtcdManager) maintainSharedLease(ctx context.Context, ttl int64) error {
	if mgr.client == nil {
		return fmt.Errorf("etcd client not connected")
	}

	// Revoke any previous lease before creating a new one
	mgr.sharedKeysMu.Lock()
	oldLeaseID := mgr.sharedLeaseID
	mgr.sharedKeysMu.Unlock()

	if oldLeaseID != 0 {
		mgr.logger.Infof("Revoking stale shared lease %d before creating new lease", oldLeaseID)
		revokeCtx, cancel := context.WithTimeout(context.Background(), DefaultEtcdTimeout)
		_, err := mgr.client.Revoke(revokeCtx, oldLeaseID)
		cancel()
		if err != nil {
			mgr.logger.Debugf("Failed to revoke old shared lease %d: %v (will create new lease anyway)", oldLeaseID, err)
		}
	}

	// Create a new lease with proper timeout
	grantCtx, grantCancel := context.WithTimeout(ctx, DefaultEtcdTimeout)
	lease, err := mgr.client.Grant(grantCtx, ttl)
	grantCancel()
	if err != nil {
		return fmt.Errorf("failed to grant shared lease: %w", err)
	}

	// Store the lease ID
	mgr.sharedKeysMu.Lock()
	mgr.sharedLeaseID = lease.ID
	mgr.sharedKeysMu.Unlock()

	mgr.logger.Infof("Created shared lease %d with TTL %d", lease.ID, ttl)

	// Ensure the lease is revoked when this function exits
	defer func() {
		revokeCtx, cancel := context.WithTimeout(context.Background(), DefaultEtcdTimeout)
		defer cancel()

		_, err := mgr.client.Revoke(revokeCtx, lease.ID)
		if err != nil {
			mgr.logger.Debugf("Failed to revoke shared lease %d on cleanup: %v", lease.ID, err)
		} else {
			mgr.logger.Debugf("Revoked shared lease %d on cleanup", lease.ID)
		}

		// Clear the lease ID if it's still the current one
		mgr.sharedKeysMu.Lock()
		if mgr.sharedLeaseID == lease.ID {
			mgr.sharedLeaseID = 0
		}
		mgr.sharedKeysMu.Unlock()
	}()

	// Put all keys with the new lease
	mgr.sharedKeysMu.Lock()
	keysCopy := make(map[string]string)
	for k, v := range mgr.sharedKeys {
		keysCopy[k] = v
	}
	mgr.sharedKeysMu.Unlock()

	for key, value := range keysCopy {
		putCtx, putCancel := context.WithTimeout(ctx, DefaultEtcdTimeout)
		_, err := mgr.client.Put(putCtx, key, value, clientv3.WithLease(lease.ID))
		putCancel()
		if err != nil {
			mgr.logger.Warnf("Failed to put key %s with shared lease: %v", key, err)
			return fmt.Errorf("failed to put key %s: %w", key, err)
		}
		mgr.logger.Debugf("Put key=%s with shared lease %d", key, lease.ID)
	}

	// Keep the lease alive - this is a long-running operation and uses the parent context
	// The etcd client handles keepalive internally with appropriate timeouts
	keepAliveCh, err := mgr.client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return fmt.Errorf("failed to keep alive shared lease: %w", err)
	}

	mgr.logger.Infof("Started keepalive for shared lease %d", lease.ID)

	// Process keep-alive responses until channel closes or context is cancelled
	for {
		select {
		case ka, ok := <-keepAliveCh:
			if !ok {
				mgr.logger.Warnf("Shared lease keepalive channel closed for lease %d, will retry", lease.ID)
				return fmt.Errorf("keepalive channel closed")
			}
			if ka != nil {
				mgr.logger.Debugf("Shared lease keepalive response for lease %d, TTL: %d", ka.ID, ka.TTL)
			}
		case <-ctx.Done():
			mgr.logger.Infof("Context cancelled, stopping shared lease keepalive for lease %d", lease.ID)
			return nil
		}
	}
}

// getAllNodesForTesting retrieves all registered nodes from etcd (internal use only)
func (mgr *EtcdManager) getAllNodesForTesting(ctx context.Context) ([]string, int64, error) {
	if mgr.client == nil {
		return nil, 0, fmt.Errorf("etcd client not connected")
	}

	ctx, cancel := WithEtcdDeadline(ctx)
	defer cancel()

	nodesPrefix := mgr.prefix + "/nodes/"
	resp, err := mgr.client.Get(ctx, nodesPrefix, clientv3.WithPrefix())
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
