package callcontext

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
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

func TestWithCallerIdentity(t *testing.T) {
	ctx := context.Background()
	id := &CallerIdentity{UserID: "user1", Roles: []string{"admin", "viewer"}}

	ctx = WithCallerIdentity(ctx, id)

	got := GetCallerIdentity(ctx)
	if got == nil {
		t.Fatal("Expected CallerIdentity, got nil")
	}
	if got.UserID != id.UserID {
		t.Errorf("Expected UserID %q, got %q", id.UserID, got.UserID)
	}
	if len(got.Roles) != len(id.Roles) || got.Roles[0] != id.Roles[0] {
		t.Errorf("Expected Roles %v, got %v", id.Roles, got.Roles)
	}
}

func TestGetCallerIdentity_NotPresent(t *testing.T) {
	ctx := context.Background()
	if got := GetCallerIdentity(ctx); got != nil {
		t.Errorf("Expected nil CallerIdentity, got %+v", got)
	}
}

func TestWithCallerIdentity_NilIdentity(t *testing.T) {
	ctx := WithCallerIdentity(context.Background(), nil)
	if got := GetCallerIdentity(ctx); got != nil {
		t.Errorf("Expected nil after storing nil identity, got %+v", got)
	}
}

func TestCallerIdentity_IsolatedFromOtherContexts(t *testing.T) {
	id := &CallerIdentity{UserID: "user1"}
	ctx1 := WithCallerIdentity(context.Background(), id)
	ctx2 := context.Background()

	if GetCallerIdentity(ctx2) != nil {
		t.Error("Expected ctx2 to have no CallerIdentity")
	}
	if GetCallerIdentity(ctx1) == nil {
		t.Error("Expected ctx1 to have CallerIdentity")
	}
}

func TestCallerIdentity_PreservedInDerivedContext(t *testing.T) {
	id := &CallerIdentity{UserID: "user1", Roles: []string{"editor"}}
	ctx := WithCallerIdentity(context.Background(), id)
	derived := context.WithValue(ctx, "other-key", "other-value")

	got := GetCallerIdentity(derived)
	if got == nil || got.UserID != "user1" {
		t.Errorf("Expected CallerIdentity to be preserved in derived context, got %+v", got)
	}
}

func TestCallerUserID(t *testing.T) {
	ctx := WithCallerIdentity(context.Background(), &CallerIdentity{UserID: "alice"})
	if got := CallerUserID(ctx); got != "alice" {
		t.Errorf("CallerUserID = %q, want %q", got, "alice")
	}
}

