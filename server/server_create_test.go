package server

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestObject is a simple test object for testing
type TestObject struct {
	object.BaseObject
}

func (obj *TestObject) OnCreated() {
	// No-op for testing
}

// TestServerCreateObject_RequiresID tests that Server.CreateObject requires a non-empty ID
func TestServerCreateObject_RequiresID(t *testing.T) {
	// Reset cluster state before this test
	resetClusterForTesting(t)

	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:       "localhost:47200",
		AdvertiseAddress:    "localhost:47200",
		ClientListenAddress: "localhost:47201",
		EtcdAddress:         "localhost:2379",
		EtcdPrefix:          etcdPrefix,
	}

	server := NewServer(config)
	if server == nil {
		t.Fatal("NewServer should return a server instance")
	}

	// Register test object type
	server.Node.RegisterObjectType((*TestObject)(nil))

	ctx := context.Background()

	// Test 1: Empty ID should fail
	req := &goverse_pb.CreateObjectRequest{
		Type:     "TestObject",
		Id:       "", // Empty ID
		InitData: nil,
	}

	_, err := server.CreateObject(ctx, req)
	if err == nil {
		t.Fatal("Expected error when creating object with empty ID, got nil")
	}
	expectedMsg := "object ID must be specified in CreateObject request"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}

	// Test 2: Non-empty ID
	req2 := &goverse_pb.CreateObjectRequest{
		Type:     "TestObject",
		Id:       "test-obj-123",
		InitData: nil,
	}

	// The server will try to check shard mapping which requires etcd
	// If etcd is not running, this is expected to fail with a shard-related error
	_, err = server.CreateObject(ctx, req2)
	if err != nil {
		// Expected errors when etcd is not available:
		// - "shard mapper not initialized"
		// - "failed to determine target node"
		// - "etcd client not connected"
		if containsStr(err.Error(), "shard mapper not initialized") || 
		   containsStr(err.Error(), "failed to determine target node") ||
		   containsStr(err.Error(), "etcd client not connected") {
			t.Logf("Expected error due to shard mapping/etcd not being available: %v", err)
		} else {
			// If we got a different error, it might be the "object ID must be specified" check passing
			// which is what we want to verify
			if containsStr(err.Error(), "object ID must be specified") {
				t.Fatalf("Should not get 'object ID must be specified' error for non-empty ID, got: %v", err)
			}
			t.Logf("Got error (acceptable if not about ID validation): %v", err)
		}
	}
}

// TestServerCreateObject_ValidatesShardMapping tests that Server.CreateObject validates shard ownership
func TestServerCreateObject_ValidatesShardMapping(t *testing.T) {
	t.Skip("Skipping test that requires full cluster setup - covered by integration tests")
	// This test would require:
	// 1. Starting a full server with etcd
	// 2. Initializing shard mapping
	// 3. Creating an object with an ID that doesn't belong to this node
	// 4. Verifying the error message
	// This is better tested in integration tests where the full cluster is running
}

// Helper function to check if a string contains a substring
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
