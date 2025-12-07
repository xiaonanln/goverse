# Cluster Info Example

This example demonstrates how to use the `GetNodesInfo()` and `GetGatesInfo()` APIs to retrieve information about nodes and gates in the Goverse cluster.

## What It Does

The example shows how to:
- Get information about all nodes in the cluster
- Get information about all gates in the cluster
- Check which nodes/gates are configured (from config file) vs discovered (from etcd)
- Perform health checks to detect configured but missing components

## API Overview

### GetNodesInfo()

Returns a map of node addresses to `NodeInfo` structs:

```go
type NodeInfo struct {
    Address             string
    Configured          bool  // true if from config file
    FoundInClusterState bool  // true if registered in etcd
}

nodesInfo := goverseapi.GetNodesInfo()
for addr, info := range nodesInfo {
    fmt.Printf("Node %s: Configured=%v, InCluster=%v\n",
        addr, info.Configured, info.FoundInClusterState)
}
```

### GetGatesInfo()

Returns a map of gate addresses to `GateInfo` structs:

```go
type GateInfo struct {
    Address             string
    Configured          bool  // true if from config file
    FoundInClusterState bool  // true if registered in etcd
}

gatesInfo := goverseapi.GetGatesInfo()
for addr, info := range gatesInfo {
    fmt.Printf("Gate %s: Configured=%v, InCluster=%v\n",
        addr, info.Configured, info.FoundInClusterState)
}
```

## Behavior Modes

### CLI Mode (No Config File)

When starting nodes and gates with CLI flags only:
- All nodes/gates have `Configured = false`
- Only active (etcd-registered) nodes/gates appear
- Example: `go run main.go --listen :48000 --etcd localhost:2379`

### Config File Mode

When using a YAML config file:
- Configured nodes/gates have `Configured = true`
- `FoundInClusterState` indicates if they're actually running
- Both configured and dynamically discovered nodes/gates appear
- Example: `go run main.go --config config.yaml --node-id node1`

## Use Cases

1. **Cluster Monitoring**: Track which configured nodes/gates are active
2. **Health Checks**: Detect when configured components are down
3. **Debugging**: Understand cluster topology and configuration vs reality
4. **Alerting**: Trigger alerts when expected nodes/gates are missing

## Running the Example

1. Start etcd:
   ```bash
   docker run -d -p 2379:2379 quay.io/coreos/etcd:v3.5.0 \
     /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \
     --advertise-client-urls http://localhost:2379
   ```

2. Run the example:
   ```bash
   cd examples/cluster_info
   go run main.go --listen :48000 --advertise localhost:48000 --etcd localhost:2379
   ```

The output will show information about the current node and any other nodes or gates discovered in the cluster.
