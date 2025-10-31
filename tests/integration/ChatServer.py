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
from typing import Dict, List, Any
from google.protobuf.any_pb2 import Any as AnyProto
from BinaryHelper import BinaryHelper

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
    
    process: subprocess.Popen | None
    channel: grpc.Channel | None
    stub: goverse_pb2_grpc.GoverseStub | None
    
    def __init__(self, server_index=0, listen_port=None, client_port=None, 
                 binary_path=None):
        self.server_index = server_index
        self.listen_port = listen_port if listen_port is not None else 47000 + server_index
        self.client_port = client_port if client_port is not None else 48000 + server_index
        self.binary_path = binary_path if binary_path is not None else '/tmp/chat_server'
        self.process = None
        self.channel = None
        self.stub = None
        self.name = f"Chat Server {server_index + 1}"
        
        # Build binary if needed
        if not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./samples/chat/server/', self.binary_path, 'chat server'):
                raise RuntimeError(f"Failed to build chat server binary at {self.binary_path}")
    
    def start(self) -> None:
        """Start the chat server process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        print(f"Starting {self.name} (ports {self.listen_port}, {self.client_port})...")
        
        # Start the process (inherits GOCOVERDIR from environment if set)
        cmd: List[str] = [
            self.binary_path,
            '-listen', f'localhost:{self.listen_port}',
            '-advertise', f'localhost:{self.listen_port}',
            '-client-listen', f'localhost:{self.client_port}'
        ]
        
        self.process = subprocess.Popen(
            cmd, 
            stdout=subprocess.DEVNULL, 
            stderr=subprocess.DEVNULL
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
    
    def connect(self) -> None:
        """Establish gRPC connection to the server."""
        if self.channel is None:
            self.channel = grpc.insecure_channel(f'localhost:{self.listen_port}')
            self.stub = goverse_pb2_grpc.GoverseStub(self.channel)
    
    def Status(self, timeout: float = 5) -> str:
        self.connect()
        try:
            response: goverse_pb2.StatusResponse = self.stub.Status(goverse_pb2.Empty(), timeout=timeout)
            result: Dict[str, str] = {
                "advertiseAddr": response.advertise_addr,
                "numObjects": str(response.num_objects),
                "uptimeSeconds": str(response.uptime_seconds)
            }
            return json.dumps(result, indent=2)
        except grpc.RpcError as e:
            return f"Error: RPC failed - {e.code().name}: {e.details()}"
        except Exception as e:
            return f"Error: {str(e)}"
    
    def ListObjects(self, timeout: float = 5) -> str:
        self.connect()
        try:
            response: goverse_pb2.ListObjectsResponse = self.stub.ListObjects(goverse_pb2.Empty(), timeout=timeout)
            objects: List[Dict[str, str]] = []
            for obj in response.objects:
                objects.append({
                    "id": obj.id,
                    "type": obj.type
                })
            result: Dict[str, Any] = {
                "objectCount": len(objects),
                "objects": objects
            }
            return json.dumps(result, indent=2)
        except grpc.RpcError as e:
            return f"Error: RPC failed - {e.code().name}: {e.details()}"
        except Exception as e:
            return f"Error: {str(e)}"
    
    def CallObject(self, object_id: str, method: str, request: AnyProto, timeout: float = 5) -> goverse_pb2.CallObjectResponse:
        self.connect()
        call_request = goverse_pb2.CallObjectRequest(
            id=object_id,
            method=method,
            request=request
        )
        return self.stub.CallObject(call_request, timeout=timeout)
    
    def wait_for_objects(self, min_count: int, timeout: float = 30) -> bool:
        """Wait for at least min_count objects to be created on the server.
        
        This is useful for waiting for the cluster to be ready and objects to be initialized.
        
        Args:
            min_count: Minimum number of objects to wait for
            timeout: Maximum time to wait in seconds
            
        Returns:
            True if the minimum object count is reached, False if timeout
        """
        self.connect()
        start_time = time.time()
        
        while time.time() - start_time < timeout:
            try:
                response: goverse_pb2.ListObjectsResponse = self.stub.ListObjects(goverse_pb2.Empty(), timeout=5)
                object_count = len(response.objects)
                
                if object_count >= min_count:
                    print(f"✅ {self.name} has {object_count} objects (>= {min_count} required)")
                    return True
                    
                time.sleep(1)
            except Exception as e:
                # Ignore errors and keep retrying
                time.sleep(1)
        
        print(f"❌ Timeout waiting for {min_count} objects on {self.name}")
        return False
    
    def close(self) -> int:
        self._close_channel()
        return self._stop_process()

    def _stop_process(self) -> int:
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
    
    def _close_channel(self) -> None:
        if self.channel:
            self.channel.close()
            self.channel = None
            self.stub = None

    def __enter__(self) -> 'ChatServer':
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> bool:
        self.close()
        return False
