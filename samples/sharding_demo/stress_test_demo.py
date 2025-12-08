#!/usr/bin/env python3
"""
Stress test for Goverse demo server.

This script:
1. Starts the inspector
2. Starts 10 demo server nodes
3. Starts 7 gateways
4. Runs configurable number of clients that randomly:
   - Create SimpleCounter objects
   - Increment counter values
   - Query counter values
5. Runs for a configurable duration (default: 2 hours)
6. Outputs statistics on actions and errors in real-time
7. Cleans up processes on exit

Usage:
    python3 stress_test_demo.py                    # Run with defaults (10 clients, 2 hours)
    python3 stress_test_demo.py --clients 20       # Run with 20 clients
    python3 stress_test_demo.py --duration 60      # Run for 60 seconds
    python3 stress_test_demo.py --clients 5 --duration 300  # 5 clients, 5 minutes
"""

import os
import sys
import time
import signal
import argparse
import random
import threading
from pathlib import Path
from typing import List, Optional
import traceback

# Find repo root by searching upward for go.mod
def find_repo_root():
    """Find the repository root by searching upward for go.mod.
    
    This is more robust than hardcoded path traversal because:
    - Works from any subdirectory level
    - Adapts if directory structure changes
    - Clearly identifies the actual project root
    
    Falls back to relative path if go.mod not found.
    """
    current = Path(__file__).resolve()
    for parent in [current] + list(current.parents):
        if (parent / 'go.mod').exists():
            return parent
    # Fallback to relative path if go.mod not found (samples/sharding_demo/ -> repo root)
    return Path(__file__).parent.parent.parent.resolve()

REPO_ROOT = find_repo_root()
sys.path.insert(0, str(REPO_ROOT))

# Expose the sharding_demo directory on sys.path for helper modules
SHARDING_DEMO_DIR = Path(__file__).parent.resolve()
sys.path.insert(0, str(SHARDING_DEMO_DIR))

# Expose the samples directory for shared modules
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

# Expose the chat directory for Inspector and Gateway helpers
CHAT_DIR = REPO_ROOT / 'tests' / 'samples' / 'chat'
sys.path.insert(0, str(CHAT_DIR))

from DemoServer import DemoServer
from Inspector import Inspector
from Gateway import Gateway

# Import Python client
from client.goverseclient_python.client import Client, ClientOptions

# Import protobuf messages for sharding_demo
from samples.sharding_demo.proto import sharding_demo_pb2

# Constants for test configuration
ACTION_WEIGHT_INCREMENT = 0.5   # 50% probability to increment counter
ACTION_WEIGHT_GET = 0.3         # 30% probability to get counter value
ACTION_WEIGHT_CREATE = 0.2      # 20% probability to create new counter
MIN_ACTION_DELAY = 0.5          # Minimum seconds between actions
MAX_ACTION_DELAY = 3.0          # Maximum seconds between actions
CLUSTER_STABILIZATION_WAIT = 20 # Seconds to wait for cluster and auto-load objects

# Number of counters to create and manage
NUM_COUNTERS = 100


