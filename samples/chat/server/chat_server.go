package main

import (
	"context"
	"flag"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("ChatServer")

func main() {
	var (
		listenAddr        = flag.String("listen", "localhost:47000", "Server listen address")
		advertiseAddr     = flag.String("advertise", "localhost:47000", "Server advertise address")
		metricsListenAddr = flag.String("metrics-listen", "localhost:9100", "Metrics listen address")
	)
	flag.Parse()

	config := &goverseapi.ServerConfig{
		ListenAddress:        *listenAddr,
		AdvertiseAddress:     *advertiseAddr,
		MetricsListenAddress: *metricsListenAddr,
	}
	// Create and run the server
	server, err := goverseapi.NewServer(config)
	if err != nil {
		panic(err)
	}
	initializeChatServer()
	err = server.Run()
	if err != nil {
		panic(err)
	}
}

func initializeChatServer() {
	// TODO: Refactor chat sample to match GATEWAY_DESIGN.md section 4.1-4.3
	// The ChatClient object type is part of the old client-to-node architecture.
	// Per the design doc, clients should call ChatRoom objects directly via CallObject,
	// without ChatClient objects on nodes. Client proxies should live in the gateway.
	goverseapi.RegisterObjectType((*ChatClient)(nil))
	goverseapi.RegisterObjectType((*ChatRoomMgr)(nil))
	goverseapi.RegisterObjectType((*ChatRoom)(nil))

	// Create ChatRoomMgr0 when cluster is ready
	go func() {
		serverLogger.Infof("Waiting for cluster to be ready...")
		<-goverseapi.ClusterReady()
		serverLogger.Infof("Cluster is ready, creating ChatRoomMgr0 in 5 seconds...")
		time.Sleep(5 * time.Second)
		goverseapi.CreateObject(context.Background(), "ChatRoomMgr", "ChatRoomMgr0")
	}()
}
