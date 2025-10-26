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
    
    def __init__(self, binary_path=None, http_port=8080, grpc_port=8081, 
                 build_if_needed=True, base_cov_dir=None):
        """Initialize and optionally build the inspector.
        
        Args:
            binary_path: Path to inspector binary (defaults to /tmp/inspector)
            http_port: HTTP server port (default: 8080)
            grpc_port: gRPC server port (default: 8081)
            build_if_needed: Whether to build the binary if it doesn't exist
            base_cov_dir: Base directory for coverage data (optional)
        """
        self.binary_path = binary_path if binary_path is not None else '/tmp/inspector'
        self.http_port = http_port
        self.grpc_port = grpc_port
        self.base_cov_dir = base_cov_dir
        self.process = None
        self.channel = None
        self.stub = None
        self.name = "Inspector"
        
        # Build binary if needed
        if build_if_needed and not os.path.exists(self.binary_path):
            if not self._build_binary():
                raise RuntimeError(f"Failed to build inspector binary at {self.binary_path}")
    
    def _build_binary(self):
        """Build the inspector binary."""
        print(f"Building inspector...")
        try:
            enable_coverage = os.environ.get('ENABLE_COVERAGE', '').lower() in ('true', '1', 'yes')
            
            cmd = ['go', 'build']
            if enable_coverage:
                cmd.append('-cover')
                print(f"  Coverage instrumentation enabled for inspector")
            
            cmd.extend(['-o', self.binary_path, './cmd/inspector/'])
            
            subprocess.run(cmd, check=True, capture_output=True, text=True, cwd=str(REPO_ROOT))
            print(f"✅ inspector built successfully")
            return True
        except subprocess.CalledProcessError as e:
            print(f"❌ Failed to build inspector")
            print(f"Error: {e.stderr}")
            return False
    
    def start(self):
        """Start the inspector process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        print(f"Starting {self.name}...")
        
        # Prepare environment for coverage (inherited if already exported)
        inspector_env = None
        if self.base_cov_dir:
            inspector_env = os.environ.copy()
            inspector_env['GOCOVERDIR'] = self.base_cov_dir
        
        # Start the process
        self.process = subprocess.Popen(
            [self.binary_path], 
            stdout=subprocess.DEVNULL, 
            stderr=subprocess.DEVNULL,
            env=inspector_env
        )
        print(f"✅ {self.name} started with PID: {self.process.pid}")
    
    def wait_for_ready(self, timeout=30):
        """Wait for the inspector to be ready to accept connections.
        
        Args:
            timeout: Maximum time to wait in seconds
            
        Returns:
            True if inspector is ready, False otherwise
        """
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
                time.sleep(1)
            
            print(f"❌ HTTP server failed to respond within {timeout} seconds")
            return False
        
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
    
    def connect(self):
        """Establish gRPC connection to the inspector."""
        if self.channel is None:
            self.channel = grpc.insecure_channel(f'localhost:{self.grpc_port}')
            self.stub = inspector_pb2_grpc.InspectorServiceStub(self.channel)
    
    def Ping(self, timeout=5):
        """Call the Ping RPC and return the response.
        
        Args:
            timeout: RPC timeout in seconds
            
        Returns:
            JSON formatted ping response or error message
        """
        self.connect()
        try:
            response = self.stub.Ping(inspector_pb2.PingRequest(), timeout=timeout)
            result = {
                "message": response.message
            }
            return json.dumps(result, indent=2)
        except grpc.RpcError as e:
            return f"Error: RPC failed - {e.code().name}: {e.details()}"
        except Exception as e:
            return f"Error: {str(e)}"
    
    def stop(self):
        """Stop the inspector process gracefully."""
        if self.process is None:
            return -1
        
        if self.process.poll() is not None:
            return self.process.returncode if self.process.returncode is not None else -1
        
        print(f"Gracefully stopping {self.name} (PID: {self.process.pid})...")
        
        try:
            self.process.send_signal(signal.SIGINT)
            self.process.wait(timeout=10)
            return self.process.returncode if self.process.returncode is not None else -1
        except subprocess.TimeoutExpired:
            pass
        except Exception:
            pass
        
        try:
            self.process.terminate()
            self.process.wait(timeout=5)
            return self.process.returncode if self.process.returncode is not None else -1
        except subprocess.TimeoutExpired:
            pass
        except Exception:
            pass
        
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
