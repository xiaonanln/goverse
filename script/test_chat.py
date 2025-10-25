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
REPO_ROOT = Path(__file__).parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))

# Ensure proto directory is a Python package
proto_init = REPO_ROOT / 'proto' / '__init__.py'
if not proto_init.exists():
    proto_init.touch()

# Try to import grpc first
try:
    import grpc
except ImportError:
    print("❌ grpcio not found. Please install it first:")
    print("    pip install grpcio grpcio-tools")
    sys.exit(1)

# Generate proto files if they don't exist
proto_file = REPO_ROOT / 'proto' / 'goverse.proto'
pb2_file = REPO_ROOT / 'proto' / 'goverse_pb2.py'
if not pb2_file.exists():
    print("⚠️  Generating Python proto files...")
    try:
        subprocess.run([
            sys.executable, '-m', 'grpc_tools.protoc',
            f'-I{REPO_ROOT}',
            f'--python_out={REPO_ROOT}',
            f'--grpc_python_out={REPO_ROOT}',
            str(proto_file)
        ], check=True, capture_output=True, text=True)
        print("✅ Proto files generated successfully")
    except subprocess.CalledProcessError as proto_error:
        print(f"❌ Failed to generate proto files: {proto_error}")
        print(f"stdout: {proto_error.stdout}")
        print(f"stderr: {proto_error.stderr}")
        sys.exit(1)

# Now import the proto modules
try:
    from proto import goverse_pb2
    from proto import goverse_pb2_grpc
except ImportError as import_error:
    print(f"❌ Failed to import proto modules: {import_error}")
    print(f"Proto directory contents: {list((REPO_ROOT / 'proto').glob('*.py'))}")
    print("Make sure the proto files were generated correctly.")
    sys.exit(1)


class ProcessManager:
    """Manages background processes with cleanup."""
    
    def __init__(self):
        self.processes = []
    
    def start(self, cmd, name, shell=False):
        """Start a process in the background."""
        print(f"Starting {name}...")
        if shell:
            process = subprocess.Popen(cmd, shell=True, stdout=subprocess.DEVNULL, 
                                     stderr=subprocess.DEVNULL)
        else:
            process = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, 
                                     stderr=subprocess.DEVNULL)
        self.processes.append((process, name))
        print(f"✅ {name} started with PID: {process.pid}")
        return process
    
    def cleanup(self):
        """Clean up all managed processes."""
        print("\nCleaning up processes...")
        for process, name in self.processes:
            if process.poll() is None:  # Process is still running
                print(f"Stopping {name} (PID: {process.pid})...")
                try:
                    process.terminate()
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                except Exception as e:
                    print(f"Error stopping {name}: {e}")


def check_port(port, timeout=30):
    """Wait for a port to be available."""
    print(f"Waiting for port {port} to be ready...")
    start_time = time.time()
    
    while time.time() - start_time < timeout:
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(1)
            result = sock.connect_ex(('localhost', port))
            sock.close()
            if result == 0:
                print(f"✅ Port {port} is ready")
                return True
        except Exception:
            pass
        
        elapsed = int(time.time() - start_time)
        if elapsed % 5 == 0 and elapsed > 0:
            print(f"  Waiting for port {port}... ({elapsed}/{timeout}s)")
        
        time.sleep(1)
    
    print(f"❌ Port {port} failed to become ready within {timeout} seconds")
    return False


def check_http_server(url, timeout=30):
    """Wait for HTTP server to respond."""
    print(f"Waiting for HTTP server at {url}...")
    start_time = time.time()
    
    while time.time() - start_time < timeout:
        try:
            # Use curl to check the HTTP server
            result = subprocess.run(['curl', '-s', '-o', '/dev/null', '-w', '%{http_code}', url],
                                  capture_output=True, text=True, timeout=2)
            if result.stdout == '200':
                print(f"✅ HTTP server is responding with status 200")
                return True
        except Exception:
            pass
        
        elapsed = int(time.time() - start_time)
        if elapsed % 5 == 0 and elapsed > 0:
            print(f"  Waiting for HTTP server... ({elapsed}/{timeout}s)")
        
        time.sleep(1)
    
    print(f"❌ HTTP server failed to respond within {timeout} seconds")
    return False


