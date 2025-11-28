package main

import (
	"context"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("CounterServer")

func main() {
	// Create and run the server using command-line flags
	server := goverseapi.NewServer()

	initializeCounterServer()

	err := server.Run(context.Background())
	if err != nil {
		panic(err)
	}
}

func initializeCounterServer() {
	// Register the Counter object type
	goverseapi.RegisterObjectType((*Counter)(nil))
	serverLogger.Infof("Counter object type registered")
}
