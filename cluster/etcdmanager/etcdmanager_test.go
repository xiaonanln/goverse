package etcdmanager

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// cleanupEtcd removes all test data from etcd
// This ensures tests start with a clean slate and don't interfere with each other
func cleanupEtcd(t *testing.T, mgr *EtcdManager) {
	if mgr == nil || mgr.GetClient() == nil {
		return
	}

	ctx := context.Background()

	// Delete all keys under the manager's prefix (includes nodes and any test keys)
	_, err := mgr.GetClient().Delete(ctx, mgr.GetPrefix()+"/", clientv3.WithPrefix())
	if err != nil {
		t.Logf("Warning: failed to cleanup etcd: %v", err)
	}

	// Also clean up any test keys that might not be under the prefix
	testKeyPrefixes := []string{"test-key", "test/key"}
	for _, prefix := range testKeyPrefixes {
		_, err := mgr.GetClient().Delete(ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			t.Logf("Warning: failed to cleanup test keys with prefix %s: %v", prefix, err)
		}
	}
}

// setupEtcdTest creates a manager, connects, and registers cleanup
// Returns nil if etcd is not available (test should be skipped)
func setupEtcdTest(t *testing.T) *EtcdManager {
	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return nil
	}

	// Clean up before test
	cleanupEtcd(t, mgr)

	// Register cleanup after test (runs even if test fails)
	t.Cleanup(func() {
		cleanupEtcd(t, mgr)
		mgr.Close()
	})

	return mgr
}

// TestNewEtcdManager tests creating a new etcd manager
func TestNewEtcdManager(t *testing.T) {
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
			
			// Verify nodes prefix is derived correctly
			expectedNodesPrefix := tt.wantPrefix + "/nodes/"
			if mgr.GetNodesPrefix() != expectedNodesPrefix {
				t.Fatalf("GetNodesPrefix() = %s, want %s", mgr.GetNodesPrefix(), expectedNodesPrefix)
			}
		})
	}
}

// TestEtcdManagerConnect tests connecting to etcd
func TestEtcdManagerConnect(t *testing.T) {
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

// TestEtcdManagerPutGet tests put and get operations
func TestEtcdManagerPutGet(t *testing.T) {
	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "simple key-value",
			key:   "test-key-1",
			value: "test-value-1",
		},
		{
			name:  "key with slash",
			key:   "test/key/2",
			value: "test-value-2",
		},
		{
			name:  "empty value",
			key:   "test-key-3",
			value: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Put value
			err := mgr.Put(ctx, tt.key, tt.value)
			if err != nil {
				t.Fatalf("Put() error = %v", err)
				return
			}

			// Get value
			got, err := mgr.Get(ctx, tt.key)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
				return
			}

			if got != tt.value {
				t.Fatalf("Get() = %v, want %v", got, tt.value)
			}

			// Cleanup individual key (though cleanupEtcd will handle it too)
			mgr.Delete(ctx, tt.key)
		})
	}
}

// TestEtcdManagerGetNonExistent tests getting non-existent key
func TestEtcdManagerGetNonExistent(t *testing.T) {
	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Try to get non-existent key
	_, err := mgr.Get(ctx, "non-existent-key-12345")
	if err == nil {
		t.Fatal("Get() should return error for non-existent key")
	}
}

// TestEtcdManagerDelete tests delete operation
func TestEtcdManagerDelete(t *testing.T) {
	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Put a value
	key := "test-delete-key"
	value := "test-delete-value"
	err := mgr.Put(ctx, key, value)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Verify it exists
	got, err := mgr.Get(ctx, key)
	if err != nil || got != value {
		t.Fatalf("Get() after Put() failed")
	}

	// Delete the key
	err = mgr.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	_, err = mgr.Get(ctx, key)
	if err == nil {
		t.Fatalf("Get() should fail after Delete()")
	}
}

