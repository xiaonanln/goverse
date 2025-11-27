package main

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// TestInspectorEtcdRegistration tests that the inspector can register its address in etcd
func TestInspectorEtcdRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	// Check if etcd is available before running the test
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Skipf("Skipping test: failed to create etcd client: %v", err)
		return
	}

	// Test connection with a quick operation
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = cli.Get(ctx, "test-connection")
	cancel()
	if err != nil {
		cli.Close()
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}
	cli.Close()

	// Now run the actual test with a unique prefix
	prefix := "/goverse-test/TestInspectorEtcdRegistration"

	// Create an etcd manager
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	if err := mgr.Connect(); err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}
	defer mgr.Close()

	ctx = context.Background()

	// Clean up the prefix before the test
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 5*time.Second)
	_, _ = mgr.GetClient().Delete(cleanupCtx, prefix+"/", clientv3.WithPrefix())
	cleanupCancel()

	// Clean up after the test
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = mgr.GetClient().Delete(cleanupCtx, prefix+"/", clientv3.WithPrefix())
	})

	// Test registering inspector address
	inspectorKey := mgr.GetPrefix() + "/inspector"
	inspectorAddress := ":8081"

	_, err = mgr.RegisterKeyLease(ctx, inspectorKey, inspectorAddress, etcdmanager.NodeLeaseTTL)
	if err != nil {
		t.Fatalf("Failed to register inspector with etcd: %v", err)
	}

	// Wait for the key to be registered
	time.Sleep(500 * time.Millisecond)

	// Verify the key exists in etcd
	resp, err := mgr.GetClient().Get(ctx, inspectorKey)
	if err != nil {
		t.Fatalf("Failed to get inspector key from etcd: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Inspector key not found in etcd")
	}
	if string(resp.Kvs[0].Value) != inspectorAddress {
		t.Fatalf("Inspector address mismatch: got %s, want %s", string(resp.Kvs[0].Value), inspectorAddress)
	}

	// Verify the key has a lease (ephemeral)
	if resp.Kvs[0].Lease == 0 {
		t.Fatalf("Inspector key should have a lease")
	}

	t.Logf("Inspector registered at key %s with value %s and lease %d", inspectorKey, inspectorAddress, resp.Kvs[0].Lease)

	// Unregister and verify the key is removed
	err = mgr.UnregisterKeyLease(ctx, inspectorKey)
	if err != nil {
		t.Fatalf("Failed to unregister inspector from etcd: %v", err)
	}

	// Wait for the key to be unregistered
	time.Sleep(500 * time.Millisecond)

	resp, err = mgr.GetClient().Get(ctx, inspectorKey)
	if err != nil {
		t.Fatalf("Failed to check inspector key after unregister: %v", err)
	}
	if len(resp.Kvs) > 0 {
		t.Fatalf("Inspector key should be removed after unregister")
	}

	t.Logf("Inspector unregistered successfully")
}

// TestInspectorKeyPath tests the inspector key path format
func TestInspectorKeyPath(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantKey string
	}{
		{
			name:    "default prefix",
			prefix:  "/goverse",
			wantKey: "/goverse/inspector",
		},
		{
			name:    "custom prefix",
			prefix:  "/myapp",
			wantKey: "/myapp/inspector",
		},
		{
			name:    "prefix with trailing slash",
			prefix:  "/test/",
			wantKey: "/test//inspector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the key construction logic from main.go
			inspectorKey := tt.prefix + "/inspector"
			if inspectorKey != tt.wantKey {
				t.Fatalf("Inspector key mismatch: got %s, want %s", inspectorKey, tt.wantKey)
			}
		})
	}
}
