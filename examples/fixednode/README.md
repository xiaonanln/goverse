# Fixed Node Address for Objects

This example demonstrates the new fixed node addressing feature for object IDs in Goverse.

## Overview

Object IDs can now use the format `nodeAddress/objectID` (e.g., `localhost:7001/my-object`) to pin objects to specific nodes, similar to how client IDs work in the system.

## Format

```
nodeAddress/objectID
```

Where:
- `nodeAddress`: The address of the specific node where the object should be created/accessed (e.g., `localhost:7001`, `192.168.1.100:8080`)
- `/`: The separator character
- `objectID`: The actual object identifier (e.g., `my-object`, `session-abc123`)

## Examples

### Fixed Node Address
```go
// This object will always be created/accessed on localhost:7001
objectID := "localhost:7001/my-special-object"
_, err := goverseapi.CreateObject(ctx, "MyObjectType", objectID, nil)
```

### Regular Object ID (Shard-based routing)
```go
// This object will be placed based on shard mapping
objectID := "my-regular-object"
_, err := goverseapi.CreateObject(ctx, "MyObjectType", objectID, nil)
```

## Use Cases

1. **Session Affinity**: Pin session objects to specific nodes for better performance
2. **Data Locality**: Keep related objects on the same node
3. **Testing**: Force objects to specific nodes for testing scenarios
4. **Migration**: Gradually move objects between nodes during cluster rebalancing

## How It Works

When you call `CreateObject` or `CallObject` with an object ID containing a `/` separator:

1. The system extracts the node address (part before `/`)
2. The request is routed directly to that node
3. If the node address matches the current node, the operation is handled locally
4. If it's a different node, the operation is forwarded via gRPC

This is consistent with how client IDs work in the format `nodeAddress/clientID`.

## Running the Example

```bash
cd examples/fixednode
go run main.go
```

## See Also

- [Client Service Documentation](../../client/README.md) - Client IDs use the same format
- [Cluster Documentation](../../cluster/README.md) - Cluster routing and shard mapping
- [Sharding Documentation](../../cluster/sharding/README.md) - Shard-based object placement