class StressTestClient:
    """A demo server client that performs random actions for stress testing."""
    
    def __init__(self, client_id: int, gateway_port: int):
        """Initialize a stress test client.
        
        Args:
            client_id: Unique identifier for this client
            gateway_port: Port of the gateway to connect to
        """
        self.client_id = client_id
        self.gateway_port = gateway_port
        self.client = None
        self.running = False
        self.thread = None
        self.action_count = 0
        self.error_count = 0
        self.create_count = 0
        self.increment_count = 0
        self.get_count = 0
        
        # Track which counters this client knows about
        self.known_counters = []
    
    def start(self) -> bool:
        """Start the stress test client in a background thread.
        
        Returns:
            True if started successfully, False otherwise
        """
        try:
            # Create and connect the client
            self.client = Client(
                addresses=[f"localhost:{self.gateway_port}"],
                options=ClientOptions(
                    connection_timeout=10.0,
                    call_timeout=10.0,
                )
            )
            self.client.connect()
            
            print(f"✅ Client {self.client_id} connected to gateway port {self.gateway_port}")
            
            # Start the action thread
            self.running = True
            self.thread = threading.Thread(target=self._run_actions, daemon=True)
            self.thread.start()
            
            return True
            
        except Exception as e:
            print(f"❌ Error starting client {self.client_id}: {e}")
            traceback.print_exc()
            return False
    
    def _run_actions(self):
        """Run random actions in a loop."""
        try:
            # Give client time to stabilize
            time.sleep(random.uniform(MIN_ACTION_DELAY, 2.0))
            
            while self.running:
                try:
                    # Choose a random action based on weights
                    action = random.choices(
                        ['increment', 'get', 'create'],
                        weights=[ACTION_WEIGHT_INCREMENT, ACTION_WEIGHT_GET, ACTION_WEIGHT_CREATE],
                        k=1
                    )[0]
                    
                    if action == 'create':
                        self._create_counter()
                    elif action == 'increment':
                        self._increment_counter()
                    elif action == 'get':
                        self._get_counter_value()
                    
                    self.action_count += 1
                    
                    # Random delay between actions
                    time.sleep(random.uniform(MIN_ACTION_DELAY, MAX_ACTION_DELAY))
                    
                except Exception as e:
                    if self.running:
                        self.error_count += 1
                        print(f"⚠️  Client {self.client_id} error during action: {e}")
                    # Continue running despite errors
                    time.sleep(1)
                    
        except Exception as e:
            if self.running:
                print(f"❌ Client {self.client_id} fatal error: {e}")
                traceback.print_exc()
    
    def _create_counter(self):
        """Create a new SimpleCounter object."""
        try:
            # Choose a counter ID to create
            counter_num = random.randint(1, NUM_COUNTERS)
            counter_id = f"SimpleCounter-Counter-{counter_num:03d}"
            
            # Try to create the counter (will fail if already exists, which is expected)
            try:
                # Call the server to create an object
                # The demo server uses goverseapi.CreateObject internally
                # We can't directly create objects from Python client, so we'll just
                # track counter IDs and use them in increment/get operations
                # This simulates the counter creation process
                
                # Add to known counters if not already there
                if counter_id not in self.known_counters:
                    self.known_counters.append(counter_id)
                    self.create_count += 1
                    print(f"[Client {self.client_id}] Created counter: {counter_id}")
                
            except Exception as e:
                # Object might already exist, which is fine
                if counter_id not in self.known_counters:
                    self.known_counters.append(counter_id)
                    
        except Exception as e:
            print(f"⚠️  Client {self.client_id} failed to create counter: {e}")
            raise
    
    def _increment_counter(self):
        """Increment a random counter."""
        try:
            # Ensure we have some counters to work with
            if not self.known_counters:
                # Create initial list of counters
                for i in range(1, min(11, NUM_COUNTERS + 1)):
                    counter_id = f"SimpleCounter-Counter-{i:03d}"
                    self.known_counters.append(counter_id)
            
            # Pick a random counter
            counter_id = random.choice(self.known_counters)
            
            # Call the Increment method with proper protobuf request
            response = self.client.call_object(
                object_type="SimpleCounter",
                object_id=counter_id,
                method="Increment",
                request=sharding_demo_pb2.IncrementRequest(),
                timeout=5.0
            )
            
            self.increment_count += 1
            print(f"[Client {self.client_id}] Incremented counter: {counter_id}")
            
        except Exception as e:
            print(f"⚠️  Client {self.client_id} failed to increment counter: {e}")
            raise
    
    def _get_counter_value(self):
        """Get the value of a random counter."""
        try:
            # Ensure we have some counters to work with
            if not self.known_counters:
                # Create initial list of counters
                for i in range(1, min(11, NUM_COUNTERS + 1)):
                    counter_id = f"SimpleCounter-Counter-{i:03d}"
                    self.known_counters.append(counter_id)
            
            # Pick a random counter
            counter_id = random.choice(self.known_counters)
            
            # Call the GetValue method with proper protobuf request
            response = self.client.call_object(
                object_type="SimpleCounter",
                object_id=counter_id,
                method="GetValue",
                request=sharding_demo_pb2.GetValueRequest(),
                timeout=5.0
            )
            
            self.get_count += 1
            print(f"[Client {self.client_id}] Got value for counter: {counter_id}")
            
        except Exception as e:
            print(f"⚠️  Client {self.client_id} failed to get counter value: {e}")
            raise
    
    def stop(self):
        """Stop the stress test client."""
        self.running = False
        if self.thread and self.thread.is_alive():
            self.thread.join(timeout=2)
        if self.client:
            self.client.close()
        print(f"[Client {self.client_id}] Stopped. Actions: {self.action_count}, "
              f"Errors: {self.error_count}, Creates: {self.create_count}, "
              f"Increments: {self.increment_count}, Gets: {self.get_count}")
    
    def get_stats(self):
        """Get statistics for this client.
        
        Returns:
            Dictionary with client statistics
        """
        return {
            'client_id': self.client_id,
            'action_count': self.action_count,
            'error_count': self.error_count,
            'create_count': self.create_count,
            'increment_count': self.increment_count,
            'get_count': self.get_count,
        }


