package goverseapi

import (
	"context"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/server"
	"github.com/xiaonanln/goverse/util/callcontext"
	"google.golang.org/protobuf/proto"
)

// Type aliases for core components
type ServerConfig = server.ServerConfig
type Server = server.Server
type Node = node.Node
type Object = object.Object
type BaseObject = object.BaseObject
type BaseClient = client.BaseClient
type Cluster = cluster.Cluster

func NewServer(config *ServerConfig) (*Server, error) {
	return server.NewServer(config)
}

func RegisterObjectType(obj Object) {
	cluster.This().GetThisNode().RegisterObjectType(obj)
}

func CreateObject(ctx context.Context, objType, objID string) (string, error) {
	return cluster.This().CreateObject(ctx, objType, objID)
}

func DeleteObject(ctx context.Context, objID string) error {
	return cluster.This().DeleteObject(ctx, objID)
}

func CallObject(ctx context.Context, objType, id string, method string, request proto.Message) (proto.Message, error) {
	return cluster.This().CallObject(ctx, objType, id, method, request)
}

// PushMessageToClient sends a message to a client via the gate connection
// This allows distributed objects to push notifications/messages to connected clients
// The client ID has the format: gateAddress/uniqueId (e.g., "localhost:7001/abc123")
func PushMessageToClient(ctx context.Context, clientID string, message proto.Message) error {
	return cluster.This().PushMessageToClient(ctx, clientID, message)
}

// ClusterReady returns a channel that will be closed when the cluster is ready.
// The cluster is considered ready when:
// - Nodes are connected
// - Shard mapping has been successfully generated and loaded
//
// Usage:
//
//	<-goverseapi.ClusterReady()  // blocks until cluster is ready
//
//	// or with select:
//	select {
//	case <-goverseapi.ClusterReady():
//	    // cluster is ready
//	case <-ctx.Done():
//	    // timeout or cancel
//	}
func ClusterReady() <-chan bool {
	return cluster.This().ClusterReady()
}

// GetClientID retrieves the client ID from the call context.
// Returns empty string if the call did not originate from a client via the gate.
//
// The client ID format is: "gateAddress/uniqueId" (e.g., "localhost:7001/abc123")
//
// Usage in object methods:
//
//	func (obj *MyObject) MyMethod(ctx context.Context, req *MyRequest) (*MyResponse, error) {
//	    clientID := goverseapi.GetClientID(ctx)
//	    if clientID != "" {
//	        // Call came from a client via gate
//	    } else {
//	        // Call came from another object or local cluster
//	    }
//	    // ...
//	}
func GetClientID(ctx context.Context) string {
	return callcontext.ClientID(ctx)
}

// HasClientID checks if the call context contains a client ID.
// Returns true if the call originated from a client via the gate.
//
// Usage:
//
//	if goverseapi.HasClientID(ctx) {
//	    // Handle client call
//	} else {
//	    // Handle internal call
//	}
func HasClientID(ctx context.Context) bool {
	return callcontext.FromClient(ctx)
}
