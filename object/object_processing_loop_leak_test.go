package object

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

// countingProvider wraps MockPersistenceProvider and counts
// GetPendingReliableCalls invocations. Used to detect a runaway
// runProcessingLoop goroutine.
type countingProvider struct {
	*MockPersistenceProvider
	pendingCalls atomic.Int64
}

func (p *countingProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*ReliableCall, error) {
	p.pendingCalls.Add(1)
	return nil, nil
}

// TestProcessingLoopExitsAfterEagerDrain reproduces the goroutine leak that
// occurs when ProcessPendingReliableCalls is invoked with math.MaxInt64 to
// kick off an eager drain (see node.createObject). The MaxInt64 waiter is
// never popped, so runProcessingLoop polls GetPendingReliableCalls forever
// via tryExitIfNoWaiters. Across thousands of objects this drives massive
// load against the persistence layer.
func TestProcessingLoopExitsAfterEagerDrain(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "leak-test")

	provider := &countingProvider{MockPersistenceProvider: NewMockPersistenceProvider()}

	// Mirror the eager-drain call site in node.createObject.
	_ = obj.ProcessPendingReliableCalls(provider, math.MaxInt64)

	// Loop polls every 10ms; give it plenty of time to settle.
	time.Sleep(150 * time.Millisecond)
	countAfterSettle := provider.pendingCalls.Load()

	// If the loop has correctly exited, no further fetches happen.
	time.Sleep(150 * time.Millisecond)
	countAfterWait := provider.pendingCalls.Load()

	if countAfterWait != countAfterSettle {
		t.Fatalf("processing loop never exited: GetPendingReliableCalls grew from %d to %d during idle period", countAfterSettle, countAfterWait)
	}

	obj.seqWaitersMu.Lock()
	running := obj.processingCalls
	waiters := len(obj.seqWaiters)
	obj.seqWaitersMu.Unlock()

	if running {
		t.Fatalf("processingCalls flag still true after eager drain (waiters=%d); goroutine leaked", waiters)
	}
}