// TestEtcdManagerWatch tests watch functionality
func TestEtcdManagerWatch(t *testing.T) {
	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "test-watch-key"
	value := "test-watch-value"

	// Start watching
	watchChan := mgr.Watch(ctx, key)
	if watchChan == nil {
		t.Fatal("Watch() returned nil channel")
	}

	// Put a value in another goroutine
	go func() {
		time.Sleep(100 * time.Millisecond)
		mgr.Put(context.Background(), key, value)
	}()

	// Wait for watch event
	select {
	case watchResp := <-watchChan:
		if len(watchResp.Events) == 0 {
			t.Fatalf("Watch() returned no events")
			return
		}
		event := watchResp.Events[0]
		if event.Type != clientv3.EventTypePut {
			t.Fatalf("Watch() event type = %v, want %v", event.Type, clientv3.EventTypePut)
		}
		if string(event.Kv.Key) != key {
			t.Fatalf("Watch() event key = %s, want %s", string(event.Kv.Key), key)
		}
		if string(event.Kv.Value) != value {
			t.Fatalf("Watch() event value = %s, want %s", string(event.Kv.Value), value)
		}
	case <-ctx.Done():
		t.Fatalf("Watch() timeout waiting for event")
	}
}

// TestEtcdManagerOperationsWithoutConnect tests operations without connecting
func TestEtcdManagerOperationsWithoutConnect(t *testing.T) {
	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	ctx := context.Background()

	// Try operations without connecting
	err = mgr.Put(ctx, "key", "value")
	if err == nil {
		t.Fatalf("Put() should fail when not connected")
	}

	_, err = mgr.Get(ctx, "key")
	if err == nil {
		t.Fatalf("Get() should fail when not connected")
	}

	err = mgr.Delete(ctx, "key")
	if err == nil {
		t.Fatalf("Delete() should fail when not connected")
	}

	watchChan := mgr.Watch(ctx, "key")
	if watchChan != nil {
		t.Fatalf("Watch() should return nil when not connected")
	}
}

// TestEtcdManagerClose tests closing the connection
func TestEtcdManagerClose(t *testing.T) {
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

// TestEtcdManagerRegisterMultipleNodes tests that registering multiple different nodes with one manager fails
func TestEtcdManagerRegisterMultipleNodes(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Define 3 nodes with different addresses
	nodeAddresses := []string{
		"localhost:43000",
		"localhost:43001",
		"localhost:43002",
	}

	// Register first node - should succeed
	err := mgr.RegisterNode(ctx, nodeAddresses[0])
	if err != nil {
		t.Fatalf("Failed to register first node %s: %v", nodeAddresses[0], err)
	}

	// Try to register second and third nodes - should fail
	for i := 1; i < len(nodeAddresses); i++ {
		err = mgr.RegisterNode(ctx, nodeAddresses[i])
		if err == nil {
			t.Fatalf("RegisterNode(%s) should have failed after node %s was already registered, but succeeded", nodeAddresses[i], nodeAddresses[0])
		} else {
			t.Logf("Expected error for node %s: %v", nodeAddresses[i], err)
		}
	}

	// Get all registered nodes
	registeredNodes, _, err := mgr.getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes() error = %v", err)
		return
	}

	// Verify only the first node is registered
	if len(registeredNodes) != 1 {
		t.Fatalf("GetNodes() returned %d nodes, want 1", len(registeredNodes))
	}

	// Verify the registered node is the first one
	if len(registeredNodes) > 0 && registeredNodes[0] != nodeAddresses[0] {
		t.Fatalf("Registered node is %s, want %s", registeredNodes[0], nodeAddresses[0])
	}

	// Cleanup - unregister the node
	err = mgr.UnregisterNode(ctx, nodeAddresses[0])
	if err != nil {
		t.Fatalf("Failed to unregister node %s: %v", nodeAddresses[0], err)
	}

	// Verify node is unregistered
	remainingNodes, _, err := mgr.getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes() error = %v", err)
		return
	}

	if len(remainingNodes) != 0 {
		t.Fatalf("After cleanup, GetNodes() returned %d nodes, want 0", len(remainingNodes))
	}
}

// TestEtcdManagerRegisterNode tests node registration
func TestEtcdManagerRegisterNode(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:47001"

	// Register node
	err := mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
		return
	}

	// Verify node is registered
	nodes, _, err := mgr.getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes() error = %v", err)
		return
	}

	found := false
	for _, node := range nodes {
		if node == nodeAddress {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Registered node %s not found in node list", nodeAddress)
	}

	// Cleanup handled by t.Cleanup in setupEtcdTest
}

