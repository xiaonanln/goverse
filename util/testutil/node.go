package testutil

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
)

func MustNewNode(ctx context.Context, t *testing.T, advertiseAddr string) *node.Node {
	n := node.NewNode(advertiseAddr, TestNumShards)
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	t.Cleanup(func() { n.Stop(ctx) })
	return n
}
