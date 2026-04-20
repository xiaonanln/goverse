package object

import (
	"context"
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

// TestProcessingLoopExitsAfterEagerDrain verifies that triggering an eager
// drain on object activation (mirroring node.createObject) does not leak the
// processing goroutine.
//
// Pre-fix behavior: createObject called
//
//	ProcessPendingReliableCalls(provider, math.MaxInt64)
//
// which inserted a waiter at seq=MaxInt64 that notifyWaiters could never pop.
// tryExitIfNoWaiters then kept the loop alive, polling
// GetPendingReliableCalls every 10ms forever — for every persistent object
// on every node.
//
// Post-fix: createObject calls EnsureProcessingLoopRunning, which starts the
// loop without registering a waiter. The loop fetches once and exits.
func TestProcessingLoopExitsAfterEagerDrain(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "leak-test")

	provider := &countingProvider{MockPersistenceProvider: NewMockPersistenceProvider()}

	// Mirror the eager-drain call site in node.createObject.
	obj.EnsureProcessingLoopRunning(provider)

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