// TestEtcdManagerRegisterNodeMultipleTimes tests that registering multiple different nodes fails
func TestEtcdManagerRegisterNodeMultipleTimes(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress1 := "localhost:47001"
	nodeAddress2 := "localhost:47002"

	// Register first node
	err := mgr.RegisterNode(ctx, nodeAddress1)
	if err != nil {
		t.Fatalf("First RegisterNode() error = %v", err)
	}

	// Try to register same node again - should succeed (no-op)
	err = mgr.RegisterNode(ctx, nodeAddress1)
	if err != nil {
		t.Fatalf("RegisterNode() with same node ID should succeed, got error: %v", err)
	}

	// Try to register different node - should fail
	err = mgr.RegisterNode(ctx, nodeAddress2)
	if err == nil {
		t.Fatal("RegisterNode() with different node ID should fail, but succeeded")
	} else {
		t.Logf("Expected error: %v", err)
	}

	// Verify only first node is registered
	nodes, _, err := mgr.getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes() error = %v", err)
		return
	}

	foundNode1 := false
	foundNode2 := false
	for _, node := range nodes {
		if node == nodeAddress1 {
			foundNode1 = true
		}
		if node == nodeAddress2 {
			foundNode2 = true
		}
	}

	if !foundNode1 {
		t.Fatalf("First registered node %s not found in node list", nodeAddress1)
	}
	if foundNode2 {
		t.Fatalf("Second node %s should not be in node list", nodeAddress2)
	}

	// Cleanup
	mgr.UnregisterNode(ctx, nodeAddress1)

	// After unregister, should be able to register a new node
	err = mgr.RegisterNode(ctx, nodeAddress2)
	if err != nil {
		t.Fatalf("RegisterNode() after unregister should succeed, got error: %v", err)
	}

	// Cleanup
	mgr.UnregisterNode(ctx, nodeAddress2)
}

// TestEtcdManagerUnregisterNode tests node unregistration
func TestEtcdManagerUnregisterNode(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:47002"

	// Register node first
	err := mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Unregister node
	err = mgr.UnregisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("UnregisterNode() error = %v", err)
		return
	}

	// Verify node is unregistered
	nodes, _, err := mgr.getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes() error = %v", err)
		return
	}

	for _, node := range nodes {
		if node == nodeAddress {
			t.Fatalf("Unregistered node %s still found in node list", nodeAddress)
		}
	}
}

// TestEtcdManagerGetAllNodes tests retrieving all nodes
func TestEtcdManagerGetAllNodes(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddresses := []string{"localhost:47003", "localhost:47004", "localhost:47005"}

	// Register multiple nodes
	for _, addr := range nodeAddresses {
		err := mgr.RegisterNode(ctx, addr)
		if err != nil {
			t.Fatalf("RegisterNode(%s) error = %v", addr, err)
		}
		// Need to create separate managers for each node registration
		// to avoid lease conflict
		break // For now, just test with one node
	}

	// Get all nodes
	nodes, _, err := mgr.getAllNodes(ctx)
	if err != nil {
		t.Fatalf("getAllNodes() error = %v", err)
		return
	}

	if len(nodes) == 0 {
		t.Fatal("getAllNodes() returned empty list")
	}

	// Cleanup
	for _, addr := range nodeAddresses {
		mgr.UnregisterNode(ctx, addr)
	}
}

// TestEtcdManagerWatchNodes tests watching for node changes
func TestEtcdManagerWatchNodes(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Start watching nodes
	err := mgr.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() error = %v", err)
	}

	// Register a node
	nodeAddress := "localhost:47006"
	err = mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Wait for watch to process the event
	time.Sleep(3 * time.Second)

	// Check if node appears in the list
	nodes := mgr.GetNodes()
	found := false
	for _, node := range nodes {
		if node == nodeAddress {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Registered node %s not found in watched node list", nodeAddress)
	}

	// Unregister node
	err = mgr.UnregisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("UnregisterNode() error = %v", err)
	}

	// Wait for watch to process the delete event
	time.Sleep(3 * time.Second)

	// Check if node is removed from the list
	nodes = mgr.GetNodes()
	for _, node := range nodes {
		if node == nodeAddress {
			t.Fatalf("Unregistered node %s still in watched node list", nodeAddress)
		}
	}
}