def call_status_rpc(host, port, repo_root):
    """Call the Goverse Status RPC and return the response."""
    try:
        # Create a gRPC channel
        channel = grpc.insecure_channel(f'{host}:{port}')
        stub = goverse_pb2_grpc.GoverseStub(channel)
        
        # Call the Status RPC
        response = stub.Status(goverse_pb2.Empty(), timeout=5)
        
        # Format the response as JSON
        result = {
            "advertiseAddr": response.advertise_addr,
            "numObjects": str(response.num_objects),
            "uptimeSeconds": str(response.uptime_seconds)
        }
        
        # Close the channel
        channel.close()
        
        # Return formatted JSON
        return json.dumps(result, indent=2)
        
    except grpc.RpcError as e:
        return f"Error: RPC failed - {e.code().name}: {e.details()}"
    except Exception as e:
        return f"Error: {str(e)}"


def build_binary(source_path, output_path, name):
    """Build a Go binary."""
    print(f"Building {name}...")
    try:
        subprocess.run(['go', 'build', '-o', output_path, source_path],
                      check=True, capture_output=True, text=True)
        print(f"✅ {name} built successfully")
        return True
    except subprocess.CalledProcessError as e:
        print(f"❌ Failed to build {name}")
        print(f"Error: {e.stderr}")
        return False


def run_chat_test(client_path, num_servers=1):
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
    
    # Create test input - adjust messages if clustered
    if num_servers > 1:
        test_input = """/list
/join General
Hello from clustered test!
This is message 2 from random server
Final test message
/messages
/quit
"""
    else:
        test_input = """/list
/join General
Hello from test!
This is message 2
Final test message
/messages
/quit
"""
    
    # Create temporary files
    with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as input_file:
        input_file.write(test_input)
        input_file_path = input_file.name
    
    output_file_path = tempfile.mktemp(suffix='.txt')
    
    try:
        print(f"Running chat client with test input (connecting to localhost:{selected_port})...")
        
        # Run the chat client with timeout
        with open(input_file_path, 'r') as stdin_file, \
             open(output_file_path, 'w') as stdout_file:
            try:
                subprocess.run([client_path, '-server', f'localhost:{selected_port}', '-user', 'testuser'],
                             stdin=stdin_file, stdout=stdout_file, stderr=subprocess.STDOUT,
                             timeout=30)
            except subprocess.TimeoutExpired:
                print("Chat client timed out (this is expected)")
        
        # Read and display output
        with open(output_file_path, 'r') as f:
            output = f.read()
        
        print("\nChat client output:")
        print(output)
        
        # Verify the output
        print("\nVerifying test results...")
        
        # Define test expectations (adjust messages based on number of servers)
        msg1, msg2 = ("Hello from clustered test!", "This is message 2 from random server") if num_servers > 1 else ("Hello from test!", "This is message 2")
        
        tests = [
            ("Available Chatrooms:", "/list command executed successfully"),
            ("General", "Chatroom 'General' listed"),
            ("Technology", "Chatroom 'Technology' listed"),
            ("Joined chatroom General", "Joined chatroom 'General'", lambda s: "Joined chatroom General" in s or "[General]" in s),
            (msg1, f"Message '{msg1}' sent and received"),
            (msg2, f"Message '{msg2}' sent and received"),
            ("Final test message", "Message 'Final test message' sent and received"),
            ("Recent messages in", "/messages command executed successfully"),
        ]
        
        all_passed = True
        for test_data in tests:
            if len(test_data) == 2:
                search_str, success_msg = test_data
                check_func = lambda s, search=search_str: search in s
            else:
                search_str, success_msg, check_func = test_data
            
            if check_func(output):
                print(f"✅ {success_msg}")
            else:
                print(f"❌ {success_msg} - not found")
                all_passed = False
        
        return all_passed
        
    finally:
        # Clean up temporary files
        try:
            os.unlink(input_file_path)
        except:
            pass
        try:
            os.unlink(output_file_path)
        except:
            pass


