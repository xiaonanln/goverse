#!/usr/bin/env python3
"""
Test script for Goverse chat client and server.

This script:
1. Starts the inspector
2. Starts one or more chat servers (supports up to 4 servers)
3. Runs the chat client with test input
4. Verifies the chat functionality
5. Cleans up processes

This script can be run locally or in CI/CD pipelines.
"""

import os
import sys
import subprocess
import time
import socket
import signal
import tempfile
import argparse
import random
import json
from pathlib import Path

# Add the repo root to the path for proto imports
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
# Expose the integration directory on sys.path so helper modules in this folder
# (e.g., ChatServer.py, Inspector.py, ChatClient.py) can be imported directly.
INTEGRATION_DIR = REPO_ROOT / 'tests' / 'integration'
sys.path.insert(0, str(INTEGRATION_DIR))

from ChatServer import ChatServer
from Inspector import Inspector
from ChatClient import ChatClient

def run_push_messaging_test(num_servers=1):
    """Test push-based messaging between two chat clients."""
    print("\nTesting push-based messaging...")
    
    # Select a random server from available servers
    server_ports = [48000 + i for i in range(num_servers)]
    selected_port = random.choice(server_ports)
    selected_server_idx = server_ports.index(selected_port)
    
    print(f"Available servers: {len(server_ports)}")
    if num_servers > 1:
        print(f"Randomly selected server: localhost:{selected_port} (server {selected_server_idx + 1})")
    else:
        print(f"Using server: localhost:{selected_port}")
    
    # Create two chat clients
    client1 = ChatClient()
    client2 = ChatClient()
    
    try:
        # Start both clients in background
        print("Starting client1 (sender)...")
        if not client1.start_interactive(selected_port, 'user1'):
            print("❌ Failed to start client1")
            return False
        
        print("Starting client2 (receiver)...")
        if not client2.start_interactive(selected_port, 'user2'):
            print("❌ Failed to start client2")
            return False
        
        # Give clients time to connect
        time.sleep(2)
        
        # Have both clients join the same chatroom
        print("Both clients joining 'General' chatroom...")
        client1.join_chatroom('General')
        client2.join_chatroom('General')
        time.sleep(1)
        
        # Client 1 sends a message
        test_message = "Hello from user1 via push!"
        print(f"Client1 sending message: '{test_message}'")
        client1.send_message(test_message)
        
        # Wait for push message to be delivered
        time.sleep(2)
        
        # Get output from client2 (receiver) - should have the message via push
        client2_output = client2.get_output()
        
        print("\nClient2 output:")
        print(client2_output)
        
        # Verify that client2 received the message via push (not by polling /messages)
        # The message should appear in the output before any /messages command
        success = test_message in client2_output
        
        if success:
            print(f"✅ Client2 received push message: '{test_message}'")
        else:
            print(f"❌ Client2 did not receive push message: '{test_message}'")
        
        # Also send another message to verify push continues to work
        test_message2 = "Another message from user1!"
        print(f"\nClient1 sending second message: '{test_message2}'")
        client1.send_message(test_message2)
        time.sleep(2)
        
        # Get updated output (incremental read)
        client2_output_updated = client2.get_output()
        success2 = test_message2 in client2_output_updated
        
        if success2:
            print(f"✅ Client2 received second push message: '{test_message2}'")
        else:
            print(f"❌ Client2 did not receive second push message: '{test_message2}'")
        
        return success and success2
        
    finally:
        # Clean up clients
        print("\nStopping clients...")
        client1.stop()
        client2.stop()

def run_chat_test(num_servers=1):
    """Run the chat client with test input and verify output."""
    print("\nTesting chat client and server interaction...")
    
    # Select a random server from available servers
    server_ports = [48000 + i for i in range(num_servers)]
    selected_port = random.choice(server_ports)
    selected_server_idx = server_ports.index(selected_port)
    
    print(f"Available servers: {len(server_ports)}")
    if num_servers > 1:
        print(f"Randomly selected server: localhost:{selected_port} (server {selected_server_idx + 1})")
    else:
        print(f"Using server: localhost:{selected_port}")
    
    # Create ChatClient instance
    chat_client = ChatClient()
    
    # Define test messages - adjust if clustered
    if num_servers > 1:
        messages = [
            'Hello from clustered test!',
            'This is message 2 from random server',
            'Final test message'
        ]
    else:
        messages = [
            'Hello from test!',
            'This is message 2',
            'Final test message'
        ]
    
    # Run the test and get output
    output = chat_client.run_test(server_port=selected_port, messages=messages)
    
    # Define test expectations (adjust messages based on number of servers)
    msg1, msg2 = messages[0], messages[1]
    
    expected_patterns = [
        ("Available Chatrooms:", "/list command executed successfully"),
        ("General", "Chatroom 'General' listed"),
        ("Technology", "Chatroom 'Technology' listed"),
        ("Joined chatroom General", "Joined chatroom 'General'", lambda s: "Joined chatroom General" in s or "[General]" in s),
        (msg1, f"Message '{msg1}' sent and received"),
        (msg2, f"Message '{msg2}' sent and received"),
        ("Final test message", "Message 'Final test message' sent and received"),
        ("Recent messages in", "/messages command executed successfully"),
    ]
    
    # Verify the output
    return chat_client.verify_output(output, expected_patterns)


