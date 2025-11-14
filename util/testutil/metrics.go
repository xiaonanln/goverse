package testutil

import (
	"sync"
	"testing"
)

// Global mutex for metrics-related tests to prevent parallel execution
// This ensures that tests accessing global Prometheus metrics don't interfere with each other
var metricsTestMutex sync.Mutex

// LockMetrics acquires a global lock for metrics-related tests.
// This prevents multiple metrics tests from running in parallel, which would cause
// race conditions on global Prometheus metric registries.
//
// The lock is automatically released via t.Cleanup when the test completes.
//
// Usage example:
//
//	func TestMyMetrics(t *testing.T) {
//	    testutil.LockMetrics(t)
//
//	    // Reset metrics - safe because we have exclusive access
//	    metrics.AssignedShardsTotal.Reset()
//
//	    // ... rest of test
//	}
//
// Why this is needed:
// Prometheus metrics (like metrics.AssignedShardsTotal) are global shared state.
// When tests run in parallel:
// - One test's Reset() clears metrics while another test reads them
// - Multiple tests update the same metric labels concurrently
// - Tests see stale or incorrect values from other tests
//
// This lock ensures sequential execution of all metrics tests, providing
// exclusive access to the global Prometheus registry.
func LockMetrics(t *testing.T) {
	t.Helper()

	// Acquire the global metrics test lock
	metricsTestMutex.Lock()

	// Register cleanup to release the lock when test completes
	t.Cleanup(func() {
		metricsTestMutex.Unlock()
	})

	t.Logf("Acquired global metrics test lock")
}