func TestCallerUserID_Unauthenticated(t *testing.T) {
	if got := CallerUserID(context.Background()); got != "" {
		t.Errorf("CallerUserID on unauthenticated context = %q, want \"\"", got)
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

func TestInjectCallerToOutgoing_UserIDOnly(t *testing.T) {
	ctx := WithCallerIdentity(context.Background(), &CallerIdentity{UserID: "alice"})
	out := InjectCallerToOutgoing(ctx)

	md, ok := metadata.FromOutgoingContext(out)
	if !ok {
		t.Fatal("Expected outgoing metadata to be set")
	}
	if vals := md[mdKeyCallerUserID]; len(vals) == 0 || vals[0] != "alice" {
		t.Errorf("Expected %q = %q, got %v", mdKeyCallerUserID, "alice", vals)
	}
	if vals := md[mdKeyCallerRoles]; len(vals) != 0 {
		t.Errorf("Expected no roles metadata, got %v", vals)
	}
}

func TestInjectCallerToOutgoing_WithRoles(t *testing.T) {
	ctx := WithCallerIdentity(context.Background(), &CallerIdentity{
		UserID: "alice",
		Roles:  []string{"admin", "viewer"},
	})
	out := InjectCallerToOutgoing(ctx)

	md, ok := metadata.FromOutgoingContext(out)
	if !ok {
		t.Fatal("Expected outgoing metadata to be set")
	}
	if vals := md[mdKeyCallerUserID]; len(vals) == 0 || vals[0] != "alice" {
		t.Errorf("Expected %q = %q, got %v", mdKeyCallerUserID, "alice", vals)
	}
	roles := md[mdKeyCallerRoles]
	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "viewer" {
		t.Errorf("Expected roles [admin viewer], got %v", roles)
	}
}

func TestInjectCallerToOutgoing_NoIdentity(t *testing.T) {
	ctx := context.Background()
	out := InjectCallerToOutgoing(ctx)

	if md, ok := metadata.FromOutgoingContext(out); ok {
		if vals := md[mdKeyCallerUserID]; len(vals) > 0 {
			t.Errorf("Expected no %q metadata, got %v", mdKeyCallerUserID, vals)
		}
	}
}

func TestExtractCallerFromIncoming_UserIDOnly(t *testing.T) {
	md := metadata.Pairs(mdKeyCallerUserID, "bob")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	out := ExtractCallerFromIncoming(ctx)

	id := GetCallerIdentity(out)
	if id == nil {
		t.Fatal("Expected CallerIdentity to be set")
	}
	if id.UserID != "bob" {
		t.Errorf("Expected UserID %q, got %q", "bob", id.UserID)
	}
	if len(id.Roles) != 0 {
		t.Errorf("Expected no roles, got %v", id.Roles)
	}
}

func TestExtractCallerFromIncoming_WithRoles(t *testing.T) {
	md := metadata.Pairs(
		mdKeyCallerUserID, "carol",
		mdKeyCallerRoles, "editor",
		mdKeyCallerRoles, "viewer",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	out := ExtractCallerFromIncoming(ctx)

	id := GetCallerIdentity(out)
	if id == nil {
		t.Fatal("Expected CallerIdentity to be set")
	}
	if id.UserID != "carol" {
		t.Errorf("Expected UserID %q, got %q", "carol", id.UserID)
	}
	if len(id.Roles) != 2 || id.Roles[0] != "editor" || id.Roles[1] != "viewer" {
		t.Errorf("Expected roles [editor viewer], got %v", id.Roles)
	}
}

func TestExtractCallerFromIncoming_NoMetadata(t *testing.T) {
	ctx := context.Background()
	out := ExtractCallerFromIncoming(ctx)

	if id := GetCallerIdentity(out); id != nil {
		t.Errorf("Expected no CallerIdentity, got %+v", id)
	}
}

func TestExtractCallerFromIncoming_EmptyUserID(t *testing.T) {
	md := metadata.Pairs(mdKeyCallerUserID, "")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	out := ExtractCallerFromIncoming(ctx)

	if id := GetCallerIdentity(out); id != nil {
		t.Errorf("Expected no CallerIdentity for empty user ID, got %+v", id)
	}
}

func TestInjectExtract_RoundTrip(t *testing.T) {
	// Simulate the full gate→node propagation: inject on outgoing, extract on incoming.
	// gRPC doesn't let us directly convert outgoing→incoming MD in a unit test,
	// so we build the incoming context manually from the same pairs.
	original := &CallerIdentity{UserID: "dave", Roles: []string{"admin", "editor"}}
	outCtx := InjectCallerToOutgoing(WithCallerIdentity(context.Background(), original))

	outMD, _ := metadata.FromOutgoingContext(outCtx)
	inCtx := metadata.NewIncomingContext(context.Background(), outMD)
	restored := GetCallerIdentity(ExtractCallerFromIncoming(inCtx))

	if restored == nil {
		t.Fatal("Expected CallerIdentity after round-trip, got nil")
	}
	if restored.UserID != original.UserID {
		t.Errorf("UserID: want %q, got %q", original.UserID, restored.UserID)
	}
	if len(restored.Roles) != len(original.Roles) {
		t.Fatalf("Roles len: want %d, got %d", len(original.Roles), len(restored.Roles))
	}
	for i, r := range original.Roles {
		if restored.Roles[i] != r {
			t.Errorf("Role[%d]: want %q, got %q", i, r, restored.Roles[i])
		}
	}
}
