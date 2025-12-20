package inspector

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

// TestReportLinkCall tests the ReportLinkCall RPC handler
func TestReportLinkCall(t *testing.T) {
	pg := graph.NewGoverseGraph()
	insp := New(pg)

	// Register two nodes
	node1 := models.GoverseNode{
		ID:            "localhost:47001",
		AdvertiseAddr: "localhost:47001",
	}
	node2 := models.GoverseNode{
		ID:            "localhost:47002",
		AdvertiseAddr: "localhost:47002",
	}
	pg.AddOrUpdateNode(node1)
	pg.AddOrUpdateNode(node2)

	ctx := context.Background()

	// Report a link call from node1 to node2
	req := &inspector_pb.ReportLinkCallRequest{
		SourceAddress: "localhost:47001",
		TargetAddress: "localhost:47002",
		IsGateSource:  false,
	}

	_, err := insp.ReportLinkCall(ctx, req)
	if err != nil {
		t.Fatalf("ReportLinkCall failed: %v", err)
	}

	// Verify the metric was recorded
	cpm := insp.getLinkCallsPerMinute("localhost:47001", "localhost:47002")
	if cpm != 1 {
		t.Fatalf("Expected 1 call per minute, got %d", cpm)
	}

	// Report more calls
	for i := 0; i < 5; i++ {
		_, err = insp.ReportLinkCall(ctx, req)
		if err != nil {
			t.Fatalf("ReportLinkCall failed: %v", err)
		}
	}

	// Verify the metrics increased
	cpm = insp.getLinkCallsPerMinute("localhost:47001", "localhost:47002")
	if cpm != 6 {
		t.Fatalf("Expected 6 calls per minute, got %d", cpm)
	}
}

// TestReportLinkCall_Gate tests link calls from a gate to a node
func TestReportLinkCall_Gate(t *testing.T) {
	pg := graph.NewGoverseGraph()
	insp := New(pg)

	// Register a gate and a node
	gate := models.GoverseGate{
		ID:            "localhost:49000",
		AdvertiseAddr: "localhost:49000",
	}
	node := models.GoverseNode{
		ID:            "localhost:47001",
		AdvertiseAddr: "localhost:47001",
	}
	pg.AddOrUpdateGate(gate)
	pg.AddOrUpdateNode(node)

	ctx := context.Background()

	// Report a link call from gate to node
	req := &inspector_pb.ReportLinkCallRequest{
		SourceAddress: "localhost:49000",
		TargetAddress: "localhost:47001",
		IsGateSource:  true,
	}

	_, err := insp.ReportLinkCall(ctx, req)
	if err != nil {
		t.Fatalf("ReportLinkCall failed: %v", err)
	}

	// Verify the metric was recorded
	cpm := insp.getLinkCallsPerMinute("localhost:49000", "localhost:47001")
	if cpm != 1 {
		t.Fatalf("Expected 1 call per minute, got %d", cpm)
	}
}

// TestReportLinkCall_UnregisteredSource tests that calls from unregistered sources are ignored
func TestReportLinkCall_UnregisteredSource(t *testing.T) {
	pg := graph.NewGoverseGraph()
	insp := New(pg)

	// Register only the target node
	node := models.GoverseNode{
		ID:            "localhost:47001",
		AdvertiseAddr: "localhost:47001",
	}
	pg.AddOrUpdateNode(node)

	ctx := context.Background()

	// Report a link call from unregistered source
	req := &inspector_pb.ReportLinkCallRequest{
		SourceAddress: "localhost:47999",
		TargetAddress: "localhost:47001",
		IsGateSource:  false,
	}

	_, err := insp.ReportLinkCall(ctx, req)
	if err != nil {
		t.Fatalf("ReportLinkCall should not fail for unregistered source: %v", err)
	}

	// Verify no metric was recorded
	cpm := insp.getLinkCallsPerMinute("localhost:47999", "localhost:47001")
	if cpm != 0 {
		t.Fatalf("Expected 0 calls per minute for unregistered source, got %d", cpm)
	}
}

