package server

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
)

func TestValidateServerConfig_NilConfig(t *testing.T) {
	err := validateServerConfig(nil)
	if err == nil {
		t.Error("validateServerConfig should return error for nil config")
	}
	expectedMsg := "config cannot be nil"
	if err.Error() != expectedMsg {
		t.Errorf("validateServerConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestValidateServerConfig_EmptyListenAddress(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "",
		AdvertiseAddress:    "localhost:8080",
		ClientListenAddress: "localhost:8081",
	}
	err := validateServerConfig(config)
	if err == nil {
		t.Error("validateServerConfig should return error for empty ListenAddress")
	}
	expectedMsg := "ListenAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Errorf("validateServerConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestValidateServerConfig_EmptyAdvertiseAddress(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:8080",
		AdvertiseAddress:    "",
		ClientListenAddress: "localhost:8081",
	}
	err := validateServerConfig(config)
	if err == nil {
		t.Error("validateServerConfig should return error for empty AdvertiseAddress")
	}
	expectedMsg := "AdvertiseAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Errorf("validateServerConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestValidateServerConfig_ValidConfig(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:8080",
		AdvertiseAddress:    "localhost:8080",
		ClientListenAddress: "localhost:8081",
	}
	err := validateServerConfig(config)
	if err != nil {
		t.Errorf("validateServerConfig should not return error for valid config, got: %v", err)
	}
}

func TestNewServer_ValidConfig(t *testing.T) {
	config := &ServerConfig{
		ListenAddress:       "localhost:9090",
		AdvertiseAddress:    "localhost:9090",
		ClientListenAddress: "localhost:9091",
	}
	
	server := NewServer(config)
	
	if server == nil {
		t.Error("NewServer should return a server instance")
	}
	
	if server.Node == nil {
		t.Error("NewServer should initialize a Node")
	}
	
	if server.config != config {
		t.Error("NewServer should store the provided config")
	}
	
	if server.logger == nil {
		t.Error("NewServer should initialize a logger")
	}
	
	// Verify that the cluster has an etcd manager set
	clusterInstance := cluster.Get()
	if clusterInstance.GetEtcdManager() == nil {
		t.Error("NewServer should set the etcd manager on the cluster")
	}
	
	// Verify that the cluster has this node set
	if clusterInstance.GetThisNode() == nil {
		t.Error("NewServer should set the node on the cluster")
	}
	
	// Verify that the cluster's node matches the server's node
	if clusterInstance.GetThisNode() != server.Node {
		t.Error("Cluster's node should match the server's node")
	}
	
	// Test ListObjects on this server
	ctx := context.Background()
	resp, err := server.ListObjects(ctx, &goverse_pb.Empty{})
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}
	
	// Initially should have no objects
	if len(resp.Objects) != 0 {
		t.Errorf("Expected 0 objects initially, got %d", len(resp.Objects))
	}
}

func TestNode_ListObjects(t *testing.T) {
	// Create a node directly without going through NewServer
	n := node.NewNode("localhost:9094")
	
	// Test that ListObjects returns empty list initially
	objectInfos := n.ListObjects()
	
	if len(objectInfos) != 0 {
		t.Errorf("Expected 0 objects initially, got %d", len(objectInfos))
	}
}
