package main

import (
	"context"
	"flag"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("CounterServer")

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

	initializeCounterServer()

	err = server.Run(context.Background())
	if err != nil {
		panic(err)
	}
}

func initializeCounterServer() {
	// Register the Counter object type
	goverseapi.RegisterObjectType((*Counter)(nil))
	serverLogger.Infof("Counter object type registered")
}