// TestEtcdManagerMultipleNodes tests multiple nodes scenario
func TestEtcdManagerMultipleNodes(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Create two separate managers for two nodes
	mgr1 := setupEtcdTest(t)
	if mgr1 == nil {
		return
	}

	mgr2, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() {
		mgr2.Close()
	})

	ctx := context.Background()

	// Start watching on both managers
	err = mgr1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr1 error = %v", err)
	}

	err = mgr2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr2 error = %v", err)
	}

	// Register node 1
	node1Address := "localhost:47007"
	err = mgr1.RegisterNode(ctx, node1Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node1 error = %v", err)
	}

	// Register node 2
	node2Address := "localhost:47008"
	err = mgr2.RegisterNode(ctx, node2Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node2 error = %v", err)
	}

	// Wait for watches to process
	time.Sleep(500 * time.Millisecond)

	// Both managers should see both nodes
	nodes1 := mgr1.GetNodes()
	nodes2 := mgr2.GetNodes()

	if len(nodes1) < 2 {
		t.Fatalf("Manager 1 should see at least 2 nodes, got %d", len(nodes1))
	}

	if len(nodes2) < 2 {
		t.Fatalf("Manager 2 should see at least 2 nodes, got %d", len(nodes2))
	}

	// Cleanup
	mgr1.UnregisterNode(ctx, node1Address)
	mgr2.UnregisterNode(ctx, node2Address)
}

// TestEtcdManagerRegisterNodeWithoutConnect tests registering without connection
func TestEtcdManagerRegisterNodeWithoutConnect(t *testing.T) {
	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	ctx := context.Background()

	// Try to register without connecting
	err = mgr.RegisterNode(ctx, "localhost:47009")
	if err == nil {
		t.Fatal("RegisterNode() should fail when not connected")
	}
}

