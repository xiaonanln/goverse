package main

import (
	"context"
	"flag"

	"github.com/simonlingoogle/pulse/pulseapi"
)

func main() {
	var (
		listenAddr       = flag.String("listen", "localhost:47000", "Server listen address")
		advertiseAddr    = flag.String("advertise", "localhost:47000", "Server advertise address")
		clientListenAddr = flag.String("client-listen", "localhost:48000", "Client listen address")
	)
	flag.Parse()

	config := &pulseapi.ServerConfig{
		ListenAddress:       *listenAddr,
		AdvertiseAddress:    *advertiseAddr,
		ClientListenAddress: *clientListenAddr,
	}
	// Create and run the server
	server := pulseapi.NewServer(config)
	initializeChatServer()
	err := server.Run()
	if err != nil {
		panic(err)
	}
}

func initializeChatServer() {
	pulseapi.RegisterClientType((*ChatClient)(nil))
	pulseapi.RegisterObjectType((*ChatRoomMgr)(nil))
	pulseapi.RegisterObjectType((*ChatRoom)(nil))
	pulseapi.CreateObject(context.Background(), "ChatRoomMgr", "ChatRoomMgr0", nil)
}
