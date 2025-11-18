package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	client_pb "github.com/xiaonanln/goverse/client/proto"
	cache_pb "github.com/xiaonanln/goverse/samples/distributed-cache/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
)

func main() {
	serverAddr := flag.String("server", "localhost:48000", "Server address")
	flag.Parse()

	// Connect to the cache server
	conn, err := grpc.Dial(*serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	client := client_pb.NewClientServiceClient(conn)

	// Register as a client
	stream, err := client.Register(context.Background(), &client_pb.Empty{})
	if err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	// Read initial registration response
	msgAny, err := stream.Recv()
	if err != nil {
		log.Fatalf("Failed to receive registration response: %v", err)
	}

	msg, err := msgAny.UnmarshalNew()
	if err != nil {
		log.Fatalf("Failed to unmarshal registration response: %v", err)
	}

	regResp := msg.(*client_pb.RegisterResponse)
	clientID := regResp.ClientId
	fmt.Printf("✅ Connected to cache server (Client ID: %s)\n\n", clientID)

	// Demonstrate cache operations
	demonstrateCacheOperations(client, clientID)
}

func demonstrateCacheOperations(client client_pb.ClientServiceClient, clientID string) {
	fmt.Println("=== Distributed Cache Demo ===")
	fmt.Println()

	// Set operations
	fmt.Println("1. Setting cache entries...")
	setCacheEntry(client, clientID, "user:1001", "Alice", 0)
	setCacheEntry(client, clientID, "user:1002", "Bob", 0)
	setCacheEntry(client, clientID, "session:abc123", "active", 30) // 30 second TTL
	setCacheEntry(client, clientID, "temp:data", "temporary", 5)    // 5 second TTL
	fmt.Println()

	// Get operations
	fmt.Println("2. Getting cache entries...")
	getCacheEntry(client, clientID, "user:1001")
	getCacheEntry(client, clientID, "user:1002")
	getCacheEntry(client, clientID, "session:abc123")
	getCacheEntry(client, clientID, "nonexistent:key")
	fmt.Println()

	// Update operation
	fmt.Println("3. Updating cache entry...")
	setCacheEntry(client, clientID, "user:1001", "Alice Updated", 0)
	getCacheEntry(client, clientID, "user:1001")
	fmt.Println()

	// Stats
	fmt.Println("4. Getting cache statistics...")
	getStats(client, clientID)
	fmt.Println()

	// Wait for TTL expiration
	fmt.Println("5. Testing TTL expiration (waiting 6 seconds)...")
	time.Sleep(6 * time.Second)
	getCacheEntry(client, clientID, "temp:data") // Should be expired
	getCacheEntry(client, clientID, "user:1001") // Should still exist
	fmt.Println()

	// Delete operation
	fmt.Println("6. Deleting cache entry...")
	deleteCacheEntry(client, clientID, "user:1002")
	getCacheEntry(client, clientID, "user:1002") // Should not be found
	fmt.Println()

	// Final stats
	fmt.Println("7. Final cache statistics...")
	getStats(client, clientID)
	fmt.Println()

	fmt.Println("=== Demo Complete ===")
}

func setCacheEntry(client client_pb.ClientServiceClient, clientID, key, value string, ttl int64) {
	ctx := context.Background()

	setReq := &cache_pb.Client_SetRequest{
		Key:        key,
		Value:      value,
		TtlSeconds: ttl,
	}

	anyReq, _ := anypb.New(setReq)
	resp, err := client.Call(ctx, &client_pb.CallRequest{
		ClientId: clientID,
		Method:   "Set",
		Request:  anyReq,
	})

	if err != nil {
		fmt.Printf("   ❌ Set failed for key '%s': %v\n", key, err)
		return
	}

	setResp := &cache_pb.Client_SetResponse{}
	resp.Response.UnmarshalTo(setResp)

	if setResp.Success {
		if ttl > 0 {
			fmt.Printf("   ✅ Set key '%s' = '%s' (TTL: %d seconds)\n", key, value, ttl)
		} else {
			fmt.Printf("   ✅ Set key '%s' = '%s' (no expiration)\n", key, value)
		}
	} else {
		fmt.Printf("   ❌ Set failed for key '%s'\n", key)
	}
}

func getCacheEntry(client client_pb.ClientServiceClient, clientID, key string) {
	ctx := context.Background()

	getReq := &cache_pb.Client_GetRequest{
		Key: key,
	}

	anyReq, _ := anypb.New(getReq)
	resp, err := client.Call(ctx, &client_pb.CallRequest{
		ClientId: clientID,
		Method:   "Get",
		Request:  anyReq,
	})

	if err != nil {
		fmt.Printf("   ❌ Get failed for key '%s': %v\n", key, err)
		return
	}

	getResp := &cache_pb.Client_GetResponse{}
	resp.Response.UnmarshalTo(getResp)

	if getResp.Found {
		fmt.Printf("   ✅ Get key '%s' = '%s'\n", key, getResp.Value)
	} else {
		fmt.Printf("   ⚠️  Key '%s' not found or expired\n", key)
	}
}

func deleteCacheEntry(client client_pb.ClientServiceClient, clientID, key string) {
	ctx := context.Background()

	deleteReq := &cache_pb.Client_DeleteRequest{
		Key: key,
	}

	anyReq, _ := anypb.New(deleteReq)
	resp, err := client.Call(ctx, &client_pb.CallRequest{
		ClientId: clientID,
		Method:   "Delete",
		Request:  anyReq,
	})

	if err != nil {
		fmt.Printf("   ❌ Delete failed for key '%s': %v\n", key, err)
		return
	}

	deleteResp := &cache_pb.Client_DeleteResponse{}
	resp.Response.UnmarshalTo(deleteResp)

	if deleteResp.Success {
		fmt.Printf("   ✅ Deleted key '%s'\n", key)
	} else {
		fmt.Printf("   ❌ Delete failed for key '%s'\n", key)
	}
}

func getStats(client client_pb.ClientServiceClient, clientID string) {
	ctx := context.Background()

	statsReq := &cache_pb.Client_GetStatsRequest{}

	anyReq, _ := anypb.New(statsReq)
	resp, err := client.Call(ctx, &client_pb.CallRequest{
		ClientId: clientID,
		Method:   "GetStats",
		Request:  anyReq,
	})

	if err != nil {
		fmt.Printf("   ❌ GetStats failed: %v\n", err)
		return
	}

	statsResp := &cache_pb.Client_GetStatsResponse{}
	resp.Response.UnmarshalTo(statsResp)

	fmt.Printf("   📊 Cache Statistics:\n")
	fmt.Printf("      - Node: %s\n", statsResp.NodeAddress)
	fmt.Printf("      - Hits: %d\n", statsResp.HitCount)
	fmt.Printf("      - Misses: %d\n", statsResp.MissCount)
	fmt.Printf("      - Hit Rate: %.2f%%\n", calculateHitRate(statsResp.HitCount, statsResp.MissCount))
}

func calculateHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total) * 100.0
}
