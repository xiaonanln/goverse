package main

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	cache_pb "github.com/xiaonanln/goverse/samples/distributed-cache/proto"
)

// DistributedCache represents a single cache entry in the distributed cache system.
// Each grain is identified by its key and holds a value with optional TTL.
// These grains are automatically distributed across the cluster using goverse's sharding.
type DistributedCache struct {
	goverseapi.BaseObject

	value     string
	expiresAt int64 // Unix timestamp in seconds, 0 means no expiration
	mu        sync.Mutex
}

func (cache *DistributedCache) OnCreated() {
	cache.Logger.Infof("DistributedCache entry %s created", cache.Id())
}

// SetValue sets the value and optional TTL for this cache entry
func (cache *DistributedCache) SetValue(ctx context.Context, request *cache_pb.DistributedCache_SetValueRequest) (*cache_pb.DistributedCache_SetValueResponse, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.value = request.GetValue()

	// Set expiration if TTL is provided
	if request.GetTtlSeconds() > 0 {
		cache.expiresAt = time.Now().Unix() + request.GetTtlSeconds()
		cache.Logger.Infof("Set cache entry %s with TTL %d seconds (expires at %d)", cache.Id(), request.GetTtlSeconds(), cache.expiresAt)
	} else {
		cache.expiresAt = 0
		cache.Logger.Infof("Set cache entry %s with no expiration", cache.Id())
	}

	return &cache_pb.DistributedCache_SetValueResponse{
		Success: true,
	}, nil
}

// GetValue retrieves the value from this cache entry, checking expiration
func (cache *DistributedCache) GetValue(ctx context.Context, request *cache_pb.DistributedCache_GetValueRequest) (*cache_pb.DistributedCache_GetValueResponse, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Check if entry has expired
	if cache.expiresAt > 0 && time.Now().Unix() > cache.expiresAt {
		cache.Logger.Infof("Cache entry %s has expired", cache.Id())
		return &cache_pb.DistributedCache_GetValueResponse{
			Found: false,
		}, nil
	}

	cache.Logger.Infof("Cache hit for entry %s", cache.Id())
	return &cache_pb.DistributedCache_GetValueResponse{
		Found:     cache.value != "",
		Value:     cache.value,
		ExpiresAt: cache.expiresAt,
	}, nil
}

// Delete marks this cache entry as deleted (in practice, the grain can be deactivated)
func (cache *DistributedCache) Delete(ctx context.Context, request *cache_pb.DistributedCache_DeleteRequest) (*cache_pb.DistributedCache_DeleteResponse, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.Logger.Infof("Deleting cache entry %s", cache.Id())
	cache.value = ""
	cache.expiresAt = 0

	return &cache_pb.DistributedCache_DeleteResponse{
		Success: true,
	}, nil
}
