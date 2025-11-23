package callcontext

import (
	"context"
	"testing"
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
