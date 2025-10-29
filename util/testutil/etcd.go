package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdTestMutex ensures only one etcd integration test runs at a time across all packages.
// This prevents tests from interfering with each other when using the same etcd instance.
//
// Usage in test files:
//
//	import "github.com/xiaonanln/goverse/util/testutil"
//
//	func TestSomethingWithEtcd(t *testing.T) {
//	    testutil.EtcdTestMutex.Lock()
//	    defer testutil.EtcdTestMutex.Unlock()
//	    // ... test code that uses etcd
//	}
var EtcdTestMutex sync.Mutex

// PrepareEtcdPrefix prepares a unique etcd prefix for testing purposes.
// This function:
// 1. Generates a unique prefix based on the test name
// 2. Cleans everything under the generated prefix to ensure test isolation
// 3. Registers a cleanup function to remove everything under the prefix after the test
// 4. Returns the generated prefix for use with etcd managers or other purposes
//
// The function connects to etcd at the specified address to perform cleanup.
// If etcd is not available, the test will be skipped.
//
// Usage example:
//
//	func TestMyEtcdFeature(t *testing.T) {
//	    prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
//	    
//	    // Use the prefix with an etcd manager
//	    mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
//	    if err != nil {
//	        t.Fatalf("Failed to create etcd manager: %v", err)
//	    }
//	    // ... rest of test
//	}
//
// Parameters:
//   - t: The testing.T instance for the current test
//   - etcdAddress: The address of the etcd server (e.g., "localhost:2379")
//
// Returns:
//   - string: The unique prefix for this test (e.g., "/goverse-test/TestMyFeature")
func PrepareEtcdPrefix(t *testing.T, etcdAddress string) string {
	// Generate a unique prefix for this test based on the test name
	prefix := "/goverse-test/" + t.Name()

	// Create a temporary etcd client for cleanup operations with a timeout
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdAddress},
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Skipf("Skipping test: failed to connect to etcd at %s: %v", etcdAddress, err)
		return ""
	}

	// Clean up everything under the prefix before the test starts
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	_, err = cli.Delete(ctx, prefix+"/", clientv3.WithPrefix())
	if err != nil {
		t.Logf("Warning: failed to clean etcd prefix before test: %v", err)
	}

	// Register cleanup to remove everything under the prefix after the test
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		_, err := cli.Delete(ctx, prefix+"/", clientv3.WithPrefix())
		if err != nil {
			t.Logf("Warning: failed to clean etcd prefix after test: %v", err)
		}
		cli.Close()
	})

	return prefix
}
