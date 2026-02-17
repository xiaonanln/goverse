package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/consensusmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCluster_Stop_DefaultsToQuickMode(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	nodeAddr := "test-node:1234"
	n := node.NewNode(nodeAddr, 8)

	config := Config{
		EtcdAddress: etcdManager.GetAddress(),
		EtcdPrefix:  etcdManager.GetPrefix(),
		NumShards:   8,
	}

	cluster, err := NewClusterWithNode(config, n)
	require.NoError(t, err)

	ctx := context.Background()
	err = cluster.Start(ctx, n)
	require.NoError(t, err)

	// Test that Stop() uses quick mode by default (should complete immediately)
	start := time.Now()
	err = cluster.Stop(ctx)
	duration := time.Since(start)
	
	assert.NoError(t, err)
	// Quick shutdown should be very fast (less than 1 second)
	assert.Less(t, duration, 1*time.Second, "Quick shutdown should complete quickly")
}

func TestCluster_StopWithMode_QuickMode(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	nodeAddr := "test-node:1234"
	n := node.NewNode(nodeAddr, 8)

	config := Config{
		EtcdAddress: etcdManager.GetAddress(),
		EtcdPrefix:  etcdManager.GetPrefix(),
		NumShards:   8,
	}

	cluster, err := NewClusterWithNode(config, n)
	require.NoError(t, err)

	ctx := context.Background()
	err = cluster.Start(ctx, n)
	require.NoError(t, err)

	// Test explicit quick mode
	start := time.Now()
	err = cluster.StopWithMode(ctx, consensusmanager.ShutdownModeQuick)
	duration := time.Since(start)
	
	assert.NoError(t, err)
	assert.Less(t, duration, 1*time.Second, "Quick shutdown should complete quickly")
}

func TestCluster_StopWithMode_CleanMode_NoShards(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	nodeAddr := "test-node:1234"
	n := node.NewNode(nodeAddr, 8)

	config := Config{
		EtcdAddress: etcdManager.GetAddress(),
		EtcdPrefix:  etcdManager.GetPrefix(),
		NumShards:   8,
	}

	cluster, err := NewClusterWithNode(config, n)
	require.NoError(t, err)

	ctx := context.Background()
	err = cluster.Start(ctx, n)
	require.NoError(t, err)

	// Test clean mode with no shards (should complete quickly since nothing to release)
	start := time.Now()
	err = cluster.StopWithMode(ctx, consensusmanager.ShutdownModeClean)
	duration := time.Since(start)
	
	assert.NoError(t, err)
	// Should still be fast when no shards to release
	assert.Less(t, duration, 2*time.Second, "Clean shutdown with no shards should be relatively quick")
}

func TestCluster_StopWithMode_CleanMode_WithShards(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	nodeAddr := "test-node:1234"
	otherNodeAddr := "other-node:5678"
	
	// Set up first cluster/node
	n1 := node.NewNode(nodeAddr, 8)
	config := Config{
		EtcdAddress: etcdManager.GetAddress(),
		EtcdPrefix:  etcdManager.GetPrefix(),
		NumShards:   8,
	}

	cluster1, err := NewClusterWithNode(config, n1)
	require.NoError(t, err)

	ctx := context.Background()
	err = cluster1.Start(ctx, n1)
	require.NoError(t, err)
	
	// Set up second cluster/node to claim released shards
	n2 := node.NewNode(otherNodeAddr, 8)
	cluster2, err := NewClusterWithNode(config, n2)
	require.NoError(t, err)
	
	err = cluster2.Start(ctx, n2)
	require.NoError(t, err)
	defer cluster2.Stop(ctx)

	// Wait for both nodes to be ready and have claimed some shards
	require.Eventually(t, func() bool {
		return cluster1.IsReady() && cluster2.IsReady()
	}, 10*time.Second, 100*time.Millisecond, "Both clusters should become ready")

	// Give some time for shard assignment and claiming
	time.Sleep(2 * time.Second)

	// Test clean shutdown - should release shards and wait for handoff
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	err = cluster1.StopWithMode(shutdownCtx, consensusmanager.ShutdownModeClean)
	assert.NoError(t, err)
}

func TestCluster_StopWithMode_GateIgnoresShutdownMode(t *testing.T) {
	etcdManager, cleanup := testutil.SetupTestEtcd(t)
	defer cleanup()

	gateAddr := "test-gate:1234"
	
	// Create a cluster with a gate (not a node)
	config := Config{
		EtcdAddress: etcdManager.GetAddress(),
		EtcdPrefix:  etcdManager.GetPrefix(),
		NumShards:   8,
	}

	// Use NewClusterWithGate instead of NewClusterWithNode
	// First create a basic gate - we'll use a mock since gate creation is complex
	gw := &mockGate{address: gateAddr}
	
	cluster, err := NewClusterWithGate(config, gw)
	require.NoError(t, err)

	ctx := context.Background()
	
	// Test that gates ignore shutdown mode (both should behave the same)
	err = cluster.StopWithMode(ctx, consensusmanager.ShutdownModeQuick)
	assert.NoError(t, err)
	
	// Create another cluster instance for clean mode test
	cluster2, err := NewClusterWithGate(config, gw)
	require.NoError(t, err)
	
	err = cluster2.StopWithMode(ctx, consensusmanager.ShutdownModeClean) 
	assert.NoError(t, err)
}

// mockGate is a minimal mock implementation for testing
type mockGate struct {
	address string
}

func (m *mockGate) GetAdvertiseAddress() string {
	return m.address
}

func (m *mockGate) Start(ctx context.Context) error {
	return nil
}

func (m *mockGate) Stop() error {
	return nil
}