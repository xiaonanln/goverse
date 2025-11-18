package main

import (
	"context"

	"github.com/xiaonanln/goverse/goverseapi"
	cache_pb "github.com/xiaonanln/goverse/samples/distributed-cache/proto"
)

// CacheClient represents a connected client and provides the client-side API
// for interacting with the distributed cache.
type CacheClient struct {
	goverseapi.BaseClient
	cacheManagerID string
}

func (cc *CacheClient) OnCreated() {
	cc.BaseClient.OnCreated()
	// Use the node's address to determine which CacheManager to use
	// In a real deployment, this would be the local node's CacheManager
	cc.cacheManagerID = "CacheManager0"
	cc.Logger.Infof("CacheClient %s created, using CacheManager: %s", cc.Id(), cc.cacheManagerID)
}

// Set stores a key-value pair in the cache
func (cc *CacheClient) Set(ctx context.Context, request *cache_pb.Client_SetRequest) (*cache_pb.Client_SetResponse, error) {
	cc.Logger.Infof("Client setting cache key: %s", request.GetKey())

	resp, err := goverseapi.CallObject(ctx, "CacheManager", cc.cacheManagerID, "Set", &cache_pb.CacheManager_SetRequest{
		Key:        request.GetKey(),
		Value:      request.GetValue(),
		TtlSeconds: request.GetTtlSeconds(),
	})
	if err != nil {
		return nil, err
	}

	setResp := resp.(*cache_pb.CacheManager_SetResponse)
	return &cache_pb.Client_SetResponse{
		Success: setResp.GetSuccess(),
	}, nil
}

// Get retrieves a value from the cache
func (cc *CacheClient) Get(ctx context.Context, request *cache_pb.Client_GetRequest) (*cache_pb.Client_GetResponse, error) {
	cc.Logger.Infof("Client getting cache key: %s", request.GetKey())

	resp, err := goverseapi.CallObject(ctx, "CacheManager", cc.cacheManagerID, "Get", &cache_pb.CacheManager_GetRequest{
		Key: request.GetKey(),
	})
	if err != nil {
		return nil, err
	}

	getResp := resp.(*cache_pb.CacheManager_GetResponse)
	return &cache_pb.Client_GetResponse{
		Found: getResp.GetFound(),
		Value: getResp.GetValue(),
	}, nil
}

// Delete removes a key from the cache
func (cc *CacheClient) Delete(ctx context.Context, request *cache_pb.Client_DeleteRequest) (*cache_pb.Client_DeleteResponse, error) {
	cc.Logger.Infof("Client deleting cache key: %s", request.GetKey())

	resp, err := goverseapi.CallObject(ctx, "CacheManager", cc.cacheManagerID, "Delete", &cache_pb.CacheManager_DeleteRequest{
		Key: request.GetKey(),
	})
	if err != nil {
		return nil, err
	}

	deleteResp := resp.(*cache_pb.CacheManager_DeleteResponse)
	return &cache_pb.Client_DeleteResponse{
		Success: deleteResp.GetSuccess(),
	}, nil
}

// GetStats retrieves cache statistics
func (cc *CacheClient) GetStats(ctx context.Context, request *cache_pb.Client_GetStatsRequest) (*cache_pb.Client_GetStatsResponse, error) {
	cc.Logger.Infof("Client getting cache stats")

	resp, err := goverseapi.CallObject(ctx, "CacheManager", cc.cacheManagerID, "Stats", &cache_pb.CacheManager_StatsRequest{})
	if err != nil {
		return nil, err
	}

	statsResp := resp.(*cache_pb.CacheManager_StatsResponse)
	return &cache_pb.Client_GetStatsResponse{
		TotalEntries: statsResp.GetTotalEntries(),
		HitCount:     statsResp.GetHitCount(),
		MissCount:    statsResp.GetMissCount(),
		NodeAddress:  statsResp.GetNodeAddress(),
	}, nil
}
