package server

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestValidateServerConfig_NilConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	err := validateServerConfig(nil)
	if err == nil {
		t.Fatal("validateServerConfig should return error for nil config")
	}
	expectedMsg := "config cannot be nil"
	if err.Error() != expectedMsg {
		t.Fatalf("validateServerConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestValidateServerConfig_EmptyListenAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := &ServerConfig{
		ListenAddress:             "",
		AdvertiseAddress:          "localhost:8080",
		ClientListenAddress:       "localhost:8081",
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}
	err := validateServerConfig(config)
	if err == nil {
		t.Fatal("validateServerConfig should return error for empty ListenAddress")
	}
	expectedMsg := "ListenAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Fatalf("validateServerConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestValidateServerConfig_EmptyAdvertiseAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := &ServerConfig{
		ListenAddress:             "localhost:8080",
		AdvertiseAddress:          "",
		ClientListenAddress:       "localhost:8081",
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}
	err := validateServerConfig(config)
	if err == nil {
		t.Fatal("validateServerConfig should return error for empty AdvertiseAddress")
	}
	expectedMsg := "AdvertiseAddress cannot be empty"
	if err.Error() != expectedMsg {
		t.Fatalf("validateServerConfig error = %v; want %v", err.Error(), expectedMsg)
	}
}

func TestValidateServerConfig_ValidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := &ServerConfig{
		ListenAddress:             "localhost:8080",
		AdvertiseAddress:          "localhost:8080",
		ClientListenAddress:       "localhost:8081",
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}
	err := validateServerConfig(config)
	if err != nil {
		t.Fatalf("validateServerConfig should not return error for valid config, got: %v", err)
	}
}

func TestServerConfig_NodeStabilityDuration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create config with custom NodeStabilityDuration
	customDuration := 3 * time.Second
	config := &ServerConfig{
		ListenAddress:             "localhost:9097",
		AdvertiseAddress:          "localhost:9097",
		ClientListenAddress:       "localhost:9098",
		NodeStabilityDuration:     customDuration,
		ShardMappingCheckInterval: 1 * time.Second,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("NewServer should return a server instance")
		return
	}

	// Verify the cluster has the custom NodeStabilityDuration
	if server.cluster.GetClusterStateStabilityDurationForTesting() != customDuration {
		t.Fatalf("Expected cluster NodeStabilityDuration to be %v, got %v",
			customDuration, server.cluster.GetClusterStateStabilityDurationForTesting())
	}
}

func TestServerConfig_DefaultNodeStabilityDuration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create config without custom NodeStabilityDuration
	config := &ServerConfig{
		ListenAddress:             "localhost:9099",
		AdvertiseAddress:          "localhost:9099",
		ClientListenAddress:       "localhost:9100",
		ShardMappingCheckInterval: 1 * time.Second,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("NewServer should return a server instance")
		return
	}

	// Verify the cluster uses the default DefaultNodeStabilityDuration
	if server.cluster.GetClusterStateStabilityDurationForTesting() != cluster.DefaultNodeStabilityDuration {
		t.Fatalf("Expected cluster NodeStabilityDuration to be default %v, got %v",
			cluster.DefaultNodeStabilityDuration, server.cluster.GetClusterStateStabilityDurationForTesting())
	}
}

func TestNewServer_ValidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := &ServerConfig{
		ListenAddress:             "localhost:9090",
		AdvertiseAddress:          "localhost:9090",
		ClientListenAddress:       "localhost:9091",
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("NewServer should return a server instance")
	}

	if server.Node == nil {
		t.Fatal("NewServer should initialize a Node")
	}

	if server.config != config {
		t.Fatal("NewServer should store the provided config")
	}

	if server.logger == nil {
		t.Fatal("NewServer should initialize a logger")
	}

	// Test ListObjects on this server
	ctx := context.Background()
	resp, err := server.ListObjects(ctx, &goverse_pb.Empty{})
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}

	// Initially should have no objects
	if len(resp.Objects) != 0 {
		t.Fatalf("Expected 0 objects initially, got %d", len(resp.Objects))
	}
}

