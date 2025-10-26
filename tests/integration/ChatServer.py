#!/usr/bin/env python3
"""ChatServer helper moved out of test_chat.py for reuse and clarity."""
import os
import subprocess
import json
import grpc
import signal
import socket
import time
from pathlib import Path

# Repo root (tests/integration/ChatServer.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()

try:
    from proto import goverse_pb2
    from proto import goverse_pb2_grpc
except Exception:
    # When imported from test runner, sys.path should include repo root so
    # the `proto` package is available. If not, raise a clear error.
    raise


class ChatServer:
    """Manages a Goverse chat server process and gRPC connections."""
    
    def __init__(self, server_index=0, listen_port=None, client_port=None, 
                 binary_path=None, build_if_needed=True, base_cov_dir=None):
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
        
        # Prepare environment for coverage (inherited if already exported)
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
        """Wait for the server to be ready to accept connections."""
        print(f"Verifying {self.name}...")
        
        def check_port(port, timeout=30):
            start_time = time.time()
            while time.time() - start_time < timeout:
                try:
                    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                    sock.settimeout(1)
                    result = sock.connect_ex(('localhost', port))
                    sock.close()
                    if result == 0:
                        return True
                except Exception:
                    pass
                time.sleep(1)
            return False

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
        self.connect()
        call_request = goverse_pb2.CallObjectRequest(
            id=object_id,
            method=method,
            request=request
        )
        return self.stub.CallObject(call_request, timeout=timeout)
    
    def stop(self):
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
        if self.channel:
            self.channel.close()
            self.channel = None
            self.stub = None

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
        return False
