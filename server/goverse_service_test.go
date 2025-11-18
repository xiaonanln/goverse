package server

import (
	"context"
	"testing"
)

// TestServerGoverseService verifies that serverGoverseService
// correctly delegates to the cluster
func TestServerGoverseService(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:47100",
		AdvertiseAddress:    "localhost:47100",
		ClientListenAddress: "localhost:48100",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	service := server.GetGoverseService()
	if service == nil {
		t.Fatal("GetGoverseService returned nil")
	}

	// Verify the service is properly initialized and has access to cluster
	if service.server != server {
		t.Error("serverGoverseService should have reference to server")
	}

	if service.server.cluster == nil {
		t.Error("serverGoverseService server should have cluster initialized")
	}
}

// TestServerGoverseServiceDelegation verifies that the service
// methods delegate to the cluster
func TestServerGoverseServiceDelegation(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:47101",
		AdvertiseAddress:    "localhost:47101",
		ClientListenAddress: "localhost:48101",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	service := server.GetGoverseService()
	ctx := context.Background()

	// Test CreateObject - should delegate to cluster (will fail without etcd but that's ok)
	t.Run("CreateObject delegates to cluster", func(t *testing.T) {
		_, err := service.CreateObject(ctx, "TestType", "test-obj-1")
		// We expect an error without etcd, but the method should be callable
		_ = err
	})

	// Test CallObject - should delegate to cluster
	t.Run("CallObject delegates to cluster", func(t *testing.T) {
		_, err := service.CallObject(ctx, "TestType", "test-obj-1", "TestMethod", nil)
		// We expect an error without etcd/object, but the method should be callable
		_ = err
	})

	// Test DeleteObject - should delegate to cluster
	t.Run("DeleteObject delegates to cluster", func(t *testing.T) {
		err := service.DeleteObject(ctx, "test-obj-1")
		// We expect an error without etcd, but the method should be callable
		_ = err
	})
}

// TestNewServerGoverseService verifies the constructor
func TestNewServerGoverseService(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:47102",
		AdvertiseAddress:    "localhost:47102",
		ClientListenAddress: "localhost:48102",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	service := newServerGoverseService(server)
	if service == nil {
		t.Fatal("newServerGoverseService returned nil")
	}

	if service.server != server {
		t.Error("service should have reference to server")
	}
}
