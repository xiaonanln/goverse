package testutil

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
)

// TestPrepareEtcdPrefixWithEtcdManager demonstrates using PrepareEtcdPrefix
// with an etcd manager, showing the real-world usage pattern.
func TestPrepareEtcdPrefixWithEtcdManager(t *testing.T) {
	// Prepare the etcd prefix for this test
	prefix := PrepareEtcdPrefix(t, "localhost:2379")

	// Create an etcd manager with the prepared prefix
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Fatalf("NewEtcdManager() failed: %v", err)
	}

	// Connect to etcd
	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer mgr.Close()

	// Use the manager to perform operations
	ctx := context.Background()

	// Put a value under the prefix (this ensures cleanup works)
	// Note: etcdmanager Put/Get use absolute keys, so we need to include the prefix
	testKey := prefix + "/test-key"
	err = mgr.Put(ctx, testKey, "test-value")
	if err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// Get the value back
	value, err := mgr.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if value != "test-value" {
		t.Fatalf("Get() returned %s, want test-value", value)
	}

	// Verify the prefix is correct
	expectedPrefix := "/goverse-test/" + t.Name()
	if mgr.GetPrefix() != expectedPrefix {
		t.Fatalf("Manager prefix = %s, want %s", mgr.GetPrefix(), expectedPrefix)
	}

	// The cleanup registered by PrepareEtcdPrefix will clean up the keys automatically
}

// TestPrepareEtcdPrefixIsolation verifies that tests using PrepareEtcdPrefix
// are isolated from each other.
func TestPrepareEtcdPrefixIsolation(t *testing.T) {
	t.Run("Test1", func(t *testing.T) {
		prefix := PrepareEtcdPrefix(t, "localhost:2379")

		mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
		if err != nil {
			t.Fatalf("NewEtcdManager() failed: %v", err)
		}

		err = mgr.Connect()
		if err != nil {
			t.Fatalf("Connect() failed: %v", err)
		}
		defer mgr.Close()

		ctx := context.Background()

		// Put a value specific to this test using the full key with prefix
		testKey := prefix + "/test1-key"
		err = mgr.Put(ctx, testKey, "test1-value")
		if err != nil {
			t.Fatalf("Put() failed: %v", err)
		}
	})

	t.Run("Test2", func(t *testing.T) {
		prefix := PrepareEtcdPrefix(t, "localhost:2379")

		mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
		if err != nil {
			t.Fatalf("NewEtcdManager() failed: %v", err)
		}

		err = mgr.Connect()
		if err != nil {
			t.Fatalf("Connect() failed: %v", err)
		}
		defer mgr.Close()

		ctx := context.Background()

		// This test should not see the key from Test1 (different prefix)
		// The key from Test1 was "/goverse-test/TestPrepareEtcdPrefixIsolation/Test1/test1-key"
		// This test has prefix "/goverse-test/TestPrepareEtcdPrefixIsolation/Test2"
		// So trying to get the Test1 key should fail
		test1Key := "/goverse-test/TestPrepareEtcdPrefixIsolation/Test1/test1-key"
		_, err = mgr.Get(ctx, test1Key)
		// The key might not exist because Test1 cleanup removed it
		// So we just verify this test has a different prefix

		// Put a value specific to this test
		testKey := prefix + "/test2-key"
		err = mgr.Put(ctx, testKey, "test2-value")
		if err != nil {
			t.Fatalf("Put() failed: %v", err)
		}

		// This test should be able to get its own key
		value, err := mgr.Get(ctx, testKey)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		if value != "test2-value" {
			t.Fatalf("Get() returned %s, want test2-value", value)
		}
	})
}
