package main

import (
	"context"
	"testing"

	pb "github.com/xiaonanln/goverse/samples/sharding_demo/proto"
)

func TestSimpleCounter_Increment(t *testing.T) {
	counter := &SimpleCounter{}
	counter.OnInit(counter, "test-counter")
	counter.OnCreated()

	resp, err := counter.Increment(context.Background(), &pb.IncrementRequest{})
	if err != nil {
		t.Fatalf("Increment failed: %v", err)
	}

	if resp.Value != 1 {
		t.Errorf("Expected value 1, got %d", resp.Value)
	}

	// Increment again
	resp, err = counter.Increment(context.Background(), &pb.IncrementRequest{})
	if err != nil {
		t.Fatalf("Second Increment failed: %v", err)
	}

	if resp.Value != 2 {
		t.Errorf("Expected value 2, got %d", resp.Value)
	}
}

func TestSimpleCounter_GetValue(t *testing.T) {
	counter := &SimpleCounter{}
	counter.OnInit(counter, "test-counter")
	counter.OnCreated()

	// Initially should be 0
	resp, err := counter.GetValue(context.Background(), &pb.GetValueRequest{})
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}

	if resp.Value != 0 {
		t.Errorf("Expected value 0, got %d", resp.Value)
	}

	// Increment and check again
	counter.Increment(context.Background(), &pb.IncrementRequest{})
	resp, err = counter.GetValue(context.Background(), &pb.GetValueRequest{})
	if err != nil {
		t.Fatalf("GetValue after increment failed: %v", err)
	}

	if resp.Value != 1 {
		t.Errorf("Expected value 1, got %d", resp.Value)
	}
}

func TestShardMonitor_UpdateObjectCount(t *testing.T) {
	monitor := &ShardMonitor{}
	monitor.OnInit(monitor, "shard#5/ShardMonitor")
	monitor.OnCreated()

	resp, err := monitor.UpdateObjectCount(context.Background(), &pb.UpdateObjectCountRequest{Count: 42})
	if err != nil {
		t.Fatalf("UpdateObjectCount failed: %v", err)
	}

	if resp == nil {
		t.Fatalf("Expected non-nil response")
	}

	// Verify the count was set
	statsResp, err := monitor.GetStats(context.Background(), &pb.GetStatsRequest{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if statsResp.ObjectCount != 42 {
		t.Errorf("Expected object count 42, got %d", statsResp.ObjectCount)
	}
}

func TestShardMonitor_GetStats(t *testing.T) {
	monitor := &ShardMonitor{}
	monitor.OnInit(monitor, "shard#7/ShardMonitor")
	monitor.OnCreated()

	resp, err := monitor.GetStats(context.Background(), &pb.GetStatsRequest{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if resp.ShardId != 7 {
		t.Errorf("Expected shard ID 7, got %d", resp.ShardId)
	}

	if resp.ObjectCount != 0 {
		t.Errorf("Expected initial object count 0, got %d", resp.ObjectCount)
	}
}

func TestNodeMonitor_GetStats(t *testing.T) {
	monitor := &NodeMonitor{}
	monitor.OnInit(monitor, "node-1")
	monitor.OnCreated()

	resp, err := monitor.GetStats(context.Background(), &pb.GetNodeStatsRequest{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	expectedStatus := "NodeMonitor node-1 active"
	if resp.Status != expectedStatus {
		t.Errorf("Expected status '%s', got '%s'", expectedStatus, resp.Status)
	}
}

func TestGlobalMonitor_GetUptime(t *testing.T) {
	monitor := &GlobalMonitor{}
	monitor.OnInit(monitor, "GlobalMonitor")
	monitor.OnCreated()

	resp, err := monitor.GetUptime(context.Background(), &pb.GetUptimeRequest{})
	if err != nil {
		t.Fatalf("GetUptime failed: %v", err)
	}

	if resp.Uptime == "" {
		t.Errorf("Expected non-empty uptime, got empty string")
	}
}