def main():
    """Main test execution."""
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='Test Goverse chat client and server')
    parser.add_argument('--num-servers', type=int, default=1, choices=[1, 2, 3, 4],
                       help='Number of chat servers to start (1-4, default: 1)')
    args = parser.parse_args()
    
    num_servers = args.num_servers
    
    # Get the repository root directory (from tests/integration/test_chat.py -> repo root)
    repo_root = Path(__file__).parent.parent.parent.resolve()
    os.chdir(repo_root)
    print(f"Working directory: {os.getcwd()}")
    
    print("=" * 60)
    print(f"Goverse Chat Client/Server Test ({num_servers} server{'s' if num_servers > 1 else ''})")
    print("=" * 60)
    print()
    
    try:
        # Add go bin to PATH
        go_bin_path = subprocess.run(['go', 'env', 'GOPATH'], 
                                    capture_output=True, text=True, check=True).stdout.strip()
        os.environ['PATH'] = f"{os.environ['PATH']}:{go_bin_path}/bin"
        
        base_cov_dir = os.environ.get('GOCOVERDIR', '').strip()
        # If a base coverage dir was provided, ensure it exists and export it so
        # all child processes inherit the same GOCOVERDIR automatically.
        if base_cov_dir:
            os.makedirs(base_cov_dir, exist_ok=True)

        # Start inspector using Inspector class
        inspector = Inspector()
        inspector.start()
        
        # Wait for inspector to be ready
        if not inspector.wait_for_ready(timeout=30):
            return 1

        # Start multiple chat servers using ChatServer class
        chat_servers = []
        for i in range(num_servers):
            server = ChatServer(server_index=i)
            server.start()
            chat_servers.append(server)

        # Give all servers a moment to start
        wait_time = 5 if num_servers == 1 else 8
        print(f"\nWaiting {wait_time} seconds for all chat servers to start...")
        time.sleep(wait_time)

        # Verify all chat servers are ready
        for server in chat_servers:
            if not server.wait_for_ready(timeout=20):
                return 1

        # Call Status RPC for each chat server
        print("\n" + "=" * 60)
        print("Calling Status RPC for each chat server:")
        print("=" * 60)
        for server in chat_servers:
            print(f"\n{server.name} (localhost:{server.listen_port}) Status:")
            status_response = server.Status()
            print(status_response)

        # Call ListObjects RPC for each chat server
        print("\n" + "=" * 60)
        print("Listing Objects on each chat server:")
        print("=" * 60)
        for server in chat_servers:
            print(f"\n{server.name} (localhost:{server.listen_port}) Objects:")
            objects_response = server.ListObjects()
            print(objects_response)

        # Run push messaging test
        push_ok = run_push_messaging_test(num_servers)

        # Run chat test
        chat_ok = run_chat_test(num_servers)

        # Stop chat servers (gracefully) and check exit codes
        print("\nStopping chat servers...")
        servers_ok = True
        for server in reversed(chat_servers):
            code = server.close()
            print(f"{server.name} exited with code {code}")
            if code != 0:
                servers_ok = False

        # Stop inspector and check exit code
        print("\nStopping inspector...")
        inspector_ok = True
        code = inspector.close()
        print(f"Inspector exited with code {code}")
        if code != 0:
            inspector_ok = False

        if not push_ok:
            print("\n❌ Push messaging test failed!")
            return 1
        if not chat_ok:
            print("\n❌ Chat test failed!")
            return 1
        if not servers_ok:
            print("\n❌ One or more chat servers exited with non-zero status")
            return 1
        if not inspector_ok:
            print("\n❌ Inspector exited with non-zero status")
            return 1

        print("\n" + "=" * 60)
        print(f"✅ All chat client/server tests passed ({num_servers} server{'s' if num_servers > 1 else ''})!")
        print("=" * 60)
        return 0
        
    except KeyboardInterrupt:
        print("\n\nTest interrupted by user")
        return 1
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1


if __name__ == '__main__':
    sys.exit(main())
