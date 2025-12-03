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

func TestCreateObjectID(t *testing.T) {
	// Test that CreateObjectID returns a non-empty string
	id := CreateObjectID()
	if id == "" {
		t.Fatal("CreateObjectID() returned empty string")
	}

	// Test that multiple calls return different IDs
	id1 := CreateObjectID()
	id2 := CreateObjectID()
	if id1 == id2 {
		t.Fatal("CreateObjectID() returned same ID for consecutive calls")
	}
}

func TestCreateObjectIDOnShard(t *testing.T) {
	// Test with valid shard IDs
	testCases := []struct {
		name          string
		shardID       int
		shouldContain string
	}{
		{"minimum shard", 0, "shard#0/"},
		{"low shard", 5, "shard#5/"},
		{"mid shard", 4096, "shard#4096/"},
		{"high shard", 8191, "shard#8191/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id := CreateObjectIDOnShard(tc.shardID)
			if id == "" {
				t.Fatal("CreateObjectIDOnShard() returned empty string")
			}

			// Check format
			if len(id) < len(tc.shouldContain) {
				t.Fatalf("CreateObjectIDOnShard(%d) = %s, too short", tc.shardID, id)
			}

			if id[:len(tc.shouldContain)] != tc.shouldContain {
				t.Fatalf("CreateObjectIDOnShard(%d) = %s, want prefix %s", tc.shardID, id, tc.shouldContain)
			}

			// Test uniqueness
			id2 := CreateObjectIDOnShard(tc.shardID)
			if id == id2 {
				t.Fatalf("CreateObjectIDOnShard(%d) returned same ID twice", tc.shardID)
			}
		})
	}
}

func TestCreateObjectIDOnShard_InvalidInput(t *testing.T) {
	testCases := []struct {
		name    string
		shardID int
	}{
		{"negative shard", -1},
		{"shard equals numShards", 8192},
		{"shard exceeds numShards", 9000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("CreateObjectIDOnShard(%d) should panic, but didn't", tc.shardID)
				}
			}()
			CreateObjectIDOnShard(tc.shardID)
		})
	}
}

func TestCreateObjectIDOnNode(t *testing.T) {
	// Test with valid node addresses
	testCases := []string{
		"localhost:7001",
		"192.168.1.100:8080",
		"node1.example.com:9000",
	}

	for _, nodeAddr := range testCases {
		t.Run(nodeAddr, func(t *testing.T) {
			id := CreateObjectIDOnNode(nodeAddr)
			if id == "" {
				t.Fatal("CreateObjectIDOnNode() returned empty string")
			}

			// Check format
			expectedPrefix := nodeAddr + "/"
			if len(id) < len(expectedPrefix) {
				t.Fatalf("CreateObjectIDOnNode(%s) = %s, too short", nodeAddr, id)
			}

			if id[:len(expectedPrefix)] != expectedPrefix {
				t.Fatalf("CreateObjectIDOnNode(%s) = %s, want prefix %s", nodeAddr, id, expectedPrefix)
			}

			// Test uniqueness
			id2 := CreateObjectIDOnNode(nodeAddr)
			if id == id2 {
				t.Fatalf("CreateObjectIDOnNode(%s) returned same ID twice", nodeAddr)
			}
		})
	}
}

func TestCreateObjectIDOnNode_InvalidInput(t *testing.T) {
	// Test that empty node address panics
	defer func() {
		if r := recover(); r == nil {
			t.Error("CreateObjectIDOnNode(\"\") should panic, but didn't")
		}
	}()
	CreateObjectIDOnNode("")
}
