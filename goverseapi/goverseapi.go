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
	return cluster.Get().CreateObject(ctx, objType, objID, initData)
}

func CallObject(ctx context.Context, id string, method string, request proto.Message) (proto.Message, error) {
	return cluster.Get().CallObject(ctx, id, method, request)
}

// PushMessageToClient sends a message to a client's message channel
// This allows distributed objects to push notifications/messages to connected clients
// The client ID has the format: nodeAddress/uniqueId (e.g., "localhost:7001/abc123")
// This method automatically routes the message to the correct node in the cluster
func PushMessageToClient(ctx context.Context, clientID string, message proto.Message) error {
	return cluster.Get().PushMessageToClient(ctx, clientID, message)
}

// ClusterReady returns a channel that will be closed when the cluster is ready.
// The cluster is considered ready when:
// - Nodes are connected
// - Shard mapping has been successfully generated and loaded
// 
// Usage:
//   <-goverseapi.ClusterReady()  // blocks until cluster is ready
//   
//   // or with select:
//   select {
//   case <-goverseapi.ClusterReady():
//       // cluster is ready
//   case <-ctx.Done():
//       // timeout or cancel
//   }
func ClusterReady() <-chan bool {
	return cluster.Get().ClusterReady()
}