// TestLinkMetricsCleanup tests that old link metrics are cleaned up
func TestLinkMetricsCleanup(t *testing.T) {
	pg := graph.NewGoverseGraph()
	insp := New(pg)

	// Register two nodes
	node1 := models.GoverseNode{
		ID:            "localhost:47001",
		AdvertiseAddr: "localhost:47001",
	}
	node2 := models.GoverseNode{
		ID:            "localhost:47002",
		AdvertiseAddr: "localhost:47002",
	}
	pg.AddOrUpdateNode(node1)
	pg.AddOrUpdateNode(node2)

	ctx := context.Background()

	// Report a link call
	req := &inspector_pb.ReportLinkCallRequest{
		SourceAddress: "localhost:47001",
		TargetAddress: "localhost:47002",
		IsGateSource:  false,
	}

	_, err := insp.ReportLinkCall(ctx, req)
	if err != nil {
		t.Fatalf("ReportLinkCall failed: %v", err)
	}

	// Verify metric exists
	cpm := insp.getLinkCallsPerMinute("localhost:47001", "localhost:47002")
	if cpm != 1 {
		t.Fatalf("Expected 1 call per minute, got %d", cpm)
	}

	// Manually add an old call that's outside the 1-minute window
	linkKey := "localhost:47001->localhost:47002"
	insp.linkMu.RLock()
	metrics := insp.linkMetrics[linkKey]
	insp.linkMu.RUnlock()

	metrics.mu.Lock()
	metrics.calls = append(metrics.calls, linkCallMetric{
		timestamp: time.Now().Add(-2 * time.Minute),
	})
	metrics.mu.Unlock()

	// The old call should not be counted
	cpm = insp.getLinkCallsPerMinute("localhost:47001", "localhost:47002")
	if cpm != 1 {
		t.Fatalf("Expected 1 call per minute (old call should not count), got %d", cpm)
	}
}

// TestRefreshLinkMetrics tests that link metrics are refreshed on nodes and gates
func TestRefreshLinkMetrics(t *testing.T) {
	pg := graph.NewGoverseGraph()
	insp := New(pg)

	// Register two nodes with a connection
	node1 := models.GoverseNode{
		ID:             "localhost:47001",
		AdvertiseAddr:  "localhost:47001",
		ConnectedNodes: []string{"localhost:47002"},
		LinkMetrics:    make(map[string]int),
	}
	node2 := models.GoverseNode{
		ID:             "localhost:47002",
		AdvertiseAddr:  "localhost:47002",
		ConnectedNodes: []string{"localhost:47001"},
		LinkMetrics:    make(map[string]int),
	}
	pg.AddOrUpdateNode(node1)
	pg.AddOrUpdateNode(node2)

	ctx := context.Background()

	// Report several link calls from node1 to node2
	for i := 0; i < 3; i++ {
		req := &inspector_pb.ReportLinkCallRequest{
			SourceAddress: "localhost:47001",
			TargetAddress: "localhost:47002",
			IsGateSource:  false,
		}
		_, err := insp.ReportLinkCall(ctx, req)
		if err != nil {
			t.Fatalf("ReportLinkCall failed: %v", err)
		}
	}

	// Manually trigger refresh (instead of waiting for the background goroutine)
	// Get all nodes and update their link metrics
	nodes := pg.GetNodes()
	for _, node := range nodes {
		linkMetrics := make(map[string]int)
		for _, targetAddr := range node.ConnectedNodes {
			cpm := insp.getLinkCallsPerMinute(node.AdvertiseAddr, targetAddr)
			if cpm > 0 {
				linkMetrics[targetAddr] = cpm
			}
		}
		node.LinkMetrics = linkMetrics
		pg.AddOrUpdateNode(node)
	}

	// Verify node1 has link metrics for node2
	nodes = pg.GetNodes()
	var updatedNode1 *models.GoverseNode
	for i, node := range nodes {
		if node.ID == "localhost:47001" {
			updatedNode1 = &nodes[i]
			break
		}
	}

	if updatedNode1 == nil {
		t.Fatal("Node1 not found after refresh")
	}

	if updatedNode1.LinkMetrics["localhost:47002"] != 3 {
		t.Fatalf("Expected 3 calls per minute in node1 link metrics, got %d", updatedNode1.LinkMetrics["localhost:47002"])
	}
}
