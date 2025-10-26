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


class ChatServer:
    """Manages a Goverse chat server process and gRPC connections."""
    
    def __init__(self, server_index=0, listen_port=None, client_port=None, 
                 binary_path=None, build_if_needed=True, base_cov_dir=None):
        """Initialize and optionally start a chat server.
        
        Args:
            server_index: Server index number (for multi-server setups)
            listen_port: Server listen port (defaults to 47000 + server_index)
            client_port: Client listen port (defaults to 48000 + server_index)
            binary_path: Path to chat server binary (defaults to /tmp/chat_server)
            build_if_needed: Whether to build the binary if it doesn't exist
            base_cov_dir: Base directory for coverage data (optional)
        """
        self.server_index = server_index
        self.listen_port = listen_port if listen_port is not None else 47000 + server_index
        self.client_port = client_port if client_port is not None else 48000 + server_index
        self.binary_path = binary_path if binary_path is not None else '/tmp/chat_server'
        self.base_cov_dir = base_cov_dir
        self.process = None
        self.channel = None
        self.stub = None
        self.name = f"Chat Server {server_index + 1}"
        
        # Build binary if needed
        if build_if_needed and not os.path.exists(self.binary_path):
            if not self._build_binary():
                raise RuntimeError(f"Failed to build chat server binary at {self.binary_path}")
    
    def _build_binary(self):
        """Build the chat server binary."""
        print(f"Building chat server...")
        try:
            enable_coverage = os.environ.get('ENABLE_COVERAGE', '').lower() in ('true', '1', 'yes')
            
            cmd = ['go', 'build']
            if enable_coverage:
                cmd.append('-cover')
                print(f"  Coverage instrumentation enabled")
            
            cmd.extend(['-o', self.binary_path, './samples/chat/server/'])
            
            subprocess.run(cmd, check=True, capture_output=True, text=True, cwd=str(REPO_ROOT))
            print(f"✅ Chat server built successfully")
            return True
        except subprocess.CalledProcessError as e:
            print(f"❌ Failed to build chat server")
            print(f"Error: {e.stderr}")
            return False
    
    def start(self):
        """Start the chat server process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return
        
        print(f"Starting {self.name} (ports {self.listen_port}, {self.client_port})...")
        
        # Prepare environment for coverage
        server_env = None
        if self.base_cov_dir:
            server_env = os.environ.copy()
            server_env['GOCOVERDIR'] = self.base_cov_dir
        
        # Start the process
        cmd = [
            self.binary_path,
            '-listen', f'localhost:{self.listen_port}',
            '-advertise', f'localhost:{self.listen_port}',
            '-client-listen', f'localhost:{self.client_port}'
        ]
        
        self.process = subprocess.Popen(
            cmd, 
            stdout=subprocess.DEVNULL, 
            stderr=subprocess.DEVNULL,
            env=server_env
        )
        print(f"✅ {self.name} started with PID: {self.process.pid}")
    
    def wait_for_ready(self, timeout=20):
        """Wait for the server to be ready to accept connections.
        
        Args:
            timeout: Maximum time to wait in seconds
            
        Returns:
            True if server is ready, False otherwise
        """
        print(f"Verifying {self.name}...")
        
        if not check_port(self.listen_port, timeout=timeout):
            print(f"❌ {self.name} failed to start on port {self.listen_port} (ListenAddress)")
            return False
        
        if not check_port(self.client_port, timeout=timeout):
            print(f"❌ {self.name} failed to start on port {self.client_port} (ClientListenAddress)")
            return False
        
        print(f"✅ {self.name} is running and ready")
        return True
    
    def connect(self):
        """Establish gRPC connection to the server."""
        if self.channel is None:
            self.channel = grpc.insecure_channel(f'localhost:{self.listen_port}')
            self.stub = goverse_pb2_grpc.GoverseStub(self.channel)
    
    def Status(self, timeout=5):
        """Call the Goverse Status RPC and return the response.
        
        Args:
            timeout: RPC timeout in seconds
            
        Returns:
            JSON formatted status response or error message
        """
        self.connect()
        try:
            response = self.stub.Status(goverse_pb2.Empty(), timeout=timeout)
            
            result = {
                "advertiseAddr": response.advertise_addr,
                "numObjects": str(response.num_objects),
                "uptimeSeconds": str(response.uptime_seconds)
            }
            
            return json.dumps(result, indent=2)
            
        except grpc.RpcError as e:
            return f"Error: RPC failed - {e.code().name}: {e.details()}"
        except Exception as e:
            return f"Error: {str(e)}"
    
    def ListObjects(self, timeout=5):
        """Call the Goverse ListObjects RPC and return the response.
        
        Args:
            timeout: RPC timeout in seconds
            
        Returns:
            JSON formatted list of objects or error message
        """
        self.connect()
        try:
            response = self.stub.ListObjects(goverse_pb2.Empty(), timeout=timeout)
            
            objects = []
            for obj in response.objects:
                objects.append({
                    "id": obj.id,
                    "type": obj.type
                })
            
            result = {
                "objectCount": len(objects),
                "objects": objects
            }
            return json.dumps(result, indent=2)
            
        except grpc.RpcError as e:
            return f"Error: RPC failed - {e.code().name}: {e.details()}"
        except Exception as e:
            return f"Error: {str(e)}"
    
    def CallObject(self, object_id, method, request, timeout=5):
        """Make a generic RPC call to an object method.
        
        Args:
            object_id: The ID of the target object
            method: The method name to call
            request: The request message (google.protobuf.Any)
            timeout: RPC timeout in seconds
            
        Returns:
            The response from the server
            
        Raises:
            grpc.RpcError: If the RPC call fails
        """
        self.connect()
        call_request = goverse_pb2.CallObjectRequest(
            id=object_id,
            method=method,
            request=request
        )
        return self.stub.CallObject(call_request, timeout=timeout)
    
    def stop(self):
        """Stop the chat server process gracefully."""
        if self.process is None:
            return -1
        
        if self.process.poll() is not None:
            return self.process.returncode if self.process.returncode is not None else -1
        
        print(f"Gracefully stopping {self.name} (PID: {self.process.pid})...")
        
        # Try SIGINT first
        try:
            self.process.send_signal(signal.SIGINT)
            self.process.wait(timeout=10)
            return self.process.returncode if self.process.returncode is not None else -1
        except subprocess.TimeoutExpired:
            pass
        except Exception:
            pass
        
        # Then try SIGTERM
        try:
            self.process.terminate()
            self.process.wait(timeout=5)
            return self.process.returncode if self.process.returncode is not None else -1
        except subprocess.TimeoutExpired:
            pass
        except Exception:
            pass
        
        # Finally force kill
        try:
            self.process.kill()
            self.process.wait()
        except Exception:
            pass
        
        return self.process.returncode if self.process.returncode is not None else -1
    
    def close(self):
        """Close the gRPC channel."""
        if self.channel:
            self.channel.close()
            self.channel = None
            self.stub = None
    
    def __enter__(self):
        """Support context manager protocol."""
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        """Close the channel when exiting context."""
        self.close()
        return False


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

        # Step 1: Build inspector
        inspector_path = '/tmp/inspector'
        if not build_binary('./cmd/inspector/', inspector_path, "inspector"):
            return 1
        
        # Step 2: Start inspector (it inherits GOCOVERDIR from the environment)
        inspector_proc = pm.start([inspector_path], "Inspector")
        
        # Step 3: Wait for inspector to be ready
        if not check_http_server('http://localhost:8080/', timeout=30):
            print("❌ Inspector HTTP server failed to start")
            return 1
        
        if not check_port(8081, timeout=30):
            print("❌ Inspector gRPC server failed to start")
            return 1
        
        print("✅ Inspector is running and ready")
        
        # Step 4: Build chat server binary (will be built once and shared)
        chat_server_path = '/tmp/chat_server'
        if not build_binary('./samples/chat/server/', chat_server_path, "chat server"):
            return 1

        # Step 5: Start multiple chat servers using ChatServer class
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

        # Step 6: Verify all chat servers are ready
        for server in chat_servers:
            if not server.wait_for_ready(timeout=20):
                return 1

        # Step 6.5: Call Status RPC for each chat server
        print("\n" + "=" * 60)
        print("Calling Status RPC for each chat server:")
        print("=" * 60)
        for server in chat_servers:
            print(f"\n{server.name} (localhost:{server.listen_port}) Status:")
            status_response = server.Status()
            print(status_response)

        # Step 6.6: Call ListObjects RPC for each chat server
        print("\n" + "=" * 60)
        print("Listing Objects on each chat server:")
        print("=" * 60)
        for server in chat_servers:
            print(f"\n{server.name} (localhost:{server.listen_port}) Objects:")
            objects_response = server.ListObjects()
            print(objects_response)

        # Step 7: Build chat client
        chat_client_path = '/tmp/chat_client'
        if not build_binary('./samples/chat/client/', chat_client_path, "chat client"):
            return 1

        # Step 8: Run chat test
        chat_ok = run_chat_test(chat_client_path, num_servers, base_cov_dir=base_cov_dir if base_cov_dir else None)

        # Step 9: Stop chat servers (gracefully) and check exit codes
        print("\nStopping chat servers...")
        servers_ok = True
        for server in reversed(chat_servers):
            code = server.stop()
            print(f"{server.name} exited with code {code}")
            if code != 0:
                servers_ok = False
            server.close()  # Close gRPC connections

        # Step 10: Stop inspector and check exit code
        print("\nStopping inspector...")
        inspector_ok = True
        code = graceful_stop(inspector_proc, "Inspector")
        print(f"Inspector exited with code {code}")
        if code != 0:
            inspector_ok = False

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
