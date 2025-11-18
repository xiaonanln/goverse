package main

import (
	"context"
	"flag"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("CacheServer")

func main() {
	var (
		listenAddr        = flag.String("listen", "localhost:47000", "Server listen address")
		advertiseAddr     = flag.String("advertise", "localhost:47000", "Server advertise address")
		clientListenAddr  = flag.String("client-listen", "localhost:48000", "Client listen address")
		metricsListenAddr = flag.String("metrics-listen", "localhost:9100", "Metrics listen address")
	)
	flag.Parse()

	config := &goverseapi.ServerConfig{
		ListenAddress:        *listenAddr,
		AdvertiseAddress:     *advertiseAddr,
		ClientListenAddress:  *clientListenAddr,
		MetricsListenAddress: *metricsListenAddr,
	}

	// Create and run the server
	server, err := goverseapi.NewServer(config)
	if err != nil {
		panic(err)
	}

	initializeCacheServer()

	err = server.Run()
	if err != nil {
		panic(err)
	}
}

func initializeCacheServer() {
	// Register all types
	goverseapi.RegisterClientType((*CacheClient)(nil))
	goverseapi.RegisterObjectType((*CacheManager)(nil))
	goverseapi.RegisterObjectType((*DistributedCache)(nil))

	// Create the CacheManager when cluster is ready
	go func() {
		serverLogger.Infof("Waiting for cluster to be ready...")
		<-goverseapi.ClusterReady()
		serverLogger.Infof("Cluster is ready, creating CacheManager in 5 seconds...")
		time.Sleep(5 * time.Second)
		_, err := goverseapi.CreateObject(context.Background(), "CacheManager", "CacheManager0")
		if err != nil {
			serverLogger.Errorf("Failed to create CacheManager: %v", err)
		} else {
			serverLogger.Infof("CacheManager0 created successfully")
		}
	}()
}
