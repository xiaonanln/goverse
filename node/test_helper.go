package node

import (
	"context"
	"testing"
)

// testNumShards is the number of shards to use in tests.
// Using a smaller number (64) instead of production default (8192)
// makes tests faster and reduces resource usage.
const testNumShards = 64

// MustNewNode creates and starts a new node for testing.
// The node is automatically stopped when the test completes via t.Cleanup.
func MustNewNode(ctx context.Context, t *testing.T, advertiseAddr string, numShards int) *Node {
	n := NewNode(advertiseAddr, numShards)
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	t.Cleanup(func() { n.Stop(ctx) })
	return n
}
