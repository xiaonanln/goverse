package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/xiaonanln/goverse/goverseapi"
	cache_pb "github.com/xiaonanln/goverse/samples/distributed-cache/proto"
)

// CacheManager is a node-local grain that manages cache operations for a specific node.
// Each node has its own CacheManager with ID format: "nodeAddress/cachemanager"
// Clients connect to the local CacheManager which routes requests to the appropriate
// DistributedCache grains based on key sharding.
type CacheManager struct {
	goverseapi.BaseObject

	hitCount  atomic.Int64
	missCount atomic.Int64
	mu        sync.Mutex
}

func (mgr *CacheManager) OnCreated() {
	mgr.Logger.Infof("CacheManager %s created", mgr.Id())
}

// Set creates or updates a cache entry
func (mgr *CacheManager) Set(ctx context.Context, request *cache_pb.CacheManager_SetRequest) (*cache_pb.CacheManager_SetResponse, error) {
	key := request.GetKey()
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	// Create the cache entry grain ID based on the key
	cacheID := "DistributedCache-" + key

	mgr.Logger.Infof("Setting cache entry for key: %s", key)

	// Create the DistributedCache grain if it doesn't exist (it will be created on-demand by goverse)
	// Call SetValue on the DistributedCache grain
	_, err := goverseapi.CallObject(ctx, "DistributedCache", cacheID, "SetValue", &cache_pb.DistributedCache_SetValueRequest{
		Value:      request.GetValue(),
		TtlSeconds: request.GetTtlSeconds(),
	})
	if err != nil {
		mgr.Logger.Errorf("Failed to set cache entry %s: %v", key, err)
		return &cache_pb.CacheManager_SetResponse{
			Success: false,
		}, err
	}

	return &cache_pb.CacheManager_SetResponse{
		Success: true,
	}, nil
}

// Get retrieves a value from the cache
func (mgr *CacheManager) Get(ctx context.Context, request *cache_pb.CacheManager_GetRequest) (*cache_pb.CacheManager_GetResponse, error) {
	key := request.GetKey()
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	cacheID := "DistributedCache-" + key

	mgr.Logger.Infof("Getting cache entry for key: %s", key)

	// Call GetValue on the DistributedCache grain
	resp, err := goverseapi.CallObject(ctx, "DistributedCache", cacheID, "GetValue", &cache_pb.DistributedCache_GetValueRequest{})
	if err != nil {
		mgr.Logger.Errorf("Failed to get cache entry %s: %v", key, err)
		mgr.missCount.Add(1)
		return &cache_pb.CacheManager_GetResponse{
			Found: false,
		}, nil // Return empty response instead of error for missing keys
	}

	getResp := resp.(*cache_pb.DistributedCache_GetValueResponse)
	if getResp.GetFound() {
		mgr.hitCount.Add(1)
		mgr.Logger.Infof("Cache hit for key: %s", key)
	} else {
		mgr.missCount.Add(1)
		mgr.Logger.Infof("Cache miss for key: %s", key)
	}

	return &cache_pb.CacheManager_GetResponse{
		Found: getResp.GetFound(),
		Value: getResp.GetValue(),
	}, nil
}

// Delete removes a cache entry
func (mgr *CacheManager) Delete(ctx context.Context, request *cache_pb.CacheManager_DeleteRequest) (*cache_pb.CacheManager_DeleteResponse, error) {
	key := request.GetKey()
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	cacheID := "DistributedCache-" + key

	mgr.Logger.Infof("Deleting cache entry for key: %s", key)

	// Call Delete on the DistributedCache grain
	_, err := goverseapi.CallObject(ctx, "DistributedCache", cacheID, "Delete", &cache_pb.DistributedCache_DeleteRequest{})
	if err != nil {
		mgr.Logger.Errorf("Failed to delete cache entry %s: %v", key, err)
		return &cache_pb.CacheManager_DeleteResponse{
			Success: false,
		}, err
	}

	// Also delete the object from the cluster
	err = goverseapi.DeleteObject(ctx, cacheID)
	if err != nil {
		mgr.Logger.Warnf("Failed to delete cache object %s: %v", cacheID, err)
		// Don't fail the operation - the value was cleared
	}

	return &cache_pb.CacheManager_DeleteResponse{
		Success: true,
	}, nil
}

// Stats returns cache statistics for this node
func (mgr *CacheManager) Stats(ctx context.Context, request *cache_pb.CacheManager_StatsRequest) (*cache_pb.CacheManager_StatsResponse, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// Note: In a real implementation, we would need to query all DistributedCache grains on this node
	// For simplicity, we just return hit/miss counts here
	hits := mgr.hitCount.Load()
	misses := mgr.missCount.Load()

	mgr.Logger.Infof("Cache stats - Hits: %d, Misses: %d", hits, misses)

	return &cache_pb.CacheManager_StatsResponse{
		TotalEntries: 0, // Would need to query all cache grains
		HitCount:     hits,
		MissCount:    misses,
		NodeAddress:  mgr.Id(),
	}, nil
}
