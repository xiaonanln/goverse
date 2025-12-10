package server

import (
	"context"
	"strings"
	"testing"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestServerReliableCallObject_RequiresCallID tests that Server.ReliableCallObject requires a non-empty call_id
func TestServerReliableCallObject_RequiresCallID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:    "localhost:47300",
		AdvertiseAddress: "localhost:47300",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Skipf("Skipping test - NewServer failed (etcd may not be available): %v", err)
		return
	}
	if server == nil {
		t.Skip("NewServer returned nil (etcd may not be available)")
		return
	}

	ctx := context.Background()

	// Test: Empty call_id should fail
	req := &goverse_pb.ReliableCallObjectRequest{
		CallId:     "", // Empty call_id
		ObjectType: "TestObject",
		ObjectId:   "test-obj-123",
	}

	_, err2 := server.ReliableCallObject(ctx, req)
	if err2 == nil {
		t.Fatal("Expected error when calling ReliableCallObject with empty call_id, got nil")
	}
	expectedMsg := "call_id must be specified in ReliableCallObject request"
	if err2.Error() != expectedMsg {
		t.Fatalf("Expected error message '%s', got '%s'", expectedMsg, err2.Error())
	}
}

// TestServerReliableCallObject_RequiresObjectType tests that Server.ReliableCallObject requires a non-empty object_type
func TestServerReliableCallObject_RequiresObjectType(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:    "localhost:47301",
		AdvertiseAddress: "localhost:47301",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Skipf("Skipping test - NewServer failed (etcd may not be available): %v", err)
		return
	}
	if server == nil {
		t.Skip("NewServer returned nil (etcd may not be available)")
		return
	}

	ctx := context.Background()

	// Test: Empty object_type should fail
	req := &goverse_pb.ReliableCallObjectRequest{
		CallId:     "test-call-123",
		ObjectType: "", // Empty object_type
		ObjectId:   "test-obj-123",
	}

	_, err2 := server.ReliableCallObject(ctx, req)
	if err2 == nil {
		t.Fatal("Expected error when calling ReliableCallObject with empty object_type, got nil")
	}
	expectedMsg := "object_type must be specified in ReliableCallObject request"
	if err2.Error() != expectedMsg {
		t.Fatalf("Expected error message '%s', got '%s'", expectedMsg, err2.Error())
	}
}

// TestServerReliableCallObject_RequiresObjectID tests that Server.ReliableCallObject requires a non-empty object_id
func TestServerReliableCallObject_RequiresObjectID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:    "localhost:47302",
		AdvertiseAddress: "localhost:47302",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Skipf("Skipping test - NewServer failed (etcd may not be available): %v", err)
		return
	}
	if server == nil {
		t.Skip("NewServer returned nil (etcd may not be available)")
		return
	}

	ctx := context.Background()

	// Test: Empty object_id should fail
	req := &goverse_pb.ReliableCallObjectRequest{
		CallId:     "test-call-123",
		ObjectType: "TestObject",
		ObjectId:   "", // Empty object_id
	}

	_, err2 := server.ReliableCallObject(ctx, req)
	if err2 == nil {
		t.Fatal("Expected error when calling ReliableCallObject with empty object_id, got nil")
	}
	expectedMsg := "object_id must be specified in ReliableCallObject request"
	if err2.Error() != expectedMsg {
		t.Fatalf("Expected error message '%s', got '%s'", expectedMsg, err2.Error())
	}
}

// TestServerReliableCallObject_ValidRequest tests that Server.ReliableCallObject accepts valid requests
func TestServerReliableCallObject_ValidRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:    "localhost:47303",
		AdvertiseAddress: "localhost:47303",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Skipf("Skipping test - NewServer failed (etcd may not be available): %v", err)
		return
	}
	if server == nil {
		t.Skip("NewServer returned nil (etcd may not be available)")
		return
	}

	ctx := context.Background()

	// Test: Valid request with all required parameters
	req := &goverse_pb.ReliableCallObjectRequest{
		CallId:     "test-call-123",
		ObjectType: "TestObject",
		ObjectId:   "test-obj-123",
	}

	// The server will try to check shard mapping which requires etcd
	// If etcd is not running, this is expected to fail with a shard-related error
	resp, err2 := server.ReliableCallObject(ctx, req)
	if err2 != nil {
		// Expected errors when etcd is not available:
		// - "shard mapper not initialized"
		// - "failed to determine target node"
		// - "etcd client not connected"
		if strings.Contains(err2.Error(), "shard mapper not initialized") ||
			strings.Contains(err2.Error(), "failed to determine target node") ||
			strings.Contains(err2.Error(), "etcd client not connected") {
			t.Logf("Expected error due to shard mapping/etcd not being available: %v", err2)
		} else {
			// If we got a different error, check if it's a validation error
			if strings.Contains(err2.Error(), "must be specified") {
				t.Fatalf("Should not get validation error for valid request, got: %v", err2)
			}
			t.Logf("Got error (acceptable if not about validation): %v", err2)
		}
	} else {
		// If no error, verify we got a response
		if resp == nil {
			t.Fatal("Expected non-nil response for valid request")
		}
		// Stub implementation returns empty result_data and no error
		if resp.Error != "" {
			t.Fatalf("Expected empty error field in response, got: %s", resp.Error)
		}
		t.Logf("Successfully received response: result_data length=%d", len(resp.ResultData))
	}
}
