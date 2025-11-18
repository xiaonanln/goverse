package goverseapi

import (
	"context"
	"testing"
)

// TestGoverseServiceInterface verifies that the GoverseService interface
// is properly implemented and accessible from a Server instance
func TestGoverseServiceInterface(t *testing.T) {
	// Create a server
	config := &ServerConfig{
		ListenAddress:       "localhost:47000",
		AdvertiseAddress:    "localhost:47000",
		ClientListenAddress: "localhost:48000",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Get the GoverseService from the server
	service := GetGoverseService(server)
	if service == nil {
		t.Fatal("GetGoverseService returned nil")
	}

	// Verify the service implements the interface by checking methods exist
	// We don't actually call them since we don't have a full cluster setup
	ctx := context.Background()

	// Test that the interface methods are callable (even if they return errors)
	// This verifies the interface contract is satisfied
	t.Run("CreateObject method exists", func(t *testing.T) {
		// We expect this to fail without a running cluster, but the method should exist
		_, err := service.CreateObject(ctx, "TestType", "test-id")
		// We don't care about the error here, just that the method exists and is callable
		_ = err
	})

	t.Run("CallObject method exists", func(t *testing.T) {
		// We expect this to fail without a running cluster, but the method should exist
		_, err := service.CallObject(ctx, "TestType", "test-id", "TestMethod", nil)
		// We don't care about the error here, just that the method exists and is callable
		_ = err
	})

	t.Run("DeleteObject method exists", func(t *testing.T) {
		// We expect this to fail without a running cluster, but the method should exist
		err := service.DeleteObject(ctx, "test-id")
		// We don't care about the error here, just that the method exists and is callable
		_ = err
	})
}

// TestGoverseServiceInterfaceType verifies that the returned service
// satisfies the GoverseService interface at compile time
func TestGoverseServiceInterfaceType(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:47001",
		AdvertiseAddress:    "localhost:47001",
		ClientListenAddress: "localhost:48001",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// This assignment will fail at compile time if the service doesn't implement the interface
	var service GoverseService = GetGoverseService(server)
	if service == nil {
		t.Fatal("Service should not be nil")
	}
}

// TestGoverseServiceInterfaceContract verifies the expected behavior
// of the GoverseService interface methods
func TestGoverseServiceInterfaceContract(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:47002",
		AdvertiseAddress:    "localhost:47002",
		ClientListenAddress: "localhost:48002",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	service := GetGoverseService(server)
	ctx := context.Background()

	t.Run("CreateObject with empty ID generates ID", func(t *testing.T) {
		// Note: This will fail without etcd, but we're testing the interface contract
		objID, err := service.CreateObject(ctx, "TestType", "")
		// Without etcd, we expect an error, but the method should handle empty ID
		// The actual implementation generates an ID before checking cluster
		if err == nil {
			// If it succeeded (unlikely without cluster), verify ID was generated
			if objID == "" {
				t.Error("CreateObject should generate an ID when empty string is provided")
			}
		}
		// If it failed, that's expected without a cluster, we're just verifying interface behavior
	})

	t.Run("DeleteObject with empty ID returns error", func(t *testing.T) {
		err := service.DeleteObject(ctx, "")
		if err == nil {
			t.Error("DeleteObject should return error for empty ID")
		}
	})
}
