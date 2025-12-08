# Demo Server Stress Test

This directory contains a stress testing script for the Goverse demo server.

## Overview

The `stress_test_demo.py` script performs comprehensive stress testing of the demo server by:

1. Starting the cluster infrastructure (inspector, nodes, gates)
2. Running multiple concurrent clients
3. Performing random operations on SimpleCounter objects
4. Tracking and reporting statistics
5. Handling graceful cleanup on exit

## Prerequisites

1. **etcd** - Must be running on localhost:2379
   ```bash
   # Option 1: Use Docker
   docker run -d --name etcd-test -p 2379:2379 quay.io/coreos/etcd:latest \
     /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \
     --advertise-client-urls http://localhost:2379
   
   # Option 2: Install and run locally
   brew install etcd  # macOS
   # or
   sudo apt-get install etcd  # Linux
   
   etcd
   ```

2. **Python 3.8+** with required packages:
   ```bash
   pip install grpcio protobuf pyyaml
   ```

3. **Go 1.21+** for building the demo server and infrastructure

4. **Compiled proto files** - Run from the repository root:
   ```bash
   ./script/compile-proto.sh
   ```

## Usage

### Basic Usage

Run with default settings (10 clients, 2 hours):
```bash
python3 stress_test_demo.py
```

### Custom Configuration

Specify number of clients:
```bash
python3 stress_test_demo.py --clients 20
```

Specify test duration (in seconds):
```bash
python3 stress_test_demo.py --duration 60
```

Specify statistics interval (in seconds):
```bash
python3 stress_test_demo.py --stats-interval 30
```

Combined example (5 clients, 5 minutes, stats every 30 seconds):
```bash
python3 stress_test_demo.py --clients 5 --duration 300 --stats-interval 30
```

## Configuration File

The stress test uses `stress_config_demo.yml` which defines:

- **Inspector**: Port 8091 (gRPC), Port 8090 (HTTP)
- **10 Nodes**: Ports 9211-9220 (gRPC), 8211-8220 (HTTP)
- **7 Gates**: Ports 10111-10117 (gRPC)
- **64 Shards**: Reduced from production (8192) for faster testing
- **Auto-load Objects**:
  - `GlobalMonitor`: Single global instance
  - `ShardMonitor`: One per shard (64 total)
  - `NodeMonitor`: One per node (10 total)

## Client Operations

Each stress test client performs random operations:

- **Create Counter** (20% probability): Track a new SimpleCounter object ID
- **Increment Counter** (50% probability): Call the `Increment` method on a counter
- **Get Counter Value** (30% probability): Call the `GetValue` method on a counter

Clients operate independently with random delays between actions (0.5-3.0 seconds).

## Statistics

The script prints statistics at regular intervals showing:

- **Per Client**:
  - Total actions performed
  - Error count
  - Number of creates, increments, and gets
  
- **Aggregate**:
  - Total actions across all clients
  - Total errors
  - Total operations by type

Example output:
```
================================================================================
CLIENT STATISTICS
================================================================================
Client  1: Actions:   123 | Errors:    0 | Creates:   25 | Increments:   61 | Gets:   37
Client  2: Actions:   118 | Errors:    1 | Creates:   23 | Increments:   59 | Gets:   36
...
--------------------------------------------------------------------------------
TOTAL: Actions: 1234, Errors: 5, Creates: 246, Increments: 617, Gets: 371
================================================================================
```

## File Structure

```
examples/demo_server/
├── main.go                    # Demo server implementation
├── demo-cluster.yml           # Production cluster config
├── stress_config_demo.yml     # Stress test config
├── stress_test_demo.py        # Stress test script
├── DemoServer.py              # Demo server process helper
├── STRESS_TEST_README.md      # This file
├── run-cluster.sh             # Production cluster startup
└── stop-cluster.sh            # Production cluster shutdown
```

## Cleanup

The script automatically cleans up all processes on exit (normal or Ctrl+C):