func TestNewServer_WithCustomEtcdPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use a custom etcd prefix for testing
	customPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	config := &ServerConfig{
		ListenAddress:             "localhost:9095",
		AdvertiseAddress:          "localhost:9095",
		ClientListenAddress:       "localhost:9096",
		EtcdAddress:               "localhost:2379",
		EtcdPrefix:                customPrefix,
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}

	server, err := NewServer(config)
	// NewServer may fail if etcd is not running, which is expected
	if err != nil {
		t.Logf("NewServer failed (expected if etcd not running): %v", err)
		// Don't continue the test if NewServer failed
		return
	}

	if server == nil {
		t.Fatal("NewServer should return a server instance")
	}

	if server.config != config {
		t.Fatal("NewServer should store the provided config")
	}

	// With the new pattern, NewServer automatically initializes the cluster with etcd
	clusterInstance := server.cluster

	// Verify that the cluster has an etcd manager with the custom prefix
	etcdMgr := clusterInstance.GetEtcdManagerForTesting()
	if etcdMgr == nil {
		t.Fatal("NewServer should create the etcd manager on the cluster")
		return
	}

	if etcdMgr.GetPrefix() != customPrefix {
		t.Fatalf("EtcdManager prefix = %s, want %s", etcdMgr.GetPrefix(), customPrefix)
	}

	// Verify that the cluster's node matches the server's node
	if clusterInstance.GetThisNode() != server.Node {
		t.Fatal("Cluster's node should match the server's node")
	}

	// Verify the node is registered in the correct etcd key
	// Connect etcd manager to verify node registration
	err = etcdMgr.Connect()
	if err != nil {
		t.Skipf("Skipping etcd verification: failed to connect to etcd: %v", err)
		return
	}
	defer etcdMgr.Close()

	// Register the node using shared lease API
	ctx := context.Background()
	nodesPrefix := etcdMgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + config.AdvertiseAddress
	_, err = etcdMgr.RegisterKeyLease(ctx, key, config.AdvertiseAddress, etcdmanager.NodeLeaseTTL)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Wait for registration to complete
	time.Sleep(500 * time.Millisecond)

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

	// Cleanup: unregister the node using shared lease API
	err = etcdMgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Logf("Warning: failed to unregister node: %v", err)
	}
}

func TestNode_ListObjects(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a node directly without going through NewServer
	ctx := context.Background()
	n := testutil.MustNewNode(ctx, t, "localhost:9094")

	// Test that ListObjects returns empty list initially
	objectInfos := n.ListObjects()

	if len(objectInfos) != 0 {
		t.Fatalf("Expected 0 objects initially, got %d", len(objectInfos))
	}
}

// TestServerStartupWithEtcd tests that a server can:
// 1. Start successfully
// 2. Register its node address to etcd
// 3. Become the sole leader
func TestServerStartupWithEtcd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix to get a unique prefix for test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create server config with unique ports and etcd prefix
	config := &ServerConfig{
		ListenAddress:             "localhost:47100",
		AdvertiseAddress:          "localhost:47100",
		ClientListenAddress:       "localhost:47101",
		EtcdAddress:               "localhost:2379",
		EtcdPrefix:                etcdPrefix,
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
	}

	// Create server
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
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
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	getResp, err := mgr.GetClient().Get(ctx, nodesPrefix, clientv3.WithPrefix())
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
	clusterInstance := cluster.This()
	if clusterInstance == nil {
		t.Fatal("Cluster instance should not be nil")
	}

	// Give cluster time to process node list changes
	time.Sleep(500 * time.Millisecond)

	// The node should see itself in the node list
	nodeAddresses := clusterInstance.GetNodes()
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

	// Give some time for shard mapping to initialize
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Verify shard mapping was created and stored
	shardMapping := clusterInstance.GetShardMapping(ctx)

	if shardMapping == nil {
		t.Fatal("Shard mapping should not be nil after initialization")
	}

	if len(shardMapping.Shards) != 8192 {
		t.Fatal("Shard mapping should contain 8192 shards, got ", len(shardMapping.Shards))
	}

	// Verify the leader node is in the shard mapping
	foundInMapping := false
	for _, shard := range shardMapping.Shards {
		if shard.TargetNode == config.AdvertiseAddress {
			foundInMapping = true
			break
		}
	}
	if !foundInMapping {
		t.Fatalf("Leader node %s not found in shard mapping", config.AdvertiseAddress)
	}

	// Get unique nodes from shard mapping
	uniqueNodes := make(map[string]bool)
	for _, shard := range shardMapping.Shards {
		uniqueNodes[shard.TargetNode] = true
	}

	t.Logf("Shard mapping initialized successfully with %d unique nodes, %d shards",
		len(uniqueNodes), len(shardMapping.Shards))

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

	getResp, err = mgr.GetClient().Get(ctx, nodesPrefix, clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("Failed to get nodes from etcd after stop: %v", err)
	}

	// Should have no nodes registered after server stops
	if len(getResp.Kvs) != 0 {
		t.Logf("Warning: Expected 0 nodes after server stop, got %d (may be due to lease expiry delay)", len(getResp.Kvs))
	}

	t.Log("Test completed successfully")
}
