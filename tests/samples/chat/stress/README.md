# Chat Stress Test

This directory contains a stress test for the Goverse chat system.

## Overview

The stress test simulates realistic chat usage by running multiple concurrent clients that perform random actions:
- Join chatrooms
- Leave chatrooms  
- Send messages
- Receive and display messages in real-time

## Files

- `stress_test.py` - Main stress test script
- `stress_config.yml` - Configuration file for the test cluster (2 gates, 3 nodes, inspector)

## Requirements

- Go 1.21 or later
- Python 3.8 or later
- grpcio Python package
- protobuf Python package
- etcd running on localhost:2379 (optional, tests skip if unavailable)

## Configuration

The `stress_config.yml` file defines:
- **Inspector**: HTTP and gRPC ports for cluster monitoring
- **3 Nodes**: Chat servers hosting distributed objects
- **2 Gates**: Client-facing gRPC endpoints for load distribution
- **Cluster settings**: 64 shards for testing (vs 8192 in production)

## Usage

### Basic Usage

```bash
# Run with defaults (10 clients, 2 hours)
python3 stress_test.py

# Run with custom number of clients
python3 stress_test.py --clients 20

# Run for a specific duration (in seconds)
python3 stress_test.py --duration 300  # 5 minutes

# Combine options
python3 stress_test.py --clients 5 --duration 60  # 5 clients, 1 minute
```

### Options

- `--clients N` - Number of concurrent clients (default: 10)
- `--duration N` - Test duration in seconds (default: 7200 = 2 hours)
- `--stats-interval N` - Interval for printing statistics in seconds (default: 300 = 5 minutes)

### Examples

```bash
# Quick test with 5 clients for 1 minute
python3 stress_test.py --clients 5 --duration 60

# Medium test with 20 clients for 30 minutes
python3 stress_test.py --clients 20 --duration 1800

# Full stress test with 50 clients for 2 hours
python3 stress_test.py --clients 50 --duration 7200
```

## What the Test Does

1. **Infrastructure Setup**
   - Starts the inspector for cluster monitoring
   - Starts 3 chat server nodes
   - Starts 2 gateway servers for client connections
   - Waits for cluster to stabilize and create chatrooms (General, Technology, Crypto, Sports, Movies)

2. **Client Simulation**
   - Spawns specified number of clients
   - Distributes clients across both gateways
   - Each client runs independently in its own thread

3. **Client Behavior**
   - Randomly joins chatrooms from the 5 available rooms (if not in one)
   - Randomly leaves chatrooms (if in one)
   - Randomly sends messages (when in a room)
   - Displays received messages in real-time
   - Action delays: Random interval between 0.5-5.0 seconds
   - Action weights (configurable in code):
     - 60% probability to send message
     - 10% probability to leave room
     - 30% probability to stay/do nothing

4. **Statistics**
   - Prints client statistics at regular intervals
   - Shows actions performed and messages sent per client
   - Displays total counts across all clients

5. **Cleanup**
   - Gracefully stops all clients
   - Shuts down gateways
   - Stops chat servers
   - Stops inspector

## Output

The test provides detailed output including:
- Infrastructure startup progress
- Client connection status
- Real-time message delivery to clients
- Periodic statistics reports
- Final summary on completion

Example output:
```
[Client 1] Joined chatroom: Technology
[Client 3] Sent: Stress test message #5
[Client 2] Received: [10:23:45] stress_user_3: Stress test message #5
```

## Interrupting the Test

Press `Ctrl+C` to stop the test early. The script will:
- Print current statistics
- Gracefully clean up all processes
- Exit with proper cleanup

## Notes

- The test uses dynamic port allocation for all components
- Clients are distributed across both gateways for load balancing
- Each client operates independently in its own thread
- The test is designed to run continuously for extended periods
- All processes are cleaned up properly on exit, even if interrupted
- "Leave room" action simulates leaving by clearing local state (triggers re-join behavior for stress testing)
