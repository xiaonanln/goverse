package goverseapi

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:    "localhost:7070",
		AdvertiseAddress: "localhost:7070",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("NewServer should return a server instance")
	}
}

func TestNewServer_InvalidConfig(t *testing.T) {
	// Test with nil config - should return error
	_, err := NewServer(nil)
	if err == nil {
		t.Fatal("NewServer with nil config should return error")
	}
	expectedMsg := "invalid server configuration: config cannot be nil"
	if err.Error() != expectedMsg {
		t.Fatalf("NewServer error = %v; want %v", err.Error(), expectedMsg)
	}

	// Test with empty ListenAddress - should return error
	config := &ServerConfig{
		ListenAddress:    "",
		AdvertiseAddress: "localhost:7072",
	}
	_, err = NewServer(config)
	if err == nil {
		t.Fatal("NewServer with empty ListenAddress should return error")
	}
	expectedMsg = "invalid server configuration: ListenAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Fatalf("NewServer error = %v; want %v", err.Error(), expectedMsg)
	}

	// Test with empty AdvertiseAddress - should return error
	config = &ServerConfig{
		ListenAddress:    "localhost:7074",
		AdvertiseAddress: "",
	}
	_, err = NewServer(config)
	if err == nil {
		t.Fatal("NewServer with empty AdvertiseAddress should return error")
	}
	expectedMsg = "invalid server configuration: AdvertiseAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Fatalf("NewServer error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestTypeAliases(t *testing.T) {
	// Test that type aliases are properly defined
	var _ *ServerConfig
	var _ *Server
	var _ *Node
	var _ Object
	var _ ClientObject
	var _ *BaseObject
	var _ *BaseClient
	var _ *Cluster
}