// TestEtcdManagerGetNodesInitiallyEmpty tests that GetNodes returns empty list initially
func TestEtcdManagerGetNodesInitiallyEmpty(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	nodes := mgr.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("GetNodes() should return empty list initially, got %d nodes", len(nodes))
	}
}
func isGithubAction() bool {
	// GitHub Actions sets GITHUB_ACTIONS=true; check the environment variable directly
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// TestEtcdManagerServerCrash tests that EtcdManager handles etcd server crash gracefully
// This test simulates an etcd server crash by stopping the etcd service, verifying that:
// 1. The EtcdManager continues to run without panicking
// 2. Keep-alive channel closure is handled gracefully
// 3. Watch channel closure is handled gracefully
// 4. Operations after crash return appropriate errors without causing failures
func TestEtcdManagerServerCrash(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Only skip when NOT running in GitHub Actions (we want this to run in CI)
	if !isGithubAction() {
		t.Skipf("Skipping test: manual etcd crash simulation is intended for local runs")
	} else {
		t.Log("Running in GitHub Actions - will attempt to stop etcd service for crash simulation")
	}
	// First, verify etcd is running
	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:47010"

	// Start watching nodes to ensure watch channel is active
	err = mgr.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() error = %v", err)
	}

	// Register a node to ensure keep-alive channel is active
	err = mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Wait a bit to ensure registration and watch are fully established
	time.Sleep(500 * time.Millisecond)

	// Simulate etcd crash by stopping the service
	if isGithubAction() {
		t.Log("Simulating etcd server crash by stopping etcd service (GitHub Action)...")
		stopCmd := "sudo systemctl stop etcd"
		result, err := executeCommand(stopCmd)
		if err != nil {
			t.Fatalf("Failed to stop etcd service: %v, output: %s", err, result)
		}
	} else {
		t.Log("PLEASE MANUALLY STOP THE ETCD SERVER NOW, THEN PRESS ENTER IN THE TERMINAL. SLEEPING FOR 10 SECONDS TO ALLOW ETCD TO STOP...")
		time.Sleep(5 * time.Second)
	}

	// Wait for the crash to be detected
	time.Sleep(2 * time.Second)

	// Verify that EtcdManager is still running (didn't panic or crash)
	// The manager should still be accessible and not cause panic
	t.Log("Verifying EtcdManager continues to run after etcd crash...")

	// Test 1: GetNodes should still work and return existing data
	nodes := mgr.GetNodes()
	t.Logf("GetNodes() returned %d nodes after crash (expected to have cached data)", len(nodes))
	// This should not panic and should return the last known state

	// Test 2: Operations should fail gracefully with errors, not panic
	// Use a short timeout to avoid blocking the test
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = mgr.Put(ctxWithTimeout, "test-key-after-crash", "test-value")
	if err == nil {
		t.Log("Put() succeeded despite etcd being down (unexpected but not a failure)")
	} else {
		t.Logf("Put() failed as expected after etcd crash: %v", err)
	}

	// Test 3: Get operation should fail gracefully
	ctxGet, cancelGet := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelGet()

	_, err = mgr.Get(ctxGet, "test-key-after-crash")
	if err == nil {
		t.Log("Get() succeeded despite etcd being down (unexpected but not a failure)")
	} else {
		t.Logf("Get() failed as expected after etcd crash: %v", err)
	}

	// Restart etcd for subsequent tests
	if isGithubAction() {
		t.Log("Restarting etcd service for subsequent tests (GitHub Action)...")
		startCmd := "sudo systemctl start etcd"
		result, err := executeCommand(startCmd)
		if err != nil {
			t.Logf("Warning: Failed to restart etcd service: %v, output: %s", err, result)
		}
		// Wait for etcd to be ready
		time.Sleep(2 * time.Second)
	} else {
		t.Log("PLEASE MANUALLY RESTART THE ETCD SERVER NOW.")
		time.Sleep(5 * time.Second)
	}

	// After etcd restart, verify that get/put operations work again
	// Wait a bit for etcd to be ready
	time.Sleep(2 * time.Second)
	// After etcd restart, verify that the original manager (mgr) can reconnect and resume operations

	ctx2 := context.Background()
	testKey := "test-key-after-restart"
	testValue := "test-value-after-restart"

	// Put should succeed using the original manager
	err = mgr.Put(ctx2, testKey, testValue)
	if err != nil {
		t.Fatalf("Put() after etcd restart (original manager) failed: %v", err)
	}

	// Get should succeed using the original manager
	got, err := mgr.Get(ctx2, testKey)
	if err != nil {
		t.Fatalf("Get() after etcd restart (original manager) failed: %v", err)
	} else if got != testValue {
		t.Fatalf("Get() after etcd restart (original manager) = %v, want %v", got, testValue)
	}

	// Cleanup
	err = mgr.Delete(ctx2, testKey)
	if err != nil {
		t.Logf("Cleanup Delete() after etcd restart (original manager) failed: %v", err)
	}

	t.Logf("Wait for 20 seconds for invalid leases to expire")
	time.Sleep(20 * time.Second) // Wait for etcd to stabilize

	// Create a new manager and connect after etcd restart
	mgr2, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() (mgr2) failed: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Connect() (mgr2) after etcd restart failed: %v", err)
	}
	defer mgr2.Close()

	err = mgr2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() (mgr2) after etcd restart failed: %v", err)
	}

	// Register a new node address with mgr2
	newNodeAddr := "localhost:47011"
	err = mgr2.RegisterNode(ctx2, newNodeAddr)
	if err != nil {
		t.Fatalf("RegisterNode() (mgr2) failed: %v", err)
	}

	// Wait for etcd propagation
	time.Sleep(500 * time.Millisecond)

	// Old manager (mgr) should see the new node in GetNodes()
	nodesAfter := mgr.GetNodes()
	t.Logf("Nodes after etcd restart: %v", nodesAfter)
	foundNew := false
	for _, addr := range nodesAfter {
		if addr == newNodeAddr {
			foundNew = true
			break
		}
	}
	if !foundNew {
		t.Fatalf("Old manager did not see new node %s registered by mgr2", newNodeAddr)
	}

	// Verify mgr2 sees both nodes (newNodeAddr and nodeAddress)
	nodesMgr2 := mgr2.GetNodes()
	t.Logf("mgr2.GetNodes() returned: %v", nodesMgr2)
	if len(nodesMgr2) < 2 {
		t.Fatalf("mgr2.GetNodes() returned %d nodes, want at least 2", len(nodesMgr2))
	}

	// Cleanup
	err = mgr2.UnregisterNode(ctx2, newNodeAddr)
	if err != nil {
		t.Logf("Cleanup UnregisterNode() (mgr2) failed: %v", err)
	}

	// Verify the test passed - if we got here without panic, the test succeeded
	t.Log("Test passed: EtcdManager handled etcd server crash gracefully without failure")
}

