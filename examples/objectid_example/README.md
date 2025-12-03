# Object ID Creation Example

This example demonstrates the three ways to create object IDs in Goverse.

## Overview

Goverse provides three methods for creating object IDs, each with different routing behaviors:

### 1. Normal Object IDs (Hash-Based Sharding)

```go
objID := goverseapi.CreateObjectID()
```

- Creates a unique object ID using timestamp and random data
- Objects are distributed across nodes using consistent hashing
- Best for general-purpose distributed objects
- Format: Base64-encoded unique identifier (e.g., `AAZFAQkHBTCBNKeB8CDT`)

### 2. Fixed-Shard Object IDs

```go
objID := goverseapi.CreateObjectIDOnShard(5)
```

- Creates an object ID that will be placed on a specific shard
- All objects with the same shard number will be on the same node
- Useful for grouping related objects together
- Format: `shard#<shardID>/<uniqueID>` (e.g., `shard#5/AAZFAQkHBWFxJkjOKHgz`)
- Valid shard range: 0 to 8191 (production), 0 to 63 (tests)

### 3. Fixed-Node Object IDs

```go
objID := goverseapi.CreateObjectIDOnNode("localhost:7001")
```

- Creates an object ID that will be placed on a specific node
- Objects are pinned to the specified node address
- Useful for node-local resources or when you need precise placement control
- Format: `<nodeAddress>/<uniqueID>` (e.g., `localhost:7001/AAZFAQkHBYPMjtdAA2_4`)

## Running the Example

```bash
cd examples/objectid_example
go run main.go
```

## When to Use Each Type

- **Normal IDs**: Default choice for most use cases. Provides automatic load balancing and fault tolerance.
- **Fixed-Shard IDs**: When you need objects to be co-located on the same node (e.g., objects that frequently interact with each other).
- **Fixed-Node IDs**: When you need precise control over object placement (e.g., objects that need to be on the same node as a specific resource).

## Important Notes

- All three ID formats work seamlessly with `goverseapi.CreateObject()` and `goverseapi.CallObject()`
- The system automatically routes requests to the correct node based on the object ID format
- Fixed-shard and fixed-node formats do not override the normal sharding logic if the format is invalid
