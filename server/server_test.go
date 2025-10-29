package server

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// resetClusterForTesting resets the cluster singleton state between tests
// This is necessary because cluster.Get() returns a singleton and tests may interfere with each other
func resetClusterForTesting(t *testing.T) {
	clusterInstance := cluster.Get()
	clusterInstance.ResetForTesting()
	t.Logf("Cluster state reset for testing")
}

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
	// Reset cluster state before this test
	resetClusterForTesting(t)

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
	// Note: This test validates the integration with the global cluster singleton
	// (cluster.Get()), which is the actual production behavior. We intentionally
	// test the real NewServer() behavior with the global singleton rather than
	// using an isolated test cluster instance. The test is designed to run first
	// to avoid conflicts with the singleton's "ThisNode is already set" check.
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

func TestNewServer_WithCustomEtcdPrefix(t *testing.T) {
	// Reset cluster state before this test
	resetClusterForTesting(t)

	// Use a custom etcd prefix for testing
	customPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:       "localhost:9095",
		AdvertiseAddress:    "localhost:9095",
		ClientListenAddress: "localhost:9096",
		EtcdAddress:         "localhost:2379",
		EtcdPrefix:          customPrefix,
	}

	server := NewServer(config)

	if server == nil {
		t.Error("NewServer should return a server instance")
	}

	if server.config != config {
		t.Error("NewServer should store the provided config")
	}

	// Verify that the cluster has an etcd manager set with the custom prefix
	clusterInstance := cluster.Get()
	if clusterInstance.GetEtcdManager() == nil {
		t.Error("NewServer should set the etcd manager on the cluster")
	}

	etcdMgr := clusterInstance.GetEtcdManager()
	if etcdMgr.GetPrefix() != customPrefix {
		t.Errorf("EtcdManager prefix = %s, want %s", etcdMgr.GetPrefix(), customPrefix)
	}

	// Verify that the cluster's node matches the server's node
	if clusterInstance.GetThisNode() != server.Node {
		t.Error("Cluster's node should match the server's node")
	}

	// Verify the node is registered in the correct etcd key
	// Connect etcd manager to verify node registration
	err := etcdMgr.Connect()
	if err != nil {
		t.Skipf("Skipping etcd verification: failed to connect to etcd: %v", err)
		return
	}
	defer etcdMgr.Close()

	// Register the node
	ctx := context.Background()
	err = etcdMgr.RegisterNode(ctx, config.AdvertiseAddress)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Verify the node is stored at the correct etcd path with custom prefix
	expectedNodePath := customPrefix + "/nodes/" + config.AdvertiseAddress
	getResp, err := etcdMgr.GetClient().Get(ctx, expectedNodePath)
	if err != nil {
		t.Fatalf("Failed to get node from etcd: %v", err)
	}

	if len(getResp.Kvs) != 1 {
		t.Fatalf("Expected 1 key in etcd at %s, got %d", expectedNodePath, len(getResp.Kvs))
	}

	// Verify the stored value is the node address
	storedAddress := string(getResp.Kvs[0].Value)
	if storedAddress != config.AdvertiseAddress {
		t.Fatalf("Stored address %s does not match advertise address %s", storedAddress, config.AdvertiseAddress)
	}

	t.Logf("Node successfully registered at custom etcd path: %s with value: %s", expectedNodePath, storedAddress)

	// Cleanup: unregister the node
	err = etcdMgr.UnregisterNode(ctx, config.AdvertiseAddress)
	if err != nil {
		t.Logf("Warning: failed to unregister node: %v", err)
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

// TestServerStartupWithEtcd tests that a server can:
// 1. Start successfully
// 2. Register its node address to etcd
// 3. Become the sole leader
func TestServerStartupWithEtcd(t *testing.T) {
	// Reset cluster state before this test
	resetClusterForTesting(t)

	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create server config with unique ports and etcd prefix
	config := &ServerConfig{
		ListenAddress:       "localhost:47100",
		AdvertiseAddress:    "localhost:47100",
		ClientListenAddress: "localhost:47101",
		EtcdAddress:         "localhost:2379",
		EtcdPrefix:          etcdPrefix,
	}

	// Create server
	server := NewServer(config)
	if server == nil {
		t.Fatal("NewServer should return a server instance")
	}

	// Start server in background
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run()
	}()

	// Give server time to start and register with etcd
	time.Sleep(2 * time.Second)

	// Verify server started successfully (Run should still be running)
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Server Run() failed: %v", err)
		}
		t.Fatal("Server Run() exited prematurely")
	default:
		// Server is still running - this is expected
	}

	// Verify node is registered in etcd
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", etcdPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	err = mgr.Connect()
	if err != nil {
		server.cancel()
		<-serverDone
		t.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer mgr.Close()

	// Get all registered nodes using etcd manager's GetClient
	ctx := context.Background()
	getResp, err := mgr.GetClient().Get(ctx, mgr.GetNodesPrefix(), clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("Failed to get nodes from etcd: %v", err)
	}

	// Should have exactly one node registered
	if len(getResp.Kvs) != 1 {
		t.Fatalf("Expected 1 node registered in etcd, got %d", len(getResp.Kvs))
	}

	// Verify the registered address matches our advertise address
	registeredAddress := string(getResp.Kvs[0].Value)
	if registeredAddress != config.AdvertiseAddress {
		t.Fatalf("Registered address %s does not match advertise address %s", registeredAddress, config.AdvertiseAddress)
	}

	t.Logf("Node successfully registered in etcd: %s", registeredAddress)

	// Verify server is the leader
	// Since it's the only node, it should be the leader
	clusterInstance := cluster.Get()
	if clusterInstance == nil {
		t.Fatal("Cluster instance should not be nil")
	}

	etcdMgr := clusterInstance.GetEtcdManager()
	if etcdMgr == nil {
		t.Fatal("Etcd manager should be set in cluster")
	}

	// The node should see itself in the node list
	nodeAddresses := etcdMgr.GetNodes()
	if len(nodeAddresses) < 1 {
		t.Fatal("Node should see at least itself in the node list")
	}

	found := false
	for _, addr := range nodeAddresses {
		if addr == config.AdvertiseAddress {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Node should see itself (%s) in node list: %v", config.AdvertiseAddress, nodeAddresses)
	}

	// Verify this node is the leader
	if !clusterInstance.IsLeader() {
		t.Fatal("Server should be the leader as the only node in the cluster")
	}

	leaderNode := clusterInstance.GetLeaderNode()
	if leaderNode != config.AdvertiseAddress {
		t.Fatalf("Leader node is %s, expected %s", leaderNode, config.AdvertiseAddress)
	}

	t.Logf("Server successfully started and registered as sole node/leader: %s", leaderNode)

	// Initialize shard mapping (leader responsibility)
	err = clusterInstance.InitializeShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize shard mapping: %v", err)
	}

	// Verify shard mapping was created and stored
	shardMapping, err := clusterInstance.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping: %v", err)
	}

	if shardMapping == nil {
		t.Fatal("Shard mapping should not be nil after initialization")
	}

	if len(shardMapping.Shards) == 0 {
		t.Fatal("Shard mapping should contain at least one shard")
	}

	// Verify the leader node is in the shard mapping
	foundInMapping := false
	for _, nodeAddr := range shardMapping.Shards {
		if nodeAddr == config.AdvertiseAddress {
			foundInMapping = true
			break
		}
	}
	if !foundInMapping {
		t.Fatalf("Leader node %s not found in shard mapping", config.AdvertiseAddress)
	}

	// Get unique nodes from shard mapping
	uniqueNodes := make(map[string]bool)
	for _, nodeAddr := range shardMapping.Shards {
		uniqueNodes[nodeAddr] = true
	}

	t.Logf("Shard mapping initialized successfully with %d unique nodes, %d shards, version %d",
		len(uniqueNodes), len(shardMapping.Shards), shardMapping.Version)

	// Gracefully stop the server
	server.cancel()

	// Wait for server to stop (with timeout)
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Logf("Server stopped with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}

	// Verify node was unregistered from etcd after stopping
	time.Sleep(1 * time.Second) // Give time for cleanup

	getResp, err = mgr.GetClient().Get(ctx, mgr.GetNodesPrefix(), clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("Failed to get nodes from etcd after stop: %v", err)
	}

	// Should have no nodes registered after server stops
	if len(getResp.Kvs) != 0 {
		t.Logf("Warning: Expected 0 nodes after server stop, got %d (may be due to lease expiry delay)", len(getResp.Kvs))
	}

	t.Log("Test completed successfully")
}
