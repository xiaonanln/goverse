package main

import (
	"context"
	"testing"
	"time"

	cache_pb "github.com/xiaonanln/goverse/samples/distributed-cache/proto"
)

func TestDistributedCache_SetAndGet(t *testing.T) {
	cache := &DistributedCache{}
	cache.OnInit(cache, "test-cache-1")
	cache.OnCreated()

	ctx := context.Background()

	// Set a value
	setReq := &cache_pb.DistributedCache_SetValueRequest{
		Value:      "test value",
		TtlSeconds: 0,
	}
	setResp, err := cache.SetValue(ctx, setReq)
	if err != nil {
		t.Fatalf("SetValue failed: %v", err)
	}
	if !setResp.Success {
		t.Fatal("SetValue returned success=false")
	}

	// Get the value
	getReq := &cache_pb.DistributedCache_GetValueRequest{}
	getResp, err := cache.GetValue(ctx, getReq)
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}
	if !getResp.Found {
		t.Fatal("GetValue returned found=false")
	}
	if getResp.Value != "test value" {
		t.Fatalf("Expected value 'test value', got '%s'", getResp.Value)
	}
}

func TestDistributedCache_TTLExpiration(t *testing.T) {
	cache := &DistributedCache{}
	cache.OnInit(cache, "test-cache-ttl")
	cache.OnCreated()

	ctx := context.Background()

	// Set a value with 1 second TTL
	setReq := &cache_pb.DistributedCache_SetValueRequest{
		Value:      "expiring value",
		TtlSeconds: 1,
	}
	_, err := cache.SetValue(ctx, setReq)
	if err != nil {
		t.Fatalf("SetValue failed: %v", err)
	}

	// Immediately get - should exist
	getReq := &cache_pb.DistributedCache_GetValueRequest{}
	getResp, err := cache.GetValue(ctx, getReq)
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}
	if !getResp.Found {
		t.Fatal("GetValue should have found the value")
	}

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Get again - should be expired
	getResp, err = cache.GetValue(ctx, getReq)
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}
	if getResp.Found {
		t.Fatal("GetValue should not have found the expired value")
	}
}

func TestDistributedCache_Delete(t *testing.T) {
	cache := &DistributedCache{}
	cache.OnInit(cache, "test-cache-delete")
	cache.OnCreated()

	ctx := context.Background()

	// Set a value
	setReq := &cache_pb.DistributedCache_SetValueRequest{
		Value:      "to be deleted",
		TtlSeconds: 0,
	}
	_, err := cache.SetValue(ctx, setReq)
	if err != nil {
		t.Fatalf("SetValue failed: %v", err)
	}

	// Delete the value
	deleteReq := &cache_pb.DistributedCache_DeleteRequest{}
	deleteResp, err := cache.Delete(ctx, deleteReq)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if !deleteResp.Success {
		t.Fatal("Delete returned success=false")
	}

	// Get should not find it
	getReq := &cache_pb.DistributedCache_GetValueRequest{}
	getResp, err := cache.GetValue(ctx, getReq)
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}
	if getResp.Found {
		t.Fatal("GetValue should not have found the deleted value")
	}
}

func TestDistributedCache_UpdateValue(t *testing.T) {
	cache := &DistributedCache{}
	cache.OnInit(cache, "test-cache-update")
	cache.OnCreated()

	ctx := context.Background()

	// Set initial value
	setReq := &cache_pb.DistributedCache_SetValueRequest{
		Value:      "initial value",
		TtlSeconds: 0,
	}
	_, err := cache.SetValue(ctx, setReq)
	if err != nil {
		t.Fatalf("SetValue failed: %v", err)
	}

	// Update value
	setReq = &cache_pb.DistributedCache_SetValueRequest{
		Value:      "updated value",
		TtlSeconds: 0,
	}
	_, err = cache.SetValue(ctx, setReq)
	if err != nil {
		t.Fatalf("SetValue update failed: %v", err)
	}

	// Get the updated value
	getReq := &cache_pb.DistributedCache_GetValueRequest{}
	getResp, err := cache.GetValue(ctx, getReq)
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}
	if getResp.Value != "updated value" {
		t.Fatalf("Expected 'updated value', got '%s'", getResp.Value)
	}
}

func TestCacheManager_Stats(t *testing.T) {
	mgr := &CacheManager{}
	mgr.OnInit(mgr, "test-manager")
	mgr.OnCreated()

	ctx := context.Background()

	// Get initial stats
	statsReq := &cache_pb.CacheManager_StatsRequest{}
	statsResp, err := mgr.Stats(ctx, statsReq)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if statsResp.HitCount != 0 {
		t.Fatalf("Expected 0 hits, got %d", statsResp.HitCount)
	}
	if statsResp.MissCount != 0 {
		t.Fatalf("Expected 0 misses, got %d", statsResp.MissCount)
	}
}

func TestCacheManager_EmptyKeyValidation(t *testing.T) {
	mgr := &CacheManager{}
	mgr.OnInit(mgr, "test-manager-validation")
	mgr.OnCreated()

	ctx := context.Background()

	// Try to set with empty key
	setReq := &cache_pb.CacheManager_SetRequest{
		Key:   "",
		Value: "some value",
	}
	_, err := mgr.Set(ctx, setReq)
	if err == nil {
		t.Fatal("Set with empty key should have failed")
	}

	// Try to get with empty key
	getReq := &cache_pb.CacheManager_GetRequest{
		Key: "",
	}
	_, err = mgr.Get(ctx, getReq)
	if err == nil {
		t.Fatal("Get with empty key should have failed")
	}

	// Try to delete with empty key
	deleteReq := &cache_pb.CacheManager_DeleteRequest{
		Key: "",
	}
	_, err = mgr.Delete(ctx, deleteReq)
	if err == nil {
		t.Fatal("Delete with empty key should have failed")
	}
}

func TestCacheClient_Initialization(t *testing.T) {
	client := &CacheClient{}
	client.OnInit(client, "test-client-1")
	client.OnCreated()

	if client.cacheManagerID != "CacheManager0" {
		t.Fatalf("Expected cacheManagerID to be 'CacheManager0', got '%s'", client.cacheManagerID)
	}
}
