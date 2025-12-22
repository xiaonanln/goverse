package goverseapi

import (
	"context"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/node/serverconfig"
	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
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

// ReliableCallObject performs a reliable call to an object method with exactly-once semantics.
// This ensures the call is executed exactly once, even in the presence of failures, retries, or network issues.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - callID: Unique identifier for this call (used for deduplication). Generate using GenerateCallID()
//   - objType: The type name of the target object
//   - objID: The unique identifier of the target object
//   - method: The name of the method to call on the object
//   - request: The protobuf message to pass as the method argument
//
// Returns:
//   - proto.Message: The result of the method call (nil on error)
//   - goverse_pb.ReliableCallStatus: The execution status (SUCCESS, FAILED, SKIPPED, etc.)
//   - error: Error information if the call failed
//
// The callID is used for deduplication - if you retry with the same callID, the cached result
// is returned without re-executing the method. This provides exactly-once semantics.
//
// The status indicates whether the call was executed:
//   - SUCCESS: Call executed successfully
//   - FAILED: Call executed but method invocation failed
//   - SKIPPED: Call failed before execution (deserialization, validation errors)
//
// Callers can use the status to determine if retry is safe:
//   - SKIPPED: Safe to retry with corrected data
//   - FAILED: May have side effects, retry with caution
//
// Example:
//
//	callID := goverseapi.GenerateCallID()
//	result, status, err := goverseapi.ReliableCallObject(ctx, callID, "OrderProcessor", "order-123", "ProcessPayment", request)
//	if err != nil {
//	    if status == goverse_pb.ReliableCallStatus_SKIPPED {
//	        // Safe to retry - call was never executed
//	    } else if status == goverse_pb.ReliableCallStatus_FAILED {
//	        // Caution - call was executed, may have side effects
//	    }
//	}
func ReliableCallObject(ctx context.Context, callID string, objType, objID string, method string, request proto.Message) (proto.Message, goverse_pb.ReliableCallStatus, error) {
	return cluster.This().ReliableCallObject(ctx, callID, objType, objID, method, request)
}

// PushMessageToClients sends a message to one or more clients via the gate connection.
// This allows distributed objects to push notifications/messages to connected clients.
// This is efficient whether pushing to a single client or multiple clients
// on the same gate due to automatic fan-out optimization.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - clientIDs: List of client IDs (format: "gateAddress/uniqueId"). Can be a single client or multiple clients.
//   - message: The protobuf message to push to all clients
//
// Returns:
//   - error: Non-nil if any push operation fails
//
// Example (single client):
//
//	err := goverseapi.PushMessageToClients(ctx, []string{"localhost:7001/user1"}, notification)
//
// Example (multiple clients):
//
//	clientIDs := []string{"localhost:7001/user1", "localhost:7001/user2"}
//	err := goverseapi.PushMessageToClients(ctx, clientIDs, notification)
func PushMessageToClients(ctx context.Context, clientIDs []string, message proto.Message) error {
	return cluster.This().PushMessageToClients(ctx, clientIDs, message)
}

// BroadcastToAllClients sends a message to all clients connected to all gates.
// The message is sent to all connected gates, and each gate fans out the message
// to all its connected clients.
//
// This is useful for server-wide announcements, notifications, or events that
// should be delivered to every connected client regardless of their state or location.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - message: The protobuf message to broadcast to all clients
//
// Returns:
//   - error: Non-nil if no gates are connected or if push operations fail
//
// Example:
//
//	announcement := &MyNotification{
//	    Type: "server_announcement",
//	    Text: "Server maintenance in 5 minutes",
//	}
//	err := goverseapi.BroadcastToAllClients(ctx, announcement)
//	if err != nil {
//	    log.Printf("Failed to broadcast: %v", err)
//	}
func BroadcastToAllClients(ctx context.Context, message proto.Message) error {
	return cluster.This().BroadcastToAllClients(ctx, message)
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

// GetAutoLoadObjectIDsByType returns all object IDs for auto-load objects of the specified type.
// This is useful when you need to interact with auto-load objects from your application code.
//
// For global objects, it returns a single ID.
// For per-shard objects, it returns IDs in the format: shard#<N>/<baseName>
// For per-node objects, it returns IDs in the format: <nodeAddr>/<baseName>
//
// Example:
//
//	// Get all IDs for "GameLobby" auto-load objects
//	lobbyIDs := goverseapi.GetAutoLoadObjectIDsByType("GameLobby")
//	for _, id := range lobbyIDs {
//	    resp, _ := goverseapi.CallObject(ctx, "GameLobby", id, "GetStatus", nil)
//	}
func GetAutoLoadObjectIDsByType(objType string) []string {
	return cluster.This().GetAutoLoadObjectIDsByType(objType)
}

// GetAutoLoadObjectIDs returns a map of object type to list of object IDs for all auto-load objects.
// This provides a convenient way to discover all auto-load objects configured in the cluster.
//
// Example:
//
//	// Get all auto-load object IDs grouped by type
//	allAutoLoadIDs := goverseapi.GetAutoLoadObjectIDs()
//	for objType, ids := range allAutoLoadIDs {
//	    fmt.Printf("Type: %s has %d objects\n", objType, len(ids))
//	    for _, id := range ids {
//	        // Process each object
//	    }
//	}
func GetAutoLoadObjectIDs() map[string][]string {
	return cluster.This().GetAutoLoadObjectIDs()
}

// GenerateCallID generates a unique call ID for reliable calls.
// The generated ID is guaranteed to be unique and can be used for reliable call deduplication.
//
// Use this function when you need to ensure exactly-once semantics for inter-object calls.
// By providing the same call ID in retry attempts, the reliable call system ensures the
// operation is only executed once, with cached results returned for subsequent attempts.
//
// Example:
//
//	// Generate a unique call ID
//	callID := goverseapi.GenerateCallID()
//
//	// Use it with reliable call
//	result, err := goverseapi.ReliableCallObject(ctx, callID, "OrderProcessor", "order-123", "ProcessPayment", request)
func GenerateCallID() string {
	return uniqueid.UniqueId()
}
