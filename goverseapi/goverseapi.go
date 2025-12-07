package goverseapi

import (
	"context"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/node/serverconfig"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/server"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/uniqueid"
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

// NewServer creates a Server using command-line flags.
// This is the recommended way to create a server in applications.
// This is a convenience wrapper around serverconfig.Get() and NewServerWithConfig().
//
// Supports both CLI flags and config file modes:
//
// CLI mode:
//
//	go run . --listen :47000 --advertise localhost:47000 --etcd localhost:2379
//
// Config file mode:
//
//	go run . --config config.yaml --node-id node1
//
// Available flags:
//   - --listen: Node listen address (default: :48000)
//   - --advertise: Node advertise address (default: localhost:48000)
//   - --http-listen: HTTP listen address for metrics
//   - --etcd: Etcd address (default: localhost:2379)
//   - --etcd-prefix: Etcd key prefix (default: /goverse)
//   - --config: Path to YAML config file
//   - --node-id: Node ID (required with --config)
//
// Panics on configuration errors or server creation failures.
func NewServer() *Server {
	config := serverconfig.Get()
	server, err := NewServerWithConfig(config)
	if err != nil {
		panic(err)
	}
	return server
}

// NewServerWithConfig creates a Server with the given configuration.
func NewServerWithConfig(config *ServerConfig) (*Server, error) {
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

// CallerClientID retrieves the client ID from the call context.
// Returns empty string if the call did not originate from a client via the gate.
//
// The client ID format is: "gateAddress/uniqueId" (e.g., "localhost:7001/abc123")
//
// Usage in object methods:
//
//	func (obj *MyObject) MyMethod(ctx context.Context, req *MyRequest) (*MyResponse, error) {
//	    clientID := goverseapi.CallerClientID(ctx)
//	    if clientID != "" {
//	        // Call came from a client via gate
//	    } else {
//	        // Call came from another object or local cluster
//	    }
//	    // ...
//	}
func CallerClientID(ctx context.Context) string {
	return callcontext.ClientID(ctx)
}

// GetClientID is deprecated. Use CallerClientID instead.
func GetClientID(ctx context.Context) string {
	return CallerClientID(ctx)
}

// CallerIsClient checks if the call context contains a client ID.
// Returns true if the call originated from a client via the gate.
//
// Usage:
//
//	if goverseapi.CallerIsClient(ctx) {
//	    // Handle client call
//	} else {
//	    // Handle internal call
//	}
func CallerIsClient(ctx context.Context) bool {
	return callcontext.FromClient(ctx)
}

// HasClientID is deprecated. Use CallerIsClient instead.
func HasClientID(ctx context.Context) bool {
	return CallerIsClient(ctx)
}

// CreateObjectID creates a normal object ID using a unique identifier.
// The object will be distributed to a node based on hash-based sharding.
//
// Example:
//
//	objID := goverseapi.CreateObjectID()
//	goverseapi.CreateObject(ctx, "MyObject", objID)
func CreateObjectID() string {
	return uniqueid.CreateObjectID()
}

// CreateObjectIDOnShard creates an object ID that will be placed on a specific shard.
// The shard ID must be in the valid range [0, numShards).
// By default, Goverse uses 8192 shards (sharding.NumShards).
//
// Example:
//
//	objID := goverseapi.CreateObjectIDOnShard(5)
//	goverseapi.CreateObject(ctx, "MyObject", objID)
func CreateObjectIDOnShard(shardID int) string {
	return uniqueid.CreateObjectIDOnShard(shardID, sharding.NumShards)
}

// CreateObjectIDOnNode creates an object ID that will be placed on a specific node.
// The node address should be in the format "host:port".
//
// Example:
//
//	objID := goverseapi.CreateObjectIDOnNode("localhost:7001")
//	goverseapi.CreateObject(ctx, "MyObject", objID)
func CreateObjectIDOnNode(nodeAddress string) string {
	return uniqueid.CreateObjectIDOnNode(nodeAddress)
}

// NodeInfo represents information about a node
type NodeInfo = cluster.NodeInfo

// GateInfo represents information about a gate
type GateInfo = cluster.GateInfo

// GetNodesInfo returns a map of node addresses to their information.
// Keys are node addresses, values contain whether configured and whether found in cluster state.
//
// When using a config file, configured nodes will have Configured=true.
// When using CLI flags only, all nodes will have Configured=false.
//
// All nodes currently active in the cluster (registered in etcd) will have IsAlive=true.
// The leader node will have IsLeader=true.
//
// Example:
//
//	nodesInfo := goverseapi.GetNodesInfo()
//	for addr, info := range nodesInfo {
//	    fmt.Printf("Node %s: Configured=%v, IsAlive=%v, IsLeader=%v\n",
//	        addr, info.Configured, info.IsAlive, info.IsLeader)
//	}
func GetNodesInfo() map[string]NodeInfo {
	return cluster.This().GetNodesInfo()
}

// GetGatesInfo returns a map of gate addresses to their information.
// Keys are gate addresses, values contain whether configured and whether found in cluster state.
//
// When using a config file, configured gates will have Configured=true.
// When using CLI flags only, all gates will have Configured=false.
//
// All gates currently active in the cluster (registered in etcd) will have IsAlive=true.
//
// Example:
//
//	gatesInfo := goverseapi.GetGatesInfo()
//	for addr, info := range gatesInfo {
//	    fmt.Printf("Gate %s: Configured=%v, IsAlive=%v\n",
//	        addr, info.Configured, info.IsAlive)
//	}
func GetGatesInfo() map[string]GateInfo {
	return cluster.This().GetGatesInfo()
}