def main():
    """Main test execution."""
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='Test Goverse chat client and server')
    parser.add_argument('--num-servers', type=int, default=1, choices=[1, 2, 3, 4],
                       help='Number of chat servers to start (1-4, default: 1)')
    args = parser.parse_args()
    
    num_servers = args.num_servers
    
    # Get the repository root directory
    repo_root = Path(__file__).parent.parent.resolve()
    os.chdir(repo_root)
    
    print("=" * 60)
    print(f"Goverse Chat Client/Server Test ({num_servers} server{'s' if num_servers > 1 else ''})")
    print("=" * 60)
    print()
    
    # Create process manager for cleanup
    pm = ProcessManager()
    
    try:
        # Add go bin to PATH
        go_bin_path = subprocess.run(['go', 'env', 'GOPATH'], 
                                    capture_output=True, text=True, check=True).stdout.strip()
        os.environ['PATH'] = f"{os.environ['PATH']}:{go_bin_path}/bin"
        
        # Step 1: Start inspector
        inspector_cmd = ['go', 'run', 'cmd/inspector/main.go']
        pm.start(inspector_cmd, "Inspector")
        
        # Step 2: Wait for inspector to be ready
        if not check_http_server('http://localhost:8080/', timeout=30):
            print("❌ Inspector HTTP server failed to start")
            return 1
        
        if not check_port(8081, timeout=30):
            print("❌ Inspector gRPC server failed to start")
            return 1
        
        print("✅ Inspector is running and ready")
        
        # Step 3: Build chat server
        chat_server_path = '/tmp/chat_server'
        if not build_binary('./samples/chat/server/', chat_server_path, "chat server"):
            return 1
        
        # Step 4: Start multiple chat servers
        for i in range(num_servers):
            listen_port = 47000 + i
            client_port = 48000 + i
            server_name = f"Chat Server {i + 1}"
            
            print(f"\nStarting {server_name} (ports {listen_port}, {client_port})...")
            pm.start([
                chat_server_path,
                '-listen', f'localhost:{listen_port}',
                '-advertise', f'localhost:{listen_port}',
                '-client-listen', f'localhost:{client_port}'
            ], server_name)
        
        # Give all servers a moment to start
        wait_time = 5 if num_servers == 1 else 8
        print(f"\nWaiting {wait_time} seconds for all chat servers to start...")
        time.sleep(wait_time)
        
        # Step 5: Verify all chat servers are ready
        for i in range(num_servers):
            listen_port = 47000 + i
            client_port = 48000 + i
            
            print(f"\nVerifying Chat Server {i + 1}...")
            if not check_port(listen_port, timeout=20):
                print(f"❌ Chat server {i + 1} failed to start on port {listen_port} (ListenAddress)")
                return 1
            
            if not check_port(client_port, timeout=20):
                print(f"❌ Chat server {i + 1} failed to start on port {client_port} (ClientListenAddress)")
                return 1
            
            print(f"✅ Chat server {i + 1} is running and ready")
        
        # Step 5.5: Call Status RPC for each chat server
        print("\n" + "=" * 60)
        print("Calling Status RPC for each chat server:")
        print("=" * 60)
        for i in range(num_servers):
            listen_port = 47000 + i
            print(f"\nChat Server {i + 1} (localhost:{listen_port}) Status:")
            status_response = call_status_rpc('localhost', listen_port, repo_root)
            print(status_response)
        
        # Step 6: Build chat client
        chat_client_path = '/tmp/chat_client'
        if not build_binary('./samples/chat/client/', chat_client_path, "chat client"):
            return 1
        
        # Step 7: Run chat test
        if not run_chat_test(chat_client_path, num_servers):
            print("\n❌ Chat test failed!")
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
    finally:
        pm.cleanup()


if __name__ == '__main__':
    sys.exit(main())
