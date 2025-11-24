package callcontext

import (
	"context"
	"testing"
	"time"
)

func TestWithClientID(t *testing.T) {
	ctx := context.Background()
	clientID := "localhost:7001/abc123"

	// Add client ID to context
	ctx = WithClientID(ctx, clientID)

	// Verify it was stored
	if !FromClient(ctx) {
		t.Error("Expected context to have client ID")
	}

	// Verify we can retrieve it
	retrievedID := ClientID(ctx)
	if retrievedID != clientID {
		t.Errorf("Expected client ID %q, got %q", clientID, retrievedID)
	}
}

func TestGetClientID_NotPresent(t *testing.T) {
	ctx := context.Background()

	// Context without client ID should return empty string
	if FromClient(ctx) {
		t.Error("Expected context to not have client ID")
	}

	clientID := ClientID(ctx)
	if clientID != "" {
		t.Errorf("Expected empty string, got %q", clientID)
	}
}

func TestGetClientID_EmptyString(t *testing.T) {
	ctx := context.Background()
	ctx = WithClientID(ctx, "")

	// Even empty string should be stored
	if !FromClient(ctx) {
		t.Error("Expected context to have client ID (even if empty)")
	}

	clientID := ClientID(ctx)
	if clientID != "" {
		t.Errorf("Expected empty string, got %q", clientID)
	}
}

func TestContextIsolation(t *testing.T) {
	ctx1 := context.Background()
	ctx2 := context.Background()

	// Add client ID to ctx1
	ctx1 = WithClientID(ctx1, "gate1/client1")

	// ctx2 should not have the client ID
	if FromClient(ctx2) {
		t.Error("Expected ctx2 to not have client ID")
	}

	// ctx1 should still have it
	if !FromClient(ctx1) {
		t.Error("Expected ctx1 to have client ID")
	}
}

func TestContextChaining(t *testing.T) {
	ctx := context.Background()
	ctx = WithClientID(ctx, "gate1/client1")

	// Derive a new context from ctx
	derivedCtx := context.WithValue(ctx, "other-key", "other-value")

	// Client ID should still be accessible in derived context
	if !FromClient(derivedCtx) {
		t.Error("Expected derived context to have client ID")
	}

	clientID := ClientID(derivedCtx)
	if clientID != "gate1/client1" {
		t.Errorf("Expected client ID %q, got %q", "gate1/client1", clientID)
	}
}

func TestWithDefaultTimeout_NoExistingDeadline(t *testing.T) {
	ctx := context.Background()
	defaultTimeout := 5 * time.Second

	// Context without deadline should have default timeout applied
	newCtx, cancel := WithDefaultTimeout(ctx, defaultTimeout)
	defer cancel()

	deadline, hasDeadline := newCtx.Deadline()
	if !hasDeadline {
		t.Fatal("Expected context to have a deadline after WithDefaultTimeout")
	}

	// Deadline should be approximately defaultTimeout from now
	expectedDeadline := time.Now().Add(defaultTimeout)
	diff := deadline.Sub(expectedDeadline)
	if diff < -100*time.Millisecond || diff > 100*time.Millisecond {
		t.Errorf("Deadline diff from expected: %v (should be within ±100ms)", diff)
	}
}

func TestWithDefaultTimeout_ExistingDeadline(t *testing.T) {
	// Create context with existing deadline
	existingTimeout := 2 * time.Second
	ctx, existingCancel := context.WithTimeout(context.Background(), existingTimeout)
	defer existingCancel()

	existingDeadline, _ := ctx.Deadline()

	// Apply default timeout (longer than existing)
	defaultTimeout := 10 * time.Second
	newCtx, cancel := WithDefaultTimeout(ctx, defaultTimeout)
	defer cancel()

	// Deadline should be preserved (not replaced)
	newDeadline, hasDeadline := newCtx.Deadline()
	if !hasDeadline {
		t.Fatal("Expected context to still have deadline")
	}

	if !newDeadline.Equal(existingDeadline) {
		t.Errorf("Expected deadline to be preserved at %v, got %v", existingDeadline, newDeadline)
	}
}

func TestWithDefaultTimeout_CancelNoopWhenDeadlineExists(t *testing.T) {
	// Create context with existing deadline
	ctx, existingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer existingCancel()

	// Apply default timeout
	_, cancel := WithDefaultTimeout(ctx, 10*time.Second)

	// Cancel should be a no-op and not panic
	cancel()
	cancel() // Call multiple times to ensure it's safe
}