// executeCommand is a helper function to execute shell commands
func executeCommand(cmd string) (string, error) {
	parts := []string{"sh", "-c", cmd}
	execCmd := &struct {
		name string
		args []string
	}{
		name: parts[0],
		args: parts[1:],
	}

	// Using a simple approach since we need sudo commands
	var output []byte
	var err error

	// Import os/exec if not already imported
	output, err = exec.Command(execCmd.name, execCmd.args...).CombinedOutput()
	return string(output), err
}

// TestEtcdManagerGetLeaderNode_EmptyNodes tests GetLeaderNode with no nodes
func TestEtcdManagerGetLeaderNode_EmptyNodes(t *testing.T) {
	mgr, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	leader := mgr.GetLeaderNode()
	if leader != "" {
		t.Fatalf("GetLeaderNode() should return empty string when no nodes, got %s", leader)
	}
}

// TestEtcdManagerGetLeaderNode_SingleNode tests GetLeaderNode with one node
func TestEtcdManagerGetLeaderNode_SingleNode(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Start watching nodes
	err := mgr.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() error = %v", err)
	}

	// Register a single node
	nodeAddress := "localhost:50001"
	err = mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Wait for watch to process
	time.Sleep(500 * time.Millisecond)

	// The leader should be the only node
	leader := mgr.GetLeaderNode()
	if leader != nodeAddress {
		t.Fatalf("GetLeaderNode() = %s, want %s", leader, nodeAddress)
	}

	// Cleanup
	mgr.UnregisterNode(ctx, nodeAddress)
}

// TestEtcdManagerGetLeaderNode_MultipleNodes tests GetLeaderNode with multiple nodes
func TestEtcdManagerGetLeaderNode_MultipleNodes(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Create multiple managers for multiple nodes
	mgr1 := setupEtcdTest(t)
	if mgr1 == nil {
		return
	}

	mgr2, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() for mgr2 failed: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Connect() for mgr2 error = %v", err)
	}
	t.Cleanup(func() {
		mgr2.Close()
	})

	mgr3, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() for mgr3 failed: %v", err)
	}
	err = mgr3.Connect()
	if err != nil {
		t.Fatalf("Connect() for mgr3 error = %v", err)
	}
	t.Cleanup(func() {
		mgr3.Close()
	})

	ctx := context.Background()

	// Start watching on all managers
	err = mgr1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr1 error = %v", err)
	}

	err = mgr2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr2 error = %v", err)
	}

	err = mgr3.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr3 error = %v", err)
	}

	// Register nodes with different addresses
	// Note: lexicographically ordered, node2 < node1 < node3
	node1Address := "localhost:50003"
	node2Address := "localhost:50001"
	node3Address := "localhost:50005"

	err = mgr1.RegisterNode(ctx, node1Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node1 error = %v", err)
	}

	err = mgr2.RegisterNode(ctx, node2Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node2 error = %v", err)
	}

	err = mgr3.RegisterNode(ctx, node3Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node3 error = %v", err)
	}

	// Wait for watches to process all registrations
	time.Sleep(1 * time.Second)

	// All managers should see the same leader (node2 with smallest address)
	leader1 := mgr1.GetLeaderNode()
	leader2 := mgr2.GetLeaderNode()
	leader3 := mgr3.GetLeaderNode()

	expectedLeader := node2Address // "localhost:50001" is smallest

	if leader1 != expectedLeader {
		t.Fatalf("GetLeaderNode() on mgr1 = %s, want %s", leader1, expectedLeader)
	}

	if leader2 != expectedLeader {
		t.Fatalf("GetLeaderNode() on mgr2 = %s, want %s", leader2, expectedLeader)
	}

	if leader3 != expectedLeader {
		t.Fatalf("GetLeaderNode() on mgr3 = %s, want %s", leader3, expectedLeader)
	}

	// Cleanup
	mgr1.UnregisterNode(ctx, node1Address)
	mgr2.UnregisterNode(ctx, node2Address)
	mgr3.UnregisterNode(ctx, node3Address)
}

