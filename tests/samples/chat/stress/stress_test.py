#!/usr/bin/env python3
"""
Stress test for Goverse chat client and server.

This script:
1. Starts the inspector
2. Starts 3 chat servers (nodes)
3. Starts 2 gateways
4. Runs configurable number of chat clients that randomly:
   - Join a chatroom (if not in one)
   - Leave a chatroom (if in one)
   - Send messages to the current chatroom
5. Runs for a configurable duration (default: 2 hours)
6. Each client outputs received messages in real-time
7. Cleans up processes on exit

Usage:
    python3 stress_test.py                    # Run with defaults (10 clients, 2 hours)
    python3 stress_test.py --clients 20       # Run with 20 clients
    python3 stress_test.py --duration 60      # Run for 60 seconds
    python3 stress_test.py --clients 5 --duration 300  # 5 clients, 5 minutes
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
    # Fallback to relative path if go.mod not found (tests/samples/chat/stress/ -> repo root)
    return Path(__file__).parent.parent.parent.parent.resolve()

REPO_ROOT = find_repo_root()
sys.path.insert(0, str(REPO_ROOT))

# Expose the chat directory on sys.path for helper modules
CHAT_DIR = Path(__file__).parent.parent.resolve()
sys.path.insert(0, str(CHAT_DIR))

# Expose the samples directory for shared modules
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from ChatServer import ChatServer
from Inspector import Inspector
from ChatClient import ChatClient
from Gateway import Gateway

# Constants for test configuration
ACTION_WEIGHT_SEND = 0.6      # 60% probability to send message
ACTION_WEIGHT_LEAVE = 0.1     # 10% probability to leave room
ACTION_WEIGHT_STAY = 0.3      # 30% probability to stay/do nothing
MIN_ACTION_DELAY = 0.5        # Minimum seconds between actions
MAX_ACTION_DELAY = 5.0        # Maximum seconds between actions
CLUSTER_STABILIZATION_WAIT = 15  # Seconds to wait for cluster and chatroom creation


class StressTestClient:
    """A chat client that performs random actions for stress testing."""
    
    def __init__(self, client_id: int, gateway_port: int, chatrooms: List[str]):
        """Initialize a stress test client.
        
        Args:
            client_id: Unique identifier for this client
            gateway_port: Port of the gateway to connect to
            chatrooms: List of available chatroom names
        """
        self.client_id = client_id
        self.gateway_port = gateway_port
        self.chatrooms = chatrooms
        self.client = ChatClient()
        self.username = f"stress_user_{client_id}"
        self.running = False
        self.thread = None
        self.current_room = None
        self.message_count = 0
        self.action_count = 0
    
    def start(self) -> bool:
        """Start the stress test client in a background thread.
        
        Returns:
            True if started successfully, False otherwise
        """
        try:
            # Start the interactive client
            if not self.client.start_interactive(self.gateway_port, self.username):
                print(f"❌ Client {self.client_id} failed to connect")
                return False
            
            print(f"✅ Client {self.client_id} ({self.username}) connected to gateway port {self.gateway_port}")
            
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
                    # Choose a random action based on current state
                    if self.current_room is None:
                        # Not in a room - join one
                        self._join_random_room()
                    else:
                        # In a room - randomly choose to leave, send message, or stay
                        action = random.choices(
                            ['send', 'leave', 'stay'],
                            weights=[ACTION_WEIGHT_SEND, ACTION_WEIGHT_LEAVE, ACTION_WEIGHT_STAY],
                            k=1
                        )[0]
                        
                        if action == 'send':
                            self._send_random_message()
                        elif action == 'leave':
                            self._leave_room()
                    
                    self.action_count += 1
                    
                    # Random delay between actions
                    time.sleep(random.uniform(MIN_ACTION_DELAY, MAX_ACTION_DELAY))
                    
                except Exception as e:
                    if self.running:
                        print(f"⚠️  Client {self.client_id} error during action: {e}")
                    # Continue running despite errors
                    time.sleep(1)
                    
        except Exception as e:
            if self.running:
                print(f"❌ Client {self.client_id} fatal error: {e}")
                traceback.print_exc()
    
    def _join_random_room(self):
        """Join a random chatroom."""
        try:
            room = random.choice(self.chatrooms)
            self.client.join_chatroom(room)
            self.current_room = room
            print(f"[Client {self.client_id}] Joined chatroom: {room}")
        except Exception as e:
            print(f"⚠️  Client {self.client_id} failed to join room: {e}")
    
    def _leave_room(self):
        """Leave the current chatroom.
        
        Note: This simulates leaving by clearing the local room state.
        A full implementation would call a server-side Leave method if available.
        For stress testing purposes, this is sufficient to trigger re-joining behavior.
        """
        try:
            room = self.current_room
            self.current_room = None
            print(f"[Client {self.client_id}] Left chatroom: {room}")
        except Exception as e:
            print(f"⚠️  Client {self.client_id} failed to leave room: {e}")
    
    def _send_random_message(self):
        """Send a random message to the current room."""
        try:
            messages = [
                f"Stress test message #{self.message_count}",
                f"Hello from {self.username}!",
                f"Testing... {random.randint(1, 1000)}",
                "How is everyone?",
                "This is a stress test",
                f"Message count: {self.message_count}",
            ]
            
            message = random.choice(messages)
            self.client.send_message(message)
            self.message_count += 1
            print(f"[Client {self.client_id}] Sent: {message}")
            
        except Exception as e:
            print(f"⚠️  Client {self.client_id} failed to send message: {e}")
    
    def stop(self):
        """Stop the stress test client."""
        self.running = False
        if self.thread and self.thread.is_alive():
            self.thread.join(timeout=2)
        self.client.stop()
        print(f"[Client {self.client_id}] Stopped. Actions: {self.action_count}, Messages sent: {self.message_count}")
    
    def get_stats(self):
        """Get statistics for this client.
        
        Returns:
            Dictionary with client statistics
        """
        return {
            'client_id': self.client_id,
            'username': self.username,
            'current_room': self.current_room,
            'message_count': self.message_count,
            'action_count': self.action_count,
        }


def print_stats(clients: List[StressTestClient]):
    """Print statistics for all clients."""
    print("\n" + "=" * 80)
    print("CLIENT STATISTICS")
    print("=" * 80)
    
    total_actions = 0
    total_messages = 0
    
    for client in clients:
        stats = client.get_stats()
        print(f"Client {stats['client_id']:2d} ({stats['username']:20s}): "
              f"Room: {stats['current_room'] or 'None':15s} | "
              f"Actions: {stats['action_count']:5d} | "
              f"Messages: {stats['message_count']:5d}")
        total_actions += stats['action_count']
        total_messages += stats['message_count']
    
    print("-" * 80)
    print(f"TOTAL: Actions: {total_actions}, Messages: {total_messages}")
    print("=" * 80 + "\n")


def main():
    """Main stress test execution."""
    # Parse command line arguments
    parser = argparse.ArgumentParser(
        description='Stress test for Goverse chat system',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s                           # Run with defaults (10 clients, 2 hours)
  %(prog)s --clients 20              # Run with 20 clients
  %(prog)s --duration 60             # Run for 60 seconds
  %(prog)s --clients 5 --duration 300  # 5 clients, 5 minutes
        """
    )
    parser.add_argument('--clients', type=int, default=10,
                       help='Number of concurrent clients (default: 10)')
    parser.add_argument('--duration', type=int, default=7200,
                       help='Test duration in seconds (default: 7200 = 2 hours)')
    parser.add_argument('--stats-interval', type=int, default=300,
                       help='Interval for printing statistics in seconds (default: 300 = 5 minutes)')
    args = parser.parse_args()
    
    num_clients = args.clients
    duration_seconds = args.duration
    stats_interval = args.stats_interval
    
    # Use the already-computed REPO_ROOT
    os.chdir(REPO_ROOT)
    print(f"Working directory: {os.getcwd()}")
    
    print("=" * 80)
    print(f"Goverse Chat Stress Test")
    print("=" * 80)
    print(f"Clients: {num_clients}")
    print(f"Duration: {duration_seconds} seconds ({duration_seconds / 60:.1f} minutes)")
    print(f"Stats interval: {stats_interval} seconds ({stats_interval / 60:.1f} minutes)")
    print("=" * 80)
    print()
    
    # Track all started processes and clients for cleanup
    inspector = None
    chat_servers = []
    gateways = []
    clients = []
    
    # List of chatrooms for clients to join (must match server's chatRooms list)
    chatrooms = ['General', 'Technology', 'Crypto', 'Sports', 'Movies']
    
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
        config_file = Path(__file__).parent / 'stress_config.yml'
        
        # Start inspector with config file
        # Force rebuild by removing existing binaries
        # This ensures stress tests always run with latest code
        # These paths match the defaults in Inspector, ChatServer, and Gateway classes
        print("\n" + "=" * 80)
        print("CLEANING OLD BINARIES")
        print("=" * 80)
        binaries_to_remove = ['/tmp/inspector', '/tmp/chat_server', '/tmp/gateway']
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
        print(f"Using config file: {
}")
        inspector = Inspector(config_file=str(config_file))
        inspector.start()
        
        if not inspector.wait_for_ready(timeout=30):
            print("❌ Inspector failed to start")
            return 1
        
        # Start 3 chat servers
        print("\n" + "=" * 80)
        print("STARTING CHAT SERVERS")
        print("=" * 80)
        node_ids = ["stress-node-1", "stress-node-2", "stress-node-3"]
        for i in range(3):
            server = ChatServer(
                server_index=i,
                config_file=str(config_file),
                node_id=node_ids[i]
            )
            server.start()
            chat_servers.append(server)
        
        # Give servers time to start
        print(f"\nWaiting for all chat servers to start...")
        time.sleep(8)
        
        # Verify all chat servers are ready
        for server in chat_servers:
            if not server.wait_for_ready(timeout=20):
                print(f"❌ {server.name} failed to start")
                return 1
        
        # Start 2 gateways
        print("\n" + "=" * 80)
        print("STARTING GATEWAYS")
        print("=" * 80)
        gate_ids = ["stress-gate-1", "stress-gate-2"]
        for i in range(2):
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
        
        # Wait for cluster to stabilize and chat rooms to be created
        # The chat server creates ChatRoomMgr0 after 5 seconds of cluster ready,
        # then ChatRoomMgr creates all 5 chatrooms immediately
        print("\nWaiting for cluster to stabilize and chat rooms to be created...")
        print(f"(This takes ~{CLUSTER_STABILIZATION_WAIT} seconds: 5s for cluster ready + 5s for ChatRoomMgr + 5s buffer)")
        time.sleep(CLUSTER_STABILIZATION_WAIT)
        
        # Start clients
        print("\n" + "=" * 80)
        print(f"STARTING {num_clients} CLIENTS")
        print("=" * 80)
        
        for i in range(num_clients):
            # Distribute clients across both gateways
            gateway_port = gateways[i % len(gateways)].listen_port
            
            client = StressTestClient(i + 1, gateway_port, chatrooms)
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
        
        # Stop chat servers
        print("Stopping chat servers...")
        for server in reversed(chat_servers):
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
