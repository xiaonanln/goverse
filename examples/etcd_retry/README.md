# Etcd Keep-Alive Retry Example

This example demonstrates the automatic keep-alive retry mechanism in `etcdmanager`. It shows how the EtcdManager maintains node registration even when etcd becomes temporarily unavailable.

## Features Demonstrated

- **Automatic Lease Renewal**: The manager continuously renews the lease for registered nodes
- **Retry on Failure**: When the keep-alive channel closes (due to network issues, etcd restart, etc.), the manager automatically retries
- **Exponential Backoff**: Retry delays increase exponentially from 1s to 30s max to avoid overwhelming etcd
- **Seamless Recovery**: When etcd becomes available again, the node is automatically re-registered

## Prerequisites

You need a running etcd instance. Install etcd:

```bash
# On macOS
brew install etcd

# On Ubuntu/Debian
sudo apt-get install etcd

# Or download from https://github.com/etcd-io/etcd/releases
```

## Running the Example

1. Start etcd in a terminal:
```bash
etcd --listen-client-urls http://localhost:2379 --advertise-client-urls http://localhost:2379
```

2. In another terminal, run the example:
```bash
cd examples/etcd_retry
go run example_retry.go
```

3. You should see output like:
```
Connected to etcd
Registered node localhost:50000
Node will stay registered even if etcd becomes temporarily unavailable
Try stopping and restarting etcd to see the automatic retry in action
Press Ctrl+C to exit
Currently tracking 0 node(s)
```

## Testing the Retry Mechanism

To see the automatic retry in action:

1. While the example is running, stop etcd (Ctrl+C in the etcd terminal)
2. Observe the retry logs in the example output - you'll see warnings about failed keep-alive
3. Restart etcd
4. The example will automatically re-register the node and continue running

The logs will show something like:
```
[WARN] Keep-alive channel closed for lease 123456, will retry
[WARN] Failed to maintain lease for node localhost:50000: keep-alive channel closed, retrying in 1s
[WARN] Failed to maintain lease for node localhost:50000: failed to grant lease: context deadline exceeded, retrying in 2s
[INFO] Registered node localhost:50000 with lease ID 123457
```

## How It Works

1. **Registration**: `RegisterNode()` creates a lease and starts a background goroutine
2. **Keep-Alive Loop**: The goroutine continuously:
   - Creates a new lease (15 second TTL)
   - Registers the node with that lease
   - Monitors the keep-alive channel
   - When the channel closes or an error occurs, it retries with exponential backoff
3. **Cleanup**: `UnregisterNode()` or `Close()` stops the loop and revokes the lease

## Key Code Points

```go
// Register node - starts automatic keep-alive
mgr.RegisterNode(ctx, nodeAddress)

// The keep-alive loop runs in background and handles:
// - Lease creation
// - Node registration
// - Keep-alive monitoring
// - Automatic retry on failure

// Clean shutdown
mgr.UnregisterNode(ctx, nodeAddress)
```

## Related Files

- `cluster/etcdmanager/etcdmanager.go` - Main implementation
- `cluster/etcdmanager/keepalive_test.go` - Tests for the retry mechanism