// TestEtcdManagerGetLeaderNode_LeaderChanges tests leader changes when nodes leave
func TestEtcdManagerGetLeaderNode_LeaderChanges(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	// Create two managers for two nodes
	mgr1 := setupEtcdTest(t)
	if mgr1 == nil {
		return
	}

	mgr2, err := NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() for mgr2 failed: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Connect() for mgr2 error = %v", err)
	}
	t.Cleanup(func() {
		mgr2.Close()
	})

	ctx := context.Background()

	// Start watching on both managers
	err = mgr1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr1 error = %v", err)
	}

	err = mgr2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() on mgr2 error = %v", err)
	}

	// Register nodes - node1 is lexicographically smaller
	node1Address := "localhost:50010"
	node2Address := "localhost:50020"

	err = mgr1.RegisterNode(ctx, node1Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node1 error = %v", err)
	}

	err = mgr2.RegisterNode(ctx, node2Address)
	if err != nil {
		t.Fatalf("RegisterNode() for node2 error = %v", err)
	}

	// Wait for watches to process
	time.Sleep(1 * time.Second)

	// Leader should be node1 (smallest address)
	leader := mgr2.GetLeaderNode()
	if leader != node1Address {
		t.Fatalf("Initial leader = %s, want %s", leader, node1Address)
	}

	// Unregister node1 (current leader)
	err = mgr1.UnregisterNode(ctx, node1Address)
	if err != nil {
		t.Fatalf("UnregisterNode() for node1 error = %v", err)
	}

	// Wait for watch to detect removal
	time.Sleep(1 * time.Second)

	// Leader should now be node2 (the only remaining node)
	leader = mgr2.GetLeaderNode()
	if leader != node2Address {
		t.Fatalf("After node1 left, leader = %s, want %s", leader, node2Address)
	}

	// Cleanup
	mgr2.UnregisterNode(ctx, node2Address)
}

// TestEtcdManagerGetLeaderNode_LexicographicOrder tests lexicographic ordering
func TestEtcdManagerGetLeaderNode_LexicographicOrder(t *testing.T) {
	// Serialize etcd tests to prevent interference
	testutil.EtcdTestMutex.Lock()
	defer testutil.EtcdTestMutex.Unlock()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Start watching
	err := mgr.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("WatchNodes() error = %v", err)
	}

	// Create managers for different node addresses
	managers := make([]*EtcdManager, 0)
	nodeAddresses := []string{
		"node-b:5000",
		"node-a:5000",
		"node-c:5000",
		"127.0.0.1:5000",
		"192.168.1.1:5000",
	}

	// Register first node with mgr
	err = mgr.RegisterNode(ctx, nodeAddresses[0])
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Create and register other nodes
	for i := 1; i < len(nodeAddresses); i++ {
		m, err := NewEtcdManager("localhost:2379", "")
		if err != nil {
			t.Fatalf("NewEtcdManager() failed: %v", err)
		}
		err = m.Connect()
		if err != nil {
			t.Fatalf("Connect() failed: %v", err)
		}
		managers = append(managers, m)

		err = m.WatchNodes(ctx)
		if err != nil {
			t.Fatalf("WatchNodes() error = %v", err)
		}

		err = m.RegisterNode(ctx, nodeAddresses[i])
		if err != nil {
			t.Fatalf("RegisterNode(%s) error = %v", nodeAddresses[i], err)
		}
	}

	// Wait for all watches to process
	time.Sleep(1 * time.Second)

	// The expected leader is the lexicographically smallest address
	// Among: "127.0.0.1:5000", "192.168.1.1:5000", "node-a:5000", "node-b:5000", "node-c:5000"
	// "127.0.0.1:5000" is the smallest (numbers come before letters in ASCII)
	expectedLeader := "127.0.0.1:5000"

	leader := mgr.GetLeaderNode()
	if leader != expectedLeader {
		t.Fatalf("GetLeaderNode() = %s, want %s", leader, expectedLeader)
	}

	// Cleanup
	mgr.UnregisterNode(ctx, nodeAddresses[0])
	for i, m := range managers {
		m.UnregisterNode(ctx, nodeAddresses[i+1])
		m.Close()
	}
}
