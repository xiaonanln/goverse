package goverseapi

import (
	"context"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/server"
	"google.golang.org/protobuf/proto"
)

// Type aliases for core components
type ServerConfig = server.ServerConfig
type Server = server.Server
type Node = node.Node
type Object = object.Object
type ClientObject = client.ClientObject
type BaseObject = object.BaseObject
type BaseClient = client.BaseClient
type Cluster = cluster.Cluster

func NewServer(config *ServerConfig) *Server {
	return server.NewServer(config)
}

func RegisterClientType(clientObj ClientObject) {
	cluster.Get().GetThisNode().RegisterClientType(clientObj)
}

func RegisterObjectType(obj Object) {
	cluster.Get().GetThisNode().RegisterObjectType(obj)
}

func CreateObject(ctx context.Context, objType, objID string, initData proto.Message) (string, error) {
	return cluster.Get().GetThisNode().CreateObject(ctx, objType, objID, initData)
}

func CallObject(ctx context.Context, id string, method string, request proto.Message) (proto.Message, error) {
	return cluster.Get().CallObject(ctx, id, method, request)
}
