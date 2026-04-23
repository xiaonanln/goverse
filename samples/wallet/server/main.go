package main

import (
	"context"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("WalletServer")

func main() {
	server := goverseapi.NewServer()

	goverseapi.RegisterObjectType((*Wallet)(nil))
	serverLogger.Infof("Wallet object type registered")

	if err := server.Run(context.Background()); err != nil {
		panic(err)
	}
}
