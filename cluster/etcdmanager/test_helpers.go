package etcdmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
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
