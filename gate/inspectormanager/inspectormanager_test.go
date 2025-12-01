package inspectormanager

import (
	"context"
	"testing"
	"time"
)

func TestGateInspectorManager_NewGateInspectorManager(t *testing.T) {
	t.Parallel()

	gateAddr := "localhost:49000"
	mgr := NewGateInspectorManager(gateAddr)

	if mgr == nil {
		t.Fatal("NewGateInspectorManager returned nil")
	}

	if mgr.gateAddress != gateAddr {
		t.Fatalf("Expected gateAddress %s, got %s", gateAddr, mgr.gateAddress)
	}

	if mgr.logger == nil {
		t.Fatal("Logger should be initialized")
	}
}

func TestGateInspectorManager_StartStop(t *testing.T) {
	t.Parallel()

	mgr := NewGateInspectorManager("localhost:49000")
	ctx := context.Background()

	// Start should not fail even if inspector is not available
	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Allow some time for the management goroutine to start
	time.Sleep(100 * time.Millisecond)

	// Stop should not fail
	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify context was canceled
	select {
	case <-mgr.ctx.Done():
		// Expected - context should be canceled
	default:
		t.Fatal("Context should be canceled after Stop")
	}
}

func TestGateInspectorManager_StartStopMultipleTimes(t *testing.T) {
	t.Parallel()

	mgr := NewGateInspectorManager("localhost:49000")
	ctx := context.Background()

	// First start/stop cycle
	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = mgr.Stop()
	if err != nil {
		t.Fatalf("First Stop failed: %v", err)
	}

	// Create a new manager for second cycle (as the old one is stopped)
	mgr = NewGateInspectorManager("localhost:49000")

	// Second start/stop cycle
	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Second Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Second Stop failed: %v", err)
	}
}

func TestGateInspectorManager_GetContextForTesting(t *testing.T) {
	t.Parallel()

	mgr := NewGateInspectorManager("localhost:49000")
	ctx := context.Background()

	// Context should be nil before start
	if mgr.GetContextForTesting() != nil {
		t.Fatal("Context should be nil before Start")
	}

	// Start the manager
	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Context should be non-nil after start
	if mgr.GetContextForTesting() == nil {
		t.Fatal("Context should be non-nil after Start")
	}

	time.Sleep(100 * time.Millisecond)

	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Context should be canceled after stop
	select {
	case <-mgr.GetContextForTesting().Done():
		// Expected - context should be canceled
	default:
		t.Fatal("Context should be canceled after Stop")
	}
}

func TestGateInspectorManager_SetHealthCheckInterval(t *testing.T) {
	t.Parallel()

	mgr := NewGateInspectorManager("localhost:49000")

	// Default should be 5 seconds
	if mgr.healthCheckInterval != defaultHealthCheckInterval {
		t.Fatalf("Expected default health check interval %v, got %v", defaultHealthCheckInterval, mgr.healthCheckInterval)
	}

	// Set a new interval
	newInterval := 10 * time.Second
	mgr.SetHealthCheckInterval(newInterval)

	if mgr.healthCheckInterval != newInterval {
		t.Fatalf("Expected health check interval %v, got %v", newInterval, mgr.healthCheckInterval)
	}
}
