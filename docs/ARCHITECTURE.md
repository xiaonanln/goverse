# GoVerse Architecture

> ⚠️ **Work In Progress** - This document describes the evolving architecture of GoVerse. The system is transitioning from an all-in-one server model to a Node/Gateway split architecture. Some features described here are not yet fully implemented.

## Overview

GoVerse is a distributed object runtime that implements the virtual actor (grain) model. The architecture is designed to scale horizontally while providing automatic object placement, lifecycle management, and fault tolerance.

The system is evolving toward a clear separation between:
- **Nodes** that host distributed objects and participate in the cluster
- **Gateways** that handle client-facing protocols (gRPC, HTTP) and route requests to nodes
- **GoVerse Service** as the internal API layer for object operations across the cluster

## Components

### GoVerse Server (Node)

The **Node** is the core runtime that hosts distributed objects. Each node:

- **Hosts distributed objects**: Manages the lifecycle of objects, including activation, deactivation, and persistence
- **Participates in the cluster**: Registers with etcd, maintains cluster membership, and responds to shard mapping changes
- **Handles object-to-object calls**: Provides node-to-node RPC for objects calling other objects within the cluster
- **Manages object state**: Handles object creation, method calls, and deletion on locally-hosted objects
- **Implements the GoVerse service protocol**: Exposes gRPC methods (`CallObject`, `CreateObject`, `DeleteObject`) for internal cluster communication

Nodes do not directly serve external clients. Instead, they focus on object hosting and cluster coordination.

### GoVerse Service

The **GoVerse Service** is a conceptual API layer that defines the core operations for working with distributed objects:

- `CallObject(ctx, objectID, method, request) → response`: Call a method on a distributed object
- `CreateObject(ctx, typeName, objectID, args) → objectID`: Create a new distributed object
- `DeleteObject(ctx, objectID) → error`: Delete a distributed object

This service API is:
- **Used by nodes** for object-to-object calls (one object calls another via the cluster)
- **Used by gateways** to route client requests to the appropriate nodes
- **Implemented by nodes** via gRPC for intra-cluster communication
- **Wrapped by an interface** in the `goverseapi` package for easy consumption

The GoVerse Service abstraction allows different components (nodes, gateways) to interact with distributed objects using a consistent API, whether calling locally or remotely.

### Gateway

The **Gateway** is a separate process responsible for handling external client connections and routing their requests to nodes. Gateways:

- **Serve client-facing protocols**: Provide gRPC `ClientService` API and future HTTP/REST endpoints
- **Route requests to nodes**: Use the GoVerse Service client to forward client requests to the appropriate node based on shard mapping
- **Manage client connections**: Handle client registration, maintain bidirectional streaming connections for push messaging
- **Connect to the cluster**: Use etcd to discover nodes and access shard mapping
- **Host client proxies**: Run per-connection controllers (client objects) that handle client-specific logic

**Client proxies** (formerly "client objects") are lightweight, per-connection controllers that run in the gateway process. They are:
- **Not distributed objects**: They are regular Go objects, not virtual actors
- **Addressable only within the gateway**: They cannot be called from distributed objects
- **Ephemeral**: They exist only for the duration of the client connection
- **Used for client-specific logic**: Authentication, session management, request preprocessing

### Cluster Coordination

The cluster uses **etcd** for coordination:

- **Node registry**: Nodes register their presence and advertised addresses
- **Shard mapping**: Maps 8192 fixed shards to node addresses, managed automatically by the leader node
- **Leader election**: One node is elected as the cluster leader, responsible for shard mapping management
- **Configuration**: Stores cluster-wide configuration and state

The **ConsensusManager** provides:
- **Leadership election**: Determines which node manages shard mapping
- **Node lifecycle tracking**: Detects node joins and departures
- **Automatic shard mapping updates**: Leader node updates shard mapping when cluster membership changes

The **Sharding System** uses:
- **Fixed 8192 shards**: Object IDs are consistently hashed to one of 8192 shards
- **Shard-to-node mapping**: Each shard is assigned to a node address
- **Automatic rebalancing**: When nodes join or leave, the leader redistributes shards
- **Migration coordination**: Shards transition from `CurrentNode` to `TargetNode` during rebalancing

## Call Flows

### External Client → Gateway → Node

1. **Client connects to gateway**:
   - Client establishes gRPC connection to gateway
   - Gateway creates a client proxy (client object) for the connection
   - Gateway returns a client ID to the client