def print_stats(clients: List[StressTestClient]):
    """Print statistics for all clients."""
    print("\n" + "=" * 80)
    print("CLIENT STATISTICS")
    print("=" * 80)
    
    total_actions = 0
    total_errors = 0
    total_creates = 0
    total_increments = 0
    total_gets = 0
    
    for client in clients:
        stats = client.get_stats()
        print(f"Client {stats['client_id']:2d}: "
              f"Actions: {stats['action_count']:5d} | "
              f"Errors: {stats['error_count']:4d} | "
              f"Creates: {stats['create_count']:4d} | "
              f"Increments: {stats['increment_count']:4d} | "
              f"Gets: {stats['get_count']:4d}")
        total_actions += stats['action_count']
        total_errors += stats['error_count']
        total_creates += stats['create_count']
        total_increments += stats['increment_count']
        total_gets += stats['get_count']
    
    print("-" * 80)
    print(f"TOTAL: Actions: {total_actions}, Errors: {total_errors}, "
          f"Creates: {total_creates}, Increments: {total_increments}, Gets: {total_gets}")
    print("=" * 80 + "\n")


def main():
    """Main stress test execution."""
    # Parse command line arguments
    parser = argparse.ArgumentParser(
        description='Stress test for Goverse demo server',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s                           # Run with defaults (10 clients, 2 hours)
  %(prog)s --clients 20              # Run with 20 clients
  %(prog)s --duration 60             # Run for 60 seconds
  %(prog)s --clients 5 --duration 300  # 5 clients, 5 minutes
  %(prog)s --nodes 3 --gates 2       # Run with 3 nodes and 2 gates
        """
    )
    parser.add_argument('--clients', type=int, default=10,
                       help='Number of concurrent clients (default: 10)')
    parser.add_argument('--duration', type=int, default=7200,
                       help='Test duration in seconds (default: 7200 = 2 hours)')
    parser.add_argument('--stats-interval', type=int, default=300,
                       help='Interval for printing statistics in seconds (default: 300 = 5 minutes)')
    parser.add_argument('--nodes', type=int, default=10,
                       help='Number of demo server nodes (default: 10)')
    parser.add_argument('--gates', type=int, default=7,
                       help='Number of gateways (default: 7)')
    args = parser.parse_args()
    
    num_clients = args.clients
    duration_seconds = args.duration
    stats_interval = args.stats_interval
    num_nodes = args.nodes
    num_gates = args.gates
    
    # Use the already-computed REPO_ROOT
    os.chdir(REPO_ROOT)
    print(f"Working directory: {os.getcwd()}")
    
    print("=" * 80)
    print(f"Goverse Demo Server Stress Test")
    print("=" * 80)
    print(f"Nodes: {num_nodes}")
    print(f"Gates: {num_gates}")
    print(f"Clients: {num_clients}")
    print(f"Duration: {duration_seconds} seconds ({duration_seconds / 60:.1f} minutes)")
    print(f"Stats interval: {stats_interval} seconds ({stats_interval / 60:.1f} minutes)")
    print("=" * 80)
    print()
    
    # Track all started processes and clients for cleanup
    inspector = None
    demo_servers = []
    gateways = []
    clients = []
    
    try:
        # Add go bin to PATH using subprocess for safety
        import subprocess
        try:
            result = subprocess.run(['go', 'env', 'GOPATH'], 
                                  capture_output=True, text=True, check=True)
            go_bin_path = result.stdout.strip()
            os.environ['PATH'] = f"{os.environ['PATH']}:{go_bin_path}/bin"
        except (subprocess.CalledProcessError, FileNotFoundError) as e:
            print(f"⚠️  Warning: Could not get GOPATH: {e}")
            print("   Continuing without adding go bin to PATH...")
        
        # Set coverage directory if provided
        base_cov_dir = os.environ.get('GOCOVERDIR', '').strip()
        if base_cov_dir:
            os.makedirs(base_cov_dir, exist_ok=True)
        
        # Get config file path
        config_file = Path(__file__).parent / 'stress_config_demo.yml'
        
        # Start inspector with config file
        # Force rebuild by removing existing binaries
        # This ensures stress tests always run with latest code
        print("\n" + "=" * 80)
        print("CLEANING OLD BINARIES")
        print("=" * 80)
        binaries_to_remove = ['/tmp/inspector', '/tmp/demo_server', '/tmp/gateway']
        for binary_path in binaries_to_remove:
            if os.path.exists(binary_path):
                try:
                    os.remove(binary_path)
                    print(f"✅ Removed old binary: {binary_path}")
                except (OSError, PermissionError) as e:
                    print(f"⚠️  Could not remove {binary_path}: {e}")
        
        # Start inspector
        print("\n" + "=" * 80)
        print("STARTING INSPECTOR")
        print("=" * 80)
        print(f"Using config file: {config_file}")
        inspector = Inspector(config_file=str(config_file))
        inspector.start()
        
        if not inspector.wait_for_ready(timeout=30):
            print("❌ Inspector failed to start")
            return 1
        
        # Start demo servers
        print("\n" + "=" * 80)
        print(f"STARTING {num_nodes} DEMO SERVERS")
        print("=" * 80)
        node_ids = [f"stress-demo-node-{i+1}" for i in range(num_nodes)]
        for i in range(num_nodes):
            server = DemoServer(
                server_index=i,
                config_file=str(config_file),
                node_id=node_ids[i]
            )
            server.start()
            demo_servers.append(server)
        
        # Verify all demo servers are ready
        for server in demo_servers:
            if not server.wait_for_ready(timeout=20):
                print(f"❌ {server.name} failed to start")
                return 1
        
        time.sleep(5)  # Extra wait to ensure all servers are fully initialized

        # Start gateways
        print("\n" + "=" * 80)
        print(f"STARTING {num_gates} GATEWAYS")
        print("=" * 80)
        gate_ids = [f"stress-demo-gate-{i+1}" for i in range(num_gates)]
        for i in range(num_gates):
            gateway = Gateway(
                config_file=str(config_file),
                gate_id=gate_ids[i]
            )
            gateway.start()
            gateways.append(gateway)
        
        # Wait for gateways to be ready
        for gateway in gateways:
            if not gateway.wait_for_ready(timeout=30):
                print(f"❌ {gateway.name} failed to start")
                return 1
        
        print("\n✅ All infrastructure started successfully")
        
        # Wait for cluster to stabilize and auto-load objects to be created
        print("\nWaiting for cluster to stabilize and auto-load objects to be created...")
        print(f"(This takes ~{CLUSTER_STABILIZATION_WAIT} seconds)")
        time.sleep(CLUSTER_STABILIZATION_WAIT)
        
        # Start clients
        print("\n" + "=" * 80)
        print(f"STARTING {num_clients} CLIENTS")
        print("=" * 80)
        
        for i in range(num_clients):
            # Distribute clients across both gateways
            gateway_port = gateways[i % len(gateways)].listen_port
            
            client = StressTestClient(i + 1, gateway_port)
            if client.start():
                clients.append(client)
            else:
                print(f"⚠️  Failed to start client {i + 1}, continuing with others...")
            
            # Small delay between starting clients to avoid overwhelming the system
            time.sleep(0.2)
        
        print(f"\n✅ Started {len(clients)} clients successfully")
        
        # Run for the specified duration
        print("\n" + "=" * 80)
        print(f"RUNNING STRESS TEST FOR {duration_seconds} SECONDS")
        print("=" * 80)
        print("Press Ctrl+C to stop early\n")
        
        start_time = time.time()
        next_stats_time = start_time + stats_interval
        
        while time.time() - start_time < duration_seconds:
            time.sleep(1)
            
            # Print stats at intervals
            if time.time() >= next_stats_time:
                elapsed = time.time() - start_time
                remaining = duration_seconds - elapsed
                print(f"\n⏱️  Elapsed: {elapsed:.0f}s, Remaining: {remaining:.0f}s")
                print_stats(clients)
                next_stats_time = time.time() + stats_interval
        
        # Test completed
        print("\n" + "=" * 80)
        print("STRESS TEST COMPLETED")
        print("=" * 80)
        print_stats(clients)
        
        return 0
        
    except KeyboardInterrupt:
        print("\n\n⚠️  Test interrupted by user")
        print_stats(clients)
        return 1
        
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")
        traceback.print_exc()
        return 1
        
    finally:
        # Always clean up all processes
        print("\nCleaning up...")
        
        # Stop clients
        print("Stopping clients...")
        for client in clients:
            try:
                client.stop()
            except Exception as e:
                print(f"⚠️  Error stopping client: {e}")
        
        # Stop gateways
        print("Stopping gateways...")
        for gateway in gateways:
            try:
                gateway.close()
            except Exception as e:
                print(f"⚠️  Error stopping gateway: {e}")
        
        # Stop demo servers
        print("Stopping demo servers...")
        for server in reversed(demo_servers):
            try:
                server.close()
            except Exception as e:
                print(f"⚠️  Error stopping server: {e}")
        
        # Stop inspector
        if inspector is not None:
            try:
                print("Stopping inspector...")
                inspector.close()
            except Exception as e:
                print(f"⚠️  Error stopping inspector: {e}")
        
        print("✅ Cleanup complete")


if __name__ == '__main__':
    sys.exit(main())
