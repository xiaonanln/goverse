// Package main implements a demonstration server that showcases Goverse's sharding features.
//
// This demo includes:
// - SimpleCounter: Normal objects distributed across shards
// - ShardMonitor: Per-shard auto-load objects tracking shard stats
// - NodeMonitor: Simulated per-node objects for node-level aggregation
// - GlobalMonitor: Global singleton for overall stats
//
// Usage:
//
//	go run main.go --config demo-cluster.yml --node-id node-1
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/sharding_demo/proto"
)

// SimpleCounter is a basic counter object that demonstrates normal object distribution
type SimpleCounter struct {
	goverseapi.BaseObject
	mu    sync.Mutex
	value int64
}

func (c *SimpleCounter) OnCreated() {
	c.Logger.Infof("SimpleCounter %s created", c.Id())
}

func (c *SimpleCounter) Increment(ctx context.Context, req *pb.IncrementRequest) (*pb.IncrementResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
	return &pb.IncrementResponse{Value: c.value}, nil
}

func (c *SimpleCounter) GetValue(ctx context.Context, req *pb.GetValueRequest) (*pb.GetValueResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &pb.GetValueResponse{Value: c.value}, nil
}

// ShardMonitor tracks statistics for a specific shard (per-shard auto-load object)
type ShardMonitor struct {
	goverseapi.BaseObject
	mu          sync.Mutex
	shardID     int
	objectCount int
}

func (s *ShardMonitor) OnCreated() {
	// Extract shard ID from object ID (format: shard#N/ShardMonitor)
	id := s.Id()
	n, err := fmt.Sscanf(id, "shard#%d/", &s.shardID)
	if err != nil || n != 1 {
		s.Logger.Warnf("Failed to parse shard ID from object ID %s: %v", id, err)
		s.shardID = -1 // Invalid shard ID
	}
	s.Logger.Infof("ShardMonitor created for shard %d", s.shardID)
}

func (s *ShardMonitor) UpdateObjectCount(ctx context.Context, req *pb.UpdateObjectCountRequest) (*pb.UpdateObjectCountResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objectCount = int(req.Count)
	return &pb.UpdateObjectCountResponse{}, nil
}

func (s *ShardMonitor) GetStats(ctx context.Context, req *pb.GetStatsRequest) (*pb.GetStatsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &pb.GetStatsResponse{ShardId: int32(s.shardID), ObjectCount: int32(s.objectCount)}, nil
}

// NodeMonitor aggregates statistics and creates counters (simulated per-node via multiple auto-load entries)
type NodeMonitor struct {
	goverseapi.BaseObject
	mu               sync.Mutex
	nodeID           string
	counterNames     []string
	creationInterval time.Duration
	stopCh           chan struct{}
}

func (n *NodeMonitor) OnCreated() {
	n.nodeID = n.Id()
	n.creationInterval = 15 * time.Second
	n.stopCh = make(chan struct{})

	// Initialize counter names (Counter-001 to Counter-100)
	n.counterNames = make([]string, 100)
	for i := 0; i < 100; i++ {
		n.counterNames[i] = fmt.Sprintf("Counter-%03d", i+1)
	}

	n.Logger.Infof("NodeMonitor %s created, will periodically create counters", n.nodeID)

	// Start background goroutine to create counters
	go n.periodicCounterCreation()
}

func (n *NodeMonitor) periodicCounterCreation() {
	ticker := time.NewTicker(n.creationInterval)
	defer ticker.Stop()

	// Do initial creation after a short delay
	time.Sleep(5 * time.Second)
	n.createCounters()

	for {
		select {
		case <-ticker.C:
			n.createCounters()
		case <-n.stopCh:
			return
		}
	}
}

func (n *NodeMonitor) createCounters() {
	ctx := context.Background()
	created := 0

	n.Logger.Infof("NodeMonitor %s: Scanning counter list for objects to create...", n.nodeID)

	// For each counter name, try to create it
	// The counter will be distributed to appropriate shards based on hash
	for _, counterName := range n.counterNames {
		counterID := fmt.Sprintf("SimpleCounter-%s", counterName)
		shardID := sharding.GetShardID(counterID, sharding.NumShards)

		// Try to create the object - creation is idempotent
		_, err := goverseapi.CreateObject(ctx, "SimpleCounter", counterID)
		if err != nil {
			// Object may already exist or shard may not be owned by this node
			// Both cases are expected and don't require logging at this frequency
			continue
		}
		created++
		n.Logger.Infof("Created counter %s (shard %d)", counterID, shardID)
	}

	if created > 0 {
		n.Logger.Infof("NodeMonitor %s: Created %d new counters", n.nodeID, created)
	}
}

func (n *NodeMonitor) GetStats(ctx context.Context, req *pb.GetNodeStatsRequest) (*pb.GetNodeStatsResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return &pb.GetNodeStatsResponse{Status: fmt.Sprintf("NodeMonitor %s active", n.nodeID)}, nil
}

// GlobalMonitor provides global statistics and logging (singleton auto-load)
type GlobalMonitor struct {
	goverseapi.BaseObject
	mu        sync.Mutex
	startTime time.Time
	stopCh    chan struct{}
}

func (g *GlobalMonitor) OnCreated() {
	g.startTime = time.Now()
	g.stopCh = make(chan struct{})
	g.Logger.Infof("GlobalMonitor created at %s", g.startTime.Format(time.RFC3339))

	// Start periodic logging
	go g.periodicLogging()
}

func (g *GlobalMonitor) periodicLogging() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.logGlobalStats()
		case <-g.stopCh:
			return
		}
	}
}

func (g *GlobalMonitor) logGlobalStats() {
	g.mu.Lock()
	uptime := time.Since(g.startTime)
	g.mu.Unlock()

	g.Logger.Infof("Global Stats: Uptime=%v", uptime.Round(time.Second))
}

func (g *GlobalMonitor) GetUptime(ctx context.Context, req *pb.GetUptimeRequest) (*pb.GetUptimeResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	uptime := time.Since(g.startTime)
	return &pb.GetUptimeResponse{Uptime: uptime.String()}, nil
}

func main() {
	// Create server (uses command-line flags or config file)
	server := goverseapi.NewServer()

	// Register object types after server is created
	goverseapi.RegisterObjectType((*SimpleCounter)(nil))
	goverseapi.RegisterObjectType((*ShardMonitor)(nil))
	goverseapi.RegisterObjectType((*NodeMonitor)(nil))
	goverseapi.RegisterObjectType((*GlobalMonitor)(nil))

	// Wait for cluster to be ready
	go func() {
		<-goverseapi.ClusterReady()
		fmt.Println("✓ Demo cluster is ready!")
		fmt.Println("  - ShardMonitor objects created (one per shard)")
		fmt.Println("  - NodeMonitor objects managing counter creation")
		fmt.Println("  - GlobalMonitor providing global statistics")
	}()

	// Run the server (blocks)
	ctx := context.Background()
	if err := server.Run(ctx); err != nil {
		panic(err)
	}
}