1. All client connections are closed
2. Gateway processes are terminated
3. Demo server processes are stopped
4. Inspector process is stopped

Manual cleanup (if needed):
```bash
# Kill any remaining processes
pkill -f demo_server
pkill -f goverse-gate
pkill -f inspector

# Clean etcd data (optional - be careful!)
# This removes all cluster state
docker exec etcd-test etcdctl del --prefix /goverse/demo-stress-test
```

## Troubleshooting

### etcd Connection Issues

**Problem**: Nodes can't connect to etcd
```
Error: failed to connect to etcd
```

**Solution**: Ensure etcd is running
```bash
nc -z 127.0.0.1 2379  # Should succeed
docker logs etcd-test  # Check logs if using Docker
```

### Port Conflicts

**Problem**: Address already in use
```
Error: listen tcp 0.0.0.0:9211: bind: address already in use
```

**Solution**: 
- Stop any existing demo server processes: `pkill -f demo_server`
- Or edit `stress_config_demo.yml` to use different ports

### Build Failures

**Problem**: Binary fails to build
```
Error: failed to build demo server
```

**Solution**: Ensure proto files are compiled
```bash
cd /path/to/goverse
./script/compile-proto.sh
go build ./...
```

### Import Errors

**Problem**: Python import errors for protobuf
```
ImportError: Failed to import gate_pb2
```

**Solution**: 
1. Run `./script/compile-proto.sh` from repo root
2. Install Python dependencies: `pip install grpcio protobuf pyyaml`

### Client Connection Failures

**Problem**: Clients fail to connect to gateway
```
Error: Failed to connect to any gate server
```

**Solution**:
- Wait longer for cluster stabilization (increase `CLUSTER_STABILIZATION_WAIT`)
- Check that gateways are running: `ps aux | grep gateway`
- Verify gates are listening: `netstat -an | grep 10111`

## Comparison with Chat Stress Test

This stress test is modeled after `tests/samples/chat/stress/stress_test.py` with these adaptations:

| Aspect | Chat Stress Test | Demo Server Stress Test |
|--------|------------------|------------------------|
| **Objects** | ChatRoom, User | SimpleCounter |
| **Operations** | Join, Leave, SendMessage | Create, Increment, GetValue |
| **State** | Current room | Known counter IDs |
| **Auto-load** | ChatRoomMgr | GlobalMonitor, ShardMonitor, NodeMonitor |
| **Shards** | 64 | 64 |
| **Nodes** | 3 | 10 |
| **Gates** | 2 | 7 |

## Advanced Usage

### With Code Coverage

Enable code coverage for the Go binaries:
```bash
export ENABLE_COVERAGE=true
export GOCOVERDIR=/tmp/goverse-coverage
mkdir -p $GOCOVERDIR
python3 stress_test_demo.py --duration 60
```

After the test, merge and view coverage:
```bash
go tool covdata textfmt -i=$GOCOVERDIR -o coverage.out
go tool cover -html=coverage.out
```

### With Race Detection

Enable the race detector:
```bash
export ENABLE_RACE=true
python3 stress_test_demo.py --duration 60
```

### Long-Running Stress Test

Run overnight:
```bash
# 8 hours = 28800 seconds
nohup python3 stress_test_demo.py --clients 50 --duration 28800 --stats-interval 600 > stress.log 2>&1 &
```

Monitor progress:
```bash
tail -f stress.log
```

## Contributing

When modifying the stress test:

1. Keep the script structure similar to `tests/samples/chat/stress/stress_test.py`
2. Add docstrings to new functions and classes
3. Update this README with any new features or requirements
4. Test with different client counts and durations
5. Verify cleanup works correctly (Ctrl+C and normal exit)

## See Also

- [Demo Server README](README.md) - Main demo server documentation
- [Chat Stress Test](../../tests/samples/chat/stress/stress_test.py) - Reference implementation
- [Goverse Python Client](../../client/goverseclient_python/README.md) - Client library documentation
