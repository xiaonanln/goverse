package server

import (
	"context"
	"strings"
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

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
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

	_, err2 := server.CreateObject(ctx, req)
	if err2 == nil {
		t.Fatal("Expected error when creating object with empty ID, got nil")
	}
	expectedMsg := "object ID must be specified in CreateObject request"
	if err2.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err2.Error())
	}

	// Test 2: Non-empty ID
	req2 := &goverse_pb.CreateObjectRequest{
		Type:     "TestObject",
		Id:       "test-obj-123",
		InitData: nil,
	}

	// The server will try to check shard mapping which requires etcd
	// If etcd is not running, this is expected to fail with a shard-related error
	_, err3 := server.CreateObject(ctx, req2)
	if err3 != nil {
		// Expected errors when etcd is not available:
		// - "shard mapper not initialized"
		// - "failed to determine target node"
		// - "etcd client not connected"
		if strings.Contains(err3.Error(), "shard mapper not initialized") ||
			strings.Contains(err3.Error(), "failed to determine target node") ||
			strings.Contains(err3.Error(), "etcd client not connected") {
			t.Logf("Expected error due to shard mapping/etcd not being available: %v", err3)
		} else {
			// If we got a different error, it might be the "object ID must be specified" check passing
			// which is what we want to verify
			if strings.Contains(err3.Error(), "object ID must be specified") {
				t.Fatalf("Should not get 'object ID must be specified' error for non-empty ID, got: %v", err3)
			}
			t.Logf("Got error (acceptable if not about ID validation): %v", err3)
		}
	}
}
