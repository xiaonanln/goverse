#!/usr/bin/env python3
"""Gateway helper for managing the gateway process."""
import os
import subprocess
import signal
import socket
import sys
import time
from pathlib import Path

# Repo root (tests/samples/chat/Gateway.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
# Expose the samples directory for shared modules like BinaryHelper
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from BinaryHelper import BinaryHelper
from PortHelper import get_free_port


class Gateway:
    """Manages the Goverse gateway process."""
    
    process: subprocess.Popen | None
    
    def __init__(self, listen_port: int | None = None, binary_path: str | None = None, 
                 build_if_needed: bool = True) -> None:
        """Initialize and optionally build the gateway.
        
        Args:
            listen_port: Gateway listen port (default: dynamically allocated)
            binary_path: Path to gateway binary (defaults to /tmp/gateway)
            build_if_needed: Whether to build the binary if it doesn't exist
        """
        self.binary_path = binary_path if binary_path is not None else '/tmp/gateway'
        self.listen_port = listen_port if listen_port is not None else get_free_port()
        self.process = None
        self.name = "Gateway"
        
        # Build binary if needed
        if build_if_needed and not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./cmd/gate/', self.binary_path, 'gateway'):
                raise RuntimeError(f"Failed to build gateway binary at {self.binary_path}")
    
    def start(self) -> None:
        """Start the gateway process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        print(f"Starting {self.name} on port {self.listen_port}...")
        
        # Build address strings for the gateway
        listen_addr = f':{self.listen_port}'
        advertise_addr = f'localhost:{self.listen_port}'
        
        # Start the process with dynamic port arguments (inherits GOCOVERDIR from environment if set)
        self.process = subprocess.Popen(
            [
                self.binary_path,
                '-listen', listen_addr,
                '-advertise', advertise_addr
            ], 
            stdout=None, 
            stderr=None
        )
        print(f"✅ {self.name} started with PID: {self.process.pid}")
    
    def wait_for_ready(self, timeout: float = 30) -> bool:
        """Wait for the gateway to be ready to accept connections.
        
        Args:
            timeout: Maximum time to wait in seconds
            
        Returns:
            True if gateway is ready, False otherwise
        """
        def check_port(port: int, timeout: float = 30) -> bool:
            """Wait for a port to be available."""
            print(f"Waiting for {self.name} port {port} to be ready...")
            start_time = time.time()
            
            while time.time() - start_time < timeout:
                try:
                    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                    sock.settimeout(1)
                    result = sock.connect_ex(('localhost', port))
                    sock.close()
                    if result == 0:
                        print(f"✅ {self.name} port {port} is ready")
                        return True
                except Exception:
                    pass
                time.sleep(1)
            
            print(f"❌ {self.name} port {port} failed to become ready within {timeout} seconds")
            return False
        
        if not check_port(self.listen_port, timeout=timeout):
            print(f"❌ {self.name} failed to start")
            return False
        
        print(f"✅ {self.name} is running and ready")
        return True
    
    def close(self) -> int:
        """Stop the gateway process gracefully.
        
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
        
        return exit_code

    def __enter__(self) -> 'Gateway':
        """Support context manager protocol."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> bool:
        """Close when exiting context."""
        self.close()
        return False
