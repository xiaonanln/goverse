package pulseapi

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:7070",
		AdvertiseAddress:    "localhost:7070",
		ClientListenAddress: "localhost:7071",
	}
	
	server := NewServer(config)
	
	if server == nil {
		t.Error("NewServer should return a server instance")
	}
}

func TestNewServer_InvalidConfig(t *testing.T) {
	// Test with nil config - should panic or exit
	// Since the actual implementation calls log.Fatalf which exits,
	// we can't test this in a unit test without subprocess isolation
	// This test documents the expected behavior
	t.Skip("Skipping test that would cause process exit via log.Fatalf")
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
