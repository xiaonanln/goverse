package testutil

import (
	"context"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// TestPrepareEtcdPrefix tests the PrepareEtcdPrefix function
func TestPrepareEtcdPrefix(t *testing.T) {
	// Test basic functionality
	prefix := PrepareEtcdPrefix(t, "localhost:2379")

	// Verify prefix is generated correctly
	expectedPrefix := "/goverse-test/" + t.Name()
	if prefix != expectedPrefix {
		t.Fatalf("PrepareEtcdPrefix() = %s, want %s", prefix, expectedPrefix)
	}

	// Verify we can use the prefix with etcd operations
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	})
	if err != nil {
		t.Skipf("Skipping: etcd not available: %v", err)
		return
	}
	defer cli.Close()

	ctx := context.Background()

	// Put a test key under the prefix
	testKey := prefix + "/test-key"
	testValue := "test-value"
	_, err = cli.Put(ctx, testKey, testValue)
	if err != nil {
		t.Fatalf("Failed to put key: %v", err)
	}

	// Verify the key exists
	resp, err := cli.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatal("Key not found after put")
	}
	if string(resp.Kvs[0].Value) != testValue {
		t.Fatalf("Got value %s, want %s", string(resp.Kvs[0].Value), testValue)
	}
}

// TestPrepareEtcdPrefix_Cleanup verifies that cleanup is performed
func TestPrepareEtcdPrefix_Cleanup(t *testing.T) {
	// Create a sub-test so we can verify cleanup happens
	var prefix string
	var testKey string

	// Run a sub-test that uses PrepareEtcdPrefix
	t.Run("SubTest", func(t *testing.T) {
		prefix = PrepareEtcdPrefix(t, "localhost:2379")
		testKey = prefix + "/cleanup-test-key"

		// Connect to etcd
		cli, err := clientv3.New(clientv3.Config{
			Endpoints: []string{"localhost:2379"},
		})
		if err != nil {
			t.Skipf("Skipping: etcd not available: %v", err)
			return
		}
		defer cli.Close()

		ctx := context.Background()

		// Put a test key
		_, err = cli.Put(ctx, testKey, "cleanup-test-value")
		if err != nil {
			t.Fatalf("Failed to put key: %v", err)
		}

		// Verify key exists
		resp, err := cli.Get(ctx, testKey)
		if err != nil {
			t.Fatalf("Failed to get key: %v", err)
		}
		if len(resp.Kvs) == 0 {
			t.Fatal("Key not found after put")
		}
	})

	// After sub-test completes, cleanup should have been called
	// Verify the key was cleaned up
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	})
	if err != nil {
		t.Skipf("Skipping: etcd not available: %v", err)
		return
	}
	defer cli.Close()

	// Give cleanup a moment to complete
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	resp, err := cli.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to get key during cleanup verification: %v", err)
	}

	if len(resp.Kvs) != 0 {
		t.Fatalf("Key still exists after cleanup, expected it to be deleted")
	}
}

// TestPrepareEtcdPrefix_Isolation tests that each test gets its own isolated prefix
func TestPrepareEtcdPrefix_Isolation(t *testing.T) {
	// Run two sub-tests with different prefixes
	var prefix1, prefix2 string

	t.Run("First", func(t *testing.T) {
		prefix1 = PrepareEtcdPrefix(t, "localhost:2379")
	})

	t.Run("Second", func(t *testing.T) {
		prefix2 = PrepareEtcdPrefix(t, "localhost:2379")
	})

	// Verify prefixes are different
	if prefix1 == prefix2 {
		t.Fatalf("Expected different prefixes for different tests, got %s for both", prefix1)
	}

	// Verify both prefixes contain the parent test name
	expectedBase := "/goverse-test/" + t.Name()
	if prefix1[:len(expectedBase)] != expectedBase {
		t.Fatalf("prefix1 %s does not start with expected base %s", prefix1, expectedBase)
	}
	if prefix2[:len(expectedBase)] != expectedBase {
		t.Fatalf("prefix2 %s does not start with expected base %s", prefix2, expectedBase)
	}
}

// TestPrepareEtcdPrefix_CleanBeforeTest verifies that the prefix is cleaned before test starts
func TestPrepareEtcdPrefix_CleanBeforeTest(t *testing.T) {
	// First, manually put a key with the expected prefix
	testPrefix := "/goverse-test/" + t.Name()
	dirtyKey := testPrefix + "/dirty-key"

	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	})
	if err != nil {
		t.Skipf("Skipping: etcd not available: %v", err)
		return
	}
	defer cli.Close()

	ctx := context.Background()

	// Put a "dirty" key that should be cleaned up
	_, err = cli.Put(ctx, dirtyKey, "dirty-value")
	if err != nil {
		t.Fatalf("Failed to put dirty key: %v", err)
	}

	// Now call PrepareEtcdPrefix, which should clean this up
	prefix := PrepareEtcdPrefix(t, "localhost:2379")

	// Verify the prefix matches
	if prefix != testPrefix {
		t.Fatalf("Got prefix %s, want %s", prefix, testPrefix)
	}

	// Verify the dirty key was cleaned up
	resp, err := cli.Get(ctx, dirtyKey)
	if err != nil {
		t.Fatalf("Failed to get dirty key: %v", err)
	}

	if len(resp.Kvs) != 0 {
		t.Fatal("Dirty key still exists after PrepareEtcdPrefix, expected it to be cleaned")
	}
}

// TestPrepareEtcdPrefix_MultipleKeys tests cleanup with multiple keys
func TestPrepareEtcdPrefix_MultipleKeys(t *testing.T) {
	var prefix string
	var keys []string

	// Run a sub-test that creates multiple keys
	t.Run("SubTest", func(t *testing.T) {
		prefix = PrepareEtcdPrefix(t, "localhost:2379")

		cli, err := clientv3.New(clientv3.Config{
			Endpoints: []string{"localhost:2379"},
		})
		if err != nil {
			t.Skipf("Skipping: etcd not available: %v", err)
			return
		}
		defer cli.Close()

		ctx := context.Background()

		// Create multiple keys under the prefix
		keys = []string{
			prefix + "/key1",
			prefix + "/key2",
			prefix + "/subdir/key3",
			prefix + "/subdir/key4",
		}

		for _, key := range keys {
			_, err := cli.Put(ctx, key, "value-"+key)
			if err != nil {
				t.Fatalf("Failed to put key %s: %v", key, err)
			}
		}

		// Verify all keys exist
		for _, key := range keys {
			resp, err := cli.Get(ctx, key)
			if err != nil {
				t.Fatalf("Failed to get key %s: %v", key, err)
			}
			if len(resp.Kvs) == 0 {
				t.Fatalf("Key %s not found after put", key)
			}
		}
	})

	// After sub-test completes, verify all keys are cleaned up
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	})
	if err != nil {
		t.Skipf("Skipping: etcd not available: %v", err)
		return
	}
	defer cli.Close()

	// Give cleanup a moment to complete
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()

	// Verify all keys are gone
	for _, key := range keys {
		resp, err := cli.Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get key %s during cleanup verification: %v", key, err)
		}
		if len(resp.Kvs) != 0 {
			t.Fatalf("Key %s still exists after cleanup", key)
		}
	}

	// Verify the entire prefix is empty
	resp, err := cli.Get(ctx, prefix+"/", clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("Failed to get prefix: %v", err)
	}
	if len(resp.Kvs) != 0 {
		t.Fatalf("Found %d keys under prefix after cleanup, expected 0", len(resp.Kvs))
	}
}
