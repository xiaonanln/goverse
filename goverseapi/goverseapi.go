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

// GoverseService defines the core API for interacting with distributed objects.
// This interface abstracts the internal object-call API used by nodes and gateways.
//
// Implementations:
//   - serverGoverseService (in server package): wraps in-process object calls on nodes
//   - Future: gatewayGoverseService will implement remote calls via gRPC
type GoverseService interface {
	// CallObject calls a method on a distributed object.
	// The service determines the target node and routes the call appropriately.
	CallObject(ctx context.Context, objType, objectID, method string, request proto.Message) (proto.Message, error)

	// CreateObject creates a new distributed object.
	// If objectID is empty, a unique ID will be generated.
	// Returns the ID of the created object.
	CreateObject(ctx context.Context, objType, objectID string) (string, error)

	// DeleteObject deletes a distributed object.
	// The operation is typically asynchronous to prevent deadlocks.
	DeleteObject(ctx context.Context, objectID string) error
}

// Type aliases for core components
type ServerConfig = server.ServerConfig
type Server = server.Server
type Node = node.Node
type Object = object.Object
type ClientObject = client.ClientObject
type BaseObject = object.BaseObject
type BaseClient = client.BaseClient
type Cluster = cluster.Cluster

func NewServer(config *ServerConfig) (*Server, error) {
	return server.NewServer(config)
}

// GetGoverseService returns the GoverseService from a Server instance.
// This provides access to the internal object-call API for advanced use cases.
func GetGoverseService(srv *Server) GoverseService {
	return srv.GetGoverseService()
}

func RegisterClientType(clientObj ClientObject) {
	cluster.This().GetThisNode().RegisterClientType(clientObj)
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

// PushMessageToClient sends a message to a client via their client object
// This allows distributed objects to push notifications/messages to connected clients
// The client ID has the format: nodeAddress/uniqueId (e.g., "localhost:7001/abc123")
// This method automatically routes the message to the correct node in the cluster
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
