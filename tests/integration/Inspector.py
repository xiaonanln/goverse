#!/usr/bin/env python3
"""Inspector helper for managing the inspector process and gRPC connections."""
import os
import subprocess
import json
import grpc
import signal
import socket
import time
from pathlib import Path
from typing import Dict
from BinaryHelper import BinaryHelper

# Repo root (tests/integration/Inspector.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()

try:
    from inspector.proto import inspector_pb2
    from inspector.proto import inspector_pb2_grpc
except Exception:
    # When imported from test runner, sys.path should include repo root so
    # the inspector proto package is available. If not, raise a clear error.
    raise


class Inspector:
    """Manages the Goverse inspector process and gRPC/HTTP connections."""
    
    process: subprocess.Popen | None
    channel: grpc.Channel | None
    stub: inspector_pb2_grpc.InspectorServiceStub | None
    
    def __init__(self, binary_path: str | None = None, http_port: int = 8080, grpc_port: int = 8081, 
                 build_if_needed: bool = True) -> None:
        """Initialize and optionally build the inspector.
        
        Args:
            binary_path: Path to inspector binary (defaults to /tmp/inspector)
            http_port: HTTP server port (default: 8080)
            grpc_port: gRPC server port (default: 8081)
            build_if_needed: Whether to build the binary if it doesn't exist
        """
        self.binary_path = binary_path if binary_path is not None else '/tmp/inspector'
        self.http_port = http_port
        self.grpc_port = grpc_port
        self.process = None
        self.channel = None
        self.stub = None
        self.name = "Inspector"
        
        # Build binary if needed
        if build_if_needed and not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./cmd/inspector/', self.binary_path, 'inspector'):
                raise RuntimeError(f"Failed to build inspector binary at {self.binary_path}")
    
    def start(self) -> None:
        """Start the inspector process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        print(f"Starting {self.name}...")
        
        # Start the process (inherits GOCOVERDIR from environment if set)
        self.process = subprocess.Popen(
            [self.binary_path], 
            stdout=subprocess.DEVNULL, 
            stderr=subprocess.DEVNULL
        )
        print(f"✅ {self.name} started with PID: {self.process.pid}")
    
    def wait_for_ready(self, timeout: float = 30) -> bool:
        """Wait for the inspector to be ready to accept connections.
        
        Args:
            timeout: Maximum time to wait in seconds
            
        Returns:
            True if inspector is ready, False otherwise
        """
        def check_http_server(url: str, timeout: float = 30) -> bool:
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
                time.sleep(1)
            
            print(f"❌ HTTP server failed to respond within {timeout} seconds")
            return False
        
        def check_port(port: int, timeout: float = 30) -> bool:
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
                time.sleep(1)
            
            print(f"❌ Port {port} failed to become ready within {timeout} seconds")
            return False
        
        if not check_http_server(f'http://localhost:{self.http_port}/', timeout=timeout):
            print(f"❌ Inspector HTTP server failed to start")
            return False
        
        if not check_port(self.grpc_port, timeout=timeout):
            print(f"❌ Inspector gRPC server failed to start")
            return False
        
        print(f"✅ {self.name} is running and ready")
        return True
    
    def connect(self) -> None:
        """Establish gRPC connection to the inspector."""
        if self.channel is None:
            self.channel = grpc.insecure_channel(f'localhost:{self.grpc_port}')
            self.stub = inspector_pb2_grpc.InspectorServiceStub(self.channel)
    
    def Ping(self, timeout: float = 5) -> str:
        """Call the Ping RPC and return the response.
        
        Args:
            timeout: RPC timeout in seconds
            
        Returns:
            JSON formatted ping response or error message
        """
        self.connect()
        try:
            response = self.stub.Ping(inspector_pb2.PingRequest(), timeout=timeout)
            result: Dict[str, str] = {
                "message": response.message
            }
            return json.dumps(result, indent=2)
        except grpc.RpcError as e:
            return f"Error: RPC failed - {e.code().name}: {e.details()}"
        except Exception as e:
            return f"Error: {str(e)}"
    
    def close(self) -> int:
        """Stop the inspector process gracefully and close the gRPC channel.
        
        Returns:
            The process exit code, or -1 if process was not running
        """
        exit_code = -1
        
        # Stop the process gracefully
        if self.process is not None:
            if self.process.poll() is None:
                print(f"Gracefully stopping {self.name} (PID: {self.process.pid})...")
                
                try:
                    self.process.send_signal(signal.SIGINT)
                    self.process.wait(timeout=10)
                    exit_code = self.process.returncode if self.process.returncode is not None else -1
                except subprocess.TimeoutExpired:
                    pass
                except Exception:
                    pass
                
                if self.process.poll() is None:
                    try:
                        self.process.terminate()
                        self.process.wait(timeout=5)
                        exit_code = self.process.returncode if self.process.returncode is not None else -1
                    except subprocess.TimeoutExpired:
                        pass
                    except Exception:
                        pass
                
                if self.process.poll() is None:
                    try:
                        self.process.kill()
                        self.process.wait()
                        exit_code = self.process.returncode if self.process.returncode is not None else -1
                    except Exception:
                        pass
            else:
                exit_code = self.process.returncode if self.process.returncode is not None else -1
        
        # Close the gRPC channel
        if self.channel:
            self.channel.close()
            self.channel = None
            self.stub = None
        
        return exit_code

    def __enter__(self) -> 'Inspector':
        """Support context manager protocol."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> bool:
        """Close the channel when exiting context."""
        self.close()
        return False
