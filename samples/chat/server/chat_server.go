package main

import (
	"context"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("ChatServer")

func main() {
	// Create and run the server using command-line flags
	server := goverseapi.NewServer()
	initializeChatServer()
	err := server.Run(context.Background())
	if err != nil {
		panic(err)
	}
}

func initializeChatServer() {
	// Register object types - no ChatClient needed with new gate architecture
	// Clients call ChatRoomMgr and ChatRoom objects directly via the gate
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
