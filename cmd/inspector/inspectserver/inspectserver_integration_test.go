package inspectserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestHandleShardMapping_WithConsensusManager tests the shard endpoint with actual ConsensusManager
func TestHandleShardMapping_WithConsensusManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup etcd connection
	etcdAddr := "localhost:2379"
	prefix := testutil.PrepareEtcdPrefix(t, etcdAddr)

	pg := graph.NewGoverseGraph()

	// Create server with etcd and consensus manager
	server := New(pg, Config{
		GRPCAddr:   ":0",
		HTTPAddr:   ":0",
		StaticDir:  ".",
		EtcdAddr:   etcdAddr,
		EtcdPrefix: prefix,
		NumShards:  testutil.TestNumShards,
	})

	// Skip test if etcd is not available
	if server.consensusManager == nil {
		t.Skip("Skipping - etcd not available or consensus manager failed to initialize")
	}

	defer server.Shutdown()

	// Wait a bit for consensus manager to initialize
	time.Sleep(500 * time.Millisecond)

	handler := server.createHTTPHandler()

	// Make request to shard endpoint
	req := httptest.NewRequest(http.MethodGet, "/shards", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Verify response has expected structure
	shards, ok := result["shards"].([]interface{})
	if !ok {
		t.Fatal("Expected 'shards' field in response")
	}

	nodes, ok := result["nodes"].([]interface{})
	if !ok {
		t.Fatal("Expected 'nodes' field in response")
	}

	// With a fresh etcd, we might not have any shards yet
	t.Logf("Got %d shards and %d nodes from consensus manager", len(shards), len(nodes))
}

// TestConsensusManagerInitialization tests that ConsensusManager is properly initialized when etcd is available
func TestConsensusManagerInitialization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup etcd connection
	etcdAddr := "localhost:2379"
	prefix := testutil.PrepareEtcdPrefix(t, etcdAddr)

	pg := graph.NewGoverseGraph()

	// Create server with etcd
	server := New(pg, Config{
		GRPCAddr:   ":0",
		HTTPAddr:   ":0",
		StaticDir:  ".",
		EtcdAddr:   etcdAddr,
		EtcdPrefix: prefix,
		NumShards:  testutil.TestNumShards,
	})

	// Skip test if etcd is not available
	if server.etcdManager == nil {
		t.Skip("Skipping - etcd not available")
	}

	defer server.Shutdown()

	// Verify consensus manager was created
	if server.consensusManager == nil {
		t.Fatal("Expected consensus manager to be initialized")
	}

	// Verify consensus manager has correct number of shards
	numShards := server.consensusManager.GetNumShards()
	if numShards != testutil.TestNumShards {
		t.Fatalf("Expected %d shards, got %d", testutil.TestNumShards, numShards)
	}
}

// TestShutdownStopsConsensusManager tests that shutdown properly stops the consensus manager
func TestShutdownStopsConsensusManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup etcd connection
	etcdAddr := "localhost:2379"
	prefix := testutil.PrepareEtcdPrefix(t, etcdAddr)

	pg := graph.NewGoverseGraph()

	// Create server with etcd
	server := New(pg, Config{
		GRPCAddr:   ":0",
		HTTPAddr:   ":0",
		StaticDir:  ".",
		EtcdAddr:   etcdAddr,
		EtcdPrefix: prefix,
		NumShards:  testutil.TestNumShards,
	})

	// Skip test if etcd is not available
	if server.consensusManager == nil {
		t.Skip("Skipping - etcd not available or consensus manager failed to initialize")
	}

	// Wait a bit for initialization
	time.Sleep(100 * time.Millisecond)

	// Shutdown should complete without error
	server.Shutdown()

	// Give a moment for shutdown to complete
	time.Sleep(100 * time.Millisecond)

	// Verify we can get shard mapping (should still work, just with stopped watch)
	shardMapping := server.consensusManager.GetShardMapping()
	if shardMapping == nil {
		t.Fatal("Expected to get shard mapping after shutdown")
	}
}

// TestHandleShardMapping_WithConsensusManagerAndData tests the endpoint with data from etcd
func TestHandleShardMapping_WithConsensusManagerAndData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup etcd connection
	etcdAddr := "localhost:2379"
	prefix := testutil.PrepareEtcdPrefix(t, etcdAddr)

	pg := graph.NewGoverseGraph()

	// Create server with etcd and consensus manager
	server := New(pg, Config{
		GRPCAddr:   ":0",
		HTTPAddr:   ":0",
		StaticDir:  ".",
		EtcdAddr:   etcdAddr,
		EtcdPrefix: prefix,
		NumShards:  testutil.TestNumShards,
	})

	// Skip test if etcd is not available
	if server.consensusManager == nil {
		t.Skip("Skipping - etcd not available or consensus manager failed to initialize")
	}

	defer server.Shutdown()

	// Add nodes and shards directly via etcd manager to simulate cluster state
	ctx := context.Background()
	nodeAddr := "localhost:50001"

	// Register a node via etcd
	nodeKey := prefix + "/nodes/" + nodeAddr
	if _, err := server.etcdManager.GetClient().Put(ctx, nodeKey, nodeAddr); err != nil {
		t.Fatalf("Failed to add node to etcd: %v", err)
	}

	// Add a shard assignment via etcd
	shardKey := prefix + "/shard/0"
	shardValue := nodeAddr + "," + nodeAddr // target,current
	if _, err := server.etcdManager.GetClient().Put(ctx, shardKey, shardValue); err != nil {
		t.Fatalf("Failed to add shard to etcd: %v", err)
	}

	// Wait for watch to pick up changes
	time.Sleep(500 * time.Millisecond)

	handler := server.createHTTPHandler()

	// Make request to shard endpoint
	req := httptest.NewRequest(http.MethodGet, "/shards", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Verify we have at least one shard
	shards, ok := result["shards"].([]interface{})
	if !ok {
		t.Fatal("Expected 'shards' field in response")
	}

	if len(shards) == 0 {
		t.Fatal("Expected at least one shard in response")
	}

	// Verify we have at least one node
	nodes, ok := result["nodes"].([]interface{})
	if !ok {
		t.Fatal("Expected 'nodes' field in response")
	}

	if len(nodes) == 0 {
		t.Fatal("Expected at least one node in response")
	}

	// Verify the node we registered is in the list
	found := false
	for _, n := range nodes {
		if n.(string) == nodeAddr {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Expected to find node %s in nodes list, got %v", nodeAddr, nodes)
	}

	// Verify shard info
	shardMap := shards[0].(map[string]interface{})
	if shardMap["shard_id"].(float64) != 0 {
		t.Fatalf("Expected shard_id 0, got %v", shardMap["shard_id"])
	}
	if shardMap["target_node"].(string) != nodeAddr {
		t.Fatalf("Expected target_node %s, got %v", nodeAddr, shardMap["target_node"])
	}
	if shardMap["current_node"].(string) != nodeAddr {
		t.Fatalf("Expected current_node %s, got %v", nodeAddr, shardMap["current_node"])
	}
}
