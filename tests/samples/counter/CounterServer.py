#!/usr/bin/env python3
"""CounterServer helper for testing."""
import os
import subprocess
import json
import grpc
import signal
import socket
import time
from pathlib import Path
from typing import Dict, List, Any, Optional

# Add repo root and samples directory to path
import sys
REPO_ROOT = Path(__file__).parent.parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
# Expose the samples directory for shared modules like BinaryHelper
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from BinaryHelper import BinaryHelper

try:
    from proto import goverse_pb2
    from proto import goverse_pb2_grpc
except Exception:
    raise


class CounterServer:
    """Manages a Goverse counter server process and gRPC connections."""
    
    process: Optional[subprocess.Popen]
    channel: Optional[grpc.Channel]
    stub: Optional[goverse_pb2_grpc.GoverseStub]
    
    def __init__(self, server_index=0, listen_port=None, 
                 binary_path=None):
        self.server_index = server_index
        self.listen_port = listen_port if listen_port is not None else 47100 + server_index
        self.binary_path = binary_path if binary_path is not None else '/tmp/counter_server'
        self.process = None
        self.channel = None
        self.stub = None
        self.name = f"Counter Server {server_index + 1}"
        
        # Build binary if needed
        if not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./samples/counter/server/', self.binary_path, 'counter server'):
                raise RuntimeError(f"Failed to build counter server binary at {self.binary_path}")
    
    def start(self) -> None:
        """Start the counter server process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        print(f"Starting {self.name} (port {self.listen_port})...")
        
        # Start the process (inherits GOCOVERDIR from environment if set)
        cmd: List[str] = [
            self.binary_path,
            '-listen', f'localhost:{self.listen_port}',
            '-advertise', f'localhost:{self.listen_port}',
        ]
        
        self.process = subprocess.Popen(
            cmd, 
            stdout=None, 
            stderr=None
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
            print(f"❌ {self.name} failed to start on port {self.listen_port}")
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

    def __enter__(self) -> 'CounterServer':
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> bool:
        self.close()
        return False
