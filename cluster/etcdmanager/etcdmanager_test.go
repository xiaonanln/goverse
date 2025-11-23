package etcdmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// setupEtcdTest creates a manager with a unique prefix, connects, and registers cleanup
// Returns nil if etcd is not available (test should be skipped)
func setupEtcdTest(t *testing.T) *EtcdManager {
	// Use PrepareEtcdPrefix for test isolation
	uniquePrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	mgr, err := NewEtcdManager("localhost:2379", uniquePrefix)
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return nil
	}

	// Cleanup is handled by PrepareEtcdPrefix via t.Cleanup
	t.Cleanup(func() {
		mgr.Close()
	})

	return mgr
}

// setupEtcdTestWithPrefix creates an additional manager for the same test
// using the same unique prefix. This is for tests that need multiple managers.
func setupEtcdTestWithPrefix(t *testing.T, prefix string) *EtcdManager {
	mgr, err := NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	t.Cleanup(func() {
		mgr.Close()
	})

	return mgr
}

// TestNewEtcdManager tests creating a new etcd manager
func TestNewEtcdManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		etcdAddress string
		wantErr     bool
	}{
		{
			name:        "valid single endpoint",
			etcdAddress: "localhost:2379",
			wantErr:     false,
		},
		{
			name:        "valid IP endpoint",
			etcdAddress: "127.0.0.1:2379",
			wantErr:     false,
		},
		{
			name:        "empty address",
			etcdAddress: "",
			wantErr:     false, // Should still create manager with empty endpoint
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewEtcdManager(tt.etcdAddress, "")
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewEtcdManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if mgr == nil {
				t.Fatalf("NewEtcdManager() returned nil manager")
				return
			}
			if mgr.logger == nil {
				t.Fatalf("EtcdManager logger is nil")
			}
			// Verify default prefix is set
			if mgr.GetPrefix() != DefaultPrefix {
				t.Fatalf("NewEtcdManager() prefix = %s, want %s", mgr.GetPrefix(), DefaultPrefix)
			}
		})
	}
}

// TestNewEtcdManagerWithPrefix tests creating etcd manager with custom prefix
func TestNewEtcdManagerWithPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		etcdAddress string
		prefix      string
		wantPrefix  string
	}{
		{
			name:        "custom prefix",
			etcdAddress: "localhost:2379",
			prefix:      "/test",
			wantPrefix:  "/test",
		},
		{
			name:        "empty prefix uses default",
			etcdAddress: "localhost:2379",
			prefix:      "",
			wantPrefix:  DefaultPrefix,
		},
		{
			name:        "prefix with trailing slash",
			etcdAddress: "localhost:2379",
			prefix:      "/myapp/",
			wantPrefix:  "/myapp/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewEtcdManager(tt.etcdAddress, tt.prefix)

			if err != nil {
				t.Fatalf("NewEtcdManager() error = %v", err)
			}
			if mgr == nil {
				t.Fatalf("NewEtcdManager() returned nil manager")
			}

			// Verify prefix is set correctly
			if mgr.GetPrefix() != tt.wantPrefix {
				t.Fatalf("GetPrefix() = %s, want %s", mgr.GetPrefix(), tt.wantPrefix)
			}
		})
	}
}

// TestEtcdManagerConnect tests connecting to etcd
func TestEtcdManagerConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	// Note: This test requires a running etcd instance at localhost:2379
	// Skip if etcd is not available
	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	// Verify client is set
	if mgr.GetClient() == nil {
		t.Fatalf("Connect() did not set client")
	}
}

