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
# (e.g., ChatServer.py, Inspector.py) can be imported directly.
INTEGRATION_DIR = REPO_ROOT / 'tests' / 'integration'
sys.path.insert(0, str(INTEGRATION_DIR))

from ChatServer import ChatServer
from Inspector import Inspector

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

# Import the proto modules
try:
    from proto import goverse_pb2
    from proto import goverse_pb2_grpc
except ImportError as import_error:
    print(f"❌ Failed to import proto modules: {import_error}")
    print(f"Proto directory contents: {list((REPO_ROOT / 'proto').glob('*.py'))}")
    print("Make sure the proto files were generated correctly.")
    print("Run: ./script/compile-proto.sh")
    sys.exit(1)


class ProcessManager:
    """Manages background processes with cleanup."""
    
    def __init__(self):
        self.processes = []
    
    def start(self, cmd, name, shell=False, env: dict | None = None):
        """Start a process in the background.
        Optionally override environment with `env` (merged with os.environ).
        """
        print(f"Starting {name}...")
        popen_env = None
        if env is not None:
            popen_env = os.environ.copy()
            popen_env.update(env)
        if shell:
            process = subprocess.Popen(cmd, shell=True, stdout=subprocess.DEVNULL, 
                                     stderr=subprocess.DEVNULL, env=popen_env)
        else:
            process = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, 
                                     stderr=subprocess.DEVNULL, env=popen_env)
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
                    # Try SIGINT first to allow graceful shutdown and coverage flush
                    try:
                        process.send_signal(signal.SIGINT)
                        process.wait(timeout=10)
                        continue
                    except Exception:
                        pass
                    except subprocess.TimeoutExpired:
                        pass

                    # Then try SIGTERM
                    try:
                        process.terminate()
                        process.wait(timeout=5)
                        continue
                    except subprocess.TimeoutExpired:
                        pass

                    # Finally force kill
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


def graceful_stop(process: subprocess.Popen, name: str, timeout_int: float = 10.0, timeout_term: float = 5.0) -> int:
    """Attempt to stop a process gracefully so Go can flush coverage.
    Returns the final process returncode (or -1 if unknown).
    """
    if process is None:
        return -1
    if process.poll() is not None:
        return process.returncode if process.returncode is not None else -1

    print(f"Gracefully stopping {name} (PID: {process.pid})...")
    # 1) Try SIGINT first
    try:
        process.send_signal(signal.SIGINT)
        process.wait(timeout=timeout_int)
        return process.returncode if process.returncode is not None else -1
    except subprocess.TimeoutExpired:
        pass
    except Exception:
        pass

    # 2) Then SIGTERM
    try:
        process.terminate()
        process.wait(timeout=timeout_term)
        return process.returncode if process.returncode is not None else -1
    except subprocess.TimeoutExpired:
        pass
    except Exception:
        pass

    # 3) Finally SIGKILL
    try:
        process.kill()
        process.wait()
    except Exception:
        pass
    return process.returncode if process.returncode is not None else -1


 


def build_binary(source_path, output_path, name):
    """Build a Go binary."""
    print(f"Building {name}...")
    try:
        # Check if coverage is enabled via environment variable
        enable_coverage = os.environ.get('ENABLE_COVERAGE', '').lower() in ('true', '1', 'yes')
        
        cmd = ['go', 'build']
        if enable_coverage:
            cmd.append('-cover')
            print(f"  Coverage instrumentation enabled for {name}")
        
        cmd.extend(['-o', output_path, source_path])
        
        subprocess.run(cmd, check=True, capture_output=True, text=True, cwd=str(REPO_ROOT))
        print(f"✅ {name} built successfully")
        return True
    except subprocess.CalledProcessError as e:
        print(f"❌ Failed to build {name}")
        print(f"Error: {e.stderr}")
        return False


def run_chat_test(client_path, num_servers=1, base_cov_dir: str | None = None):
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
                # Child processes inherit GOCOVERDIR from the environment if set;
                # no per-process env override is necessary.
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
    
    # Get the repository root directory (from tests/integration/test_chat.py -> repo root)
    repo_root = Path(__file__).parent.parent.parent.resolve()
    os.chdir(repo_root)
    print(f"Working directory: {os.getcwd()}")
    
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
        
        # Determine base coverage dir (all binaries will write under this directory)
        # The Go runtime coverage tooling will create per-process subdirectories
        # automatically when they share the same GOCOVERDIR root.
        base_cov_dir = os.environ.get('GOCOVERDIR', '').strip()

        # If a base coverage dir was provided, ensure it exists and export it so
        # all child processes inherit the same GOCOVERDIR automatically.
        if base_cov_dir:
            os.makedirs(base_cov_dir, exist_ok=True)
            os.environ['GOCOVERDIR'] = base_cov_dir

        # Start inspector using Inspector class
        inspector = Inspector(base_cov_dir=base_cov_dir)
        inspector.start()
        
        # Wait for inspector to be ready
        if not inspector.wait_for_ready(timeout=30):
            return 1
        
        # Build chat server binary (will be built once and shared)
        chat_server_path = '/tmp/chat_server'
        if not build_binary('./samples/chat/server/', chat_server_path, "chat server"):
            return 1

        # Start multiple chat servers using ChatServer class
        chat_servers = []
        for i in range(num_servers):
            server = ChatServer(
                server_index=i,
                binary_path=chat_server_path,
                build_if_needed=False,  # Already built
                base_cov_dir=base_cov_dir
            )
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

        # Build chat client
        chat_client_path = '/tmp/chat_client'
        if not build_binary('./samples/chat/client/', chat_client_path, "chat client"):
            return 1

        # Run chat test
        chat_ok = run_chat_test(chat_client_path, num_servers, base_cov_dir=base_cov_dir if base_cov_dir else None)

        # Stop chat servers (gracefully) and check exit codes
        print("\nStopping chat servers...")
        servers_ok = True
        for server in reversed(chat_servers):
            code = server.stop()
            print(f"{server.name} exited with code {code}")
            if code != 0:
                servers_ok = False
            server.close()  # Close gRPC connections

        # Stop inspector and check exit code
        print("\nStopping inspector...")
        inspector_ok = True
        code = inspector.stop()
        print(f"Inspector exited with code {code}")
        if code != 0:
            inspector_ok = False
        inspector.close()  # Close gRPC connections

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
    finally:
        pm.cleanup()


if __name__ == '__main__':
    sys.exit(main())
