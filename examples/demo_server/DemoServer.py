#!/usr/bin/env python3
"""DemoServer helper for managing the demo server process."""
import os
import subprocess
import signal
import sys
import time
from pathlib import Path

# Repo root (examples/demo_server/DemoServer.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
# Expose the samples directory for shared modules like BinaryHelper
SAMPLES_DIR = REPO_ROOT / 'tests' / 'samples'
sys.path.insert(0, str(SAMPLES_DIR))

from BinaryHelper import BinaryHelper
from PortHelper import get_free_port


class DemoServer:
    """Manages a Goverse demo server process."""
    
    process: subprocess.Popen | None
    
    def __init__(self, server_index=0, binary_path=None, config_file=None, 
                 node_id=None, build_if_needed=True):
        """Initialize and optionally build the demo server.
        
        Args:
            server_index: Index of this server (for naming)
            binary_path: Path to demo server binary (defaults to /tmp/demo_server)
            config_file: Path to YAML config file (required)
            node_id: Node ID from config file (required)
            build_if_needed: Whether to build the binary if it doesn't exist
        """
        self.server_index = server_index
        self.binary_path = binary_path if binary_path is not None else '/tmp/demo_server'
        self.config_file = config_file
        self.node_id = node_id
        
        # Config file and node_id are required
        if not config_file or not node_id:
            raise ValueError("config_file and node_id are required")
        
        # Parse ports from config file
        import yaml
        with open(config_file, 'r') as f:
            config = yaml.safe_load(f)
        
        # Find the node configuration by node_id
        node_config = None
        for node in config.get('nodes', []):
            if node.get('id') == node_id:
                node_config = node
                break
        if not node_config:
            raise ValueError(f"Node ID '{node_id}' not found in config file")
        
        # Parse listen port from grpc_addr
        grpc_addr = node_config.get('grpc_addr', '0.0.0.0:9211')
        self.listen_port = int(grpc_addr.split(':')[1]) if ':' in grpc_addr else 9211
        
        self.process = None
        self.name = f"Demo Server {server_index + 1}"
        
        # Build binary if needed
        if build_if_needed and not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./examples/demo_server/', self.binary_path, 'demo server'):
                raise RuntimeError(f"Failed to build demo server binary at {self.binary_path}")
    
    def start(self) -> None:
        """Start the demo server process."""
        if self.process is not None:
            print(f"⚠️  {self.name} is already running")
            return

        # Build command line arguments (always use config file)
        cmd = [self.binary_path, '--config', self.config_file, '--node-id', self.node_id]

        # Check if coverage is enabled
        cov_dir = os.environ.get('GOCOVERDIR', '').strip()
        env = os.environ.copy()
        if cov_dir:
            # Create server-specific coverage directory
            server_cov_dir = os.path.join(cov_dir, f'demo_server_{self.server_index}')
            os.makedirs(server_cov_dir, exist_ok=True)
            env['GOCOVERDIR'] = server_cov_dir

        # Start process
        print(f"Starting {self.name} (node_id: {self.node_id}, port: {self.listen_port})...")
        self.process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            env=env,
            text=True,
            bufsize=1
        )
        
        # Give it a moment to start
        time.sleep(0.5)
        
        # Check if process is still running
        if self.process.poll() is not None:
            stdout, _ = self.process.communicate()
            raise RuntimeError(f"{self.name} failed to start. Output:\n{stdout}")
    
    def wait_for_ready(self, timeout: float = 30) -> bool:
        """Wait for the demo server to be ready.
        
        Args:
            timeout: Maximum time to wait in seconds
            
        Returns:
            True if ready, False if timeout
        """
        print(f"Waiting for {self.name} to be ready...")
        start = time.time()
        
        # The demo server is ready when it starts handling requests
        # For now, just wait a bit for it to initialize
        while time.time() - start < timeout:
            if self.process and self.process.poll() is not None:
                print(f"❌ {self.name} terminated unexpectedly")
                return False
            time.sleep(0.5)
            # Check if enough time has passed for initialization
            if time.time() - start > 3:
                print(f"✅ {self.name} is ready")
                return True
        
        print(f"⚠️  {self.name} wait timeout")
        return False
    
    def close(self) -> None:
        """Stop the demo server process."""
        if self.process is None:
            return
        
        print(f"Stopping {self.name}...")
        
        # Try graceful shutdown first
        try:
            self.process.send_signal(signal.SIGTERM)
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            # Force kill if graceful shutdown fails
            print(f"⚠️  {self.name} did not stop gracefully, force killing...")
            self.process.kill()
            self.process.wait()
        except Exception as e:
            print(f"⚠️  Error stopping {self.name}: {e}")
        
        self.process = None
        print(f"✅ {self.name} stopped")
