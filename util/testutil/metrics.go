package testutil

import (
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/util/metrics"
)

// Global mutex for metrics-related tests to prevent parallel execution
// This ensures that tests accessing global Prometheus metrics don't interfere with each other
var metricsTestMutex sync.Mutex

// LockMetrics acquires a global lock for metrics-related tests and resets all metrics.
// This prevents multiple metrics tests from running in parallel, which would cause
// race conditions on global Prometheus metric registries.
//
// The lock is automatically released via t.Cleanup when the test completes.
//
// All global metrics are reset to ensure a clean state for the test:
// - metrics.AssignedShardsTotal
// - metrics.ObjectCount
// - metrics.ClientsConnected
//
// Usage example:
//
//	func TestMyMetrics(t *testing.T) {
//	    testutil.LockMetrics(t)
//
//	    // Metrics are already reset, start testing
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

	// Reset all global metrics to ensure clean state
	metrics.AssignedShardsTotal.Reset()
	metrics.ObjectCount.Reset()
	metrics.ClientsConnected.Reset()

	// Register cleanup to release the lock when test completes
	t.Cleanup(func() {
		metricsTestMutex.Unlock()
	})

	t.Logf("Acquired global metrics test lock and reset all metrics")
}
