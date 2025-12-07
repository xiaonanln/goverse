package testutil

import (
	"testing"
	"time"
)

// WaitFor polls a condition function until it returns true or times out.
// It's useful for waiting on asynchronous operations in tests.
// The condition is checked every 100ms.
//
// Parameters:
//   - t: The testing.TB instance for the current test
//   - timeout: Maximum time to wait for the condition
//   - message: Error message to display if timeout occurs
//   - condition: A function that returns true when the desired state is reached
//
// Usage:
//
//	testutil.WaitFor(t, 10*time.Second, "shard mapping to reach expected count", func() bool {
//	    mapping := cluster.GetShardMapping(ctx)
//	    return len(mapping.Shards) == expectedCount
//	})
func WaitFor(t testing.TB, timeout time.Duration, message string, condition func() bool) {
	t.Helper()

	start := time.Now()
	t.Logf("Waiting for %s (timeout: %v)", message, timeout)

	// Check immediately first
	if condition() {
		t.Logf("Condition met immediately (0ms): %s", message)
		return
	}

	tickerInterval := 200 * time.Millisecond
	if timeout < tickerInterval {
		timeout = tickerInterval
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	checkCount := 1
	for range ticker.C {
		checkCount++
		elapsed := time.Since(start)
		t.Logf("Checking condition (attempt %d, elapsed %v): %s", checkCount, elapsed.Round(time.Millisecond), message)

		if condition() {
			t.Logf("Condition met after %v (%d attempts): %s", elapsed.Round(time.Millisecond), checkCount, message)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Timeout waiting for %s (waited %v, %d attempts)", message, timeout, checkCount)
		}
	}
}