2. **Client calls a method** (e.g., `client.CallMethod(objectID, method, request)`):
   - Client sends RPC to gateway with object ID, method, and request
   - Gateway looks up object ID in shard mapping to determine target node
   - Gateway uses GoVerse Service client to forward the call to the target node
   - Node executes the method on the object (activating it if necessary)
   - Response flows back: Node → Gateway → Client

3. **Object pushes a message to client** (server-initiated):
   - Distributed object calls `PushMessageToClient(clientID, message)`
   - Cluster routes the push to the gateway hosting the client connection
   - Gateway delivers the message over the bidirectional stream to the client

### Internal Object → Object Call

1. **Object A calls Object B**:
   - Object A (on Node X) calls `goverseapi.CallObject(ctx, objectBID, method, request)`
   - The call goes through the cluster's GoVerse Service
   - Cluster determines Object B is on Node Y via shard mapping
   - Node X makes gRPC call to Node Y using the GoVerse Service protocol
   - Node Y activates Object B (if needed) and executes the method
   - Response flows back: Node Y → Node X → Object A

2. **Local optimization**:
   - If Object A and Object B are on the same node, the call is handled locally without gRPC overhead

## Current State vs Target State

### Current State (All-in-One Server)

Currently, GoVerse runs as a single `goverse-server` binary that combines:
- Node functionality (object hosting)
- Client service (gRPC `ClientService` for external clients)
- Client objects (run inside the node process)

All object calls and client requests are handled by the same process.

### Target State (Node/Gateway Split)

The target architecture separates concerns:
- **Nodes** (`goverse-node` binary): Focus on object hosting and cluster participation
- **Gateways** (`goverse-gateway` binary): Handle client protocols (gRPC, HTTP) and route to nodes
- **Client proxies**: Run in gateways, not as distributed objects
- **GoVerse Service**: Standardized API for node-to-node and gateway-to-node communication

Benefits of the split:
- **Scalability**: Scale gateways and nodes independently based on load
- **Protocol flexibility**: Gateways can add HTTP, WebSocket, or custom protocols without affecting nodes
- **Security**: Gateways can implement authentication, rate limiting, and other client-facing concerns
- **Simplified nodes**: Nodes focus solely on object hosting and cluster coordination
- **Better resource allocation**: Gateways can be lightweight, nodes can be heavyweight with more objects

## Migration Path

The migration from the current all-in-one architecture to the Node/Gateway split is happening incrementally:

1. **Phase 1: Abstraction layer** (Current PR)
   - Introduce `GoverseService` interface in `goverseapi` package
   - Refactor internal code to route calls through the interface
   - No behavioral changes; all tests and samples continue to work

2. **Phase 2: Gateway package** (Future PR)
   - Create `gateway` package with gateway server implementation
   - Move `ClientService` from server to gateway
   - Create `goverse-gateway` binary
   - Implement GoVerse Service client for gateways to call nodes

3. **Phase 3: Client proxy refactoring** (Future PR)
   - Refactor client objects to run in gateway process only
   - Remove client object hosting from nodes
   - Update client proxy lifecycle management

4. **Phase 4: HTTP support** (Future PR)
   - Add HTTP/REST endpoints to gateway
   - Implement WebSocket support for bidirectional messaging
   - Add protocol-agnostic client proxy interface

5. **Phase 5: Migration tooling** (Future PR)
   - Provide migration guides for existing deployments
   - Support hybrid mode where old and new architectures coexist
   - Gradual rollout strategies

## Design Principles

- **Incremental migration**: Each phase should be deployable without breaking existing functionality
- **Backward compatibility**: Existing applications should continue to work during the transition
- **Clear separation of concerns**: Gateways handle client protocols, nodes handle objects
- **Consistent API**: GoVerse Service provides a uniform interface for object operations
- **Horizontal scalability**: Both gateways and nodes can scale independently
- **Fault tolerance**: System continues operating even if gateways or nodes fail

## References

- [Getting Started Guide](GET_STARTED.md) - Complete guide to building with GoVerse
- [Sharding Documentation](../cluster/sharding/README.md) - Details on shard mapping
- [Push Messaging](PUSH_MESSAGING.md) - Server-to-client push functionality
- [Prometheus Integration](PROMETHEUS_INTEGRATION.md) - Monitoring and observability
