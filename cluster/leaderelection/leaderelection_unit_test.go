package leaderelection

import (
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// TestNewLeaderElection_NilClient verifies error handling for nil client
func TestNewLeaderElection_NilClient(t *testing.T) {
	t.Parallel()

	_, err := NewLeaderElection(nil, "/test/leader", "node1", 10)
	if err == nil {
		t.Error("Expected error for nil client, got nil")
	}
	if err.Error() != "etcd client cannot be nil" {
		t.Errorf("Unexpected error message: %s", err.Error())
	}
}

// TestNewLeaderElection_EmptyNodeID verifies error handling for empty node ID
func TestNewLeaderElection_EmptyNodeID(t *testing.T) {
	t.Parallel()

	// Create a mock client (won't be used since validation happens first)
	client := &clientv3.Client{}

	_, err := NewLeaderElection(client, "/test/leader", "", 10)
	if err == nil {
		t.Error("Expected error for empty nodeID, got nil")
	}
	if err.Error() != "nodeID cannot be empty" {
		t.Errorf("Unexpected error message: %s", err.Error())
	}
}

// TestNewLeaderElection_DefaultTTL verifies default TTL is used when TTL <= 0
func TestNewLeaderElection_DefaultTTL(t *testing.T) {
	t.Parallel()

	// Create a mock client
	client := &clientv3.Client{}

	le, err := NewLeaderElection(client, "/test/leader", "node1", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if le.ttl != DefaultSessionTTL {
		t.Errorf("Expected default TTL %d, got %d", DefaultSessionTTL, le.ttl)
	}
}

// TestNewLeaderElection_NegativeTTL verifies default TTL is used when TTL is negative
func TestNewLeaderElection_NegativeTTL(t *testing.T) {
	t.Parallel()

	// Create a mock client
	client := &clientv3.Client{}

	le, err := NewLeaderElection(client, "/test/leader", "node1", -5)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if le.ttl != DefaultSessionTTL {
		t.Errorf("Expected default TTL %d, got %d", DefaultSessionTTL, le.ttl)
	}
}

// TestNewLeaderElection_CustomTTL verifies custom TTL is preserved
func TestNewLeaderElection_CustomTTL(t *testing.T) {
	t.Parallel()

	// Create a mock client
	client := &clientv3.Client{}
	customTTL := 15

	le, err := NewLeaderElection(client, "/test/leader", "node1", customTTL)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if le.ttl != customTTL {
		t.Errorf("Expected TTL %d, got %d", customTTL, le.ttl)
	}
}

// TestIsLeader_InitialState verifies initial leadership state is false
func TestIsLeader_InitialState(t *testing.T) {
	t.Parallel()

	// Create a mock client
	client := &clientv3.Client{}

	le, err := NewLeaderElection(client, "/test/leader", "node1", 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if le.IsLeader() {
		t.Error("Initial leadership state should be false")
	}
}

// TestGetLeader_InitialState verifies initial leader is empty
func TestGetLeader_InitialState(t *testing.T) {
	t.Parallel()

	// Create a mock client
	client := &clientv3.Client{}

	le, err := NewLeaderElection(client, "/test/leader", "node1", 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	leader := le.GetLeader()
	if leader != "" {
		t.Errorf("Initial leader should be empty, got %s", leader)
	}
}

// TestLeaderElection_Fields verifies all fields are properly initialized
func TestLeaderElection_Fields(t *testing.T) {
	t.Parallel()

	client := &clientv3.Client{}
	prefix := "/test/leader"
	nodeID := "node1"
	ttl := 10

	le, err := NewLeaderElection(client, prefix, nodeID, ttl)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if le.client != client {
		t.Error("Client not properly set")
	}
	if le.prefix != prefix {
		t.Errorf("Expected prefix %s, got %s", prefix, le.prefix)
	}
	if le.nodeID != nodeID {
		t.Errorf("Expected nodeID %s, got %s", nodeID, le.nodeID)
	}
	if le.ttl != ttl {
		t.Errorf("Expected TTL %d, got %d", ttl, le.ttl)
	}
	if le.logger == nil {
		t.Error("Logger should be initialized")
	}
}
