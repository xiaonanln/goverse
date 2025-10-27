package testutil

import "sync"

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
