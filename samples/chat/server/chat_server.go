package main

import (
	"context"
	"flag"

	"github.com/xiaonanln/goverse/goverseapi"
)

func main() {
	var (
		listenAddr       = flag.String("listen", "localhost:47000", "Server listen address")
		advertiseAddr    = flag.String("advertise", "localhost:47000", "Server advertise address")
		clientListenAddr = flag.String("client-listen", "localhost:48000", "Client listen address")
	)
	flag.Parse()

	config := &goverseapi.ServerConfig{
		ListenAddress:       *listenAddr,
		AdvertiseAddress:    *advertiseAddr,
		ClientListenAddress: *clientListenAddr,
	}
	// Create and run the server
	server := goverseapi.NewServer(config)
	initializeChatServer()
	err := server.Run()
	if err != nil {
		panic(err)
	}
}

func initializeChatServer() {
	goverseapi.RegisterClientType((*ChatClient)(nil))
	goverseapi.RegisterObjectType((*ChatRoomMgr)(nil))
	goverseapi.RegisterObjectType((*ChatRoom)(nil))
	
	// Register callback to create ChatRoomMgr0 when cluster is ready
	goverseapi.RegisterClusterReadyCallback(func() {
		goverseapi.CreateObject(context.Background(), "ChatRoomMgr", "ChatRoomMgr0", nil)
	})
}
