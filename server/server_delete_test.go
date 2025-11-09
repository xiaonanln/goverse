package server

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
)

func TestServerDeleteObject_Success(t *testing.T) {
	// Reset cluster state to ensure no interference from other tests
	resetClusterForTesting(t)

	// Create a node directly without using cluster
	n := node.NewNode("localhost:47000")
	n.RegisterObjectType((*TestObject)(nil))

	// Create a server without cluster (standalone mode for testing)
	config := &ServerConfig{
		ListenAddress:       "localhost:0", // Use any available port
		AdvertiseAddress:    "localhost:47000",
		ClientListenAddress: "localhost:0",
	}
	
	server := &Server{
		config: config,
		Node:   n,
		logger: logger.NewLogger("ServerTest"),
	}

	// Ensure cluster singleton is nil for this standalone test
	cluster.SetThis(nil)

	ctx := context.Background()

	// Create an object first - directly using the node to avoid cluster checks
	_, err := n.CreateObject(ctx, "TestObject", "test-delete-obj")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Verify object exists
	if n.NumObjects() != 1 {
		t.Fatalf("Expected 1 object after creation, got %d", n.NumObjects())
	}

	// Delete the object
	deleteReq := &goverse_pb.DeleteObjectRequest{
		Id: "test-delete-obj",
	}
	_, err = server.DeleteObject(ctx, deleteReq)
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}

	// Verify object is deleted
	if n.NumObjects() != 0 {
		t.Errorf("Expected 0 objects after deletion, got %d", n.NumObjects())
	}
}

func TestServerDeleteObject_RequiresID(t *testing.T) {
	// Reset cluster state to ensure no interference from other tests
	resetClusterForTesting(t)

	// Create a node
	n := node.NewNode("localhost:47000")

	// Create a server
	config := &ServerConfig{
		ListenAddress:       "localhost:0",
		AdvertiseAddress:    "localhost:47000",
		ClientListenAddress: "localhost:0",
	}
	
	server := &Server{
		config: config,
		Node:   n,
		logger: logger.NewLogger("ServerTest"),
	}

	// Ensure cluster singleton is nil for this standalone test
	cluster.SetThis(nil)

	ctx := context.Background()

	// Try to delete without specifying ID
	deleteReq := &goverse_pb.DeleteObjectRequest{
		Id: "",
	}
	_, err := server.DeleteObject(ctx, deleteReq)
	if err == nil {
		t.Fatal("Expected error when deleting without ID, got nil")
	}

	expectedErrMsg := "object ID must be specified in DeleteObject request"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestServerDeleteObject_NotFound(t *testing.T) {
	// Reset cluster state to ensure no interference from other tests
	resetClusterForTesting(t)

	// Create a node
	n := node.NewNode("localhost:47000")
	n.RegisterObjectType((*TestObject)(nil))

	// Create a server
	config := &ServerConfig{
		ListenAddress:       "localhost:0",
		AdvertiseAddress:    "localhost:47000",
		ClientListenAddress: "localhost:0",
	}
	
	server := &Server{
		config: config,
		Node:   n,
		logger: logger.NewLogger("ServerTest"),
	}

	// Ensure cluster singleton is nil for this standalone test
	cluster.SetThis(nil)

	ctx := context.Background()

	// Try to delete non-existent object
	deleteReq := &goverse_pb.DeleteObjectRequest{
		Id: "non-existent-obj",
	}
	_, err := server.DeleteObject(ctx, deleteReq)
	if err == nil {
		t.Fatal("Expected error when deleting non-existent object, got nil")
	}

	// The error should mention that the object was not found
	expectedErrContains := "not found"
	if !contains(err.Error(), expectedErrContains) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedErrContains, err.Error())
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		findInString(s, substr)))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
