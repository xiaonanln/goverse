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

// TestEtcdManagerPutGet tests put and get operations
func TestEtcdManagerPutGet(t *testing.T) {
	t.Parallel()

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

			// Get value using client directly
			client := mgr.GetClient()
			resp, err := client.Get(ctx, tt.key)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
				return
			}

			if len(resp.Kvs) == 0 {
				t.Fatalf("Get() returned no results")
			}

			got := string(resp.Kvs[0].Value)
			if got != tt.value {
				t.Fatalf("Get() = %v, want %v", got, tt.value)
			}

			// Cleanup individual key (though cleanupEtcd will handle it too)
			client.Delete(ctx, tt.key)
		})
	}
}

// TestEtcdManagerGetNonExistent tests getting non-existent key
func TestEtcdManagerGetNonExistent(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()

	// Try to get non-existent key using client directly
	client := mgr.GetClient()
	resp, err := client.Get(ctx, "non-existent-key-12345")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(resp.Kvs) != 0 {
		t.Fatal("Get() should return empty result for non-existent key")
	}
}

// TestEtcdManagerDelete tests delete operation
func TestEtcdManagerDelete(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	client := mgr.GetClient()

	// Put a value
	key := "test-delete-key"
	value := "test-delete-value"
	err := mgr.Put(ctx, key, value)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Verify it exists
	resp, err := client.Get(ctx, key)
	if err != nil || len(resp.Kvs) == 0 {
		t.Fatalf("Get() after Put() failed")
	}
	got := string(resp.Kvs[0].Value)
	if got != value {
		t.Fatalf("Get() after Put() = %v, want %v", got, value)
	}

	// Delete the key using client directly
	_, err = client.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	resp, err = client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() after Delete() error = %v", err)
	}
	if len(resp.Kvs) != 0 {
		t.Fatalf("Get() should return empty result after Delete()")
	}
}

// TestEtcdManagerWatch tests watch functionality
func TestEtcdManagerWatch(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "test-watch-key"
	value := "test-watch-value"

	// Start watching using client directly
	client := mgr.GetClient()
	watchChan := client.Watch(ctx, key)
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
	t.Parallel()

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
}

// TestEtcdManagerClose tests closing the connection
func TestEtcdManagerClose(t *testing.T) {
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

// TestEtcdManagerRegisterMultipleNodes tests that registering multiple different nodes with one manager fails
func TestEtcdManagerRegisterMultipleNodes(t *testing.T) {
	t.Parallel()

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

	// Wait a bit to allow maintainLease goroutine to create the lease and complete registration
	time.Sleep(500 * time.Millisecond)

	// Get all registered nodes
	registeredNodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
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
	remainingNodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
		return
	}

	if len(remainingNodes) != 0 {
		t.Fatalf("After cleanup, GetNodes() returned %d nodes, want 0", len(remainingNodes))
	}
}

// TestEtcdManagerRegisterNode tests node registration
func TestEtcdManagerRegisterNode(t *testing.T) {
	t.Parallel()

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

	// Wait for maintainLease to complete registration
	time.Sleep(500 * time.Millisecond)

	// Verify node is registered
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
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
	t.Parallel()

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

	// Wait for maintainLease to complete registration
	time.Sleep(500 * time.Millisecond)

	// Verify only first node is registered
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
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
	t.Parallel()

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
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
		return
	}

	for _, node := range nodes {
		if node == nodeAddress {
			t.Fatalf("Unregistered node %s still found in node list", nodeAddress)
		}
	}
}

// TestEtcdManagergetAllNodesForTesting tests retrieving all nodes
func TestEtcdManagergetAllNodesForTesting(t *testing.T) {
	t.Parallel()

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

	// Wait for maintainLease to complete registration
	time.Sleep(500 * time.Millisecond)

	// Get all nodes
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
		return
	}

	if len(nodes) == 0 {
		t.Fatal("getAllNodesForTesting() returned empty list")
	}

	// Cleanup
	for _, addr := range nodeAddresses {
		mgr.UnregisterNode(ctx, addr)
	}
}

// TestEtcdManagerRegisterNodeWithoutConnect tests registering without connection
func TestEtcdManagerRegisterNodeWithoutConnect(t *testing.T) {
	t.Parallel()

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

func isGithubAction() bool {
	// GitHub Actions sets GITHUB_ACTIONS=true; check the environment variable directly
	return os.Getenv("GITHUB_ACTIONS") == "true"
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
