package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	uerrors "github.com/xiaonanln/goverse/util/errors"
	"github.com/xiaonanln/goverse/util/metrics"
)

// TestRecordOperationResult verifies that the timeout counter is emitted only
// for timeout errors and that the operation runs for each outcome without
// panicking. Duration histogram semantics are covered by util/metrics tests.
func TestRecordOperationResult(t *testing.T) {
	const operation = "TestOp"
	// Unique address per run so the counter series is isolated from other tests.
	addr := fmt.Sprintf("recordop-%d", time.Now().UnixNano())

	timeouts := func() float64 {
		return testutil.ToFloat64(metrics.OperationTimeoutsTotal.WithLabelValues(addr, operation))
	}

	// Success: no timeout counter.
	recordOperationResult(addr, operation, time.Now(), nil)
	if got := timeouts(); got != 0 {
		t.Fatalf("success: timeout counter = %v, want 0", got)
	}

	// Non-timeout error: no timeout counter.
	recordOperationResult(addr, operation, time.Now(), fmt.Errorf("boom"))
	if got := timeouts(); got != 0 {
		t.Fatalf("error: timeout counter = %v, want 0", got)
	}

	// TimeoutError: counter increments.
	recordOperationResult(addr, operation, time.Now(),
		uerrors.NewTimeoutError(operation, "obj-1", context.DeadlineExceeded))
	if got := timeouts(); got != 1 {
		t.Fatalf("TimeoutError: timeout counter = %v, want 1", got)
	}

	// Raw context.DeadlineExceeded is also classified as a timeout.
	recordOperationResult(addr, operation, time.Now(), context.DeadlineExceeded)
	if got := timeouts(); got != 2 {
		t.Fatalf("DeadlineExceeded: timeout counter = %v, want 2", got)
	}
}
