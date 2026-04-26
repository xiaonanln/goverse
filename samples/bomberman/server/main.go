package main

import (
	"context"

	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("BombermanServer")

func main() {
	server := goverseapi.NewServer()

	goverseapi.RegisterObjectType((*Match)(nil))
	goverseapi.RegisterObjectType((*Player)(nil))
	goverseapi.RegisterObjectType((*MatchmakingQueue)(nil))
	serverLogger.Infof("Bomberman object types registered (Match, Player, MatchmakingQueue)")

	if err := server.Run(context.Background()); err != nil {
		panic(err)
	}
}
