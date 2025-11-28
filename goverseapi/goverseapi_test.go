package goverseapi

import (
	"testing"
)

func TestNewServerWithConfig(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:    "localhost:7070",
		AdvertiseAddress: "localhost:7070",
	}

	server, err := NewServerWithConfig(config)
	if err != nil {
		t.Fatalf("NewServerWithConfig failed: %v", err)
	}

	if server == nil {
		t.Fatal("NewServerWithConfig should return a server instance")
	}
}

func TestNewServerWithConfig_InvalidConfig(t *testing.T) {
	// Test with nil config - should return error
	_, err := NewServerWithConfig(nil)
	if err == nil {
		t.Fatal("NewServerWithConfig with nil config should return error")
	}
	expectedMsg := "invalid server configuration: config cannot be nil"
	if err.Error() != expectedMsg {
		t.Fatalf("NewServerWithConfig error = %v; want %v", err.Error(), expectedMsg)
	}

	// Test with empty ListenAddress - should return error
	config := &ServerConfig{
		ListenAddress:    "",
		AdvertiseAddress: "localhost:7072",
	}
	_, err = NewServerWithConfig(config)
	if err == nil {
		t.Fatal("NewServerWithConfig with empty ListenAddress should return error")
	}
	expectedMsg = "invalid server configuration: ListenAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Fatalf("NewServerWithConfig error = %v; want %v", err.Error(), expectedMsg)
	}

	// Test with empty AdvertiseAddress - should return error
	config = &ServerConfig{
		ListenAddress:    "localhost:7074",
		AdvertiseAddress: "",
	}
	_, err = NewServerWithConfig(config)
	if err == nil {
		t.Fatal("NewServerWithConfig with empty AdvertiseAddress should return error")
	}
	expectedMsg = "invalid server configuration: AdvertiseAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Fatalf("NewServerWithConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestTypeAliases(t *testing.T) {
	// Test that type aliases are properly defined
	var _ *ServerConfig
	var _ *Server
	var _ *Node
	var _ Object
	var _ *BaseObject
	var _ *BaseClient
	var _ *Cluster
}