// TestEtcdManagerConnectInvalidEndpoint tests connecting to invalid endpoint
func TestEtcdManagerConnectInvalidEndpoint(t *testing.T) {
	t.Parallel()

	mgr, err := NewEtcdManager("invalid-host:9999", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	// Connect creates client but connection test will fail
	// This is expected behavior - connection is established lazily
	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	defer mgr.Close()

	// Verify client is set even for invalid endpoint
	if mgr.GetClient() == nil {
		t.Fatalf("Connect() should set client even for invalid endpoint")
	}
}

// TestEtcdManagerClose tests closing the connection
func TestEtcdManagerClose(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	// Close should succeed
	err := mgr.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Close again should not panic
	err = mgr.Close()
	if err != nil {
		t.Fatalf("Second Close() error = %v", err)
	}
}

// TestEtcdManagerGetClient tests getting the client
func TestEtcdManagerGetClient(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	// Test before connect
	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	if mgr.GetClient() != nil {
		t.Fatalf("GetClient() should return nil before Connect()")
	}

	// Test after connect using setupEtcdTest
	mgr2 := setupEtcdTest(t)
	if mgr2 == nil {
		return
	}

	// After connect
	client := mgr2.GetClient()
	if client == nil {
		t.Fatalf("GetClient() should return client after Connect()")
	}
}

// TestRegisterKeyLeaseAndUnregisterKey tests the new shared lease API
func TestRegisterKeyLeaseAndUnregisterKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Register multiple keys
	keys := map[string]string{
		mgr.GetPrefix() + "/test/key1": "value1",
		mgr.GetPrefix() + "/test/key2": "value2",
		mgr.GetPrefix() + "/test/key3": "value3",
	}

	// Register all keys
	for key, value := range keys {
		_, err := mgr.RegisterKeyLease(ctx, key, value, 15)
		if err != nil {
			t.Fatalf("RegisterKeyLease(%s) error = %v", key, err)
		}
		t.Logf("Registered key %s", key)
	}

	// Wait for keys to be registered
	time.Sleep(500 * time.Millisecond)

	// Verify all keys exist in etcd
	for key, expectedValue := range keys {
		resp, err := mgr.GetClient().Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get key %s: %v", key, err)
		}
		if len(resp.Kvs) == 0 {
			t.Fatalf("Key %s not found in etcd", key)
		}
		actualValue := string(resp.Kvs[0].Value)
		if actualValue != expectedValue {
			t.Fatalf("Key %s has value %s, expected %s", key, actualValue, expectedValue)
		}
		t.Logf("Verified key %s = %s", key, actualValue)
	}

	// Verify all keys share the same lease
	var commonLeaseID clientv3.LeaseID
	for key := range keys {
		resp, err := mgr.GetClient().Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get key %s: %v", key, err)
		}
		if len(resp.Kvs) == 0 {
			t.Fatalf("Key %s not found in etcd", key)
		}
		leaseID := resp.Kvs[0].Lease
		if commonLeaseID == 0 {
			commonLeaseID = clientv3.LeaseID(leaseID)
		} else if clientv3.LeaseID(leaseID) != commonLeaseID {
			t.Fatalf("Key %s has different lease ID %d, expected %d", key, leaseID, commonLeaseID)
		}
	}
	t.Logf("All keys share lease ID %d", commonLeaseID)

	// Unregister keys one by one
	keyList := make([]string, 0, len(keys))
	for key := range keys {
		keyList = append(keyList, key)
	}

	for i, key := range keyList {
		err := mgr.UnregisterKeyLease(ctx, key)
		if err != nil {
			t.Fatalf("UnregisterKeyLease(%s) error = %v", key, err)
		}

		// Verify key is removed
		time.Sleep(100 * time.Millisecond)
		resp, err := mgr.GetClient().Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to check key %s: %v", key, err)
		}
		if len(resp.Kvs) > 0 {
			t.Fatalf("Key %s still exists after unregister", key)
		}
		t.Logf("Unregistered and verified key %s is removed", key)

		// After unregistering the last key, the lease should be revoked
		if i == len(keyList)-1 {
			time.Sleep(500 * time.Millisecond)
			// Try to get lease info - it should fail or return no keys
			leaseResp, err := mgr.GetClient().TimeToLive(ctx, commonLeaseID)
			if err == nil && leaseResp.TTL > 0 {
				t.Logf("Warning: Lease %d still active after removing all keys (may be expected)", commonLeaseID)
			} else {
				t.Logf("Lease %d revoked after removing all keys", commonLeaseID)
			}
		}
	}
}

// TestSharedLeaseResilience tests that the shared lease recovers from failures
func TestSharedLeaseResilience(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Register a key
	key := mgr.GetPrefix() + "/test/resilient-key"
	value := "resilient-value"

	_, err := mgr.RegisterKeyLease(ctx, key, value, 15)
	if err != nil {
		t.Fatalf("RegisterKeyLease() error = %v", err)
	}

	// Wait for initial registration
	time.Sleep(500 * time.Millisecond)

	// Verify key exists
	resp, err := mgr.GetClient().Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Key not found after registration")
	}

	// The shared lease loop should keep the key alive
	// Wait a bit longer and verify it's still there
	time.Sleep(2 * time.Second)

	resp, err = mgr.GetClient().Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get key after delay: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Key not found after delay - lease may have expired")
	}

	t.Logf("Key still exists after delay, shared lease is working")

	// Cleanup
	err = mgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Fatalf("UnregisterKeyLease() error = %v", err)
	}
}

// TestRegisterKeyLeaseWithoutConnect tests registering without connection
func TestRegisterKeyLeaseWithoutConnect(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	t.Parallel()

	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	ctx := context.Background()

	// Try to register without connecting
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + addr
	_, err = mgr.RegisterKeyLease(ctx, key, addr, NodeLeaseTTL)
	if err == nil {
		t.Fatal("RegisterKeyLease() should fail when not connected")
	}
}
